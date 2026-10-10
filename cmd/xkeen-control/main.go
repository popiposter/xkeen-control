package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/buildinfo"
	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/configview"
	"github.com/popiposter/xkeen-control/internal/geodatareader"
	"github.com/popiposter/xkeen-control/internal/httpapi"
	"github.com/popiposter/xkeen-control/internal/nativebackup"
	"github.com/popiposter/xkeen-control/internal/nativequality"
	"github.com/popiposter/xkeen-control/internal/nodes"
	"github.com/popiposter/xkeen-control/internal/notifications"
	"github.com/popiposter/xkeen-control/internal/panellistener"
	"github.com/popiposter/xkeen-control/internal/resourcepolicy"
	controlruntime "github.com/popiposter/xkeen-control/internal/runtime"
	initialsetup "github.com/popiposter/xkeen-control/internal/setup"
	"github.com/popiposter/xkeen-control/internal/splitdns"
	panelupdate "github.com/popiposter/xkeen-control/internal/update"
	"github.com/popiposter/xkeen-control/internal/validationbudget"
	"github.com/popiposter/xkeen-control/internal/webassets"
	"github.com/popiposter/xkeen-control/internal/xkeen"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

const defaultListenAddress = panellistener.DefaultAddress

func main() {
	if len(os.Args) > 1 && !updateReadOnlyCommand(os.Args[1:]) && panelupdate.MutationReady() != nil {
		log.Print(panelupdate.ErrInspectionRequired)
		os.Exit(1)
	}
	if len(os.Args) >= 2 && os.Args[1] == "setup" {
		if err := runSetupCommand(os.Args[2:]); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "native" {
		if len(os.Args) > 2 && os.Args[2] == "attach-stopped" {
			close, err := initialsetup.Normal()
			if err != nil {
				log.Print(err)
				os.Exit(1)
			}
			defer close()
		}
		if err := runNativeCommand(os.Args[2:], os.Stdout, xkeen.Discovery{}); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "version" {
		if len(os.Args) != 3 || os.Args[2] != "--json" {
			log.Print("usage: xkeen-control version --json")
			os.Exit(2)
		}
		contents, err := buildinfo.Current().JSON()
		if err != nil {
			log.Print("build metadata unavailable")
			os.Exit(1)
		}
		_, _ = os.Stdout.Write(contents)
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "password" {
		close, err := initialsetup.Normal()
		if err != nil {
			log.Print(err)
			os.Exit(1)
		}
		defer close()
		if len(os.Args) != 3 {
			log.Print("usage: xkeen-control password {init|change|bootstrap}")
			os.Exit(2)
		}
		path := getenv("XKEEN_CONTROL_AUTH_HASH", auth.PasswordHashPath)
		if os.Args[2] == "bootstrap" {
			marker := getenv("XKEEN_CONTROL_BOOTSTRAP_MARKER", auth.BootstrapMarkerPath)
			if err := auth.RunBootstrapCommand(path, marker, os.Stdout); err != nil {
				log.Print("bootstrap credential generation failed")
				os.Exit(1)
			}
			return
		}
		if err := auth.RunPasswordCommand(path, os.Args[2], os.Stdin, os.Stderr); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "self-update" {
		if len(os.Args) == 3 && os.Args[2] == "inspect-capabilities" {
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			if err := runCapabilityInspection(ctx, os.Stdout, initialsetup.InspectNormal, panelupdate.InspectCapabilities); err != nil {
				log.Print(err)
				os.Exit(1)
			}
			return
		}
		if len(os.Args) == 3 && os.Args[2] == "inspect-installed" {
			value, err := panelupdate.InspectInstalled()
			if err != nil {
				log.Print(err)
				os.Exit(1)
			}
			_ = json.NewEncoder(os.Stdout).Encode(value)
			return
		}
		if len(os.Args) == 6 && os.Args[2] == "--maintenance" && os.Args[4] == "--apply" {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			if err := panelupdate.ApplyMaintenance(ctx, os.Args[3], os.Args[5]); err != nil {
				log.Print(err)
				os.Exit(1)
			}
			return
		}
		if len(os.Args) == 3 && os.Args[2] == "inspect" {
			value, err := panelupdate.NewManager(panelupdate.Config{}).Receipt()
			if value != nil {
				_ = json.NewEncoder(os.Stdout).Encode(value)
			}
			if err != nil {
				log.Print(err)
				os.Exit(1)
			}
			return
		}
		close, err := initialsetup.Normal()
		if err != nil {
			log.Print(err)
			os.Exit(1)
		}
		defer close()
		if err := runSelfUpdateCommand(os.Args[2:]); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "nodes" {
		guard := initialsetup.Normal
		if len(os.Args) > 2 && os.Args[2] == "recovery" {
			if !validRecoveryArgs(os.Args[3:]) {
				log.Print("usage: nodes recovery {inspect|activate-current --digest SHA256|verify-existing --digest SHA256}")
				os.Exit(2)
			}
			guard = initialsetup.Maintenance
		}
		close, err := guard()
		if err != nil {
			log.Print(err)
			os.Exit(1)
		}
		defer close()
		if err := runNodesCommand(os.Args[2:]); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 {
		log.Print("unsupported panel command; use native, nodes, password, version or self-update")
		os.Exit(2)
	}
	closeSetupGuard, err := initialsetup.Normal()
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
	defer closeSetupGuard()

	listenerFile := getenv("XKEEN_CONTROL_LISTEN_FILE", panellistener.DefaultFilePath)
	listenerResolution, err := panellistener.ResolveStartup(os.Getenv("XKEEN_CONTROL_LISTEN"), listenerFile)
	if err != nil {
		log.Printf("invalid listen address: %v", err)
		os.Exit(2)
	}
	listenAddress := listenerResolution.Address
	startedAt := time.Now().UTC()
	authManager := auth.NewManager(auth.Config{
		HashPath:            getenv("XKEEN_CONTROL_AUTH_HASH", auth.PasswordHashPath),
		BootstrapMarkerPath: getenv("XKEEN_CONTROL_BOOTSTRAP_MARKER", auth.BootstrapMarkerPath),
		SecureCookies:       envBool("XKEEN_CONTROL_TLS"),
		SessionTTL:          30 * 24 * time.Hour,
		SessionPath:         filepath.Join(filepath.Dir(getenv("XKEEN_CONTROL_AUTH_HASH", auth.PasswordHashPath)), "sessions.json"),
		SessionAudience:     listenAddress,
	})
	xrayReader := xrayapi.NewClient(
		getenv("XKEEN_XRAY_API_ADDR", xrayapi.DefaultAPIAddress),
		getenv("XKEEN_XRAY_PROBE_ADDR", xrayapi.DefaultProbeAddress),
		2*time.Second,
	)
	xkeenReader := xkeen.NewReader()
	configReader := configview.NewReader(
		getenv("XKEEN_XRAY_CONFIG_DIR", "/opt/etc/xray/configs"),
		getenv("XKEEN_CONFIG_PATH", "/opt/etc/xkeen/xkeen.json"),
	)
	policy := c1.DefaultPolicy()
	probeRouter := c1.NewProbeRouter(xrayReader)
	var nodeManager *nodes.Manager
	nodeReader := func(ctx context.Context) []c1.NodeState {
		if nodeManager == nil {
			return nil
		}
		items, err := nodeManager.List()
		if err != nil {
			return nil
		}
		result := make([]c1.NodeState, 0, len(items))
		for _, item := range items {
			result = append(result, c1.NodeState{ID: item.ID, Tag: item.OutboundTag, Enabled: item.Enabled})
		}
		_ = ctx
		return result
	}
	coordinator := c1.NewCoordinator(policy, nodeReader)
	resources := resourcepolicy.NewGuard()
	resources.Conflict = xkeenReader.NativeSpeedConflict
	manualRunner := c1.NewManualNodeRunner(probeRouter)
	manualRunner.Resources = resources
	coordinator.SetManualRunner(manualRunner)
	adaptiveRunner := c1.NewAdaptiveRunner(probeRouter)
	adaptiveRunner.Resources = resources
	coordinator.SetAdaptiveRunner(adaptiveRunner)
	authorityLease := authority.NewLease()
	authorityLease.Admission = panelupdate.MutationReady
	if nodes.RecoveryNeedsInspection(getenv("XKEEN_NODE_PREVIOUS_DIR", defaultNodePreviousDir)) {
		authorityLease.Block()
	}
	panelLifecycle := panelLifecycle{coordinator: coordinator, lease: authorityLease}
	listenerService := panellistener.NewService(panellistener.Config{
		FilePath:   listenerFile,
		HelperPath: getenv("XKEEN_CONTROL_UPDATER", panellistener.DefaultHelperPath),
		Initial:    listenerResolution,
		Lifecycle:  panelLifecycle,
	})
	if err := listenerService.StartupError(); err != nil {
		log.Printf("panel listener startup initialization failed: %v", err)
		os.Exit(1)
	}
	nativeConfig := &xkeen.ConfigEditor{DraftDir: getenv("XKEEN_NATIVE_CONFIG_DRAFT_DIR", "/opt/etc/xkeen-control/secrets/config-drafts"), Dir: getenv("XKEEN_XRAY_CONFIG_DIR", defaultXrayConfigDir), XrayBinary: getenv("XKEEN_XRAY_BINARY", defaultXrayBinary), Lease: authorityLease, PreviousDir: getenv("XKEEN_NATIVE_CONFIG_PREVIOUS_DIR", "/opt/etc/xkeen-control/previous/native-config"), AssetDir: getenv("XKEEN_XRAY_ASSET_DIR", defaultXrayAssetDir)}
	nativeConfig.RegistryPath = getenv("XKEEN_NODES_PATH", defaultNodesPath)
	nodeManager = newNodeManager(coordinator, authorityLease, nativeConfig)
	nativeJobs := newNativeJobs(authorityLease)
	dnsIntegration := &splitdns.Service{
		Dir: "/opt/etc/mosdns", Init: "/opt/etc/init.d/S06mosdns", AssetDir: nativeConfig.AssetDir, Lease: authorityLease,
		ReadNative: func(ctx context.Context) (map[string][]byte, error) {
			snapshot, err := nativeConfig.Snapshot(ctx)
			return snapshot.NativeDocuments(), err
		},
		Pending: func() bool { exists, err := nativeConfig.HasSavedChanges(); return exists || err != nil },
	}
	nativeConfig.ValidateDerived = dnsIntegration.Validate
	nativeJobs.AfterCommand = func(ctx context.Context, action string) {
		switch action {
		case "start", "restart", "update-geodata", "update-xkeen", "update-xray", "geodata-sources":
			_ = dnsIntegration.ReconcileOwned(ctx)
		}
	}
	subscriptionRefresher := nodes.NewSubscriptionRefresher(nodeManager)
	nodeManager.SetAutoRefreshStatusProvider(subscriptionRefresher.AutoRefreshStatuses)
	collector := controlruntime.NewCollector(buildinfo.Current().Version, startedAt, controlruntime.Dependencies{
		Xray:             xrayReader,
		Xkeen:            xkeenReader,
		Config:           configReader,
		OutboundTagsPath: getenv("XKEEN_NODES_PATH", defaultNodesPath),
		C1:               coordinator,
		Native:           xkeen.Discovery{},
		Setup: func() controlruntime.SetupStatus {
			return controlruntime.SetupStatus{Panel: "ready", Credential: authManager.CredentialState(), State: "native", Eligible: false}
		},
	})
	collector.SetBuildInfo(buildinfo.Current())
	updateManager := panelupdate.NewManager(panelupdate.Config{Current: buildinfo.Current(), Lifecycle: panelLifecycle})
	notificationService := notifications.NewService()
	panelNotifyScheduler := panelupdate.NewNotifyScheduler(panelupdate.NotifySchedulerConfig{
		Manager: updateManager,
		Send:    notificationService.Send,
		Lifecycle: func() (bool, bool, bool) {
			state := coordinator.Snapshot()
			if state.Lifecycle == nil {
				return false, false, false
			}
			return state.Lifecycle.Maintenance, state.Lifecycle.Applying, true
		},
	})
	qualityService := &nativequality.Service{Editor: nativeConfig, Lease: authorityLease, Reader: xrayReader, Nodes: nodeReader, Measurement: coordinator, Probe: probeRouter, Control: xrayReader, Resources: resources, Jobs: nativeJobs, AutomaticDisabled: getenv("XKEEN_QUALITY_AUTO", "") == "off"}
	qualitySchedule := nativequality.NewSchedule(qualityService)
	nodeManager.OnSubscriptionRefresh = qualitySchedule.NotifyRefresh
	defer qualityService.Stop()
	nativeTransfer := &nativebackup.Service{Editor: nativeConfig, Nodes: nodeManager, Lease: authorityLease}
	handler := httpapi.New(httpapi.Config{
		MutationReady:  panelupdate.MutationReady,
		Native:         xkeen.Discovery{},
		NativeJobs:     nativeJobs,
		Geodata:        &geodatareader.Reader{Dir: getenv("XKEEN_XRAY_ASSET_DIR", defaultXrayAssetDir)},
		NativeConfig:   nativeConfig,
		SplitDNS:       dnsIntegration,
		Collector:      collector,
		Auth:           authManager,
		Nodes:          nodeManager,
		Backup:         nativeTransfer,
		NativeTransfer: nativeTransfer,
		NativeQuality:  qualityService,
		Selection:      qualityService, // Explicit native volatile pin; no panel selection loop.
		Assets:         webassets.Handler(),
		StartedAt:      startedAt,
		Manual:         coordinator,
		Updates:        updateManager,
		Notifications:  notificationService,
		Listener:       listenerService,
	})

	server := &http.Server{
		Addr:              listenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      nodes.DefaultTransactionTimeout + validationbudget.HTTPMargin,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	runtimeContext, cancelRuntime := context.WithCancel(context.Background())
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-shutdown
		cancelRuntime()
		qualityService.Stop()
		subscriptionRefresher.Stop()
		panelNotifyScheduler.Stop()
		coordinator.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	// Native Xray owns automatic selection until the panel mode is explicitly qualified.
	// Automatic subscription refresh is enabled separately from native commands.
	go func() {
		// Serve health during verification, but defer background mutation until
		// the existing updater has durably settled its intent.
		deadline := time.NewTimer(3 * time.Minute)
		defer deadline.Stop()
		for panelupdate.MutationReady() != nil {
			select {
			case <-runtimeContext.Done():
				return
			case <-deadline.C:
				return
			case <-time.After(time.Second):
			}
		}
		if runtimeContext.Err() != nil {
			return
		}
		// Remove temporary probe rules a previous process may have left in Xray.
		// On failure the probe gate stays closed and the next probe retries.
		reconcileContext, cancelReconcile := context.WithTimeout(runtimeContext, 10*time.Second)
		if err := probeRouter.Reconcile(reconcileContext); err != nil {
			log.Print("probe rule reconciliation deferred")
		}
		cancelReconcile()
		subscriptionRefresher.Start(runtimeContext)
		go dnsIntegration.Run(runtimeContext)
		go qualitySchedule.Run(runtimeContext)
		panelNotifyScheduler.Start(runtimeContext)
		go notificationService.RunControl(runtimeContext, func(ctx context.Context, command notifications.Command) notifications.ControlResult {
			if command == notifications.RefreshSubscriptions {
				if ctx.Err() == nil && subscriptionRefresher.RequestRefresh() {
					return notifications.Accepted
				}
				return notifications.Refused
			}
			if command == notifications.StatusCommand {
				facts := (xkeen.Discovery{}).Inspect(ctx)
				if facts.XrayRunning {
					return notifications.Running
				}
				return notifications.Unknown
			}
			actions := map[notifications.Command]string{notifications.StartCommand: "start", notifications.StopCommand: "stop", notifications.RestartCommand: "restart", notifications.UpdateXkeen: "update-xkeen", notifications.UpdateXray: "update-xray", notifications.UpdateGeodata: "update-geodata"}
			action, ok := actions[command]
			if !ok || nativeJobs == nil || ctx.Err() != nil {
				return notifications.Refused
			}
			if _, err := nativeJobs.StartRemote(action, nativeConfig); err != nil {
				return notifications.Refused
			}
			return notifications.Accepted
		})

	}()
	log.Printf("xkeen-control %s listening on %s", buildinfo.Current().Version, listenAddress)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Print(err)
		os.Exit(1)
	}
}

func updateReadOnlyCommand(args []string) bool {
	if len(args) == 0 {
		return true
	}
	if len(args) == 2 && (args[0] == "version" && args[1] == "--json" || args[0] == "setup" && (args[1] == "guard" || args[1] == "inspect") || args[0] == "self-update" && (args[1] == "inspect" || args[1] == "inspect-installed" || args[1] == "inspect-capabilities")) {
		return true
	}
	return len(args) == 3 && args[0] == "nodes" && args[1] == "recovery" && args[2] == "inspect"
}

func runCapabilityInspection(ctx context.Context, out io.Writer, admit func() (func(), error), inspect func(context.Context) (panelupdate.CapabilityReport, error)) error {
	close, err := admit()
	if err != nil {
		report := panelupdate.CapabilityReport{Sync: "not-run", Timeout: "not-run", Flock: "not-run", Reason: "setup-admission-unavailable"}
		if errors.Is(err, initialsetup.ErrBusy) {
			report.Reason = "setup-admission-busy"
		}
		if e := json.NewEncoder(out).Encode(report); e != nil {
			return e
		}
		return err
	}
	defer close()
	report, err := inspect(ctx)
	if e := json.NewEncoder(out).Encode(report); e != nil {
		return e
	}
	return err
}

func newNativeJobs(lease *authority.Lease) *xkeen.Jobs {
	path := getenv("XKEEN_NATIVE_JOB_RECEIPT", "/opt/etc/xkeen-control/state/native-jobs/last-job.json")
	if os.MkdirAll(filepath.Dir(path), 0700) != nil {
		lease.Block()
		log.Print("native job storage unavailable")
		return nil
	}
	jobs, err := xkeen.NewPersistentJobs(getenv("XKEEN_XKEEN_BINARY", "/opt/sbin/xkeen"), lease, path)
	if err != nil {
		lease.Block()
		log.Print("native job state requires local inspection")
		return nil
	}
	jobs.RequireInstalledCommands()
	jobs.ConfigureRecovery(xkeen.Discovery{}, filepath.Join(getenv("XKEEN_NODE_PREVIOUS_DIR", defaultNodePreviousDir), ".pending"))
	return jobs
}

func transactionJournalPresent(path string) (bool, error) {
	if path == "" {
		return false, errors.New("transaction journal path is not configured")
	}
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

const (
	defaultNodesPath       = "/opt/etc/xkeen-control/secrets/nodes.json"
	defaultLegacyPath      = "/opt/etc/xkeen-control/secrets/04_outbounds.json"
	defaultActiveOutbounds = "/opt/etc/xray/configs/04_outbounds.json"
	defaultNodePreviousDir = "/opt/etc/xkeen-control/previous"
	defaultXrayConfigDir   = "/opt/etc/xray/configs"
	defaultXrayBinary      = "/opt/sbin/xray"
	defaultXrayAssetDir    = "/opt/etc/xray/dat"
)

func newNodeManager(coordinator interface {
	BeginApply(context.Context) (func(), error)
}, lease *authority.Lease, editor *xkeen.ConfigEditor) *nodes.Manager {
	registryPath := getenv("XKEEN_NODES_PATH", defaultNodesPath)
	configDir := getenv("XKEEN_XRAY_CONFIG_DIR", defaultXrayConfigDir)
	activeOutboundsPath := getenv("XKEEN_ACTIVE_OUTBOUNDS", filepath.Join(configDir, "04_outbounds.json"))
	var beforeCommit func(context.Context) error
	if editor != nil {
		beforeCommit = func(ctx context.Context) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			pending, err := editor.HasSavedChanges()
			if err != nil || pending {
				return nodes.ErrOperationUnavailable
			}
			return nil
		}
	}
	return nodes.NewManager(nodes.Config{
		BeforeCommit: beforeCommit,
		Store:        nodes.Store{Path: registryPath},
		LegacyPath:   getenv("XKEEN_LEGACY_OUTBOUNDS", defaultLegacyPath),
		Transaction: nodes.Transaction{
			Store:               nodes.Store{Path: registryPath},
			ActiveOutboundsPath: activeOutboundsPath,
			ConfigDir:           configDir,
			PreviousDir:         getenv("XKEEN_NODE_PREVIOUS_DIR", defaultNodePreviousDir),
			Activator: nodes.CommandActivator{
				XrayBinary:          getenv("XKEEN_XRAY_BINARY", "xray"),
				XrayAssetDir:        getenv("XKEEN_XRAY_ASSET_DIR", "/opt/etc/xray/dat"),
				NativeLifecycleInit: "/opt/etc/init.d/S05xkeen",
				APIAddress:          getenv("XKEEN_XRAY_API_ADDR", xrayapi.DefaultAPIAddress),
				ActiveOutboundsPath: activeOutboundsPath,
				RoutingPath:         filepath.Join(configDir, "05_routing.json"),
			},
		},
		AuthorityLease: lease,
		Coordinator:    coordinator,
	})
}

func runNodesCommand(args []string) error {
	manager := newNodeManager(nil, authority.NewLease(), nil)
	if len(args) == 0 {
		return errors.New("usage: xkeen-control nodes {validate|render --output PATH|reconcile-runtime}")
	}
	switch args[0] {
	case "recovery":
		if !validRecoveryArgs(args[1:]) {
			return errors.New("usage: nodes recovery {inspect|activate-current --digest SHA256|verify-existing --digest SHA256}")
		}
		configDir := getenv("XKEEN_XRAY_CONFIG_DIR", defaultXrayConfigDir)
		manager = newNodeManager(nil, authority.NewLease(), &xkeen.ConfigEditor{PreviousDir: getenv("XKEEN_NATIVE_CONFIG_PREVIOUS_DIR", "/opt/etc/xkeen-control/previous/native-config")})
		runtime := nodes.ProcessRecoveryRuntime{Binary: getenv("XKEEN_XRAY_BINARY", "/opt/sbin/xray"), ConfigDir: configDir, ReceiptPath: getenv("XKEEN_NATIVE_JOB_RECEIPT", "/opt/etc/xkeen-control/state/native-jobs/last-job.json")}
		ctx, cancel := context.WithTimeout(context.Background(), nodes.DefaultTransactionTimeout)
		defer cancel()
		if args[1] == "inspect" {
			value, err := manager.InspectRecovery(ctx, runtime)
			if err != nil {
				return errors.New("node recovery inspection unavailable; inspect pending state and quiesce competing processes")
			}
			return json.NewEncoder(os.Stdout).Encode(value)
		}
		if args[1] == "verify-existing" {
			if err := manager.VerifyExistingRecovery(ctx, args[3], runtime); err != nil {
				return errors.New("existing node recovery not verified; inspect durable state, no activation was invoked")
			}
			return json.NewEncoder(os.Stdout).Encode(map[string]string{"state": "completed", "action": "verify-existing"})
		}
		if err := manager.RecoverCurrent(ctx, args[3], runtime); err != nil {
			return errors.New("node recovery not completed; inspect durable state, do not retry activation")
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]string{"state": "completed", "action": "activate-current"})
	case "validate":
		if err := manager.ValidateStored(); err != nil {
			return errors.New("node registry validation failed")
		}
		_, err := io.WriteString(os.Stdout, "node registry valid\n")
		return err
	case "render":
		if len(args) != 3 || args[1] != "--output" || args[2] == "" {
			return errors.New("usage: xkeen-control nodes render --output PATH")
		}
		contents, err := manager.RenderStored()
		if err != nil {
			return errors.New("node registry render failed")
		}
		return writeCLIOutput(args[2], contents)
	case "reconcile-runtime":
		if len(args) != 1 {
			return errors.New("usage: xkeen-control nodes reconcile-runtime")
		}
		ctx, cancel := context.WithTimeout(context.Background(), nodes.DefaultTransactionTimeout)
		defer cancel()
		if err := manager.ReconcileRuntime(ctx); err != nil {
			return errors.New("node runtime reconciliation failed")
		}
		_, err := io.WriteString(os.Stdout, "node runtime reconciled\n")
		return err
	default:
		return errors.New("usage: xkeen-control nodes {validate|render --output PATH|reconcile-runtime}")
	}
}

func validRecoveryArgs(args []string) bool {
	if len(args) == 1 && args[0] == "inspect" {
		return true
	}
	if len(args) != 3 || (args[0] != "activate-current" && args[0] != "verify-existing") || args[1] != "--digest" || len(args[2]) != 64 {
		return false
	}
	_, err := hex.DecodeString(args[2])
	return err == nil && strings.ToLower(args[2]) == args[2]
}

func writeCLIOutput(path string, contents []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.New("unable to create output directory")
	}
	temporary, err := os.CreateTemp(dir, ".xkeen-render-*")
	if err != nil {
		return errors.New("unable to create output file")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return errors.New("unable to protect output file")
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return errors.New("unable to write output file")
	}
	if err := temporary.Close(); err != nil {
		return errors.New("unable to close output file")
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return errors.New("unable to replace output file")
	}
	_ = os.Chmod(path, 0o600)
	return nil
}

func listenAddressFromEnv() (string, error) {
	resolution, err := panellistener.ResolveStartup(os.Getenv("XKEEN_CONTROL_LISTEN"), getenv("XKEEN_CONTROL_LISTEN_FILE", panellistener.DefaultFilePath))
	if err != nil {
		return "", err
	}
	return resolution.Address, nil
}

func getenv(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func runSelfUpdateCommand(args []string) error {
	channel := "stable"
	apply := false
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--channel":
			if index+1 >= len(args) {
				return errors.New("usage: xkeen-control self-update --channel {stable|beta} --apply [version]")
			}
			channel = args[index+1]
			index++
		case "--apply":
			apply = true
		default:
			if strings.HasPrefix(args[index], "-") || index != len(args)-1 {
				return errors.New("usage: xkeen-control self-update --channel {stable|beta} --apply [version]")
			}
			version := args[index]
			if !apply {
				return errors.New("self-update requires --apply")
			}
			manager := panelupdate.NewManager(panelupdate.Config{Current: buildinfo.Current()})
			return manager.Apply(context.Background(), channel, version)
		}
	}
	if !apply {
		return errors.New("self-update requires --apply")
	}
	manager := panelupdate.NewManager(panelupdate.Config{Current: buildinfo.Current()})
	return manager.Apply(context.Background(), channel, "")
}
