//go:build linux

package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/buildinfo"
	"github.com/popiposter/xkeen-control/internal/keenetic"
	"github.com/popiposter/xkeen-control/internal/nodes"
	"github.com/popiposter/xkeen-control/internal/panellistener"
	"github.com/popiposter/xkeen-control/internal/splitdns"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

const registryPath = "/opt/etc/xkeen-control/secrets/nodes.json"
const jobPath = "/opt/etc/xkeen-control/state/native-jobs/last-job.json"

func owners() (*authority.Lease, *xkeen.ConfigEditor, *xkeen.Jobs, *splitdns.Service, error) {
	lease := authority.NewLease()
	e := &xkeen.ConfigEditor{Dir: "/opt/etc/xray/configs", XrayBinary: "/opt/sbin/xray", AssetDir: "/opt/etc/xray/dat", RegistryPath: registryPath, PreviousDir: "/opt/etc/xkeen-control/previous/native-config", Lease: lease}
	d := &splitdns.Service{Dir: "/opt/etc/mosdns", Init: "/opt/etc/init.d/S06mosdns", AssetDir: e.AssetDir, Lease: lease, ReadNative: func(ctx context.Context) (map[string][]byte, error) {
		s, e1 := e.Snapshot(ctx)
		return s.NativeDocuments(), e1
	}, Pending: func() bool { v, e1 := e.HasSavedChanges(); return v || e1 != nil }}
	e.ValidateDerived = d.Validate
	parent, err := privateDirectory("/opt/etc/xkeen-control/state/native-jobs", true)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	parent.Close()
	j, err := xkeen.NewPersistentJobs("/opt/sbin/xkeen", lease, jobPath)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	j.ConfigureRecovery(xkeen.Discovery{}, "/opt/etc/xkeen-control/state/nodes/pending.json")
	return lease, e, j, d, nil
}

func persistFirmware(intent keenetic.Intent) error {
	b, e := json.Marshal(intent)
	if e != nil {
		return ErrState
	}
	// One bounded fixed firmware intent, independent of ConfigEditor's journal.
	// Last completed effect remains available for explicit drift-checked recovery.
	return replacePrivate("/opt/etc/xkeen-control/state/initial-firmware.json", b)
}

func Run(ctx context.Context, in *os.File, out io.Writer) error {
	info := buildinfo.Current()
	if info.Validate() != nil || info.Channel == "development" {
		return ErrUnsupported
	}
	lock, e := setupOwner()
	if e != nil {
		return e
	}
	defer lock.Close()
	if r, e := Inspect(); e != nil || r != nil {
		return ErrState
	}
	fmt.Fprintln(out, "Проверяем роутер и возможность свежей установки.")
	if e = fresh(); e != nil {
		return e
	}
	firmware := keenetic.New()
	base, e := firmware.Discover(ctx)
	if e != nil {
		return e
	}
	plan, e := firmware.Plan(base)
	if e != nil {
		return e
	}
	source, e := privateSource(ctx, in, out)
	if e != nil {
		return e
	}
	registry, e := nodes.PrepareInitialSource(ctx, source, nil)
	source = ""
	if e != nil {
		return e
	}
	// Artifacts are verified in bounded RAM before any owned destination changes.
	b, e := download(ctx, nativeURL, nativeSize, nativeHash)
	if e != nil {
		return e
	}
	payload, e := decodeNative(b)
	if e != nil {
		return e
	}
	b = nil
	b, e = download(ctx, dnsURL, dnsSize, dnsHash)
	if e != nil {
		return e
	}
	dnsBinary, e := decodeDNS(b)
	if e != nil {
		return e
	}
	b = nil
	if e = fresh(); e != nil {
		return e
	}
	r := Receipt{Schema: 1, Release: info, Phase: "prepared", PolicyID: plan.PolicyID, ProfileID: strconv.Itoa(plan.ProfileID)}
	if e = writeReceipt(ReceiptPath, r); e != nil {
		return e
	}
	// Snapshot only the scoped pristine firmware state; never auth sessions/full
	// credential-bearing running-config. Unknown outcomes retain the receipt.
	if e = saveFirmwareBaseline(base, plan); e != nil {
		return e
	}
	r.Phase = "policy"
	if e = writeReceipt(ReceiptPath, r); e != nil {
		return e
	}
	base, e = firmware.PreparePolicy(ctx, base, plan, persistFirmware)
	if e != nil {
		return e
	}
	r.PolicyMark, e = firmware.PolicyMark(ctx, plan)
	if e != nil {
		return e
	}
	r.Phase = "native"
	if e = writeReceipt(ReceiptPath, r); e != nil {
		return e
	}
	fmt.Fprintln(out, "Устанавливаем проверенные XKeen 2.1 и Xray.")
	if e = installTools(ctx); e != nil {
		return e
	}
	if e = firmware.VerifyPolicy(ctx, plan, false); e != nil {
		return e
	}
	if e = bootstrapNative(payload); e != nil {
		return e
	}
	payload = nil
	if e = firmware.VerifyPolicy(ctx, plan, false); e != nil {
		return e
	}
	if e = fixedCommand(ctx, 15*time.Minute, "/opt/sbin/xkeen", "-i", "auto", "cores=xray", "xray="+xrayVersion, "geo=on", "geoipset=off", "cron=off", "autostart=off"); e != nil {
		return ErrState
	}
	facts := (xkeen.Discovery{}).Inspect(ctx)
	if facts.Installation != xkeen.CapabilityAvailable || facts.Version != "2.1" || facts.Core != "xray" || facts.XrayRunning || nativeAutostart() != "off" {
		return ErrState
	}
	// Stock DSCP bypasses the dedicated policy mark. Disable it using its
	// supported non-activating command, never by patching native code or hooks.
	if firmware.VerifyPolicy(ctx, plan, false) != nil || fixedCommand(ctx, 30*time.Second, "/opt/sbin/xkeen", "-dscp", "off") != nil || nativeState("dscp_enable") != "off" {
		return ErrState
	}
	core, e := fixedOutput(ctx, 5*time.Second, 8192, "/opt/sbin/xray", "version")
	if e != nil || !strings.HasPrefix(string(core), "Xray "+strings.TrimPrefix(xrayVersion, "v")+" ") {
		return ErrState
	}
	r.Phase = "panel"
	if e = writeReceipt(ReceiptPath, r); e != nil {
		return e
	}
	fmt.Fprintln(out, "Устанавливаем панель без запуска фоновых операций.")
	if e = installPanel(ctx, lock); e != nil {
		return e
	}
	metadata, e := fixedOutput(ctx, 5*time.Second, 8192, "/opt/sbin/xkeen-control", "version", "--json")
	if e != nil {
		return ErrState
	}
	var installed buildinfo.Info
	if json.Unmarshal(metadata, &installed) != nil || installed != info {
		return ErrState
	}
	dir, e := privateDirectory("/opt/etc/xkeen-control/secrets", true)
	if e != nil {
		return e
	}
	dir.Close()
	if e = auth.RunBootstrapCommand(auth.PasswordHashPath, auth.BootstrapMarkerPath, out); e != nil {
		return e
	}
	if e = exclusiveFile(panellistener.DefaultFilePath, []byte(plan.Address.String()+":8787\n"), 0600); e != nil {
		return e
	}
	r.Phase = "dns"
	if e = writeReceipt(ReceiptPath, r); e != nil {
		return e
	}
	dnsConfig, e := splitdns.FreshConfig(plan.Address)
	if e != nil {
		return e
	}
	if e = provisionDNS(dnsBinary, dnsConfig); e != nil {
		return e
	}
	dnsBinary = nil
	lease, editor, jobs, dns, e := owners()
	if e != nil {
		return e
	}
	r.Phase = "candidate"
	if e = writeReceipt(ReceiptPath, r); e != nil {
		return e
	}
	fmt.Fprintln(out, "Настраиваем эталон маршрутизации и независимый DNS.")
	release, e := lease.TryAcquire()
	if e != nil {
		return e
	}
	native, e := editor.Snapshot(ctx)
	if e != nil {
		release()
		return e
	}
	files, data, e := Candidate(native.NativeDocuments(), registry)
	if e != nil {
		release()
		return e
	}
	baseline, e := editor.PreviewTransferUnderLease(ctx, files, data)
	if e != nil {
		release()
		return e
	}
	r.Generation, e = editor.StageTransferUnderLease(ctx, baseline, files, data)
	release()
	if e != nil {
		return e
	}
	r.Phase = "activation"
	if e = writeReceipt(ReceiptPath, r); e != nil {
		return e
	}
	policyCheck := func(ctx context.Context) error {
		mark, e := firmware.PolicyMark(ctx, plan)
		if e != nil || mark != r.PolicyMark {
			return ErrState
		}
		return nil
	}
	excluded, e := dnsExcluded()
	if e != nil {
		return e
	}
	var job xkeen.JobView
	if !excluded {
		job, e = jobs.PrepareSetupDNSExemption(func(ctx context.Context) error {
			if policyCheck(ctx) != nil {
				return ErrState
			}
			saved, e := editor.Snapshot(ctx)
			if e != nil || saved.Digest != r.Generation {
				return ErrState
			}
			return editor.VerifySetupStopped(ctx, r.Generation)
		})
		if e != nil {
			return e
		}
		if waitJob(ctx, jobs, job.ID, false) != nil {
			return ErrState
		}
		excluded, e = dnsExcluded()
		if e != nil || !excluded {
			return ErrState
		}
	}
	// Preparation cannot be mistaken for activation: -ape does not start a
	// stopped core. Reject an external/unexpected process, then start exactly
	// once through ConfigEditor's existing complete-generation apply owner.
	if editor.VerifySetupStopped(ctx, r.Generation) != nil {
		return ErrState
	}
	job, e = jobs.StartSetupConfigs(editor, r.Generation, policyCheck)
	if e != nil {
		return e
	}
	r.JobID = job.ID
	if e = writeReceipt(ReceiptPath, r); e != nil {
		return e
	}
	if e = waitJob(ctx, jobs, job.ID, true); e != nil {
		return e
	}
	if pending, e := editor.HasSavedChanges(); e != nil || pending {
		return ErrState
	}
	if e = dns.Sync(ctx); e != nil {
		return e
	}
	fmt.Fprintln(out, "Проверяем конфигурацию, VPN, DNS и область перехвата.")
	if e = verifyReady(ctx, editor, dns, lease, r.Generation, registry, r.PolicyMark); e != nil {
		return e
	}
	r.RuntimeVerified = true
	base, e = firmware.PrepareDNS(ctx, base, plan, persistFirmware)
	if e != nil {
		return e
	}
	if firmware.VerifyDNS(ctx, plan, false) != nil {
		return ErrState
	}
	r.PrerequisitesSaved = true
	if e = writeReceipt(ReceiptPath, r); e != nil {
		return e
	}
	if e = firmware.VerifyPolicy(ctx, plan, false); e != nil {
		return e
	}
	job, e = jobs.Start("initial-setup", xkeen.CommandRequest{Action: "autostart", Parameter: "on"})
	if e != nil {
		return e
	}
	if e = waitJob(ctx, jobs, job.ID, false); e != nil || nativeAutostart() != "on" {
		return ErrState
	}
	if e = startupFiles(); e != nil {
		return e
	}
	r.StartupVerified = true
	r.Phase = "home"
	if e = writeReceipt(ReceiptPath, r); e != nil {
		return e
	}
	fmt.Fprintln(out, "Подключаем всю обнаруженную домашнюю сеть.")
	base, e = firmware.AssignHOME(ctx, base, plan, persistFirmware)
	if e != nil {
		return e
	}
	r.FirmwareSaved = true
	final, e := firmware.Discover(ctx)
	if e != nil || final.Hash != base.Hash || final.HomePolicy != plan.PolicyID || final.HomeProfile != strconv.Itoa(plan.ProfileID) {
		return ErrState
	}
	if e = firmware.VerifyPolicy(ctx, plan, true); e != nil {
		return e
	}
	if firmware.VerifyDNS(ctx, plan, true) != nil {
		return ErrState
	}
	if e = verifyReady(ctx, editor, dns, lease, r.Generation, registry, r.PolicyMark); e != nil {
		return e
	}
	if pending, e := editor.HasSavedChanges(); e != nil || pending {
		return ErrState
	}
	r.Phase = "completed"
	if e = writeReceipt(ReceiptPath, r); e != nil {
		return e
	}
	// No setup writer remains after durable completion. Closing the exclusive
	// descriptor precedes normal daemon's shared-lock admission.
	lock.Close()
	if e = startPanel(ctx, info); e != nil {
		return e
	}
	enabled := 0
	for _, n := range registry.Nodes {
		if n.Enabled && !n.Stale && !n.Missing {
			enabled++
		}
	}
	fmt.Fprintf(out, "Готово. Панель: http://%s:8787/; XKeen 2.1; Xray %s; mosdns 5.3.4; активных узлов: %d.\n", plan.Address, xrayVersion, enabled)
	fmt.Fprintln(out, "Настройки сохранены. Независимая проверка с LAN-клиента и IPv6 остаётся отдельной проверкой. Плановые native-обновления выключены.")
	return nil
}

func waitJob(ctx context.Context, j *xkeen.Jobs, id string, applied bool) error {
	timer := time.NewTimer(100 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		v, e := j.Read("initial-setup", id, 0)
		if e != nil {
			return ErrState
		}
		if v.State != "running" {
			if v.State != "completed" || v.ExitCode == nil || *v.ExitCode != 0 || applied && v.ConfigurationState != "applied" {
				return ErrState
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ErrState
		case <-timer.C:
			return ErrState
		case <-tick.C:
		}
	}
}

func nativeAutostart() string {
	return nativeState("start_auto")
}
func nativeState(key string) string {
	b, e := readOwned("/opt/etc/init.d/S05xkeen", 512<<10, false)
	if e != nil {
		return ""
	}
	state := ""
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, key+"=") {
			if state != "" {
				return ""
			}
			switch strings.TrimPrefix(line, key+"=") {
			case `"on"`, "'on'":
				state = "on"
			case `"off"`, "'off'":
				state = "off"
			default:
				return ""
			}
		}
	}
	return state
}

func dnsExcluded() (bool, error) {
	proxy, e := readOwned("/opt/etc/xkeen/port_proxying.lst", 8192, false)
	if e == nil && activePorts(proxy) {
		return false, ErrState
	}
	if e != nil && !os.IsNotExist(e) {
		return false, ErrState
	}
	b, e := readOwned("/opt/etc/xkeen/port_exclude.lst", 8192, false)
	if os.IsNotExist(e) {
		return false, nil
	}
	if e != nil {
		return false, ErrState
	}
	return excludedPort(b)
}
func activePorts(b []byte) bool {
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(strings.SplitN(line, "#", 2)[0]) != "" {
			return true
		}
	}
	return false
}
func excludedPort(b []byte) (bool, error) {
	found := false
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) < 1 || len(parts) > 2 {
			return false, ErrState
		}
		lo, e := strconv.Atoi(parts[0])
		if e != nil {
			return false, ErrState
		}
		hi := lo
		if len(parts) == 2 {
			hi, e = strconv.Atoi(parts[1])
		}
		if e != nil || lo < 1 || hi > 65535 || hi < lo {
			return false, ErrState
		}
		found = found || lo <= 53 && hi >= 53
	}
	return found, nil
}
