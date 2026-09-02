package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/backup"
	"github.com/popiposter/xkeen-control/internal/restore"
)

const (
	maxRestoreRequestBody = 10 << 20
	maxRestoreJSONBody    = 16 << 10
	maxRestoreParts       = 2
	maxRestoreTokenBytes  = 256
)

func restoreModeFromQuery(rawQuery string) (restore.Mode, error) {
	if rawQuery == "" {
		return "", restore.ErrInvalidMode
	}
	for _, item := range strings.Split(rawQuery, "&") {
		if item == "" {
			return "", restore.ErrInvalidMode
		}
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil || len(values) != 1 || len(values["mode"]) != 1 {
		return "", restore.ErrInvalidMode
	}
	switch values["mode"][0] {
	case string(restore.SettingsOnly):
		return restore.SettingsOnly, nil
	case string(restore.ReplaceRegistry):
		return restore.ReplaceRegistry, nil
	case string(restore.MergeRegistry):
		return restore.MergeRegistry, nil
	default:
		return "", restore.ErrInvalidMode
	}
}

func (s *Server) tryRestorePreview() (func(), bool) {
	if s == nil || s.restorePreviewGate == nil {
		return nil, false
	}
	select {
	case s.restorePreviewGate <- struct{}{}:
		return func() { <-s.restorePreviewGate }, true
	default:
		return nil, false
	}
}

func (s *Server) previewRestore(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if !auth.ValidateCSRF(r, session) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if s.restore == nil {
		writeError(w, http.StatusServiceUnavailable, "restore unavailable")
		return
	}
	mode, err := restoreModeFromQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid restore mode")
		return
	}
	release, admitted := s.tryRestorePreview()
	if !admitted {
		writeError(w, http.StatusConflict, "restore preview busy")
		return
	}
	defer release()

	bundle, passphrase, status := parseRestoreMultipart(w, r)
	if status != 0 {
		writeRestoreUploadError(w, status)
		return
	}
	defer clearBytes(bundle)
	if !s.restoreSessionStillActive(r, session) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	preview, err := s.restore.PreviewBundle(r.Context(), session.CSRFToken, bundle, passphrase, mode)
	clearBytes(bundle)
	bundle = nil
	if err != nil {
		writeRestorePreviewError(w, err)
		return
	}
	if !s.restoreSessionStillActive(r, session) {
		s.restore.Cancel(session.CSRFToken, preview.Token)
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) applyRestore(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if !auth.ValidateCSRF(r, session) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if s.restore == nil {
		writeError(w, http.StatusServiceUnavailable, "restore unavailable")
		return
	}
	var request restoreTokenRequest
	if !s.decodeRestoreTokenRequest(w, r, &request) {
		return
	}
	result, err := s.restore.Apply(r.Context(), session.CSRFToken, request.PreviewToken)
	if err != nil {
		writeRestoreApplyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) cancelRestore(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if !auth.ValidateCSRF(r, session) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if s.restore == nil {
		writeError(w, http.StatusServiceUnavailable, "restore unavailable")
		return
	}
	var request restoreTokenRequest
	if !s.decodeRestoreTokenRequest(w, r, &request) {
		return
	}
	s.restore.Cancel(session.CSRFToken, request.PreviewToken)
	writeJSON(w, http.StatusOK, struct {
		Canceled bool `json:"canceled"`
	}{Canceled: true})
}

type restoreTokenRequest struct {
	PreviewToken string `json:"previewToken"`
}

func (s *Server) decodeRestoreTokenRequest(w http.ResponseWriter, r *http.Request, request *restoreTokenRequest) bool {
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "invalid restore request")
		return false
	}
	if r.ContentLength > maxRestoreJSONBody {
		writeError(w, http.StatusRequestEntityTooLarge, "restore request exceeds allowed size")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRestoreJSONBody)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(request); err != nil {
		writeRestoreJSONError(w, err)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil && isRestoreBodyTooLarge(err) {
			writeError(w, http.StatusRequestEntityTooLarge, "restore request exceeds allowed size")
		} else {
			writeError(w, http.StatusBadRequest, "invalid restore request")
		}
		return false
	}
	if request.PreviewToken == "" || len([]byte(request.PreviewToken)) > maxRestoreTokenBytes {
		writeError(w, http.StatusBadRequest, "invalid restore request")
		return false
	}
	return true
}

func parseRestoreMultipart(w http.ResponseWriter, r *http.Request) ([]byte, string, int) {
	if r.ContentLength > maxRestoreRequestBody {
		return nil, "", http.StatusRequestEntityTooLarge
	}
	rawMediaType := r.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(rawMediaType)
	if err != nil {
		declaredMediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(rawMediaType, ";", 2)[0]))
		if declaredMediaType == "multipart/form-data" {
			return nil, "", http.StatusBadRequest
		}
		return nil, "", http.StatusUnsupportedMediaType
	}
	if !strings.EqualFold(mediaType, "multipart/form-data") {
		return nil, "", http.StatusUnsupportedMediaType
	}
	if len(params) != 1 || strings.TrimSpace(params["boundary"]) == "" {
		return nil, "", http.StatusBadRequest
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRestoreRequestBody)
	defer r.Body.Close()
	reader := multipart.NewReader(r.Body, params["boundary"])
	var bundle []byte
	var passphraseBytes []byte
	bundleSeen := false
	passphraseSeen := false
	status := 0
	setStatus := func(value int) {
		if value == http.StatusRequestEntityTooLarge || status == 0 {
			status = value
		}
	}
	drain := func(part *multipart.Part) {
		if _, drainErr := io.Copy(io.Discard, part); drainErr != nil {
			if isRestoreBodyTooLarge(drainErr) {
				setStatus(http.StatusRequestEntityTooLarge)
			} else {
				setStatus(http.StatusBadRequest)
			}
		}
	}
	readPart := func(part *multipart.Part, limit int64) []byte {
		contents, readErr := io.ReadAll(io.LimitReader(part, limit+1))
		if readErr != nil {
			if isRestoreBodyTooLarge(readErr) {
				setStatus(http.StatusRequestEntityTooLarge)
			} else {
				setStatus(http.StatusBadRequest)
			}
			clearBytes(contents)
			return nil
		}
		if int64(len(contents)) > limit {
			setStatus(http.StatusRequestEntityTooLarge)
			clearBytes(contents)
			return nil
		}
		return contents
	}

	for partCount := 0; ; partCount++ {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			if isRestoreBodyTooLarge(nextErr) {
				setStatus(http.StatusRequestEntityTooLarge)
			} else {
				setStatus(http.StatusBadRequest)
			}
			break
		}
		if partCount >= maxRestoreParts {
			setStatus(http.StatusBadRequest)
			drain(part)
			continue
		}
		switch part.FormName() {
		case "bundle":
			if bundleSeen {
				setStatus(http.StatusBadRequest)
				drain(part)
				continue
			}
			bundleSeen = true
			bundle = readPart(part, int64(backup.MaxEncryptedEnvelope))
		case "passphrase":
			if passphraseSeen {
				setStatus(http.StatusBadRequest)
				drain(part)
				continue
			}
			passphraseSeen = true
			passphraseBytes = readPart(part, int64(backup.MaxPassphraseBytes))
		default:
			setStatus(http.StatusBadRequest)
			drain(part)
		}
	}
	if _, drainErr := io.Copy(io.Discard, r.Body); drainErr != nil {
		if isRestoreBodyTooLarge(drainErr) {
			setStatus(http.StatusRequestEntityTooLarge)
		} else {
			setStatus(http.StatusBadRequest)
		}
	}
	if status != 0 {
		clearBytes(bundle)
		clearBytes(passphraseBytes)
		return nil, "", status
	}
	if !bundleSeen {
		clearBytes(passphraseBytes)
		return nil, "", http.StatusBadRequest
	}
	passphrase := string(passphraseBytes)
	clearBytes(passphraseBytes)
	return bundle, passphrase, 0
}

func (s *Server) restoreSessionStillActive(r *http.Request, expected auth.Session) bool {
	if s == nil || s.auth == nil {
		return false
	}
	current, ok := s.auth.SessionFromRequest(r)
	return ok && current.CSRFToken == expected.CSRFToken
}

func isRestoreBodyTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr) || strings.Contains(strings.ToLower(err.Error()), "request body too large")
}

func writeRestoreUploadError(w http.ResponseWriter, status int) {
	switch status {
	case http.StatusUnsupportedMediaType:
		writeError(w, status, "multipart/form-data required")
	case http.StatusRequestEntityTooLarge:
		writeError(w, status, "restore upload exceeds allowed size")
	default:
		writeError(w, http.StatusBadRequest, "invalid restore upload")
	}
}

func writeRestoreJSONError(w http.ResponseWriter, err error) {
	if isRestoreBodyTooLarge(err) {
		writeError(w, http.StatusRequestEntityTooLarge, "restore request exceeds allowed size")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid restore request")
}

func writeRestorePreviewError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, restore.ErrUnavailable), errors.Is(err, restore.ErrRecoveryRequired),
		errors.Is(err, restore.ErrRecoveryFailed), errors.Is(err, restore.ErrAuthorityBusy):
		writeError(w, http.StatusServiceUnavailable, "restore unavailable")
	default:
		writeError(w, http.StatusBadRequest, "restore request rejected")
	}
}

func writeRestoreApplyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, restore.ErrPreviewExpired), errors.Is(err, restore.ErrPreviewStale),
		errors.Is(err, restore.ErrCompatibilityBlocked):
		writeError(w, http.StatusConflict, "restore preview is no longer applicable")
	case errors.Is(err, restore.ErrUnavailable), errors.Is(err, restore.ErrRecoveryRequired),
		errors.Is(err, restore.ErrRecoveryFailed), errors.Is(err, restore.ErrAuthorityBusy):
		writeError(w, http.StatusServiceUnavailable, "restore unavailable")
	case errors.Is(err, restore.ErrCandidateInvalid):
		writeError(w, http.StatusBadRequest, "restore candidate rejected")
	case errors.Is(err, restore.ErrInvalidMode), errors.Is(err, restore.ErrInvalidBundle),
		errors.Is(err, restore.ErrEncryptedBundleRequired):
		writeError(w, http.StatusBadRequest, "restore request rejected")
	case errors.Is(err, restore.ErrApplyFailed):
		writeError(w, http.StatusInternalServerError, "restore failed; previous generation restored")
	default:
		writeError(w, http.StatusInternalServerError, "restore failed")
	}
}
