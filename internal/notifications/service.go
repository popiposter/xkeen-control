package notifications

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// Error is a closed safe code; native errors and provider bodies are discarded.
type Error string

func (e Error) Error() string { return string(e) }
func (e Error) NotificationState() string {
	switch e {
	case "unconfigured", "disabled":
		return string(e)
	default:
		return "failed"
	}
}

type Status struct {
	Provider        string     `json:"provider"`
	Configured      bool       `json:"configured"`
	Enabled         bool       `json:"enabled"`
	AuthorityState  string     `json:"authorityState"`
	ReasonCode      string     `json:"reasonCode,omitempty"`
	DeliveryState   string     `json:"deliveryState"`
	LastAttemptAt   *time.Time `json:"lastAttemptAt,omitempty"`
	LastDeliveredAt *time.Time `json:"lastDeliveredAt,omitempty"`
	ErrorCode       string     `json:"errorCode,omitempty"`
}

// Alert is opaque outside this package. Only the three closed constructors can
// produce text; API callers cannot supply arbitrary remote message content.
type Alert struct {
	text string
	test bool
}

var digestGrammar = regexp.MustCompile(`^[a-f0-9]{64}$`)
var versionGrammar = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?(?:\+[A-Za-z0-9.-]+)?$`)

func ComponentAlert(component, channel, identity, state string, checked time.Time) Alert {
	if component != "xray" && component != "xkeen" && component != "geodata" {
		return Alert{}
	}
	if channel != "stable" && channel != "dev" {
		return Alert{}
	}
	if (component == "xkeen") != (channel == "dev") {
		return Alert{}
	}
	if state != "update-available" && state != "changed" && state != "not-installed" {
		return Alert{}
	}
	if !digestGrammar.MatchString(identity) {
		return Alert{}
	}
	return Alert{text: "xkeen-control: " + component + " " + channel + " " + state + "\nCandidate " + identity[:12] + "\nChecked " + checked.UTC().Format(time.RFC3339)}
}
func PanelAlert(version, identity string, checked time.Time) Alert {
	if len(version) > 64 || !versionGrammar.MatchString(version) || !digestGrammar.MatchString(identity) {
		return Alert{}
	}
	return Alert{text: "xkeen-control: panel stable update available " + version + "\nCandidate " + identity[:12] + "\nChecked " + checked.UTC().Format(time.RFC3339)}
}
func TestAlert() Alert { return Alert{text: "xkeen-control: outbound notification test", test: true} }

type Service struct {
	mu        sync.Mutex
	path      string
	transport *telegram
	status    Status
}

func NewService() *Service { return &Service{path: DefaultPath, transport: newTelegram()} }

// NewServiceForTest selects only a synthetic local authority, never a provider URL.
func NewServiceForTest(path string) *Service { return &Service{path: path, transport: newTelegram()} }

func (s *Service) statusLocked() Status {
	value, state := readAuthority(s.path)
	status := s.status
	status.Provider = "telegram"
	status.AuthorityState = state
	status.Configured = state == "configured"
	status.Enabled = status.Configured && value.Enabled
	status.ReasonCode = ""
	if state == "unavailable" {
		status.ReasonCode = "authority-unavailable"
	}
	if status.DeliveryState == "" {
		status.DeliveryState = "idle"
	}
	return status
}
func (s *Service) Status() Status { s.mu.Lock(); defer s.mu.Unlock(); return s.statusLocked() }
func (s *Service) Configure(token, chat string) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validCredentials(token, chat) {
		return s.statusLocked(), Error("invalid-request")
	}
	err := writeAuthority(s.path, authority{SchemaVersion: 1, Provider: "telegram", BotToken: token, ChatID: chat})
	if err == nil {
		s.status = Status{}
	}
	return s.statusLocked(), err
}
func (s *Service) SetEnabled(enabled bool) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, state := readAuthority(s.path)
	if state != "configured" {
		return s.statusLocked(), Error(state)
	}
	value.Enabled = enabled
	err := writeAuthority(s.path, value)
	return s.statusLocked(), err
}
func (s *Service) Clear() (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, state := readAuthority(s.path); state == "unconfigured" {
		s.status = Status{}
		return s.statusLocked(), nil
	}
	info, err := os.Lstat(s.path)
	if err != nil || !protected(info, false) || !safeDirectory(filepath.Dir(s.path), false) {
		return s.statusLocked(), Error("authority-unavailable")
	}
	if os.Remove(s.path) != nil {
		return s.statusLocked(), Error("authority-unavailable")
	}
	err = syncDirectory(filepath.Dir(s.path))
	s.status = Status{}
	return s.statusLocked(), err
}
func (s *Service) Send(ctx context.Context, alert Alert) error {
	// Serialize configure/clear/disable with delivery so a returned disable or
	// clear cannot leave a new send running with stale credentials.
	if !s.mu.TryLock() {
		return Error("transport-failed")
	}
	defer s.mu.Unlock()
	value, state := readAuthority(s.path)
	if state != "configured" {
		return Error(state)
	}
	if !value.Enabled && !alert.test {
		return Error("disabled")
	}
	if alert.text == "" {
		return Error("provider-rejected")
	}
	now := time.Now().UTC()
	s.status.LastAttemptAt = &now
	err := s.transport.send(ctx, value, alert.text)
	if err == nil {
		s.status.DeliveryState = "delivered"
		s.status.LastDeliveredAt = &now
		s.status.ErrorCode = ""
	} else {
		s.status.DeliveryState = "failed"
		s.status.ErrorCode = err.Error()
	}
	return err
}
func (s *Service) Test(ctx context.Context) (Status, error) {
	err := s.Send(ctx, TestAlert())
	return s.Status(), err
}
