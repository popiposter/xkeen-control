package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/popiposter/xkeen-control/internal/appliance"
	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/backup"
	"github.com/popiposter/xkeen-control/internal/buildinfo"
	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/components"
	"github.com/popiposter/xkeen-control/internal/configview"
	"github.com/popiposter/xkeen-control/internal/httpapi"
	"github.com/popiposter/xkeen-control/internal/nodes"
	"github.com/popiposter/xkeen-control/internal/restore"
	controlruntime "github.com/popiposter/xkeen-control/internal/runtime"
	panelupdate "github.com/popiposter/xkeen-control/internal/update"
	"github.com/popiposter/xkeen-control/internal/webassets"
	"github.com/popiposter/xkeen-control/internal/xkeen"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

const (
	defaultListenAddress   = "127.0.0.1:8787"
	nodeApplyResponseGrace = 15 * time.Second
	httpWriteTimeout       = nodes.DefaultTransactionTimeout + nodes.DefaultApplyGateWaitTimeout + nodeApplyResponseGrace
)

func main() {
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
		if err := runSelfUpdateCommand(os.Args[2:]); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "nodes" {
		if err := runNodesCommand(os.Args[2:]); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "appliance" {
		if err := runApplianceCommand(os.Args[2:]); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}

	listenAddress, err := listenAddressFromEnv()
	if err != nil {
		log.Printf("invalid listen address: %v", err)
		os.Exit(2)
	}
	startedAt := time.Now().UTC()
	authManager := auth.NewManager(auth.Config{
		HashPath:            getenv("XKEEN_CONTROL_AUTH_HASH", auth.PasswordHashPath),
		BootstrapMarkerPath: getenv("XKEEN_CONTROL_BOOTSTRAP_MARKER", auth.BootstrapMarkerPath),
		SecureCookies:       envBool("XKEEN_CONTROL_TLS"),
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
	probeAddress := xrayReader.ProbeAddr
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
			result = append(result, c1.NodeState{Tag: item.OutboundTag, Enabled: item.Enabled})
		}
		_ = ctx
		return result
	}
	selectionStore := c1.SelectionStore{Path: getenv("XKEEN_CONTROL_SELECTION_PATH", c1.DefaultSelectionPath)}
	supervisor := c1.NewSupervisor(policy, xrayReader, xrayReader, nodeReader, probeRouter, selectionStore)
	supervisor.SetActiveProbe(func(ctx context.Context, _ string, payload int64) error {
		return c1.HTTPProbe(ctx, policy.BenchmarkEndpoint, probeAddress, payload, 3*time.Second)
	})
	runner := c1.NewBenchmarkRunner(policy, probeRouter, c1.BenchmarkStore{Path: getenv("XKEEN_CONTROL_BENCHMARK_PATH", c1.DefaultBenchmarkPath)})
	coordinator := c1.NewCoordinator(policy, supervisor, runner, nodeReader)
	authorityLease := authority.NewLease()
	componentGate := components.NewComponentMutationGate()
	componentMaintenance := components.NewComponentMaintenance(coordinator, authorityLease)
	nodeManager = newNodeManager(coordinator, authorityLease)
	applianceService := newApplianceService(authorityLease)
	restoreService := newRestoreService(coordinator, authorityLease)
	componentXrayService := newXrayService(coordinator, authorityLease, applianceService, nodeManager, xrayReader, componentGate, componentMaintenance)
	componentGeodataService := newGeodataService(coordinator, authorityLease, applianceService, nodeManager, xrayReader, componentGate, componentMaintenance)
	stateDir := getenv("XKEEN_APPLIANCE_IMPORT_STATE_DIR", "/opt/etc/xkeen-control/state")
	restoreJournalPath := filepath.Join(stateDir, "appliance-import-transaction.json")
	restorePending, restoreJournalErr := transactionJournalPresent(restoreJournalPath)
	componentJournalPath := getenv("XKEEN_COMPONENT_TRANSACTION_PATH", components.DefaultComponentTransactionJournal)
	componentRecoveryConfig := components.ComponentRecoveryConfig{
		JournalPath:                componentJournalPath,
		RestoreJournalPath:         restoreJournalPath,
		XrayPreviousStagingPath:    getenv("XKEEN_XRAY_PREVIOUS_DIR", components.DefaultXrayPreviousDir) + ".staging",
		XrayStagingDir:             getenv("XKEEN_COMPONENT_STAGING_DIR", components.DefaultXrayComponentStagingDir),
		GeodataPreviousStagingPath: getenv("XKEEN_GEODATA_PREVIOUS_DIR", components.DefaultGeodataPreviousDir) + ".staging",
		GeodataStagingDir:          getenv("XKEEN_GEODATA_COMPONENT_STAGING_DIR", components.DefaultGeodataComponentStagingDir),
	}
	recoveryState, componentJournalErr := components.InspectComponentRecovery(componentRecoveryConfig)
	if restoreJournalErr != nil || componentJournalErr != nil {
		log.Print("startup transaction journal state is unavailable")
		os.Exit(1)
	}
	if restorePending {
		if err := restoreService.RecoverStartup(context.Background()); err != nil {
			log.Print("restore startup recovery failed")
			os.Exit(1)
		}
	}
	if recoveryState.Pending() {
		var recoveryErr error
		switch recoveryState.Kind {
		case components.KindXray:
			recoveryErr = componentXrayService.RecoverStartup(context.Background())
		case components.KindGeodata:
			recoveryErr = componentGeodataService.RecoverStartup(context.Background())
		default:
			recoveryErr = errors.New("component recovery owner is unavailable")
		}
		if recoveryErr != nil {
			log.Print("component startup recovery failed")
			os.Exit(1)
		}
		remaining, remainingErr := components.InspectComponentRecovery(componentRecoveryConfig)
		if remainingErr != nil || remaining.Pending() {
			log.Print("component startup recovery is not settled")
			os.Exit(1)
		}
	}
	if err := restoreService.Ready(); err != nil {
		log.Print("restore startup recovery is not proven")
		os.Exit(1)
	}
	if err := componentXrayService.Ready(); err != nil {
		log.Print("component startup recovery is not proven")
		os.Exit(1)
	}
	if err := componentGeodataService.Ready(); err != nil {
		log.Print("component startup recovery is not proven")
		os.Exit(1)
	}
	collector := controlruntime.NewCollector(buildinfo.Current().Version, startedAt, controlruntime.Dependencies{
		Xray:             xrayReader,
		Xkeen:            xkeenReader,
		Config:           configReader,
		OutboundTagsPath: getenv("XKEEN_NODES_PATH", defaultNodesPath),
		C1:               coordinator,
		Setup: func() controlruntime.SetupStatus {
			return controlruntime.SetupStatus{Panel: "ready", Credential: authManager.CredentialState()}
		},
	})
	collector.SetBuildInfo(buildinfo.Current())
	updateManager := panelupdate.NewManager(panelupdate.Config{Current: buildinfo.Current(), Lifecycle: coordinator})
	componentService := components.NewService(components.Config{
		Panel:                 buildinfo.Current(),
		XrayBinary:            getenv("XKEEN_XRAY_BINARY", components.DefaultXrayBinary),
		XkeenBinary:           components.DefaultXkeenBinary,
		XkeenModuleDir:        components.DefaultXkeenModuleDir,
		XkeenModuleImport:     components.DefaultXkeenModuleImport,
		XkeenRuntimeInit:      components.DefaultXkeenRuntimeInit,
		XkeenConfig:           components.DefaultXkeenConfig,
		XkeenPackageMetadata:  components.DefaultXkeenPackageMetadata,
		GeodataDir:            components.DefaultGeodataDir,
		AppliancePath:         components.DefaultAppliancePath,
		RoutingPath:           components.DefaultRoutingPath,
		DNSPath:               components.DefaultDNSPath,
		KeeneticOSVersionPath: components.DefaultKeeneticOSVersionPath,
		EntwareReleasePath:    components.DefaultEntwareReleasePath,
		EntwareBinary:         components.DefaultEntwareBinary,
		InventoryTimeout:      components.DefaultInventoryTimeout,
		XrayProbeTimeout:      components.DefaultXrayProbeTimeout,
	})
	componentChecker := components.NewChecker(components.CheckerConfig{
		InstalledSnapshot: componentService.Latest,
	})
	handler := httpapi.New(httpapi.Config{
		Collector:       collector,
		Auth:            authManager,
		Nodes:           nodeManager,
		Benchmark:       coordinator,
		Selection:       coordinator,
		Assets:          webassets.Handler(),
		StartedAt:       startedAt,
		Components:      componentService,
		ComponentChecks: componentChecker,
		Updates:         updateManager,
		Restore:         restoreService,
		Backup: backup.NewService(backup.Config{
			Appliance:      applianceService,
			Nodes:          nodeManager,
			AuthorityLease: authorityLease,
			Build:          buildinfo.Current(),
		}),
	})

	server := &http.Server{
		Addr:              listenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		// The bounded gate wait happens before the five-minute application
		// transaction budget. Keep the HTTP response window longer than both
		// windows so the operator can observe the committed or recovered result.
		WriteTimeout:   httpWriteTimeout,
		IdleTimeout:    30 * time.Second,
		MaxHeaderBytes: 16 << 10,
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-shutdown
		coordinator.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	coordinator.Start(context.Background())

	log.Printf("xkeen-control %s listening on %s", buildinfo.Current().Version, listenAddress)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Print(err)
		os.Exit(1)
	}
}

func newXrayService(coordinator *c1.Coordinator, lease *authority.Lease, applianceService *appliance.Service, nodeManager *nodes.Manager, xrayReader *xrayapi.Client, mutationGate *components.ComponentMutationGate, maintenance *components.ComponentMaintenance) *components.XrayService {
	xrayBinary := getenv("XKEEN_XRAY_BINARY", components.DefaultXrayBinary)
	xrayAssetDir := getenv("XKEEN_XRAY_ASSET_DIR", components.DefaultXrayAssetDir)
	configDir := getenv("XKEEN_XRAY_CONFIG_DIR", defaultXrayConfigDir)
	activeOutboundsPath := getenv("XKEEN_ACTIVE_OUTBOUNDS", filepath.Join(configDir, "04_outbounds.json"))
	xkeenConfigPath := getenv("XKEEN_CONFIG_PATH", "/opt/etc/xkeen/xkeen.json")
	nodesPath := getenv("XKEEN_NODES_PATH", defaultNodesPath)
	appliancePath := getenv("XKEEN_APPLIANCE_PATH", defaultAppliancePath)
	stateDir := getenv("XKEEN_APPLIANCE_IMPORT_STATE_DIR", "/opt/etc/xkeen-control/state")
	return components.NewXrayService(components.XrayConfig{
		Resolver:   components.NewXrayResolver(nil, nil),
		Downloader: components.NewXrayArtifactDownloader(nil, nil),
		Authority: components.NewFileAuthorityProvider(components.FileAuthorityConfig{
			Appliance:           applianceService,
			Nodes:               nodeManager,
			AppliancePath:       appliancePath,
			NodesPath:           nodesPath,
			ConfigDir:           configDir,
			XkeenConfigPath:     xkeenConfigPath,
			ActiveOutboundsPath: activeOutboundsPath,
		}),
		Runtime: components.CommandXrayRuntime{
			Activator: nodes.CommandActivator{
				XrayBinary:          xrayBinary,
				XrayAssetDir:        xrayAssetDir,
				XkeenBinary:         getenv("XKEEN_XKEEN_BINARY", components.DefaultXkeenBinary),
				APIAddress:          getenv("XKEEN_XRAY_API_ADDR", xrayapi.DefaultAPIAddress),
				ActiveOutboundsPath: activeOutboundsPath,
				RoutingPath:         filepath.Join(configDir, "05_routing.json"),
			},
			ActiveBinary:   xrayBinary,
			ConfigDir:      configDir,
			AssetDir:       xrayAssetDir,
			ProbeReachable: xrayReader.ProbeReachable,
		},
		CandidateProbe:     components.CommandXrayCandidateProbe{Binary: xrayBinary},
		CandidateValidator: components.CommandXrayCandidateValidator{Binary: xrayBinary},
		AuthorityLease:     lease,
		Coordinator:        coordinator,
		ActiveBinaryPath:   xrayBinary,
		ConfigDir:          configDir,
		AssetDir:           xrayAssetDir,
		PreviousDir:        getenv("XKEEN_XRAY_PREVIOUS_DIR", components.DefaultXrayPreviousDir),
		JournalPath:        getenv("XKEEN_COMPONENT_TRANSACTION_PATH", components.DefaultComponentTransactionJournal),
		StagingDir:         getenv("XKEEN_COMPONENT_STAGING_DIR", components.DefaultXrayComponentStagingDir),
		RestoreJournalPath: filepath.Join(stateDir, "appliance-import-transaction.json"),
		MutationGate:       mutationGate,
		Maintenance:        maintenance,
	})
}

func newGeodataService(coordinator *c1.Coordinator, lease *authority.Lease, applianceService *appliance.Service, nodeManager *nodes.Manager, xrayReader *xrayapi.Client, mutationGate *components.ComponentMutationGate, maintenance *components.ComponentMaintenance) *components.GeodataService {
	xrayBinary := getenv("XKEEN_XRAY_BINARY", components.DefaultXrayBinary)
	xrayAssetDir := getenv("XKEEN_XRAY_ASSET_DIR", components.DefaultXrayAssetDir)
	configDir := getenv("XKEEN_XRAY_CONFIG_DIR", defaultXrayConfigDir)
	activeOutboundsPath := getenv("XKEEN_ACTIVE_OUTBOUNDS", filepath.Join(configDir, "04_outbounds.json"))
	xkeenConfigPath := getenv("XKEEN_CONFIG_PATH", "/opt/etc/xkeen/xkeen.json")
	nodesPath := getenv("XKEEN_NODES_PATH", defaultNodesPath)
	appliancePath := getenv("XKEEN_APPLIANCE_PATH", defaultAppliancePath)
	stateDir := getenv("XKEEN_APPLIANCE_IMPORT_STATE_DIR", "/opt/etc/xkeen-control/state")
	return components.NewGeodataService(components.GeodataConfig{
		Resolver:   components.NewGeodataResolver(nil, nil),
		Downloader: components.NewGeodataArtifactDownloader(nil, nil),
		Authority: components.NewFileAuthorityProvider(components.FileAuthorityConfig{
			Appliance:           applianceService,
			Nodes:               nodeManager,
			AppliancePath:       appliancePath,
			NodesPath:           nodesPath,
			ConfigDir:           configDir,
			XkeenConfigPath:     xkeenConfigPath,
			ActiveOutboundsPath: activeOutboundsPath,
		}),
		Runtime: components.CommandXrayRuntime{
			Activator: nodes.CommandActivator{
				XrayBinary:          xrayBinary,
				XrayAssetDir:        xrayAssetDir,
				XkeenBinary:         getenv("XKEEN_XKEEN_BINARY", components.DefaultXkeenBinary),
				APIAddress:          getenv("XKEEN_XRAY_API_ADDR", xrayapi.DefaultAPIAddress),
				ActiveOutboundsPath: activeOutboundsPath,
				RoutingPath:         filepath.Join(configDir, "05_routing.json"),
			},
			ActiveBinary:   xrayBinary,
			ConfigDir:      configDir,
			AssetDir:       xrayAssetDir,
			ProbeReachable: xrayReader.ProbeReachable,
		},
		CandidateProbe:     components.CommandXrayCandidateProbe{Binary: xrayBinary},
		CandidateValidator: components.CommandXrayCandidateValidator{Binary: xrayBinary},
		AuthorityLease:     lease,
		Coordinator:        coordinator,
		ActiveBinaryPath:   xrayBinary,
		ConfigDir:          configDir,
		AssetDir:           xrayAssetDir,
		PreviousDir:        getenv("XKEEN_GEODATA_PREVIOUS_DIR", components.DefaultGeodataPreviousDir),
		JournalPath:        getenv("XKEEN_COMPONENT_TRANSACTION_PATH", components.DefaultComponentTransactionJournal),
		StagingDir:         getenv("XKEEN_GEODATA_COMPONENT_STAGING_DIR", components.DefaultGeodataComponentStagingDir),
		RestoreJournalPath: filepath.Join(stateDir, "appliance-import-transaction.json"),
		MutationGate:       mutationGate,
		Maintenance:        maintenance,
	})
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
	defaultAppliancePath   = "/opt/etc/xkeen-control/config/appliance.json"
	defaultXrayConfigDir   = "/opt/etc/xray/configs"
)

func newApplianceService(lease *authority.Lease) *appliance.Service {
	configDir := getenv("XKEEN_XRAY_CONFIG_DIR", "/opt/etc/xray/configs")
	return appliance.NewService(appliance.Config{
		AppliancePath:       getenv("XKEEN_APPLIANCE_PATH", defaultAppliancePath),
		ConfigDir:           configDir,
		XkeenConfigPath:     getenv("XKEEN_CONFIG_PATH", "/opt/etc/xkeen/xkeen.json"),
		NodesPath:           getenv("XKEEN_NODES_PATH", defaultNodesPath),
		ActiveOutboundsPath: getenv("XKEEN_ACTIVE_OUTBOUNDS", filepath.Join(configDir, "04_outbounds.json")),
		Validator: nodes.CommandActivator{
			XrayBinary:   getenv("XKEEN_XRAY_BINARY", "xray"),
			XrayAssetDir: getenv("XKEEN_XRAY_ASSET_DIR", "/opt/etc/xray/dat"),
		},
		AuthorityLease: lease,
	})
}

func newRestoreService(coordinator interface {
	BeginApply(context.Context) (func(), error)
}, lease *authority.Lease) *restore.Service {
	configDir := getenv("XKEEN_XRAY_CONFIG_DIR", "/opt/etc/xray/configs")
	return restore.NewService(restore.Config{
		AppliancePath:       getenv("XKEEN_APPLIANCE_PATH", defaultAppliancePath),
		NodesPath:           getenv("XKEEN_NODES_PATH", defaultNodesPath),
		ConfigDir:           configDir,
		XkeenConfigPath:     getenv("XKEEN_CONFIG_PATH", "/opt/etc/xkeen/xkeen.json"),
		ActiveOutboundsPath: getenv("XKEEN_ACTIVE_OUTBOUNDS", filepath.Join(configDir, "04_outbounds.json")),
		PreviousDir:         getenv("XKEEN_APPLIANCE_IMPORT_PREVIOUS_DIR", "/opt/etc/xkeen-control/previous/appliance-import"),
		StateDir:            getenv("XKEEN_APPLIANCE_IMPORT_STATE_DIR", "/opt/etc/xkeen-control/state"),
		Activator: nodes.CommandActivator{
			XrayBinary:          getenv("XKEEN_XRAY_BINARY", "xray"),
			XrayAssetDir:        getenv("XKEEN_XRAY_ASSET_DIR", "/opt/etc/xray/dat"),
			XkeenBinary:         getenv("XKEEN_XKEEN_BINARY", "xkeen"),
			APIAddress:          getenv("XKEEN_XRAY_API_ADDR", xrayapi.DefaultAPIAddress),
			ActiveOutboundsPath: getenv("XKEEN_ACTIVE_OUTBOUNDS", filepath.Join(configDir, "04_outbounds.json")),
			RoutingPath:         filepath.Join(configDir, "05_routing.json"),
		},
		Coordinator:    coordinator,
		AuthorityLease: lease,
	})
}

func newNodeManager(coordinator interface {
	BeginApply(context.Context) (func(), error)
}, lease *authority.Lease) *nodes.Manager {
	registryPath := getenv("XKEEN_NODES_PATH", defaultNodesPath)
	configDir := getenv("XKEEN_XRAY_CONFIG_DIR", defaultXrayConfigDir)
	activeOutboundsPath := getenv("XKEEN_ACTIVE_OUTBOUNDS", filepath.Join(configDir, "04_outbounds.json"))
	return nodes.NewManager(nodes.Config{
		Store:      nodes.Store{Path: registryPath},
		LegacyPath: getenv("XKEEN_LEGACY_OUTBOUNDS", defaultLegacyPath),
		Transaction: nodes.Transaction{
			Store:               nodes.Store{Path: registryPath},
			ActiveOutboundsPath: activeOutboundsPath,
			ConfigDir:           configDir,
			PreviousDir:         getenv("XKEEN_NODE_PREVIOUS_DIR", defaultNodePreviousDir),
			Activator: nodes.CommandActivator{
				XrayBinary:          getenv("XKEEN_XRAY_BINARY", "xray"),
				XrayAssetDir:        getenv("XKEEN_XRAY_ASSET_DIR", "/opt/etc/xray/dat"),
				XkeenBinary:         getenv("XKEEN_XKEEN_BINARY", "xkeen"),
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
	manager := newNodeManager(nil, authority.NewLease())
	if len(args) == 0 {
		return errors.New("usage: xkeen-control nodes {validate|render --output PATH|reconcile-runtime|migrate-legacy}")
	}
	switch args[0] {
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
	case "migrate-legacy":
		if len(args) != 1 {
			return errors.New("usage: xkeen-control nodes migrate-legacy")
		}
		if err := manager.MigrateLegacy(context.Background()); err != nil {
			return errors.New("legacy node migration failed")
		}
		_, err := io.WriteString(os.Stdout, "legacy node migration applied\n")
		return err
	default:
		return errors.New("usage: xkeen-control nodes {validate|render --output PATH|reconcile-runtime|migrate-legacy}")
	}
}

func runApplianceCommand(args []string) error {
	usage := "usage: xkeen-control appliance {validate|adopt|verify|render --output DIR}"
	if len(args) == 0 {
		return errors.New(usage)
	}
	service := newApplianceService(authority.NewLease())
	switch args[0] {
	case "validate":
		if len(args) != 1 {
			return errors.New(usage)
		}
		if err := service.ValidateStored(); err != nil {
			return errors.New("appliance authority validation failed")
		}
		_, err := io.WriteString(os.Stdout, "appliance authority valid\n")
		return err
	case "adopt":
		if len(args) != 1 {
			return errors.New(usage)
		}
		if err := service.Adopt(context.Background()); err != nil {
			return errors.New("appliance authority adoption failed")
		}
		_, err := io.WriteString(os.Stdout, "appliance authority adopted\n")
		return err
	case "verify":
		if len(args) != 1 {
			return errors.New(usage)
		}
		if err := service.Verify(context.Background()); err != nil {
			return errors.New("appliance authority verification failed")
		}
		_, err := io.WriteString(os.Stdout, "appliance authority verified\n")
		return err
	case "render":
		if len(args) != 3 || args[1] != "--output" || args[2] == "" {
			return errors.New(usage)
		}
		if err := service.Render(args[2]); err != nil {
			return errors.New("appliance candidate render failed")
		}
		_, err := io.WriteString(os.Stdout, "appliance candidate rendered\n")
		return err
	default:
		return errors.New(usage)
	}
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
	value := getenv("XKEEN_CONTROL_LISTEN", defaultListenAddress)
	host, port, err := net.SplitHostPort(value)
	if err != nil || host == "" {
		return "", fmt.Errorf("listen address must be host:port")
	}
	parsedPort, err := strconv.Atoi(port)
	if err != nil || parsedPort < 1 || parsedPort > 65535 {
		return "", fmt.Errorf("listen port must be between 1 and 65535")
	}
	if strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || ip.IsUnspecified() || (!ip.IsLoopback() && !ip.IsPrivate()) {
		return "", fmt.Errorf("listen address must be loopback or an exact private LAN address")
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(parsedPort)), nil
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
