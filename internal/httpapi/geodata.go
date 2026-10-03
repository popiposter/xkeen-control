package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/geodatareader"
)

func (s *Server) handleGeodata(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "query parameters are not supported")
		return
	}
	if s.geodata == nil {
		writeError(w, http.StatusServiceUnavailable, "installed geodata unavailable")
		return
	}
	var value any
	var err error
	if r.URL.Path == "/api/v1/geodata" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		value, err = s.geodata.Inventory()
	} else {
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
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		defer r.Body.Close()
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var q geodatareader.Request
		if decoder.Decode(&q) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid geodata query")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		value, err = s.geodata.Query(ctx, q)
	}
	if err != nil {
		writeError(w, http.StatusConflict, "installed geodata unavailable or changed; reload the file")
		return
	}
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > 256<<10 {
		writeError(w, http.StatusConflict, "geodata page exceeds response limit; use a smaller page")
		return
	}
	writeJSON(w, http.StatusOK, value)
}
