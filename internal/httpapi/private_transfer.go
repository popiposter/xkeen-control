package httpapi

import (
	"errors"
	"github.com/popiposter/xkeen-control/internal/auth"
	"net/http"
	"strings"
)

func (s *Server) tryTransferPreview() (func(), bool) {
	if s == nil || s.transferPreviewGate == nil {
		return nil, false
	}
	select {
	case s.transferPreviewGate <- struct{}{}:
		return func() { <-s.transferPreviewGate }, true
	default:
		return nil, false
	}
}

func (s *Server) transferSessionStillActive(r *http.Request, expected auth.Session) bool {
	if s == nil || s.auth == nil {
		return false
	}
	current, ok := s.auth.SessionFromRequest(r)
	return ok && current.CSRFToken == expected.CSRFToken
}

func isTransferBodyTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr) || strings.Contains(strings.ToLower(err.Error()), "request body too large")
}
