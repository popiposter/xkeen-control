package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

func (s *Server) handleNativeConfig(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if s.nativeConfig == nil {
		writeError(w, http.StatusServiceUnavailable, "native configuration editor unavailable")
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "query parameters unsupported")
		return
	}
	if r.URL.Path == "/api/v1/xkeen/config" || r.URL.Path == "/api/v1/xkeen/config/workspace" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		var value any
		var err error
		if r.URL.Path == "/api/v1/xkeen/config/workspace" {
			var index xkeen.EditorWorkspace
			index, err = s.nativeConfig.Workspace(r.Context())
			// Index only. Private text is fetched one fixed document at a time.
			for id := range index.Documents {
				index.Documents[id] = xkeen.EditorDocument{}
			}
			value = index
		} else {
			value, err = s.nativeConfig.Read(r.Context())
		}
		if err != nil {
			writeError(w, http.StatusConflict, "native configuration unavailable")
			return
		}
		writeJSON(w, http.StatusOK, value)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !auth.ValidateCSRF(r, session) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported media type")
		return
	}
	switch r.URL.Path {
	case "/api/v1/xkeen/config/example":
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		defer r.Body.Close()
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request struct {
			Text   string              `json:"text"`
			Sample xkeen.RoutingSample `json:"sample"`
		}
		if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid routing example")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		value, err := xkeen.PreviewRouting(ctx, request.Text, request.Sample, s.geodata)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid routing example")
			return
		}
		writePrivateConfigJSON(w, http.StatusOK, value)
		return
	case "/api/v1/xkeen/config/save-set", "/api/v1/xkeen/config/apply", "/api/v1/xkeen/config/inspect", "/api/v1/xkeen/config/restore-saved", "/api/v1/xkeen/config/restore-previous":
		s.handleNativeConfigSet(w, r, session.CSRFToken)
		return
	}
	if r.URL.Path == "/api/v1/xkeen/config/text" || r.URL.Path == "/api/v1/xkeen/config/draft" || r.URL.Path == "/api/v1/xkeen/config/document" {
		s.handleNativeDocument(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request struct {
		Digest string          `json:"digest"`
		Area   string          `json:"area"`
		Field  string          `json:"field"`
		Value  json.RawMessage `json:"value"`
	}
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid editor request")
		return
	}
	value, err := xkeen.ValidateEditorValue(request.Area, request.Field, request.Value)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unsupported setting or value")
		return
	}
	digest, err := s.nativeConfig.SaveField(r.Context(), request.Digest, request.Area, request.Field, value)
	if err != nil {
		writeError(w, http.StatusConflict, "configuration changed, invalid or unavailable; reload before saving")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"digest": digest, "saved": true, "restartRequired": digest != request.Digest})
}

func (s *Server) handleNativeConfigSet(w http.ResponseWriter, r *http.Request, owner string) {
	limit := int64(4096)
	if r.URL.Path == "/api/v1/xkeen/config/save-set" {
		limit = 64 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request struct {
		Digest    string            `json:"digest"`
		ID        string            `json:"id"`
		Documents map[string]string `json:"documents"`
	}
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid config set request")
		return
	}
	var digest string
	var err error
	switch r.URL.Path {
	case "/api/v1/xkeen/config/save-set":
		digest, err = s.nativeConfig.SaveTexts(r.Context(), request.Digest, request.Documents)
	case "/api/v1/xkeen/config/restore-saved":
		digest, err = s.nativeConfig.RestoreSaved(r.Context(), request.Digest)
	case "/api/v1/xkeen/config/restore-previous":
		digest, err = s.nativeConfig.RestorePrevious(r.Context(), request.Digest)
	case "/api/v1/xkeen/config/apply":
		if s.nativeJobs == nil {
			writeError(w, http.StatusServiceUnavailable, "native commands unavailable")
			return
		}
		job, startErr := s.nativeJobs.ApplyConfigs(owner, s.nativeConfig, request.Digest)
		if startErr == nil {
			writeJSON(w, http.StatusAccepted, job)
			return
		}
		err = startErr
	case "/api/v1/xkeen/config/inspect":
		if s.nativeJobs == nil {
			writeError(w, http.StatusServiceUnavailable, "native commands unavailable")
			return
		}
		job, readErr := s.nativeJobs.Read(owner, request.ID, 0)
		if readErr != nil || job.State == "running" {
			writeError(w, http.StatusConflict, "native job unavailable or still running")
			return
		}
		if job.State == "unknown" {
			if _, err := s.nativeJobs.ResolveInspection(r.Context(), owner, request.ID); err != nil {
				writeError(w, http.StatusConflict, "native outcome still requires inspection")
				return
			}
		}
		err = s.nativeConfig.InspectApplied(r.Context(), request.ID)
		if err == nil {
			writeJSON(w, http.StatusOK, map[string]any{"observed": "applied", "vpnHealth": "not-tested"})
			return
		}
	}
	if err != nil {
		var detail *xkeen.ValidationError
		if errors.As(err, &detail) {
			writePrivateConfigJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "Xray validation failed", "diagnostic": detail})
		} else {
			writeError(w, http.StatusConflict, "configuration result not confirmed; inspect and reload before another action")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"digest": digest, "saved": true})
}

func (s *Server) handleNativeDocument(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 12<<20)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request struct {
		Digest  string `json:"digest"`
		File    string `json:"file"`
		Text    string `json:"text"`
		Discard bool   `json:"discard"`
	}
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid editor document")
		return
	}
	if r.URL.Path == "/api/v1/xkeen/config/document" {
		workspace, err := s.nativeConfig.Workspace(r.Context())
		document, exists := workspace.Documents[request.File]
		if err != nil || !exists {
			writeError(w, http.StatusConflict, "native document unavailable")
			return
		}
		writePrivateConfigJSON(w, http.StatusOK, map[string]any{"digest": workspace.Digest, "document": document})
		return
	}
	if r.URL.Path == "/api/v1/xkeen/config/draft" {
		if s.nativeConfig.SaveDraft(r.Context(), request.File, request.Text, request.Discard) != nil {
			writeError(w, http.StatusConflict, "draft could not be saved")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
		return
	}
	if request.Discard {
		writeError(w, http.StatusBadRequest, "unsupported editor action")
		return
	}
	digest, err := s.nativeConfig.SaveText(r.Context(), request.Digest, request.File, request.Text)
	if err != nil {
		var validation *xkeen.ValidationError
		if errors.As(err, &validation) {
			writePrivateConfigJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "Xray validation failed", "diagnostic": validation})
		} else {
			writeError(w, http.StatusConflict, "configuration invalid, changed or unavailable; reload before saving")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"digest": digest, "saved": true, "restartRequired": digest != request.Digest})
}

// Two 2MiB strings can expand up to 24MiB as JSON. This cap is scoped to the
// private fixed document/diagnostic responses; status APIs retain their 512KiB cap.
func writePrivateConfigJSON(w http.ResponseWriter, status int, value any) {
	contents, err := json.Marshal(value)
	if err != nil || len(contents) > 32<<20 {
		writeError(w, http.StatusInternalServerError, "private document unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(contents, '\n'))
}
