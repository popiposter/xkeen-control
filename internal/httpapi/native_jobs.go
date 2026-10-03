package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

func (s *Server) handleNativeJobs(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "query parameters are not supported")
		return
	}
	if s.nativeJobs == nil {
		writeError(w, http.StatusServiceUnavailable, "native commands unavailable")
		return
	}
	if r.URL.Path == "/api/v1/xkeen/commands" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, s.nativeJobs.InstalledCatalog())
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
	// Console bodies are private: errors never contain input or native output.
	if r.Header.Get("Content-Type") != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported media type")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	decode := func(v any) bool {
		if decoder.Decode(v) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid native job request")
			return false
		}
		return true
	}
	fail := func(err error) {
		if errors.Is(err, authority.ErrBusy) || errors.Is(err, authority.ErrBlocked) {
			writeError(w, http.StatusConflict, "native operation busy or needs inspection")
		} else if errors.Is(err, xkeen.ErrCommand) {
			writeError(w, http.StatusBadRequest, "unsupported native command or parameter")
		} else {
			writeError(w, http.StatusConflict, "native job unavailable")
		}
	}
	owner := session.CSRFToken
	switch r.URL.Path {
	case "/api/v1/xkeen/jobs/start":
		var request xkeen.CommandRequest
		if !decode(&request) {
			return
		}
		var result xkeen.JobView
		var err error
		if s.nativeConfig != nil && (request.Action == "restart" || request.Action == "start") {
			if digest, exists := s.nativeConfig.PendingDigest(); exists {
				result, err = s.nativeJobs.StartConfigured(owner, request, s.nativeConfig, digest)
			} else {
				result, err = s.nativeJobs.Start(owner, request)
			}
		} else {
			result, err = s.nativeJobs.Start(owner, request)
		}
		if err != nil {
			fail(err)
			return
		}
		writeJSON(w, http.StatusAccepted, result)
	case "/api/v1/xkeen/jobs/read":
		var request struct {
			ID     string `json:"id"`
			Cursor int64  `json:"cursor"`
		}
		if !decode(&request) {
			return
		}
		result, err := s.nativeJobs.Read(owner, request.ID, request.Cursor)
		if err != nil {
			fail(err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	case "/api/v1/xkeen/jobs/input":
		var request struct {
			ID   string `json:"id"`
			Data string `json:"data"`
		}
		if !decode(&request) {
			return
		}
		if err := s.nativeJobs.Input(owner, request.ID, request.Data); err != nil {
			fail(err)
			return
		}
		writeJSON(w, http.StatusOK, struct{}{})
	case "/api/v1/xkeen/jobs/resize":
		var request struct {
			ID   string `json:"id"`
			Cols uint16 `json:"cols"`
			Rows uint16 `json:"rows"`
		}
		if !decode(&request) {
			return
		}
		if err := s.nativeJobs.Resize(owner, request.ID, request.Cols, request.Rows); err != nil {
			fail(err)
			return
		}
		writeJSON(w, http.StatusOK, struct{}{})
	case "/api/v1/xkeen/jobs/cancel":
		var request struct {
			ID string `json:"id"`
		}
		if !decode(&request) {
			return
		}
		if err := s.nativeJobs.Cancel(owner, request.ID); err != nil {
			fail(err)
			return
		}
		writeJSON(w, http.StatusOK, struct{}{})
	case "/api/v1/xkeen/jobs/resolve":
		var request struct {
			ID        string `json:"id"`
			Inspected bool   `json:"inspected"`
		}
		if !decode(&request) {
			return
		}
		if !request.Inspected {
			writeError(w, http.StatusBadRequest, "inspect the current native state first")
			return
		}
		result, err := s.nativeJobs.ResolveInspection(r.Context(), owner, request.ID)
		if err != nil {
			fail(err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
