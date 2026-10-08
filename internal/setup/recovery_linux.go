//go:build linux

package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/popiposter/xkeen-control/internal/buildinfo"
	"github.com/popiposter/xkeen-control/internal/configjson"
	"github.com/popiposter/xkeen-control/internal/keenetic"
	"github.com/popiposter/xkeen-control/internal/nodes"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

type firmwareBaseline struct {
	Before keenetic.Snapshot
	Plan   keenetic.Plan
}

func privateJSON(path string, v any) error {
	b, e := readOwned(path, 64<<10, true)
	if e != nil {
		return ErrState
	}
	if _, e = configjson.DecodeObject(b); e != nil {
		return ErrState
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		return ErrState
	}
	return nil
}
func recoveryScope(r *Receipt) (firmwareBaseline, keenetic.Intent, error) {
	var b firmwareBaseline
	var intent keenetic.Intent
	if privateJSON("/opt/etc/xkeen-control/state/initial-firmware-baseline.json", &b) != nil || privateJSON("/opt/etc/xkeen-control/state/initial-firmware.json", &intent) != nil {
		return b, intent, ErrState
	}
	if !digestPattern.MatchString(b.Before.Hash) || !digestPattern.MatchString(intent.AfterHash) || b.Plan.PolicyID != r.PolicyID || fmt.Sprint(b.Plan.ProfileID) != r.ProfileID || intent.PolicyID != r.PolicyID || intent.Home != b.Plan.Home || intent.ProfileID != b.Plan.ProfileID {
		return b, intent, ErrState
	}
	return b, intent, nil
}

// Recover is deliberately inspect-only for native/component effects. It may
// acknowledge the existing config/job readback owners and finalize a fully
// saved HOME phase, but never replays installation, activation or firmware.
func Recover(ctx context.Context, out io.Writer) error {
	lock, e := acquireLockFile(LockPath, true)
	if e != nil {
		return e
	}
	defer lock.Close()
	r, e := Inspect()
	if e != nil || r == nil || r.Release != buildinfo.Current() {
		return ErrState
	}
	if r.Phase == "completed" {
		lock.Close()
		return startPanel(ctx, r.Release)
	}
	fmt.Fprintf(out, "Проверка прерванной установки: фаза %s. Команды установки и активации не повторяются.\n", r.Phase)
	if r.Phase != "activation" && r.Phase != "home" {
		return ErrState
	}
	base, intent, e := recoveryScope(r)
	if e != nil {
		return e
	}
	lease, editor, jobs, dns, e := owners()
	if e != nil {
		return e
	}
	w, e := editor.Workspace(ctx)
	if e != nil || w.Digest != r.Generation {
		return ErrState
	}
	if w.Pending != nil {
		if w.Pending.Drift || w.Pending.ApplyID == "" || editor.InspectApplied(ctx, w.Pending.ApplyID) != nil {
			return ErrState
		}
	}
	if job, e := jobs.Read("initial-setup", "", 0); e == nil && job.State == "unknown" {
		if _, e = jobs.ResolveInspection(ctx, "initial-setup", job.ID); e != nil {
			return ErrState
		}
	}
	// A DNS operation receipt is inspected by the existing owner; absent receipt
	// uses a read-only observer rather than Sync/Reconcile (which could restart).
	if _, e = os.Lstat("/opt/etc/mosdns/panel-operation.json"); e == nil {
		if dns.Sync(ctx) != nil {
			return ErrState
		}
	} else if !os.IsNotExist(e) {
		return ErrState
	}
	if dns.InspectOwned(ctx) != nil {
		return ErrState
	}
	if r.Phase != "home" || !r.PrerequisitesSaved || !r.RuntimeVerified || !r.StartupVerified {
		return ErrState
	}
	firmware := keenetic.New()
	saved, e := firmware.Discover(ctx)
	if e != nil || saved.Hash != intent.AfterHash || saved.HomePolicy != base.Plan.PolicyID || saved.HomeProfile != r.ProfileID {
		return ErrState
	}
	mark, e := firmware.Mark(ctx, base.Plan, true)
	if e != nil || mark != r.PolicyMark || nativeAutostart() != "on" || startupFiles() != nil {
		return ErrState
	}
	if firmware.VerifyDNS(ctx, base.Plan, true) != nil {
		return ErrState
	}
	registry, e := (nodes.Store{Path: registryPath}).Load()
	if e != nil {
		return ErrState
	}
	if verifyReady(ctx, editor, dns, lease, r.Generation, registry, mark) != nil {
		return ErrState
	}
	if pending, e := editor.HasSavedChanges(); e != nil || pending {
		return ErrState
	}
	r.FirmwareSaved = true
	r.Phase = "completed"
	if writeReceipt(ReceiptPath, *r) != nil {
		return ErrState
	}
	lock.Close()
	if startPanel(ctx, r.Release) != nil {
		return ErrState
	}
	fmt.Fprintln(out, "Завершение подтверждено отдельными проверками. Панель запущена; проверка LAN-клиентов остаётся отдельной.")
	return nil
}

// Abort is an explicit bounded rollback of firmware effects, never a native
// uninstall/reinstall. Unknown native installation and unconfirmed firmware
// results remain blocked for operator inspection.
func Abort(ctx context.Context, out io.Writer) error {
	lock, e := acquireLockFile(LockPath, true)
	if e != nil {
		return e
	}
	defer lock.Close()
	r, e := Inspect()
	if e != nil || r == nil || r.Release != buildinfo.Current() || r.Phase == "completed" || r.Phase == "aborted" {
		return ErrState
	}
	base, intent, e := recoveryScope(r)
	if e != nil {
		return e
	}
	firmware := keenetic.New()
	actual, e := firmware.Discover(ctx)
	if e != nil || actual.Hash != intent.AfterHash {
		return ErrState
	}
	if r.Phase == "policy" {
		for _, p := range []string{"/opt/sbin/xkeen", "/opt/sbin/_xkeen", "/opt/sbin/.xkeen", "/opt/sbin/xray"} {
			if _, e := os.Lstat(p); !os.IsNotExist(e) {
				return ErrState
			}
		}
	} else if r.Phase == "activation" || r.Phase == "home" {
		_, editor, jobs, _, e := owners()
		if e != nil {
			return e
		}
		if pending, e := editor.HasSavedChanges(); e != nil || pending {
			return ErrState
		}
		if editor.VerifySetupRuntime(ctx, r.Generation) != nil {
			return ErrState
		}
		off, e := jobs.Start("initial-setup", xkeen.CommandRequest{Action: "autostart", Parameter: "off"})
		if e != nil || waitJob(ctx, jobs, off.ID, false) != nil || nativeAutostart() != "off" {
			return ErrState
		}
		stop, e := jobs.Start("initial-setup", xkeen.CommandRequest{Action: "stop"})
		if e != nil || waitJob(ctx, jobs, stop.ID, false) != nil {
			return ErrState
		}
		check, cancel := context.WithTimeout(ctx, 5*time.Second)
		facts := (xkeen.Discovery{}).Inspect(check)
		cancel()
		if facts.XrayRunning || facts.Installation != xkeen.CapabilityAvailable {
			return ErrState
		}
	} else {
		return ErrState
	}
	if _, e = firmware.RestoreOwned(ctx, base.Before, base.Plan, intent.AfterHash, persistFirmware); e != nil {
		return e
	}
	r.Phase = "aborted"
	r.FirmwareSaved = false
	r.RuntimeVerified = false
	r.StartupVerified = false
	if writeReceipt(ReceiptPath, *r) != nil {
		return ErrState
	}
	fmt.Fprintln(out, "Собственные изменения политики и DNS отменены и сохранены. Неполная установка остаётся заблокированной; компоненты не переустанавливались.")
	return nil
}
