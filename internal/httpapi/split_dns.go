package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/splitdns"
)

func (s *Server) handleSplitDNS(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "query parameters are not supported")
		return
	}
	if r.URL.Path == "/api/v1/dns/split" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if s.splitDNS == nil {
			writeJSON(w, http.StatusOK, splitdns.Status{State: "unconfigured"})
			return
		}
		writeJSON(w, http.StatusOK, s.splitDNS.Status(r.Context()))
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
	defer r.Body.Close()
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	var body map[string]json.RawMessage
	if d.Decode(&body) != nil || body == nil || len(body) != 0 || d.Decode(&struct{}{}) != io.EOF {
		writeError(w, http.StatusBadRequest, "expected empty synchronization request")
		return
	}
	writeError(w, http.StatusGone, "LAN DNS synchronization is retired; existing resolver status remains available")
}
