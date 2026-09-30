// Package notifications owns only fixed-provider outbound alerts and their
// separate panel-local secret authority. It has no inbound command surface.
package notifications

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
)

const DefaultPath = "/opt/etc/xkeen-control/secrets/notifications.json"
const MaxAuthorityBytes = 4 << 10

var tokenGrammar = regexp.MustCompile(`^[1-9][0-9]{0,19}:[A-Za-z0-9_-]{20,128}$`)
var chatGrammar = regexp.MustCompile(`^-?[1-9][0-9]{0,19}$`)

type authority struct {
	SchemaVersion int    `json:"schemaVersion"`
	Provider      string `json:"provider"`
	Enabled       bool   `json:"enabled"`
	BotToken      string `json:"botToken"`
	ChatID        string `json:"chatId"`
}

// DecodeObject enforces exact, case-sensitive fields, including required fields
// and duplicates. No native decoder error can carry submitted secret material.
func DecodeObject(data []byte, fields ...string) (map[string]json.RawMessage, error) {
	invalid := errors.New("invalid-request")
	if len(data) > MaxAuthorityBytes {
		return nil, invalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return nil, invalid
	}
	allowed := make(map[string]bool, len(fields))
	for _, f := range fields {
		allowed[f] = true
	}
	result := make(map[string]json.RawMessage, len(fields))
	for d.More() {
		k, err := d.Token()
		name, ok := k.(string)
		if err != nil || !ok || !allowed[name] {
			return nil, invalid
		}
		if _, exists := result[name]; exists {
			return nil, invalid
		}
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return nil, invalid
		}
		result[name] = v
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') || len(result) != len(fields) {
		return nil, invalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, invalid
	}
	return result, nil
}

func validCredentials(token, chat string) bool {
	return tokenGrammar.MatchString(token) && chatGrammar.MatchString(chat)
}

func protected(info os.FileInfo, directory bool) bool {
	if info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	if info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return false
	}
	if directory {
		if !info.IsDir() {
			return false
		}
	} else if !info.Mode().IsRegular() {
		return false
	}
	if runtime.GOOS != "windows" {
		mode := os.FileMode(0o600)
		if directory {
			mode = 0o700
		}
		if info.Mode().Perm() != mode || !rootOwned(info) {
			return false
		}
	}
	return true
}

func safeDirectory(path string, create bool) bool {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && create {
		if os.MkdirAll(path, 0o700) != nil {
			return false
		}
		info, err = os.Lstat(path)
	}
	return err == nil && protected(info, true)
}

func readAuthority(path string) (authority, string) {
	dir := filepath.Dir(path)
	if !safeDirectory(dir, false) {
		if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
			return authority{}, "unconfigured"
		}
		return authority{}, "unavailable"
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return authority{}, "unconfigured"
	}
	if err != nil || !protected(info, false) || info.Size() > MaxAuthorityBytes {
		return authority{}, "unavailable"
	}
	file, err := openAuthority(path)
	if err != nil {
		return authority{}, "unavailable"
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !protected(opened, false) || opened.Size() > MaxAuthorityBytes || opened.Size() != info.Size() || !opened.ModTime().Equal(info.ModTime()) {
		return authority{}, "unavailable"
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxAuthorityBytes+1))
	after, statErr := os.Lstat(path)
	if err != nil || statErr != nil || !os.SameFile(opened, after) || after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) || !protected(after, false) || !safeDirectory(dir, false) {
		return authority{}, "unavailable"
	}
	if _, err := DecodeObject(data, "schemaVersion", "provider", "enabled", "botToken", "chatId"); err != nil {
		return authority{}, "unavailable"
	}
	var value authority
	if json.Unmarshal(data, &value) != nil || value.SchemaVersion != 1 || value.Provider != "telegram" || !validCredentials(value.BotToken, value.ChatID) {
		return authority{}, "unavailable"
	}
	return value, "configured"
}

func writeAuthority(path string, value authority) error {
	dir := filepath.Dir(path)
	if !safeDirectory(dir, true) {
		return Error("authority-unavailable")
	}
	if info, err := os.Lstat(path); err == nil {
		if !protected(info, false) {
			return Error("authority-unavailable")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Error("authority-unavailable")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return Error("authority-unavailable")
	}
	file, err := os.CreateTemp(dir, ".notifications-*")
	if err != nil {
		return Error("authority-unavailable")
	}
	defer os.Remove(file.Name())
	if file.Chmod(0o600) != nil {
		file.Close()
		return Error("authority-unavailable")
	}
	_, err = file.Write(append(data, '\n'))
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil || !safeDirectory(dir, false) || os.Rename(file.Name(), path) != nil {
		return Error("authority-unavailable")
	}
	return syncDirectory(dir)
}

func syncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return Error("authority-unavailable")
	}
	defer dir.Close()
	if dir.Sync() != nil {
		return Error("authority-unavailable")
	}
	return nil
}
