package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/notifications"
)

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	method := http.MethodPost
	if r.URL.Path == "/api/v1/notifications" {
		method = http.MethodGet
	}
	if r.Method != method {
		methodNotAllowed(w, method)
		return
	}
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if method == http.MethodPost && !auth.ValidateCSRF(r, session) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if r.URL.RawQuery != "" {
		writeCodedError(w, http.StatusBadRequest, "invalid-request", "invalid notification request")
		return
	}
	if s.notifications == nil {
		writeCodedError(w, http.StatusServiceUnavailable, "unavailable", "notifications unavailable")
		return
	}
	if method == http.MethodGet {
		writeJSON(w, http.StatusOK, s.notifications.Status())
		return
	}
	contentTypes := r.Header.Values("Content-Type")
	if len(contentTypes) != 1 || strings.TrimSpace(contentTypes[0]) != "application/json" {
		writeCodedError(w, http.StatusUnsupportedMediaType, "invalid-request", "unsupported media type")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, notifications.MaxAuthorityBytes)
	defer r.Body.Close()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeCodedError(w, http.StatusRequestEntityTooLarge, "invalid-request", "request too large")
		return
	}
	fields := []string{}
	switch r.URL.Path {
	case "/api/v1/notifications/configure":
		fields = []string{"botToken", "chatId"}
	case "/api/v1/notifications/enabled":
		fields = []string{"enabled"}
	}
	value, err := notifications.DecodeObject(data, fields...)
	if err != nil {
		writeCodedError(w, http.StatusBadRequest, "invalid-request", "invalid notification request")
		return
	}
	var status notifications.Status
	switch r.URL.Path {
	case "/api/v1/notifications/configure":
		var token, chat string
		if json.Unmarshal(value["botToken"], &token) != nil || json.Unmarshal(value["chatId"], &chat) != nil {
			writeCodedError(w, http.StatusBadRequest, "invalid-request", "invalid notification request")
			return
		}
		status, err = s.notifications.Configure(token, chat)
	case "/api/v1/notifications/enabled":
		var enabled bool
		if json.Unmarshal(value["enabled"], &enabled) != nil {
			writeCodedError(w, http.StatusBadRequest, "invalid-request", "invalid notification request")
			return
		}
		status, err = s.notifications.SetEnabled(enabled)
	case "/api/v1/notifications/test":
		status, err = s.notifications.Test(r.Context())
	case "/api/v1/notifications/clear":
		status, err = s.notifications.Clear()
	}
	if err != nil {
		code := "unavailable"
		if safe, ok := err.(notifications.Error); ok {
			switch safe {
			case "invalid-request", "unconfigured", "disabled", "authority-unavailable", "dns-unavailable", "unsafe-destination", "timeout", "transport-failed", "provider-rejected":
				code = string(safe)
			}
		}
		httpStatus := http.StatusServiceUnavailable
		if code == "invalid-request" {
			httpStatus = http.StatusBadRequest
		}
		writeCodedError(w, httpStatus, code, "notification operation failed")
		return
	}
	writeJSON(w, http.StatusOK, status)
}
