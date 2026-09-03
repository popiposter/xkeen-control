package components

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/popiposter/xkeen-control/internal/authority"
)

// ComponentMutationGate is the one process-wide admission gate for component
// activation, rollback and recovery. It intentionally exposes no operation or
// filesystem control surface.
type ComponentMutationGate struct {
	once sync.Once
	gate chan struct{}
}

func NewComponentMutationGate() *ComponentMutationGate {
	return &ComponentMutationGate{}
}

func (g *ComponentMutationGate) init() {
	if g == nil {
		return
	}
	g.once.Do(func() { g.gate = make(chan struct{}, 1) })
}

func (g *ComponentMutationGate) Acquire(ctx context.Context) (func(), error) {
	if g == nil {
		return func() {}, nil
	}
	g.init()
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case g.gate <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-g.gate }) }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ComponentMaintenance reference-counts the global maintenance block. Xray
// and geodata may each retain an unresolved journal, but only the first owner
// blocks normal authority/coordinator admission and only the last owner may
// release it. The owner key prevents one component from releasing another's
// maintenance state.
type ComponentMaintenance struct {
	coordinator XrayMaintenanceGate
	lease       *authority.Lease

	mu     sync.Mutex
	owners map[ComponentKind]int
}

func NewComponentMaintenance(coordinator any, lease *authority.Lease) *ComponentMaintenance {
	var maintenance XrayMaintenanceGate
	if value, ok := coordinator.(XrayMaintenanceGate); ok {
		maintenance = value
	}
	return &ComponentMaintenance{coordinator: maintenance, lease: lease, owners: make(map[ComponentKind]int)}
}

func (m *ComponentMaintenance) Enter(kind ComponentKind) {
	if m == nil {
		return
	}
	m.mu.Lock()
	wasEmpty := len(m.owners) == 0
	m.owners[kind]++
	m.mu.Unlock()
	if !wasEmpty {
		return
	}
	if m.lease != nil {
		m.lease.Block()
	}
	if m.coordinator != nil {
		m.coordinator.EnterMaintenance()
	}
}

func (m *ComponentMaintenance) Exit(kind ComponentKind) {
	if m == nil {
		return
	}
	m.mu.Lock()
	count := m.owners[kind]
	if count == 0 {
		m.mu.Unlock()
		return
	}
	if count == 1 {
		delete(m.owners, kind)
	} else {
		m.owners[kind] = count - 1
	}
	wasEmpty := len(m.owners) == 0
	m.mu.Unlock()
	if !wasEmpty {
		return
	}
	if m.coordinator != nil {
		m.coordinator.ExitMaintenance()
	}
	if m.lease != nil {
		m.lease.Unblock()
	}
}

func (m *ComponentMaintenance) HasOwner(kind ComponentKind) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.owners[kind] > 0
}

type componentJournalEnvelope struct {
	SchemaVersion int           `json:"schemaVersion"`
	Component     ComponentKind `json:"component"`
	Operation     string        `json:"operation"`
	Phase         string        `json:"phase"`
}

// ComponentRecoveryConfig is the startup arbiter's closed set of component
// journal and staging locations. Callers cannot supply a raw path through an
// HTTP, UI or CLI mutation request; production wires the fixed paths here.
type ComponentRecoveryConfig struct {
	JournalPath                string
	RestoreJournalPath         string
	XrayPreviousStagingPath    string
	XrayStagingDir             string
	GeodataPreviousStagingPath string
	GeodataStagingDir          string
}

type ComponentRecoveryState struct {
	Kind                  ComponentKind
	JournalPresent        bool
	XrayStagingPresent    bool
	GeodataStagingPresent bool
}

func (s ComponentRecoveryState) Pending() bool {
	return s.JournalPresent || s.XrayStagingPresent || s.GeodataStagingPresent
}

// InspectComponentRecovery validates the single shared journal and arbitrates
// pending component-owned staging before either component is allowed to start.
// It performs only bounded local reads.
func InspectComponentRecovery(config ComponentRecoveryConfig) (ComponentRecoveryState, error) {
	if config.JournalPath == "" || config.RestoreJournalPath == "" || config.XrayPreviousStagingPath == "" || config.XrayStagingDir == "" || config.GeodataPreviousStagingPath == "" || config.GeodataStagingDir == "" {
		return ComponentRecoveryState{}, errComponentRecoveryInvalid
	}
	envelope, journalPresent, err := readComponentJournalEnvelope(config.JournalPath)
	if err != nil {
		return ComponentRecoveryState{}, err
	}
	xrayStaging, err := componentStagingPresent(config.XrayPreviousStagingPath, config.XrayStagingDir)
	if err != nil {
		return ComponentRecoveryState{}, err
	}
	geodataStaging, err := componentStagingPresent(config.GeodataPreviousStagingPath, config.GeodataStagingDir)
	if err != nil {
		return ComponentRecoveryState{}, err
	}
	restorePresent, err := componentTransactionPresent(config.RestoreJournalPath)
	if err != nil {
		return ComponentRecoveryState{}, err
	}
	if restorePresent && (journalPresent || xrayStaging || geodataStaging) {
		return ComponentRecoveryState{}, ErrComponentRecoveryConflict
	}
	if xrayStaging && geodataStaging {
		return ComponentRecoveryState{}, ErrComponentRecoveryConflict
	}
	if journalPresent {
		if envelope.Component == KindXray && geodataStaging || envelope.Component == KindGeodata && xrayStaging {
			return ComponentRecoveryState{}, ErrComponentRecoveryConflict
		}
		return ComponentRecoveryState{Kind: envelope.Component, JournalPresent: true, XrayStagingPresent: xrayStaging, GeodataStagingPresent: geodataStaging}, nil
	}
	var kind ComponentKind
	if xrayStaging {
		kind = KindXray
	} else if geodataStaging {
		kind = KindGeodata
	}
	return ComponentRecoveryState{Kind: kind, XrayStagingPresent: xrayStaging, GeodataStagingPresent: geodataStaging}, nil
}

var (
	errComponentRecoveryInvalid  = errors.New("component recovery state is invalid")
	ErrComponentRecoveryConflict = errors.New("component recovery state conflicts")
)

func readComponentJournalEnvelope(path string) (componentJournalEnvelope, bool, error) {
	contents, err := readPrivateComponentFile(path, MaxComponentJournalBytes)
	if errors.Is(err, os.ErrNotExist) {
		return componentJournalEnvelope{}, false, nil
	}
	if err != nil {
		return componentJournalEnvelope{}, false, errComponentRecoveryInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	var object map[string]json.RawMessage
	if err := decoder.Decode(&object); err != nil {
		return componentJournalEnvelope{}, false, errComponentRecoveryInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return componentJournalEnvelope{}, false, errComponentRecoveryInvalid
	}
	allowed := map[string]struct{}{
		"schemaVersion": {}, "component": {}, "operation": {}, "phase": {}, "previous": {}, "candidate": {},
	}
	for key := range object {
		if _, ok := allowed[key]; !ok {
			return componentJournalEnvelope{}, false, errComponentRecoveryInvalid
		}
	}
	for _, key := range []string{"schemaVersion", "component", "operation", "phase", "previous", "candidate"} {
		if _, ok := object[key]; !ok {
			return componentJournalEnvelope{}, false, errComponentRecoveryInvalid
		}
	}
	var envelope componentJournalEnvelope
	if err := json.Unmarshal(contents, &envelope); err != nil || envelope.SchemaVersion != XrayTransactionSchemaVersion || (envelope.Component != KindXray && envelope.Component != KindGeodata) || (envelope.Operation != xrayOperationUpdate && envelope.Operation != xrayOperationRollback) {
		return componentJournalEnvelope{}, false, errComponentRecoveryInvalid
	}
	switch envelope.Component {
	case KindXray:
		if envelope.Phase != xrayPhasePrepared && envelope.Phase != xrayPhaseBinaryCommitted && envelope.Phase != xrayPhaseRuntimeVerified {
			return componentJournalEnvelope{}, false, errComponentRecoveryInvalid
		}
	case KindGeodata:
		if envelope.Phase != geodataPhasePrepared && envelope.Phase != geodataPhaseFilesCommitted && envelope.Phase != geodataPhaseRuntimeVerified {
			return componentJournalEnvelope{}, false, errComponentRecoveryInvalid
		}
	}
	decoder = json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if envelope.Component == KindXray {
		var journal xrayTransactionJournal
		if err := decoder.Decode(&journal); err != nil || decoder.Decode(&extra) != io.EOF || validateXrayJournal(journal) != nil {
			return componentJournalEnvelope{}, false, errComponentRecoveryInvalid
		}
	} else {
		var journal geodataTransactionJournal
		if err := decoder.Decode(&journal); err != nil || decoder.Decode(&extra) != io.EOF || validateGeodataJournal(journal) != nil {
			return componentJournalEnvelope{}, false, errComponentRecoveryInvalid
		}
	}
	return envelope, true, nil
}

func componentStagingPresent(previousPath, stagingDir string) (bool, error) {
	previous, err := componentPathPresent(previousPath)
	if err != nil {
		return false, err
	}
	root, err := componentStagingRootPresent(stagingDir)
	if err != nil {
		return false, err
	}
	return previous || root, nil
}

func componentPathPresent(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return false, errComponentRecoveryInvalid
	}
	return true, nil
}

func componentStagingRootPresent(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, errComponentRecoveryInvalid
	}
	if err := checkPrivateComponentDirectory(path); err != nil {
		return false, errComponentRecoveryInvalid
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, errComponentRecoveryInvalid
	}
	return len(entries) > 0, nil
}

// componentTransactionPresent remains a small local existence helper used by
// the restore arbiter and legacy Phase C paths. The shared inspector performs
// the stricter journal classification above.
func componentTransactionPresent(path string) (bool, error) {
	if path == "" {
		return false, errComponentRecoveryInvalid
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

func componentJournalKind(path string) (ComponentKind, bool, error) {
	envelope, present, err := readComponentJournalEnvelope(path)
	if err != nil {
		return "", false, err
	}
	if !present {
		return "", false, nil
	}
	return envelope.Component, true, nil
}

func componentRecoveryPath(path, suffix string) string {
	return filepath.Clean(path) + suffix
}
