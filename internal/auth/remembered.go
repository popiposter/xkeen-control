package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"time"
)

type rememberedSession struct {
	CSRF    string    `json:"csrf"`
	Expires time.Time `json:"expires"`
}
type rememberedStore struct {
	Authority string                       `json:"authority"`
	Secure    bool                         `json:"secure"`
	Audience  string                       `json:"audience"`
	Sessions  map[string]rememberedSession `json:"sessions"`
}

func (m *Manager) sessionKey(token string) string {
	if m.config.SessionPath == "" {
		return token
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func (m *Manager) sessionAuthority() (string, error) {
	hash, err := readPasswordAuthority(m.config.HashPath)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(hash)
	return hex.EncodeToString(sum[:]), nil
}
func (m *Manager) loadSessions() {
	if m.config.SessionPath == "" {
		return
	}
	info, err := os.Lstat(m.config.SessionPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 32768 {
		return
	}
	file, err := os.Open(m.config.SessionPath)
	if err != nil {
		return
	}
	defer file.Close()
	var store rememberedStore
	decoder := json.NewDecoder(io.LimitReader(file, 32769))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&store) != nil || decoder.Decode(new(any)) != io.EOF || len(store.Sessions) > maxSessions || store.Secure != m.config.SecureCookies || store.Audience != m.config.SessionAudience {
		return
	}
	authority, err := m.sessionAuthority()
	if err != nil || store.Authority != authority {
		return
	}
	now := m.config.Now()
	for key, value := range store.Sessions {
		raw, err := hex.DecodeString(key)
		csrf, csrfErr := base64.RawURLEncoding.DecodeString(value.CSRF)
		if err != nil || len(raw) != 32 || csrfErr != nil || len(csrf) != 32 || !now.Before(value.Expires) || value.Expires.After(now.Add(m.config.SessionTTL)) {
			continue
		}
		m.sessions[key] = session{csrfToken: value.CSRF, expiresAt: value.Expires}
	}
}
func (m *Manager) saveSessions() error {
	if m.config.SessionPath == "" {
		return nil
	}
	authority, err := m.sessionAuthority()
	if err != nil {
		return err
	}
	store := rememberedStore{Authority: authority, Secure: m.config.SecureCookies, Audience: m.config.SessionAudience, Sessions: make(map[string]rememberedSession)}
	for key, value := range m.sessions {
		store.Sessions[key] = rememberedSession{CSRF: value.csrfToken, Expires: value.expiresAt}
	}
	data, err := json.Marshal(store)
	if err != nil {
		return err
	}
	return writeProtectedAtomic(m.config.SessionPath, append(data, '\n'))
}
