package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

const (
	SessionCookieName = "xkeen_session"
	CSRFHeader        = "X-CSRF-Token"
	PasswordHashPath  = "/opt/etc/xkeen-control/auth/password.bcrypt"
	passwordCost      = 12
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrLocked             = errors.New("temporarily locked")
	ErrNotConfigured      = errors.New("authentication is not configured")
	ErrInvalidPassword    = errors.New("password must be 12 to 72 bytes")
)

type Config struct {
	HashPath      string
	SecureCookies bool
	SessionTTL    time.Duration
	LockoutAfter  int
	LockoutFor    time.Duration
	Now           func() time.Time
}

type Manager struct {
	config   Config
	mu       sync.Mutex
	sessions map[string]session
	attempts map[string]attempt
}

type session struct {
	csrfToken string
	expiresAt time.Time
}

type Session struct {
	CSRFToken string
	ExpiresAt time.Time
}

type attempt struct {
	failures    int
	firstAt     time.Time
	lockedUntil time.Time
}

func NewManager(config Config) *Manager {
	if config.HashPath == "" {
		config.HashPath = PasswordHashPath
	}
	if config.SessionTTL <= 0 {
		config.SessionTTL = 8 * time.Hour
	}
	if config.LockoutAfter <= 0 {
		config.LockoutAfter = 5
	}
	if config.LockoutFor <= 0 {
		config.LockoutFor = 10 * time.Minute
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Manager{config: config, sessions: make(map[string]session), attempts: make(map[string]attempt)}
}

func (m *Manager) Login(remoteIP, password string) (Session, string, error) {
	now := m.config.Now()
	remoteIP = boundedRemoteIP(remoteIP)
	m.mu.Lock()
	current := m.attempts[remoteIP]
	if now.Before(current.lockedUntil) {
		m.mu.Unlock()
		return Session{}, "", ErrLocked
	}
	m.mu.Unlock()

	hash, err := os.ReadFile(m.config.HashPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Session{}, "", ErrNotConfigured
		}
		return Session{}, "", ErrNotConfigured
	}
	if bcrypt.CompareHashAndPassword(bytesTrimSpace(hash), []byte(password)) != nil {
		m.recordFailure(remoteIP, now)
		return Session{}, "", ErrInvalidCredentials
	}

	token, err := randomToken(32)
	if err != nil {
		return Session{}, "", ErrNotConfigured
	}
	csrf, err := randomToken(32)
	if err != nil {
		return Session{}, "", ErrNotConfigured
	}
	s := session{csrfToken: csrf, expiresAt: now.Add(m.config.SessionTTL)}
	m.mu.Lock()
	delete(m.attempts, remoteIP)
	m.sessions[token] = s
	m.mu.Unlock()
	return Session{CSRFToken: csrf, ExpiresAt: s.expiresAt}, token, nil
}

// The random session identifier is passed only to the HttpOnly cookie setter;
// the API never returns it.
func (m *Manager) SetSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		Secure:   m.config.SecureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

func (m *Manager) SessionFromRequest(r *http.Request) (Session, bool) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return Session{}, false
	}
	now := m.config.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.sessions[cookie.Value]
	if !ok || !now.Before(value.expiresAt) {
		if ok {
			delete(m.sessions, cookie.Value)
		}
		return Session{}, false
	}
	return Session{CSRFToken: value.csrfToken, ExpiresAt: value.expiresAt}, true
}

func (m *Manager) Logout(r *http.Request) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return
	}
	m.mu.Lock()
	delete(m.sessions, cookie.Value)
	m.mu.Unlock()
}

func (m *Manager) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.config.SecureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}

func ValidateCSRF(r *http.Request, session Session) bool {
	provided := r.Header.Get(CSRFHeader)
	if provided == "" || session.CSRFToken == "" || len(provided) != len(session.CSRFToken) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(session.CSRFToken)) == 1
}

func (m *Manager) OriginAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
			origin = referer
		} else {
			return true
		}
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || parsed.Host != r.Host {
		return false
	}
	expectedScheme := "http"
	if m.config.SecureCookies {
		expectedScheme = "https"
	}
	return strings.EqualFold(parsed.Scheme, expectedScheme)
}

func (m *Manager) recordFailure(remoteIP string, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value := m.attempts[remoteIP]
	if value.firstAt.IsZero() || now.Sub(value.firstAt) > m.config.LockoutFor {
		value = attempt{firstAt: now}
	}
	value.failures++
	if value.failures >= m.config.LockoutAfter {
		value.lockedUntil = now.Add(m.config.LockoutFor)
	}
	m.attempts[remoteIP] = value
}

func SetPassword(path string, password []byte) error {
	if len(password) < 12 || len(password) > 72 {
		return ErrInvalidPassword
	}
	hash, err := bcrypt.GenerateFromPassword(password, passwordCost)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data := append([]byte(nil), hash...)
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func RunPasswordCommand(path, action string, in *os.File, out io.Writer) error {
	if action != "init" && action != "change" {
		return errors.New("password action must be init or change")
	}
	if action == "init" {
		if _, err := os.Stat(path); err == nil {
			return errors.New("password is already initialized; use change")
		} else if !errors.Is(err, os.ErrNotExist) {
			return errors.New("cannot inspect password hash")
		}
	}
	if in == nil || out == nil || !term.IsTerminal(int(in.Fd())) {
		return errors.New("password setup requires an interactive terminal")
	}
	fmt.Fprint(out, "New password: ")
	first, err := term.ReadPassword(int(in.Fd()))
	fmt.Fprintln(out)
	if err != nil {
		return err
	}
	fmt.Fprint(out, "Repeat password: ")
	second, err := term.ReadPassword(int(in.Fd()))
	fmt.Fprintln(out)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(first, second) != 1 {
		return errors.New("passwords do not match")
	}
	if err := SetPassword(path, first); err != nil {
		return err
	}
	fmt.Fprintln(out, "Password hash updated.")
	return nil
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func boundedRemoteIP(value string) string {
	if len(value) > 128 {
		return value[:128]
	}
	if value == "" {
		return "unknown"
	}
	return value
}

func bytesTrimSpace(value []byte) []byte { return bytes.TrimSpace(value) }
