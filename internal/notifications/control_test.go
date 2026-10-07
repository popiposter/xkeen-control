package notifications

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func updateFixture(t *testing.T, id int64, text string) botUpdate {
	t.Helper()
	var update botUpdate
	data, _ := json.Marshal(map[string]any{"update_id": id, "message": map[string]any{"date": time.Now().Unix(), "text": text, "from": map[string]any{"id": 12345, "is_bot": false}, "chat": map[string]any{"id": -1234567890123}}})
	if json.Unmarshal(data, &update) != nil {
		t.Fatal("fixture")
	}
	return update
}
func controlFixture(t *testing.T) (*Service, authority, uint64) {
	s := fixtureService(t)
	if _, err := s.Configure(fixtureToken, fixtureChat); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetControl(true, "12345"); err != nil {
		t.Fatal(err)
	}
	s.transport.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
	})
	value, _ := readAuthority(s.path)
	return s, value, s.controlEpoch
}
func TestControlAuthenticatesFreshExactCommands(t *testing.T) {
	_, value, _ := controlFixture(t)
	cases := []struct {
		name   string
		change func(*botUpdate)
	}{
		{"foreign-user", func(u *botUpdate) { u.Message.From.ID++ }},
		{"foreign-chat", func(u *botUpdate) { u.Message.Chat.ID++ }},
		{"bot", func(u *botUpdate) { u.Message.From.Bot = true }},
		{"old", func(u *botUpdate) { u.Message.Date -= 121 }},
		{"future", func(u *botUpdate) { u.Message.Date += 60 }},
		{"forward", func(u *botUpdate) { u.Message.Forward = json.RawMessage(`{}`) }},
		{"anonymous", func(u *botUpdate) { u.Message.SenderChat = json.RawMessage(`{}`) }},
		{"args", func(u *botUpdate) { u.Message.Text = "/restart now" }},
		{"shell", func(u *botUpdate) { u.Message.Text = "/restart;reboot" }},
		{"missing", func(u *botUpdate) { u.Message = nil }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			u := updateFixture(t, 10, "/restart")
			test.change(&u)
			if botCommand(u, value, time.Now()) != "" {
				t.Fatal("unauthorized command")
			}
		})
	}
	if botCommand(updateFixture(t, 10, "/refresh"), value, time.Now()) != RefreshSubscriptions {
		t.Fatal("fixed refresh unavailable")
	}
}

func TestNativeUpdatesExplainLocalConfirmationWithoutPromisingSuccess(t *testing.T) {
	for _, command := range []Command{UpdateXkeen, UpdateXray, UpdateGeodata} {
		text := controlText(command, Accepted)
		if !strings.Contains(text, "panel console") || !strings.Contains(text, "Telegram cannot answer") || !strings.Contains(text, "unknown outcome") || !strings.Contains(text, "acceptance is not success") {
			t.Fatal(command, text)
		}
		if strings.Contains(text, "auto") || strings.Contains(text, "private") {
			t.Fatal(text)
		}
	}
	_, value, _ := controlFixture(t)
	for _, text := range []string{"1", "yes", "/update_xkeen auto", "/update_xkeen 1"} {
		if botCommand(updateFixture(t, 10, text), value, time.Now()) != "" {
			t.Fatal("prompt answer admitted", text)
		}
	}
}
func TestControlPersistsBeforeEffectAndNeverReplaysAcrossRestart(t *testing.T) {
	s, value, epoch := controlFixture(t)
	calls := 0
	handler := func(context.Context, Command) ControlResult {
		calls++
		saved, state := readAuthority(s.path)
		if state != "configured" || saved.LastUpdateID != 10 {
			t.Fatal("effect preceded durable watermark")
		}
		return Accepted
	}
	u := updateFixture(t, 10, "/restart")
	s.consume(context.Background(), value, epoch, u, handler)
	s.consume(context.Background(), value, epoch, u, handler)
	restarted := NewServiceForTest(s.path)
	restarted.consume(context.Background(), value, 0, u, handler)
	if calls != 1 {
		t.Fatal("replayed mutation", calls)
	}
	assertSafe(t, s.Status())
	if _, err := s.SetControl(false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetControl(true, "12345"); err != nil {
		t.Fatal(err)
	}
	s.consume(context.Background(), value, epoch, updateFixture(t, 11, "/restart"), handler)
	if calls != 1 {
		t.Fatal("old poll dispatched after disable/enable")
	}
	if _, err := s.Configure(fixtureToken, fixtureChat); err != nil || s.Status().ControlEnabled {
		t.Fatal("credential replacement retained remote authority")
	}
}
func TestControlProviderIsBoundedAndNeverLeaksTokenErrors(t *testing.T) {
	_, value, _ := controlFixture(t)
	transport := newTelegram()
	transport.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != telegramAddress || r.URL.Path != "/bot"+fixtureToken+"/getUpdates" {
			t.Fatal("unfixed provider")
		}
		data, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(data, &body)
		if body["offset"] != float64(-1) || body["limit"] != float64(16) || body["timeout"] != float64(0) {
			t.Fatal("unbounded poll")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", (256<<10)+1)))}, nil
	})
	if _, err := transport.updates(context.Background(), value, -1, true); err != Error("provider-rejected") {
		t.Fatal("oversized provider accepted", err)
	}
}

func TestControlAcceptsRandomLowerSequenceOnlyAfterOldCommandsExpire(t *testing.T) {
	s, value, epoch := controlFixture(t)
	value.LastUpdateID = 1000000
	value.LastMessageDate = time.Now().Add(-8 * 24 * time.Hour).Unix()
	if err := writeAuthority(s.path, value); err != nil {
		t.Fatal(err)
	}
	calls := 0
	handler := func(context.Context, Command) ControlResult { calls++; return Accepted }
	u := updateFixture(t, 10, "/restart")
	s.consume(context.Background(), value, epoch, u, handler)
	s.consume(context.Background(), value, epoch, u, handler)
	old := updateFixture(t, 1000001, "/restart")
	old.Message.Date = value.LastMessageDate
	s.consume(context.Background(), value, epoch, old, handler)
	if calls != 1 {
		t.Fatal("lower sequence lost or old command replayed", calls)
	}
}
