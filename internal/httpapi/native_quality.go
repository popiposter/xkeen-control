package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/nativequality"
)

type NativeQualityService interface {
	Read() nativequality.Status
	Start(context.Context) error
	Stage(context.Context, string) (string, error)
}

type nativeQualityInspection interface{ InspectAndResolve(context.Context) error }

func (s *Server) handleNativeQuality(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if s.nativeQuality == nil {
		writeError(w, http.StatusServiceUnavailable, "native quality unavailable")
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "query parameters unsupported")
		return
	}
	if r.URL.Path == "/api/v1/performance/quality" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, s.nativeQuality.Read())
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
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	defer r.Body.Close()
	allowed := map[string]bool{}
	if r.URL.Path == "/api/v1/performance/quality/stage" || r.URL.Path == "/api/v1/performance/quality/apply" {
		allowed["digest"] = true
	}
	fields, err := transferFields(json.NewDecoder(r.Body), allowed)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid quality request")
		return
	}
	if !s.transferSessionStillActive(r, session) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	switch r.URL.Path {
	case "/api/v1/performance/quality/inspect":
		inspector, ok := s.nativeQuality.(nativeQualityInspection)
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "quality inspection unavailable")
			return
		}
		if err := inspector.InspectAndResolve(r.Context()); err != nil {
			writeError(w, http.StatusConflict, "quality inspection inconclusive; review native configuration, job and probe state")
			return
		}
		writeJSON(w, http.StatusOK, s.nativeQuality.Read())
	case "/api/v1/performance/quality/start":
		if err := s.nativeQuality.Start(r.Context()); err != nil {
			writeError(w, http.StatusConflict, "quality comparison busy or unavailable")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]bool{"accepted": true})
	case "/api/v1/performance/quality/stage", "/api/v1/performance/quality/apply":
		var digest string
		if json.Unmarshal(fields["digest"], &digest) != nil || len(digest) != 64 || strings.Trim(digest, "0123456789abcdef") != "" {
			writeError(w, http.StatusBadRequest, "invalid quality request")
			return
		}
		apply := r.URL.Path == "/api/v1/performance/quality/apply"
		if apply && (s.nativeConfig == nil || s.nativeJobs == nil) {
			writeError(w, http.StatusServiceUnavailable, "native configuration application unavailable")
			return
		}
		value, err := s.nativeQuality.Stage(r.Context(), digest)
		if err != nil {
			writeError(w, http.StatusConflict, "recommendation unavailable; inspect saved configuration before retrying")
			return
		}
		if apply {
			// The editor and native job remain the sole lifecycle owner. A saved
			// recommendation is never replayed when restart admission fails.
			job, err := s.nativeJobs.ApplyConfigs(session.CSRFToken, s.nativeConfig, value)
			if err != nil {
				writeJSON(w, http.StatusConflict, map[string]any{"error": "recommendation saved; restart not confirmed; inspect configuration and console", "saved": true, "digest": value})
				return
			}
			writeJSON(w, http.StatusAccepted, job)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"digest": value, "restartRequired": true})
	}
}
