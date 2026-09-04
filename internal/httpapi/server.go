package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/backup"
	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/components"
	"github.com/popiposter/xkeen-control/internal/nodes"
	"github.com/popiposter/xkeen-control/internal/restore"
	controlruntime "github.com/popiposter/xkeen-control/internal/runtime"
	panelupdate "github.com/popiposter/xkeen-control/internal/update"
)

const (
	maxLoginBody             = 16 << 10
	maxMutationBody          = 384 << 10
	maxComponentCheckBody    = 4 << 10
	maxComponentMutationBody = 4 << 10
	maxJSONResponse          = 512 << 10
	csrfRequiredPath         = "/api/v1/session/logout"
)

type BackupService interface {
	Export(context.Context) ([]byte, error)
	ExportSecret(context.Context, string) ([]byte, error)
}

type RestoreService interface {
	PreviewBundle(context.Context, string, []byte, string, restore.Mode) (restore.Preview, error)
	Apply(context.Context, string, string) (restore.ApplyResult, error)
	Cancel(string, string)
	Invalidate(string)
	InvalidateAll()
}

type ComponentMutationService interface {
	Preview(context.Context, string, components.MutationRequest) (components.MutationPreview, error)
	Apply(context.Context, string, string) (components.MutationResult, error)
	Rollback(context.Context, string, string) (components.MutationResult, error)
	Cancel(string, string)
	Invalidate(string)
	InvalidateAll()
}

type Server struct {
	collector *controlruntime.Collector
	auth      *auth.Manager
	nodes     *nodes.Manager
	assets    http.Handler
	start     time.Time
	benchmark interface {
		TriggerBenchmark() error
	}
	selection interface {
		SetManualOverride(context.Context, string) error
	}
	components         components.ReadOnlyService
	componentChecks    components.CheckService
	componentMutations ComponentMutationService
	updates            panelupdate.Service
	backup             BackupService
	restore            RestoreService
	restorePreviewGate chan struct{}
}

type Config struct {
	Collector *controlruntime.Collector
	Auth      *auth.Manager
	Nodes     *nodes.Manager
	Assets    http.Handler
	StartedAt time.Time
	Benchmark interface {
		TriggerBenchmark() error
	}
	Selection interface {
		SetManualOverride(context.Context, string) error
	}
	Components         components.ReadOnlyService
	ComponentChecks    components.CheckService
	ComponentMutations ComponentMutationService
	Updates            panelupdate.Service
	Backup             BackupService
	Restore            RestoreService
}

func New(config Config) *Server {
	if config.StartedAt.IsZero() {
		config.StartedAt = time.Now().UTC()
	}
	return &Server{collector: config.Collector, auth: config.Auth, nodes: config.Nodes, assets: config.Assets, start: config.StartedAt, benchmark: config.Benchmark, selection: config.Selection, components: config.Components, componentChecks: config.ComponentChecks, componentMutations: config.ComponentMutations, updates: config.Updates, backup: config.Backup, restore: config.Restore, restorePreviewGate: make(chan struct{}, 1)}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w, strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz")

	switch r.URL.Path {
	case "/healthz":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
		return
	case "/api/v1/session/login", "/api/v1/session/logout", "/api/v1/session",
		"/api/v1/status", "/api/v1/nodes", "/api/v1/performance", "/api/v1/config-summary", "/api/v1/components", "/api/v1/components/check",
		"/api/v1/components/preview", "/api/v1/components/apply", "/api/v1/components/rollback", "/api/v1/components/cancel",
		"/api/v1/update", "/api/v1/update/check", "/api/v1/update/policy", "/api/v1/update/apply", "/api/v1/update/rollback",
		"/api/v1/session/password",
		"/api/v1/benchmark/run",
		"/api/v1/backup/export", "/api/v1/backup/export-secret",
		"/api/v1/backup/import/preview", "/api/v1/backup/import/apply", "/api/v1/backup/import/cancel",
		"/api/v1/nodes/import/preview", "/api/v1/nodes/replace/preview",
		"/api/v1/subscriptions/refresh/preview", "/api/v1/subscriptions/state/preview", "/api/v1/subscriptions/remove/preview",
		"/api/v1/selection/override",
		"/api/v1/node-changes/apply", "/api/v1/node-changes/cancel":
		if !s.originAllowed(r) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		s.handleAPI(w, r)
		return
	}
	if isNodeMutationPath(r.URL.Path) {
		if !s.originAllowed(r) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		s.handleAPI(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if s.assets == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	s.assets.ServeHTTP(w, r)
}

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v1/session/login":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.login(w, r)
	case "/api/v1/session/logout":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.logout(w, r)
	case "/api/v1/session/password":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.replacePassword(w, r)
	case "/api/v1/session":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.session(w, r)
	case "/api/v1/status":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.readOnly(w, r, func(view controlruntime.View) any { return view.Status })
	case "/api/v1/nodes":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.readNodes(w, r)
	case "/api/v1/performance":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.readOnly(w, r, func(view controlruntime.View) any { return view.Performance })
	case "/api/v1/config-summary":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.readOnly(w, r, func(view controlruntime.View) any { return view.ConfigSummary })
	case "/api/v1/components":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.readComponents(w, r)
	case "/api/v1/components/check":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.checkComponents(w, r)
	case "/api/v1/components/preview":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.previewComponentMutation(w, r)
	case "/api/v1/components/apply":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.applyComponentMutation(w, r)
	case "/api/v1/components/rollback":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.rollbackComponentMutation(w, r)
	case "/api/v1/components/cancel":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.cancelComponentMutation(w, r)
	case "/api/v1/benchmark/run":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.runBenchmark(w, r)
	case "/api/v1/backup/export":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.exportBackup(w, r)
	case "/api/v1/backup/export-secret":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.exportSecretBackup(w, r)
	case "/api/v1/backup/import/preview":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.previewRestore(w, r)
	case "/api/v1/backup/import/apply":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.applyRestore(w, r)
	case "/api/v1/backup/import/cancel":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.cancelRestore(w, r)
	case "/api/v1/nodes/import/preview":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.previewImport(w, r)
	case "/api/v1/nodes/replace/preview":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.previewReplace(w, r)
	case "/api/v1/subscriptions/refresh/preview":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.previewSubscription(w, r)
	case "/api/v1/subscriptions/state/preview":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.previewSubscriptionState(w, r)
	case "/api/v1/subscriptions/remove/preview":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.previewSubscriptionRemove(w, r)
	case "/api/v1/selection/override":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.setManualOverride(w, r)
	case "/api/v1/node-changes/apply":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.applyNodeChange(w, r)
	case "/api/v1/node-changes/cancel":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.cancelNodeChange(w, r)
	case "/api/v1/update":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.updateStatus(w, r)
	case "/api/v1/update/check":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.updateCheck(w, r)
	case "/api/v1/update/policy":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.updatePolicy(w, r)
	case "/api/v1/update/apply":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.updateApply(w, r)
	case "/api/v1/update/rollback":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		s.updateRollback(w, r)
	default:
		if isNodeMutationPath(r.URL.Path) {
			s.handleDynamicNodeMutation(w, r)
			return
		}
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) runBenchmark(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if !auth.ValidateCSRF(r, session) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if s.benchmark == nil {
		writeError(w, http.StatusServiceUnavailable, "benchmark unavailable")
		return
	}
	if err := s.benchmark.TriggerBenchmark(); err != nil {
		if errors.Is(err, c1.ErrBenchmarkBusy) {
			writeJSON(w, http.StatusConflict, struct {
				Accepted bool   `json:"accepted"`
				State    string `json:"state"`
			}{Accepted: false, State: "busy"})
			return
		}
		writeError(w, http.StatusServiceUnavailable, "benchmark unavailable")
		return
	}
	writeJSON(w, http.StatusAccepted, struct {
		Accepted bool   `json:"accepted"`
		State    string `json:"state"`
	}{Accepted: true, State: "accepted"})
}

func (s *Server) exportBackup(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSession(w, r); !ok {
		return
	}
	if s.backup == nil {
		writeError(w, http.StatusServiceUnavailable, "backup unavailable")
		return
	}
	contents, err := s.backup.Export(r.Context())
	if err != nil {
		writeBackupError(w, err)
		return
	}
	writeBackupDownload(w, contents, backup.SafeFilename, backup.BackupMediaType)
}

func (s *Server) exportSecretBackup(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if !auth.ValidateCSRF(r, session) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var request struct {
		CurrentPassword string `json:"currentPassword"`
		Passphrase      string `json:"passphrase"`
	}
	if !s.decodeSecretBackupRequest(w, r, &request) {
		return
	}
	if request.CurrentPassword == "" {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := backup.ValidatePassphrase(request.Passphrase); err != nil {
		writeError(w, http.StatusBadRequest, "passphrase is outside the allowed bounds")
		return
	}
	if err := s.auth.Reauthenticate(remoteIP(r), request.CurrentPassword); err != nil {
		s.writeReauthenticationError(w, err)
		return
	}
	if s.backup == nil {
		writeError(w, http.StatusServiceUnavailable, "backup unavailable")
		return
	}
	contents, err := s.backup.ExportSecret(r.Context(), request.Passphrase)
	if err != nil {
		writeBackupError(w, err)
		return
	}
	defer clearBytes(contents)
	// Password replacement and logout invalidate all sessions in RAM. The
	// encrypted bytes are not committed to the response until this check wins.
	current, valid := s.auth.SessionFromRequest(r)
	if !valid || current.CSRFToken != session.CSRFToken {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeBackupDownload(w, contents, backup.SecretFilename, backup.EncryptedBackupMediaType)
}

func (s *Server) writeReauthenticationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrLocked):
		writeError(w, http.StatusTooManyRequests, "temporarily unavailable")
	case errors.Is(err, auth.ErrNotConfigured):
		writeError(w, http.StatusServiceUnavailable, "authentication unavailable")
	default:
		writeError(w, http.StatusUnauthorized, "reauthentication failed")
	}
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusServiceUnavailable, "authentication unavailable")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxLoginBody)
	defer r.Body.Close()
	var request struct {
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	session, token, err := s.auth.Login(remoteIP(r), request.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrNotConfigured):
			writeError(w, http.StatusServiceUnavailable, "authentication unavailable")
		case errors.Is(err, auth.ErrLocked):
			writeError(w, http.StatusTooManyRequests, "temporarily unavailable")
		default:
			writeError(w, http.StatusUnauthorized, "unauthorized")
		}
		return
	}
	s.auth.SetSessionCookie(w, token, session.ExpiresAt)
	writeJSON(w, http.StatusOK, struct {
		Authenticated bool      `json:"authenticated"`
		CSRFToken     string    `json:"csrfToken"`
		ExpiresAt     time.Time `json:"expiresAt"`
	}{Authenticated: true, CSRFToken: session.CSRFToken, ExpiresAt: session.ExpiresAt})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if r.URL.Path == csrfRequiredPath && !auth.ValidateCSRF(r, session) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	// Remove the auth session before purging restore state. Preview performs a
	// final same-session check after C1 returns; this ordering makes a preview
	// admitted before logout fail that check even if it completes while the
	// synchronous restore purge is still running.
	s.auth.Logout(r)
	if s.restore != nil {
		s.restore.Invalidate(session.CSRFToken)
	}
	if s.nodes != nil {
		s.nodes.Invalidate(session.CSRFToken)
	}
	if s.componentMutations != nil {
		s.componentMutations.Invalidate(session.CSRFToken)
	}
	s.auth.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, struct {
		Authenticated bool `json:"authenticated"`
	}{Authenticated: false})
}

func (s *Server) replacePassword(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if !auth.ValidateCSRF(r, session) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var request struct {
		NewPassword string `json:"newPassword"`
	}
	if !s.decodeMutation(w, r, &request) {
		return
	}
	if err := s.auth.ReplacePassword([]byte(request.NewPassword)); err != nil {
		if errors.Is(err, auth.ErrInvalidPassword) {
			writeError(w, http.StatusBadRequest, "password is outside the allowed bounds")
			return
		}
		writeError(w, http.StatusInternalServerError, "password replacement failed")
		return
	}
	if s.restore != nil {
		s.restore.InvalidateAll()
	}
	if s.componentMutations != nil {
		s.componentMutations.InvalidateAll()
	}
	s.auth.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, struct {
		Authenticated bool   `json:"authenticated"`
		State         string `json:"state"`
	}{Authenticated: false, State: "reauthentication-required"})
}

func (s *Server) updateStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSession(w, r); !ok {
		return
	}
	if s.updates == nil {
		writeError(w, http.StatusServiceUnavailable, "update service unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s.updates.Status(r.Context()))
}

func (s *Server) updateCheck(w http.ResponseWriter, r *http.Request) {
	if s.updates == nil {
		writeError(w, http.StatusServiceUnavailable, "update service unavailable")
		return
	}
	session, ok := s.requireSession(w, r)
	if !ok || !auth.ValidateCSRF(r, session) {
		if ok {
			writeError(w, http.StatusForbidden, "forbidden")
		}
		return
	}
	var request struct {
		Channel string `json:"channel"`
		Version string `json:"version"`
	}
	if !s.decodeMutation(w, r, &request) {
		return
	}
	status, err := s.updates.Check(r.Context(), request.Channel, request.Version)
	if err != nil {
		writeError(w, http.StatusBadGateway, "release check failed")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) updatePolicy(w http.ResponseWriter, r *http.Request) {
	if s.updates == nil {
		writeError(w, http.StatusServiceUnavailable, "update service unavailable")
		return
	}
	session, ok := s.requireSession(w, r)
	if !ok || !auth.ValidateCSRF(r, session) {
		if ok {
			writeError(w, http.StatusForbidden, "forbidden")
		}
		return
	}
	var policy panelupdate.Policy
	if !s.decodeMutation(w, r, &policy) {
		return
	}
	status, err := s.updates.SetPolicy(policy)
	if err != nil {
		writeError(w, http.StatusBadRequest, "update policy rejected")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) updateApply(w http.ResponseWriter, r *http.Request) {
	if s.updates == nil {
		writeError(w, http.StatusServiceUnavailable, "update service unavailable")
		return
	}
	session, ok := s.requireSession(w, r)
	if !ok || !auth.ValidateCSRF(r, session) {
		if ok {
			writeError(w, http.StatusForbidden, "forbidden")
		}
		return
	}
	var request struct {
		Channel string `json:"channel"`
		Version string `json:"version"`
	}
	if !s.decodeMutation(w, r, &request) {
		return
	}
	// The external helper must outlive the HTTP handler: it stops/replaces the
	// running panel after this accepted response has been written.
	go func(channel, version string) {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
		defer cancel()
		_ = s.updates.Apply(ctx, channel, version)
	}(request.Channel, request.Version)
	writeJSON(w, http.StatusAccepted, struct {
		Accepted bool `json:"accepted"`
	}{Accepted: true})
}

func (s *Server) updateRollback(w http.ResponseWriter, r *http.Request) {
	if s.updates == nil {
		writeError(w, http.StatusServiceUnavailable, "update service unavailable")
		return
	}
	session, ok := s.requireSession(w, r)
	if !ok || !auth.ValidateCSRF(r, session) {
		if ok {
			writeError(w, http.StatusForbidden, "forbidden")
		}
		return
	}
	if err := s.updates.Rollback(r.Context()); err != nil {
		writeError(w, http.StatusConflict, "panel rollback rejected")
		return
	}
	writeJSON(w, http.StatusAccepted, struct {
		Accepted bool `json:"accepted"`
	}{Accepted: true})
}

func (s *Server) readNodes(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSession(w, r); !ok {
		return
	}
	if s.collector == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime unavailable")
		return
	}
	view := s.collector.Snapshot(r.Context())
	if s.nodes == nil {
		writeJSON(w, http.StatusOK, struct {
			Nodes []controlruntime.Node `json:"nodes"`
			Total int                   `json:"total"`
		}{Nodes: view.Nodes, Total: len(view.Nodes)})
		return
	}
	registryNodes, err := s.nodes.List()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "node registry unavailable")
		return
	}
	registrySubscriptions, err := s.nodes.ListSubscriptions()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "node registry unavailable")
		return
	}
	runtimeByTag := make(map[string]controlruntime.Node, len(view.Nodes))
	for _, node := range view.Nodes {
		runtimeByTag[node.Tag] = node
	}
	result := make([]nodeProjection, 0, len(registryNodes))
	for _, item := range registryNodes {
		runtimeNode := runtimeByTag[item.OutboundTag]
		result = append(result, nodeProjection{
			Node: runtimeNode, ID: item.ID, Name: item.Name, DisplayName: item.DisplayName, Address: item.Address, CountryCode: item.CountryCode, OutboundTag: item.OutboundTag,
			Enabled: item.Enabled, SourceType: item.SourceType, SubscriptionName: item.SubscriptionName,
			Stale: item.Stale, Missing: item.Missing,
		})
	}
	writeJSON(w, http.StatusOK, struct {
		Nodes         []nodeProjection           `json:"nodes"`
		Subscriptions []nodes.PublicSubscription `json:"subscriptions"`
		Total         int                        `json:"total"`
	}{Nodes: result, Subscriptions: registrySubscriptions, Total: len(result)})
}

type nodeProjection struct {
	controlruntime.Node
	ID               string `json:"id"`
	Name             string `json:"name"`
	DisplayName      string `json:"displayName"`
	Address          string `json:"address"`
	CountryCode      string `json:"countryCode,omitempty"`
	OutboundTag      string `json:"outboundTag"`
	Enabled          bool   `json:"enabled"`
	SourceType       string `json:"sourceType"`
	SubscriptionName string `json:"subscriptionName,omitempty"`
	Stale            bool   `json:"stale,omitempty"`
	Missing          bool   `json:"missing,omitempty"`
}

func (p nodeProjection) MarshalJSON() ([]byte, error) {
	type safe struct {
		Tag              string  `json:"tag"`
		Alive            bool    `json:"alive"`
		LatencyMS        int64   `json:"latencyMs"`
		LastSeen         string  `json:"lastSeen"`
		LastTry          string  `json:"lastTry"`
		LastError        string  `json:"lastError"`
		IsNativeSelected bool    `json:"isNativeSelected"`
		IsOverride       bool    `json:"isOverride"`
		IsEffective      bool    `json:"isEffective"`
		ThroughputKBps   float64 `json:"lastThroughputKBps"`
		LastBenchmarkAt  string  `json:"lastBenchmarkAt"`
		ThroughputError  string  `json:"lastThroughputError,omitempty"`
		ID               string  `json:"id"`
		Name             string  `json:"name"`
		DisplayName      string  `json:"displayName"`
		Address          string  `json:"address"`
		CountryCode      string  `json:"countryCode,omitempty"`
		OutboundTag      string  `json:"outboundTag"`
		Enabled          bool    `json:"enabled"`
		SourceType       string  `json:"sourceType"`
		SubscriptionName string  `json:"subscriptionName,omitempty"`
		Stale            bool    `json:"stale,omitempty"`
		Missing          bool    `json:"missing,omitempty"`
	}
	return json.Marshal(safe{
		Tag: p.Tag, Alive: p.Alive, LatencyMS: p.LatencyMS, LastSeen: p.LastSeen, LastTry: p.LastTry, LastError: p.LastError,
		IsNativeSelected: p.IsNativeSelected, IsOverride: p.IsOverride, IsEffective: p.IsEffective, ThroughputKBps: p.ThroughputKBps,
		LastBenchmarkAt: p.LastBenchmarkAt, ThroughputError: p.ThroughputError, ID: p.ID, Name: p.Name, DisplayName: p.DisplayName, Address: p.Address, CountryCode: p.CountryCode, OutboundTag: p.OutboundTag,
		Enabled: p.Enabled, SourceType: p.SourceType, SubscriptionName: p.SubscriptionName, Stale: p.Stale, Missing: p.Missing,
	})
}

func (s *Server) previewImport(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Profiles string `json:"profiles"`
	}
	if !s.decodeMutation(w, r, &request) {
		return
	}
	s.withMutationSession(w, r, func(session auth.Session) {
		if s.nodes == nil {
			writeError(w, http.StatusServiceUnavailable, "node operations unavailable")
			return
		}
		preview, err := s.nodes.PreviewImport(session.CSRFToken, request.Profiles)
		s.writeNodeOperationResult(w, preview, err)
	})
}

func (s *Server) previewReplace(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID      string `json:"id"`
		Profile string `json:"profile"`
	}
	if !s.decodeMutation(w, r, &request) {
		return
	}
	s.withMutationSession(w, r, func(session auth.Session) {
		if s.nodes == nil {
			writeError(w, http.StatusServiceUnavailable, "node operations unavailable")
			return
		}
		preview, err := s.nodes.PreviewReplace(session.CSRFToken, request.ID, request.Profile)
		s.writeNodeOperationResult(w, preview, err)
	})
}

func (s *Server) previewSubscription(w http.ResponseWriter, r *http.Request) {
	var request struct {
		SubscriptionID string `json:"subscriptionId"`
		Name           string `json:"name"`
		URL            string `json:"url"`
	}
	if !s.decodeMutation(w, r, &request) {
		return
	}
	s.withMutationSession(w, r, func(session auth.Session) {
		if s.nodes == nil {
			writeError(w, http.StatusServiceUnavailable, "node operations unavailable")
			return
		}
		preview, err := s.nodes.PreviewRefresh(r.Context(), session.CSRFToken, request.SubscriptionID, request.Name, request.URL)
		s.writeNodeOperationResult(w, preview, err)
	})
}

func (s *Server) previewSubscriptionState(w http.ResponseWriter, r *http.Request) {
	var request struct {
		SubscriptionID string `json:"subscriptionId"`
		Enabled        bool   `json:"enabled"`
	}
	if !s.decodeMutation(w, r, &request) {
		return
	}
	s.withMutationSession(w, r, func(session auth.Session) {
		if s.nodes == nil {
			writeError(w, http.StatusServiceUnavailable, "node operations unavailable")
			return
		}
		preview, err := s.nodes.PreviewSubscriptionState(session.CSRFToken, request.SubscriptionID, request.Enabled)
		s.writeNodeOperationResult(w, preview, err)
	})
}

func (s *Server) previewSubscriptionRemove(w http.ResponseWriter, r *http.Request) {
	var request struct {
		SubscriptionID string `json:"subscriptionId"`
	}
	if !s.decodeMutation(w, r, &request) {
		return
	}
	s.withMutationSession(w, r, func(session auth.Session) {
		if s.nodes == nil {
			writeError(w, http.StatusServiceUnavailable, "node operations unavailable")
			return
		}
		preview, err := s.nodes.PreviewSubscriptionRemove(session.CSRFToken, request.SubscriptionID)
		s.writeNodeOperationResult(w, preview, err)
	})
}

func (s *Server) setManualOverride(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Target string `json:"target"`
	}
	if !s.decodeMutation(w, r, &request) {
		return
	}
	s.withMutationSession(w, r, func(auth.Session) {
		if s.selection == nil {
			writeError(w, http.StatusServiceUnavailable, "selection unavailable")
			return
		}
		if err := s.selection.SetManualOverride(r.Context(), request.Target); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, struct {
			ManualOverride string `json:"manualOverride"`
		}{ManualOverride: request.Target})
	})
}

func (s *Server) applyNodeChange(w http.ResponseWriter, r *http.Request) {
	var request struct {
		PreviewToken  string `json:"previewToken"`
		AcceptMissing bool   `json:"acceptMissing"`
	}
	if !s.decodeMutation(w, r, &request) {
		return
	}
	s.withMutationSession(w, r, func(session auth.Session) {
		if s.nodes == nil {
			writeError(w, http.StatusServiceUnavailable, "node operations unavailable")
			return
		}
		result, err := s.nodes.Apply(r.Context(), session.CSRFToken, request.PreviewToken, request.AcceptMissing)
		if err != nil {
			s.writeNodeOperationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

func (s *Server) cancelNodeChange(w http.ResponseWriter, r *http.Request) {
	var request struct {
		PreviewToken string `json:"previewToken"`
	}
	if !s.decodeMutation(w, r, &request) {
		return
	}
	if request.PreviewToken == "" {
		writeError(w, http.StatusBadRequest, "node operation rejected")
		return
	}
	s.withMutationSession(w, r, func(session auth.Session) {
		if s.nodes == nil {
			writeError(w, http.StatusServiceUnavailable, "node operations unavailable")
			return
		}
		s.nodes.Cancel(session.CSRFToken, request.PreviewToken)
		writeJSON(w, http.StatusOK, struct {
			Canceled bool `json:"canceled"`
		}{Canceled: true})
	})
}

func (s *Server) handleDynamicNodeMutation(w http.ResponseWriter, r *http.Request) {
	prefix := "/api/v1/nodes/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix), "/")
	if len(parts) != 3 || parts[1] != "state" || parts[2] != "preview" {
		if len(parts) != 3 || parts[1] != "remove" || parts[2] != "preview" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var request struct{}
		if !s.decodeMutation(w, r, &request) {
			return
		}
		s.withMutationSession(w, r, func(session auth.Session) {
			if s.nodes == nil {
				writeError(w, http.StatusServiceUnavailable, "node operations unavailable")
				return
			}
			preview, err := s.nodes.PreviewRemove(session.CSRFToken, parts[0])
			s.writeNodeOperationResult(w, preview, err)
		})
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var request struct {
		Enabled bool `json:"enabled"`
	}
	if !s.decodeMutation(w, r, &request) {
		return
	}
	s.withMutationSession(w, r, func(session auth.Session) {
		if s.nodes == nil {
			writeError(w, http.StatusServiceUnavailable, "node operations unavailable")
			return
		}
		preview, err := s.nodes.PreviewState(session.CSRFToken, parts[0], request.Enabled)
		s.writeNodeOperationResult(w, preview, err)
	})
}

func (s *Server) withMutationSession(w http.ResponseWriter, r *http.Request, action func(auth.Session)) {
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if !auth.ValidateCSRF(r, session) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	action(session)
}

func (s *Server) decodeMutation(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxMutationBody)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return false
	}
	return true
}

func (s *Server) decodeComponentCheckRequest(w http.ResponseWriter, r *http.Request, value any) bool {
	if r.ContentLength > maxComponentCheckBody {
		writeError(w, http.StatusRequestEntityTooLarge, "request too large")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxComponentCheckBody)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid request")
		}
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request")
		return false
	}
	return true
}

func (s *Server) decodeComponentMutationRequest(w http.ResponseWriter, r *http.Request, value any) bool {
	contentTypes := r.Header.Values("Content-Type")
	if len(contentTypes) != 1 || strings.TrimSpace(contentTypes[0]) != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported media type")
		return false
	}
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "invalid request")
		return false
	}
	if r.ContentLength > maxComponentMutationBody {
		writeError(w, http.StatusRequestEntityTooLarge, "request too large")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxComponentMutationBody)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid request")
		}
		return false
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid request")
		return false
	}
	return true
}

func (s *Server) decodeSecretBackupRequest(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, backup.MaxSecretRequestBody)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request")
		return false
	}
	return true
}

func (s *Server) writeNodeOperationResult(w http.ResponseWriter, preview nodes.Preview, err error) {
	if err != nil {
		s.writeNodeOperationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) writeNodeOperationError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	message := "node operation rejected"
	switch {
	case errors.Is(err, nodes.ErrNodeNotFound):
		status = http.StatusNotFound
	case errors.Is(err, nodes.ErrSubscriptionNotFound):
		status = http.StatusNotFound
	case errors.Is(err, nodes.ErrSubscriptionDisabled):
		message = "subscription is disabled"
	case errors.Is(err, nodes.ErrPreviewExpired):
		status = http.StatusConflict
	case errors.Is(err, nodes.ErrMissingAcceptance):
		status = http.StatusConflict
	case errors.Is(err, nodes.ErrPreviewStale):
		status = http.StatusConflict
	case errors.Is(err, nodes.ErrOperationUnavailable):
		status = http.StatusServiceUnavailable
	case errors.Is(err, nodes.ErrSubscriptionRejected):
		message = "subscription URL rejected"
	case errors.Is(err, nodes.ErrSubscriptionFetch):
		status = http.StatusBadGateway
		message = "subscription fetch failed"
	case errors.Is(err, nodes.ErrSubscriptionContent):
		message = "subscription content rejected"
	case errors.Is(err, nodes.ErrSubscriptionDuplicate):
		message = "subscription contains duplicate node identity"
	case errors.Is(err, nodes.ErrSubscriptionNode):
		message = "subscription node rejected"
	case errors.Is(err, nodes.ErrPreviewCandidate):
		message = "preview candidate is invalid"
	case errors.Is(err, nodes.ErrRollbackFailed):
		status = http.StatusInternalServerError
		message = "node activation failed; rollback failed"
	case strings.Contains(err.Error(), "previous generation restored"):
		status = http.StatusInternalServerError
		message = "node activation failed; previous generation restored"
	default:
		if strings.Contains(err.Error(), "activation") || strings.Contains(err.Error(), "Xray") || strings.Contains(err.Error(), "registry unavailable") {
			status = http.StatusInternalServerError
			message = "node activation failed"
		}
	}
	writeError(w, status, message)
}

func isNodeMutationPath(path string) bool {
	return strings.HasPrefix(path, "/api/v1/nodes/") && (strings.HasSuffix(path, "/state/preview") || strings.HasSuffix(path, "/remove/preview"))
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Authenticated bool      `json:"authenticated"`
		CSRFToken     string    `json:"csrfToken"`
		ExpiresAt     time.Time `json:"expiresAt"`
	}{Authenticated: true, CSRFToken: session.CSRFToken, ExpiresAt: session.ExpiresAt})
}

func (s *Server) readOnly(w http.ResponseWriter, r *http.Request, selectView func(controlruntime.View) any) {
	if _, ok := s.requireSession(w, r); !ok {
		return
	}
	if s.collector == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime unavailable")
		return
	}
	writeJSON(w, http.StatusOK, selectView(s.collector.Snapshot(r.Context())))
}

func (s *Server) readComponents(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSession(w, r); !ok {
		return
	}
	if s.components == nil {
		writeError(w, http.StatusServiceUnavailable, "component inventory unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s.components.Snapshot(r.Context()))
}

func (s *Server) checkComponents(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if !auth.ValidateCSRF(r, session) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if s.componentChecks == nil {
		writeError(w, http.StatusServiceUnavailable, "component check unavailable")
		return
	}
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(r.Header.Get("Content-Type")))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported media type")
		return
	}
	var request components.CheckRequest
	if !s.decodeComponentCheckRequest(w, r, &request) {
		return
	}
	if err := components.ValidateCheckRequest(request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid component check request")
		return
	}
	result, err := s.componentChecks.Check(r.Context(), request)
	if err != nil {
		s.writeComponentCheckError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) writeComponentCheckError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, components.ErrInvalidCheckRequest):
		writeError(w, http.StatusBadRequest, "invalid component check request")
	case errors.Is(err, components.ErrCheckBusy):
		writeError(w, http.StatusConflict, "component check busy")
	case errors.Is(err, components.ErrCheckTimeout):
		writeError(w, http.StatusGatewayTimeout, "component check timed out")
	case errors.Is(err, components.ErrUpstreamRejected):
		writeError(w, http.StatusBadGateway, "component metadata rejected")
	default:
		writeError(w, http.StatusServiceUnavailable, "component check unavailable")
	}
}

func (s *Server) previewComponentMutation(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if !auth.ValidateCSRF(r, session) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if s.componentMutations == nil {
		writeError(w, http.StatusServiceUnavailable, "component mutation unavailable")
		return
	}
	var request components.MutationRequest
	if !s.decodeComponentMutationRequest(w, r, &request) {
		return
	}
	if err := components.ValidateMutationRequest(request); err != nil {
		writeComponentMutationError(w, err)
		return
	}
	preview, err := s.componentMutations.Preview(r.Context(), session.CSRFToken, request)
	if err != nil {
		writeComponentMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) applyComponentMutation(w http.ResponseWriter, r *http.Request) {
	s.applyOrRollbackComponentMutation(w, r, false)
}

func (s *Server) rollbackComponentMutation(w http.ResponseWriter, r *http.Request) {
	s.applyOrRollbackComponentMutation(w, r, true)
}

func (s *Server) applyOrRollbackComponentMutation(w http.ResponseWriter, r *http.Request, rollback bool) {
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if !auth.ValidateCSRF(r, session) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if s.componentMutations == nil {
		writeError(w, http.StatusServiceUnavailable, "component mutation unavailable")
		return
	}
	var request components.MutationTokenRequest
	if !s.decodeComponentMutationRequest(w, r, &request) {
		return
	}
	if err := components.ValidateMutationToken(request.PreviewToken); err != nil {
		writeComponentMutationError(w, err)
		return
	}
	var result components.MutationResult
	var err error
	if rollback {
		result, err = s.componentMutations.Rollback(r.Context(), session.CSRFToken, request.PreviewToken)
	} else {
		result, err = s.componentMutations.Apply(r.Context(), session.CSRFToken, request.PreviewToken)
	}
	if err != nil {
		writeComponentMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) cancelComponentMutation(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if !auth.ValidateCSRF(r, session) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if s.componentMutations == nil {
		writeError(w, http.StatusServiceUnavailable, "component mutation unavailable")
		return
	}
	var request components.MutationTokenRequest
	if !s.decodeComponentMutationRequest(w, r, &request) {
		return
	}
	if err := components.ValidateMutationToken(request.PreviewToken); err != nil {
		writeComponentMutationError(w, err)
		return
	}
	s.componentMutations.Cancel(session.CSRFToken, request.PreviewToken)
	writeJSON(w, http.StatusOK, struct {
		Canceled bool `json:"canceled"`
	}{Canceled: true})
}

func writeComponentMutationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, components.ErrInvalidMutationRequest), errors.Is(err, components.ErrMutationOperationMismatch):
		writeError(w, http.StatusBadRequest, "invalid component mutation request")
	case errors.Is(err, components.ErrMutationBusy):
		writeError(w, http.StatusConflict, "component mutation busy")
	case errors.Is(err, components.ErrMutationPreviewExpired):
		writeError(w, http.StatusConflict, "component mutation preview expired or invalid")
	case errors.Is(err, components.ErrMutationPreviewStale):
		writeError(w, http.StatusConflict, "component mutation preview is stale")
	case errors.Is(err, components.ErrMutationNoPrevious):
		writeError(w, http.StatusConflict, "previous component generation unavailable")
	case errors.Is(err, components.ErrMutationMetadataUnavailable):
		writeError(w, http.StatusBadGateway, "component metadata unavailable")
	case errors.Is(err, components.ErrMutationCandidateRejected):
		writeError(w, http.StatusBadGateway, "component candidate rejected")
	case errors.Is(err, components.ErrMutationTransactionFailed):
		writeError(w, http.StatusInternalServerError, "component transaction failed; previous generation restored")
	case errors.Is(err, components.ErrMutationRollbackUnproven):
		writeError(w, http.StatusServiceUnavailable, "component rollback or recovery is not proven")
	case errors.Is(err, components.ErrMutationMaintenance):
		writeError(w, http.StatusServiceUnavailable, "component mutation unavailable during maintenance")
	case errors.Is(err, components.ErrMutationUnavailable):
		writeError(w, http.StatusServiceUnavailable, "component mutation unavailable")
	default:
		writeError(w, http.StatusServiceUnavailable, "component mutation unavailable")
	}
}

func (s *Server) requireSession(w http.ResponseWriter, r *http.Request) (auth.Session, bool) {
	if s.auth == nil {
		writeError(w, http.StatusServiceUnavailable, "authentication unavailable")
		return auth.Session{}, false
	}
	session, ok := s.auth.SessionFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return auth.Session{}, false
	}
	return session, true
}

func (s *Server) originAllowed(r *http.Request) bool {
	if s.auth == nil {
		return true
	}
	return s.auth.OriginAllowed(r)
}

func writeBackupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, backup.ErrBusy):
		writeError(w, http.StatusConflict, "backup export busy")
	case errors.Is(err, backup.ErrInvalidPassphrase):
		writeError(w, http.StatusBadRequest, "passphrase is outside the allowed bounds")
	case errors.Is(err, backup.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "backup unavailable")
	default:
		writeError(w, http.StatusInternalServerError, "backup unavailable")
	}
}

func writeBackupDownload(w http.ResponseWriter, contents []byte, filename, mediaType string) {
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(contents)
}

func clearBytes(contents []byte) {
	for index := range contents {
		contents[index] = 0
	}
}

func setSecurityHeaders(w http.ResponseWriter, api bool) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'; object-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	if api {
		w.Header().Set("Cache-Control", "no-store")
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	contents, err := json.Marshal(value)
	if err != nil || len(contents) > maxJSONResponse {
		writeError(w, http.StatusInternalServerError, "response unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(contents, '\n'))
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, struct {
		Error string `json:"error"`
	}{Error: message})
}

func methodNotAllowed(w http.ResponseWriter, method string) {
	w.Header().Set("Allow", method)
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	if parsed, err := url.Parse("//" + r.RemoteAddr); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	return "unknown"
}
