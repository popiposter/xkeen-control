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
	if r.URL.Path == "/api/v1/performance/quality/stage" {
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
	case "/api/v1/performance/quality/start":
		if err := s.nativeQuality.Start(r.Context()); err != nil {
			writeError(w, http.StatusConflict, "quality comparison busy or unavailable")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]bool{"accepted": true})
	case "/api/v1/performance/quality/stage":
		var digest string
		if json.Unmarshal(fields["digest"], &digest) != nil || len(digest) != 64 || strings.Trim(digest, "0123456789abcdef") != "" {
			writeError(w, http.StatusBadRequest, "invalid quality request")
			return
		}
		value, err := s.nativeQuality.Stage(r.Context(), digest)
		if err != nil {
			writeError(w, http.StatusConflict, "recommendation unavailable; inspect saved configuration before retrying")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"digest": value, "restartRequired": true})
	}
}
