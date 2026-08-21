package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/popiposter/xkeen-control/internal/redact"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

type Activator interface {
	ValidateCandidate(context.Context, string) error
	Restart(context.Context) error
	WaitReady(context.Context) error
	VerifyOutboundTags(context.Context, []string) error
}

const (
	DefaultCandidateValidationTimeout = 45 * time.Second
	DefaultActivationTimeout          = 2 * time.Minute
	DefaultRollbackTimeout            = 2 * time.Minute
	// DefaultTransactionTimeout includes all three phase ceilings plus a small
	// allowance for bounded local rendering, snapshots, and atomic writes.
	DefaultTransactionTimeout = 5 * time.Minute
)

var ErrRollbackFailed = errors.New("node activation failed; rollback failed")

type TransactionBudget struct {
	CandidateValidation time.Duration
	Activation          time.Duration
	Rollback            time.Duration
	Total               time.Duration
}

func (b TransactionBudget) normalized() TransactionBudget {
	if b.CandidateValidation <= 0 {
		b.CandidateValidation = DefaultCandidateValidationTimeout
	}
	if b.Activation <= 0 {
		b.Activation = DefaultActivationTimeout
	}
	if b.Rollback <= 0 {
		b.Rollback = DefaultRollbackTimeout
	}
	if b.Total <= 0 {
		b.Total = DefaultTransactionTimeout
	}
	return b
}

func (t Transaction) totalTimeout() time.Duration { return t.Budget.normalized().Total }

type RollbackError struct {
	Cause    error
	Recovery error
}

func (e *RollbackError) Error() string { return ErrRollbackFailed.Error() }
func (e *RollbackError) Unwrap() error { return ErrRollbackFailed }

type Transaction struct {
	Store               Store
	ActiveOutboundsPath string
	ConfigDir           string
	PreviousDir         string
	Activator           Activator
	Budget              TransactionBudget
}

func (t Transaction) Apply(ctx context.Context, registry Registry) (err error) {
	budget := t.Budget.normalized()
	transactionDeadline := time.Now().Add(budget.Total)
	if callerDeadline, ok := ctx.Deadline(); ok && callerDeadline.Before(transactionDeadline) {
		transactionDeadline = callerDeadline
	}
	transactionContext, cancelTransaction := context.WithDeadline(ctx, transactionDeadline)
	defer cancelTransaction()

	if err := registry.Validate(); err != nil {
		return err
	}
	rendered, err := Render(registry)
	if err != nil {
		return err
	}
	candidateDir, err := os.MkdirTemp("", "xkeen-node-candidate-")
	if err != nil {
		return errors.New("unable to create candidate directory")
	}
	defer os.RemoveAll(candidateDir)
	if t.ConfigDir != "" {
		if err := copyTree(t.ConfigDir, candidateDir); err != nil {
			return errors.New("unable to prepare candidate Xray configuration")
		}
	}
	if err := atomicWrite(filepath.Join(candidateDir, "04_outbounds.json"), rendered, 0o600); err != nil {
		return err
	}
	if t.Activator != nil {
		validationContext, cancelValidation := context.WithTimeout(transactionContext, budget.CandidateValidation)
		validationErr := t.Activator.ValidateCandidate(validationContext, candidateDir)
		cancelValidation()
		if validationErr != nil {
			return errors.New("candidate Xray validation failed")
		}
	}

	previousRegistry, previousRegistryExists, err := loadOptionalRegistry(t.Store)
	if err != nil {
		return err
	}
	previousOutbounds, previousOutboundsExists, err := readOptional(t.ActiveOutboundsPath, MaxLegacyDocument)
	if err != nil {
		return err
	}
	if err := t.savePrevious(previousRegistry, previousRegistryExists, previousOutbounds, previousOutboundsExists); err != nil {
		return err
	}
	mutated := false
	defer func() {
		if err == nil || !mutated {
			return
		}
		rollbackDeadline := time.Now().Add(budget.Rollback)
		if transactionDeadline.Before(rollbackDeadline) {
			rollbackDeadline = transactionDeadline
		}
		rollbackContext, cancelRollback := context.WithDeadline(context.Background(), rollbackDeadline)
		rollbackErr := t.rollback(rollbackContext, previousRegistry, previousRegistryExists, previousOutbounds, previousOutboundsExists)
		cancelRollback()
		if rollbackErr != nil {
			err = &RollbackError{Cause: err, Recovery: rollbackErr}
			return
		}
		err = errors.New(err.Error() + "; previous generation restored")
	}()

	if err := t.Store.Save(registry); err != nil {
		return err
	}
	mutated = true
	if err := atomicWrite(t.ActiveOutboundsPath, rendered, 0o600); err != nil {
		return err
	}
	if t.Activator != nil {
		activationContext, cancelActivation := context.WithTimeout(transactionContext, budget.Activation)
		activationErr := t.activate(activationContext, registry)
		cancelActivation()
		if activationErr != nil {
			return activationErr
		}
	}
	return nil
}

func (t Transaction) activate(ctx context.Context, registry Registry) error {
	if err := t.Activator.Restart(ctx); err != nil {
		return errors.New("Xray restart failed")
	}
	if err := t.Activator.WaitReady(ctx); err != nil {
		return errors.New("Xray readiness failed")
	}
	if err := t.Activator.VerifyOutboundTags(ctx, enabledTags(registry)); err != nil {
		return errors.New("Xray outbound inventory failed")
	}
	return nil
}

func (t Transaction) rollback(ctx context.Context, registry Registry, registryExists bool, outbounds []byte, outboundsExists bool) error {
	var failures []error
	if err := t.restore(registry, registryExists, outbounds, outboundsExists); err != nil {
		failures = append(failures, errors.New("restore failed"))
	}
	if err := t.verifyRestored(registry, registryExists, outbounds, outboundsExists); err != nil {
		failures = append(failures, errors.New("restore verification failed"))
	}
	if t.Activator != nil {
		if err := t.Activator.Restart(ctx); err != nil {
			failures = append(failures, errors.New("rollback restart failed"))
		}
		if err := t.Activator.WaitReady(ctx); err != nil {
			failures = append(failures, errors.New("rollback readiness failed"))
		}
		expected := enabledTags(registry)
		if registryExists && len(expected) > 0 {
			if err := t.Activator.VerifyOutboundTags(ctx, expected); err != nil {
				failures = append(failures, errors.New("rollback inventory verification failed"))
			}
		}
	}
	return errors.Join(failures...)
}

func (t Transaction) savePrevious(registry Registry, registryExists bool, outbounds []byte, outboundsExists bool) error {
	if t.PreviousDir == "" {
		return nil
	}
	if err := ensurePrivateDir(t.PreviousDir); err != nil {
		return err
	}
	// At most one previous generation is retained and it is replaced only by
	// this explicit activation transaction.
	for _, name := range []string{"nodes.json", "04_outbounds.json", ".registry-absent", ".outbounds-absent"} {
		_ = os.Remove(filepath.Join(t.PreviousDir, name))
	}
	if registryExists {
		if err := (Store{Path: filepath.Join(t.PreviousDir, "nodes.json")}).Save(registry); err != nil {
			return err
		}
	} else if err := atomicWrite(filepath.Join(t.PreviousDir, ".registry-absent"), []byte("1\n"), 0o600); err != nil {
		return err
	}
	if outboundsExists {
		if err := atomicWrite(filepath.Join(t.PreviousDir, "04_outbounds.json"), outbounds, 0o600); err != nil {
			return err
		}
	} else if err := atomicWrite(filepath.Join(t.PreviousDir, ".outbounds-absent"), []byte("1\n"), 0o600); err != nil {
		return err
	}
	return nil
}

func (t Transaction) restore(registry Registry, registryExists bool, outbounds []byte, outboundsExists bool) error {
	var failures []error
	if registryExists {
		if err := t.Store.Save(registry); err != nil {
			failures = append(failures, err)
		}
	} else {
		if err := os.Remove(t.Store.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, err)
		}
	}
	if outboundsExists {
		if err := atomicWrite(t.ActiveOutboundsPath, outbounds, 0o600); err != nil {
			failures = append(failures, err)
		}
	} else {
		if err := os.Remove(t.ActiveOutboundsPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (t Transaction) verifyRestored(registry Registry, registryExists bool, outbounds []byte, outboundsExists bool) error {
	loadedRegistry, loadedRegistryExists, err := loadOptionalRegistry(t.Store)
	if err != nil || loadedRegistryExists != registryExists || (registryExists && !reflect.DeepEqual(loadedRegistry, registry)) {
		return errors.New("restored registry does not match snapshot")
	}
	loadedOutbounds, loadedOutboundsExists, err := readOptional(t.ActiveOutboundsPath, MaxLegacyDocument)
	if err != nil || loadedOutboundsExists != outboundsExists || (outboundsExists && !bytes.Equal(loadedOutbounds, outbounds)) {
		return errors.New("restored outbound artifact does not match snapshot")
	}
	return nil
}

func loadOptionalRegistry(store Store) (Registry, bool, error) {
	registry, err := store.Load()
	if err == nil {
		return registry, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return Registry{}, false, nil
	}
	return Registry{}, false, errors.New("existing registry is invalid")
}

func readOptional(path string, max int) ([]byte, bool, error) {
	if path == "" {
		return nil, false, nil
	}
	contents, err := ReadBoundedFile(path, max)
	if err == nil {
		return contents, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return nil, false, errors.New("existing outbound artifact is unreadable")
}

func enabledTags(registry Registry) []string {
	result := make([]string, 0, len(registry.Nodes))
	for _, node := range registry.SortedNodes() {
		if node.Enabled {
			result = append(result, node.OutboundTag)
		}
	}
	return result
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlinks are not allowed in candidate config")
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !info.Mode().IsRegular() || info.Size() > 8<<20 {
			return errors.New("candidate config file is unsupported")
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return atomicWrite(target, contents, info.Mode().Perm())
	})
}

type CommandActivator struct {
	XrayBinary            string
	XrayAssetDir          string
	XkeenBinary           string
	APIAddress            string
	ActiveOutboundsPath   string
	RoutingPath           string
	RestartTimeout        time.Duration
	RestartAttemptTimeout time.Duration
	ReadyTimeout          time.Duration
	RuntimeVerifier       func(context.Context, string, string, []string) error
}

func (a CommandActivator) ValidateCandidate(ctx context.Context, configDir string) error {
	if a.XrayBinary == "" {
		a.XrayBinary = "xray"
	}
	command := exec.CommandContext(ctx, a.XrayBinary, "run", "-test", "-confdir", configDir)
	command.Dir = configDir
	command.Env = xrayEnvironment(a.XrayAssetDir)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return errors.New("candidate Xray validation failed")
	}
	return nil
}

func xrayEnvironment(assetDir string) []string {
	environment := os.Environ()
	if assetDir == "" {
		return environment
	}
	const assetPrefix = "XRAY_LOCATION_ASSET="
	for index, entry := range environment {
		if strings.HasPrefix(entry, assetPrefix) {
			environment[index] = assetPrefix + assetDir
			return environment
		}
	}
	return append(environment, assetPrefix+assetDir)
}

func (a CommandActivator) Restart(ctx context.Context) error {
	if a.XkeenBinary == "" {
		a.XkeenBinary = "xkeen"
	}
	timeout := a.RestartTimeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	restartContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	restartAttemptTimeout := a.RestartAttemptTimeout
	if restartAttemptTimeout <= 0 {
		restartAttemptTimeout = 20 * time.Second
	}
	if restartAttemptTimeout >= timeout {
		restartAttemptTimeout = timeout / 2
	}
	restartAttemptContext, cancelRestartAttempt := context.WithTimeout(restartContext, restartAttemptTimeout)
	restartErr := a.runXkeenLifecycle(restartAttemptContext, "-restart")
	cancelRestartAttempt()
	if restartErr == nil {
		return nil
	}
	// XKeen can stop Xray and then return a failed -restart. Match the
	// repository lifecycle contract: if time remains, recover with -start so
	// activation and rollback can still prove readiness on the selected files.
	if restartContext.Err() == nil && a.runXkeenLifecycle(restartContext, "-start") == nil {
		return nil
	}
	return errors.New("Xray restart failed")
}

func (a CommandActivator) runXkeenLifecycle(ctx context.Context, action string) error {
	previousPIDs := xrayPIDSet(ctx)
	command := exec.Command(a.XkeenBinary, action)
	command.Env = xkeenForegroundEnvironment()
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	configureCommandProcessGroup(command)
	if err := command.Start(); err != nil {
		return errors.New("Xray restart failed")
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if err != nil {
				return errors.New("Xray restart failed")
			}
			return nil
		case <-ticker.C:
			if a.newXrayRuntimeStarted(ctx, previousPIDs) {
				// XKeen can leave its launcher attached after the replacement Xray
				// is already serving. Stop only that launcher; the separate
				// WaitReady and inventory phases still prove the new daemon.
				_ = command.Process.Kill()
				drainCommand(done)
				return nil
			}
		case <-ctx.Done():
			killCommandProcessGroup(command)
			drainCommand(done)
			return errors.New("Xray restart failed")
		}
	}
}

func (a CommandActivator) newXrayRuntimeStarted(ctx context.Context, previous map[string]struct{}) bool {
	current := xrayPIDSet(ctx)
	changed := false
	for pid := range current {
		if _, existed := previous[pid]; !existed {
			changed = true
			break
		}
	}
	if !changed {
		return false
	}
	address := a.APIAddress
	if address == "" {
		address = "127.0.0.1:10085"
	}
	connection, err := (&net.Dialer{Timeout: 500 * time.Millisecond}).DialContext(ctx, "tcp", address)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func drainCommand(done <-chan error) {
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

func xrayPIDSet(ctx context.Context) map[string]struct{} {
	result := make(map[string]struct{})
	command := exec.CommandContext(ctx, "pidof", "xray")
	contents, err := command.Output()
	if err != nil || len(contents) > 256 {
		return result
	}
	for _, pid := range strings.Fields(string(contents)) {
		if pid != "" {
			result[pid] = struct{}{}
		}
	}
	return result
}

func xkeenForegroundEnvironment() []string {
	environment := os.Environ()
	const foreground = "XKEEN_FOREGROUND=1"
	for index, entry := range environment {
		if strings.HasPrefix(entry, "XKEEN_FOREGROUND=") {
			environment[index] = foreground
			return environment
		}
	}
	return append(environment, foreground)
}

func (a CommandActivator) WaitReady(ctx context.Context) error {
	if a.XrayBinary == "" {
		a.XrayBinary = "xray"
	}
	if a.APIAddress == "" {
		a.APIAddress = "127.0.0.1:10085"
	}
	timeout := a.ReadyTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	readyContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		command := exec.CommandContext(readyContext, a.XrayBinary, "api", "lsrules", "-s", a.APIAddress)
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		if command.Run() == nil {
			return nil
		}
		select {
		case <-readyContext.Done():
			return errors.New("Xray API did not become ready")
		case <-ticker.C:
		}
	}
}

func (a CommandActivator) VerifyOutboundTags(ctx context.Context, expected []string) error {
	if len(expected) == 0 || a.ActiveOutboundsPath == "" {
		return errors.New("active outbound artifact unavailable")
	}
	contents, err := ReadBoundedFile(a.ActiveOutboundsPath, MaxLegacyDocument)
	if err != nil {
		return errors.New("active outbound artifact unavailable")
	}
	var document struct {
		Outbounds []struct {
			Tag string `json:"tag"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(contents, &document); err != nil {
		return errors.New("active outbound artifact is invalid")
	}
	tags := make(map[string]struct{}, len(document.Outbounds))
	for _, outbound := range document.Outbounds {
		tags[outbound.Tag] = struct{}{}
	}
	for _, tag := range expected {
		if !redact.IsUnifiedOutboundTag(tag) {
			return errors.New("expected outbound tag is invalid")
		}
		if _, exists := tags[tag]; !exists {
			return errors.New("expected outbound missing from active artifact")
		}
	}
	routingPath := a.RoutingPath
	if routingPath == "" {
		routingPath = filepath.Join(filepath.Dir(a.ActiveOutboundsPath), "05_routing.json")
	}
	if err := verifyBalancerSelector(routingPath, "bal-proxy", expected); err != nil {
		return err
	}
	verifyRuntime := a.RuntimeVerifier
	if verifyRuntime == nil {
		verifyRuntime = verifyXrayBalancerRuntime
	}
	if err := verifyRuntime(ctx, a.APIAddress, "bal-proxy", expected); err != nil {
		return errors.New("active Xray balancer does not expose expected outbounds")
	}
	return nil
}

func verifyBalancerSelector(path, balancerTag string, expected []string) error {
	contents, err := ReadBoundedFile(path, MaxLegacyDocument)
	if err != nil {
		return errors.New("active routing policy unavailable")
	}
	var document struct {
		Routing struct {
			Balancers []struct {
				Tag      string   `json:"tag"`
				Selector []string `json:"selector"`
				Strategy struct {
					Type string `json:"type"`
				} `json:"strategy"`
			} `json:"balancers"`
		} `json:"routing"`
	}
	if json.Unmarshal(contents, &document) != nil {
		return errors.New("active routing policy is invalid")
	}
	var selectors []string
	found := 0
	for _, balancer := range document.Routing.Balancers {
		if balancer.Tag != balancerTag {
			continue
		}
		found++
		if !strings.EqualFold(balancer.Strategy.Type, "leastPing") || len(balancer.Selector) == 0 || len(balancer.Selector) > 16 {
			return errors.New("balancer selector contract is invalid")
		}
		selectors = append(selectors, balancer.Selector...)
	}
	if found != 1 {
		return errors.New("balancer selector contract is unavailable")
	}
	for _, tag := range expected {
		matched := false
		for _, selector := range selectors {
			if selector != "" && strings.HasPrefix(tag, selector) {
				matched = true
				break
			}
		}
		if !matched {
			return errors.New("expected outbound is outside balancer selector")
		}
	}
	return nil
}

func verifyXrayBalancerRuntime(ctx context.Context, address, balancerTag string, expected []string) error {
	verifyContext, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		runtime, err := xrayapi.ReadBalancerRuntime(verifyContext, address, balancerTag, 3*time.Second)
		if err == nil && validBalancerRuntime(runtime, expected) {
			return nil
		}
		select {
		case <-verifyContext.Done():
			return errors.New("Xray balancer runtime unavailable")
		case <-ticker.C:
		}
	}
}

func validBalancerRuntime(runtime xrayapi.BalancerRuntime, expected []string) bool {
	if len(runtime.PrincipleTargets) > 16 {
		return false
	}
	allowed := make(map[string]struct{}, len(expected))
	for _, tag := range expected {
		allowed[tag] = struct{}{}
	}
	for _, tag := range runtime.PrincipleTargets {
		if _, ok := allowed[tag]; !ok {
			return false
		}
	}
	if runtime.Override != "" {
		if _, ok := allowed[runtime.Override]; !ok {
			return false
		}
	}
	return true
}
