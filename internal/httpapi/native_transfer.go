package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/popiposter/xkeen-control/internal/auth"
	"github.com/popiposter/xkeen-control/internal/backup"
	"github.com/popiposter/xkeen-control/internal/nativebackup"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

func (s *Server) handleNativeTransfer(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireSession(w, r)
	if !ok {
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
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		writeError(w, http.StatusBadRequest, "invalid transfer request")
		return
	}
	if s.nativeTransfer == nil {
		writeError(w, http.StatusServiceUnavailable, "native transfer unavailable")
		return
	}
	if r.URL.Path == "/api/v1/xkeen/transfer/preview" {
		// One upload in RAM at a time; gate acquired before reading/decrypting.
		release, admitted := s.tryRestorePreview()
		if !admitted {
			writeError(w, http.StatusConflict, "transfer preview busy")
			return
		}
		defer release()
		archive, passphrase, mapping, status := parseNativeTransferUpload(w, r)
		if status != 0 {
			writeError(w, status, "invalid or oversized encrypted transfer upload")
			return
		}
		defer clearBytes(archive)
		if !s.restoreSessionStillActive(r, session) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		preview, err := s.nativeTransfer.Preview(r.Context(), session.CSRFToken, archive, passphrase, mapping)
		if err != nil {
			writeNativeTransferError(w, err)
			return
		}
		if !s.restoreSessionStillActive(r, session) {
			s.nativeTransfer.Cancel(session.CSRFToken, preview.Token)
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		writeJSON(w, http.StatusOK, preview)
		return
	}
	allowed := map[string]bool{"token": true}
	if r.URL.Path == "/api/v1/xkeen/transfer/stage" {
		allowed["nativeSettingsChecked"] = true
	}
	if r.Header.Get("Content-Type") != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "application/json required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	defer r.Body.Close()
	fields, err := transferFields(json.NewDecoder(r.Body), allowed)
	var token string
	if err != nil || json.Unmarshal(fields["token"], &token) != nil || len(token) != 32 || strings.Trim(token, "0123456789abcdef") != "" {
		writeError(w, http.StatusBadRequest, "invalid transfer token request")
		return
	}
	if !s.restoreSessionStillActive(r, session) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if r.URL.Path == "/api/v1/xkeen/transfer/cancel" {
		s.nativeTransfer.Cancel(session.CSRFToken, token)
		writeJSON(w, http.StatusOK, map[string]bool{"canceled": true})
		return
	}
	var checked bool
	if json.Unmarshal(fields["nativeSettingsChecked"], &checked) != nil || !checked {
		writeError(w, http.StatusBadRequest, "destination native settings acknowledgement required")
		return
	}
	digest, err := s.nativeTransfer.Stage(r.Context(), session.CSRFToken, token, checked)
	if err != nil {
		writeNativeTransferError(w, err)
		return
	}
	// Stage saves data only. Restart is an explicit existing editor operation.
	writeJSON(w, http.StatusOK, map[string]any{"digest": digest, "saved": true, "restartRequired": true})
}

func transferFields(d *json.Decoder, allowed map[string]bool) (map[string]json.RawMessage, error) {
	invalid := errors.New("invalid transfer fields")
	t, err := d.Token()
	if err != nil || t != json.Delim('{') {
		return nil, invalid
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		t, err := d.Token()
		key, ok := t.(string)
		if err != nil || !ok || !allowed[key] || fields[key] != nil {
			return nil, invalid
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(value, []byte("null")) {
			return nil, invalid
		}
		fields[key] = value
	}
	t, err = d.Token()
	if err != nil || t != json.Delim('}') || d.Decode(&struct{}{}) != io.EOF || len(fields) != len(allowed) {
		return nil, invalid
	}
	return fields, nil
}

func parseNativeTransferUpload(w http.ResponseWriter, r *http.Request) (archive []byte, passphrase string, mapping map[string]string, status int) {
	if r.ContentLength > maxRestoreRequestBody {
		return nil, "", nil, http.StatusRequestEntityTooLarge
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "multipart/form-data" || len(params) != 1 || params["boundary"] == "" {
		return nil, "", nil, http.StatusBadRequest
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRestoreRequestBody)
	defer r.Body.Close()
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, "", nil, http.StatusBadRequest
	}
	parts := map[string][]byte{}
	defer func() {
		for _, value := range parts {
			clearBytes(value)
		}
		if status != 0 {
			clearBytes(archive)
			archive = nil
		}
	}()
	limits := map[string]int64{"bundle": backup.MaxEncryptedEnvelope, "passphrase": backup.MaxPassphraseBytes, "mapping": 16 << 10}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			if isRestoreBodyTooLarge(err) {
				status = http.StatusRequestEntityTooLarge
			} else {
				status = http.StatusBadRequest
			}
			return
		}
		name := part.FormName()
		limit, valid := limits[name]
		if !valid || parts[name] != nil || len(parts) >= 3 || name != "bundle" && part.FileName() != "" {
			status = http.StatusBadRequest
			return
		}
		data, err := io.ReadAll(io.LimitReader(part, limit+1))
		parts[name] = data
		if int64(len(data)) > limit || isTransferOversize(err) {
			status = http.StatusRequestEntityTooLarge
			return
		}
		if err != nil {
			status = http.StatusBadRequest
			return
		}
	}
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		if isRestoreBodyTooLarge(err) {
			status = http.StatusRequestEntityTooLarge
		} else {
			status = http.StatusBadRequest
		}
		return
	}
	if len(parts["bundle"]) == 0 || backup.ValidatePassphrase(string(parts["passphrase"])) != nil {
		status = http.StatusBadRequest
		return
	}
	mapping = map[string]string{}
	if data, exists := parts["mapping"]; exists {
		d := json.NewDecoder(bytes.NewReader(data))
		t, err := d.Token()
		if err != nil || t != json.Delim('{') {
			status = http.StatusBadRequest
			return
		}
		for d.More() {
			t, err := d.Token()
			key, ok := t.(string)
			_, duplicate := mapping[key]
			if err != nil || !ok || duplicate || len(mapping) >= 64 || len(key) == 0 || len(key) > 64 {
				status = http.StatusBadRequest
				return
			}
			var value string
			if d.Decode(&value) != nil || len(value) == 0 || len(value) > 64 {
				status = http.StatusBadRequest
				return
			}
			mapping[key] = value
		}
		t, err = d.Token()
		if err != nil || t != json.Delim('}') || d.Decode(&struct{}{}) != io.EOF {
			status = http.StatusBadRequest
			return
		}
	}
	archive = parts["bundle"]
	delete(parts, "bundle")
	passphrase = string(parts["passphrase"])
	return
}

func isTransferOversize(err error) bool { return err != nil && isRestoreBodyTooLarge(err) }

func writeNativeTransferError(w http.ResponseWriter, err error) {
	var validation *xkeen.ValidationError
	switch {
	case errors.As(err, &validation):
		writePrivateConfigJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "Xray validation failed", "diagnostic": validation})
	case errors.Is(err, backup.ErrBusy), errors.Is(err, nativebackup.ErrPreview):
		writeError(w, http.StatusConflict, "transfer preview unavailable or changed; inspect and preview again")
	case errors.Is(err, backup.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "native transfer unavailable")
	default:
		writeError(w, http.StatusBadRequest, "transfer rejected; inspect configuration before another action")
	}
}
