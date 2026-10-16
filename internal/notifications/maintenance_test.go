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

func TestMaintenanceAlertEscapesNamesAndUsesClosedCodes(t *testing.T) {
	start := time.Date(2026, 10, 12, 4, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	report := MaintenanceReport{StartedAt: start, FinishedAt: start.Add(38 * time.Minute), Subscriptions: 2, SubscriptionsUpdated: 1,
		Eligible: 46, Answered: 41, SpeedTested: 18, Valid: 15, ReviewStarted: true, Decision: "best-pool", Applied: "applied", Geodata: "completed"}
	report.Pool = []MaintenanceNode{
		{Name: "🇩🇪 <b>evil</b> & co", RTTMS: 48, DownBPS: 116e6 / 8, UpBPS: 34e6 / 8, Measured: true},
		{Name: strings.Repeat("x", 60) + "\x07", Measured: false},
	}
	alert := MaintenanceAlert(report)
	text := alert.text
	if !alert.html || len(text) > maxHTMLBytes {
		t.Fatal("report is not a bounded HTML alert", len(text))
	}
	for _, want := range []string{"✅ <b>Nightly maintenance completed</b>", "12 Oct 04:00 – 04:38 MSK", "2 refreshed, 1 changed", "41 of 46 nodes answered", "15 of 18 valid", "best measured pool applied · applied and verified", "Geodata:</b> updated", "1. <b>🇩🇪 &lt;b&gt;evil&lt;/b&gt; &amp; co</b>", "48 ms · ↓ 116 Mbps · ↑ 34 Mbps", "— <i>not measured</i>"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in\n%s", want, text)
		}
	}
	if strings.Contains(text, "<b>evil") || strings.Contains(text, "\x07") || strings.Contains(text, strings.Repeat("x", 41)) {
		t.Fatal("unescaped or unbounded name", text)
	}
	report.Geodata, report.Decision = "rm -rf", "something"
	text = MaintenanceAlert(report).text
	if !strings.Contains(text, "⚠️") || strings.Contains(text, "rm -rf") || strings.Contains(text, "something") {
		t.Fatal("open code leaked or failure not flagged", text)
	}
	many := report
	many.Pool = make([]MaintenanceNode, 20)
	for i := range many.Pool {
		many.Pool[i] = MaintenanceNode{Name: strings.Repeat("y", 40), Measured: true, RTTMS: 999, DownBPS: 1e9, UpBPS: 1e9}
	}
	if text = MaintenanceAlert(many).text; len(text) > maxHTMLBytes || !strings.Contains(text, "… and 12 more") {
		t.Fatal("pool list not bounded", len(text))
	}
}

func TestMaintenanceAlertIsSentWithHTMLParseMode(t *testing.T) {
	s := fixtureService(t)
	if _, err := s.Configure(fixtureToken, fixtureChat); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	alert := MaintenanceAlert(MaintenanceReport{ReviewStarted: true, Decision: "pool-unchanged", Geodata: "completed"})
	s.transport.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(r.Body)
		var body map[string]string
		json.Unmarshal(data, &body)
		if len(body) != 3 || body["parse_mode"] != "HTML" || body["text"] != alert.text {
			t.Fatal("report not sent as HTML", body)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: http.Header{}}, nil
	})
	if err := s.Send(context.Background(), alert); err != nil {
		t.Fatal(err)
	}
}

// The nightly report waits for a send or configuration change holding the
// lock instead of being dropped.
func TestSendReportWaitsForTheLock(t *testing.T) {
	s := fixtureService(t)
	if _, err := s.Configure(fixtureToken, fixtureChat); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	sends := 0
	s.transport.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		sends++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: http.Header{}}, nil
	})
	alert := MaintenanceAlert(MaintenanceReport{ReviewStarted: true, Decision: "pool-unchanged", Geodata: "completed"})
	s.mu.Lock()
	if err := s.Send(context.Background(), alert); err == nil {
		t.Fatal("plain send did not refuse a held lock")
	}
	go func() { time.Sleep(1200 * time.Millisecond); s.mu.Unlock() }()
	if err := s.SendReport(context.Background(), alert); err != nil || sends != 1 {
		t.Fatal("report dropped on a held lock", err, sends)
	}
}

func TestMaintenanceAlertShowsStepsTurnedOff(t *testing.T) {
	text := MaintenanceAlert(MaintenanceReport{SubscriptionsOff: true, ReviewOff: true, Geodata: "disabled"}).text
	for _, want := range []string{"✅", "Subscriptions:</b> off in settings", "Review:</b> off in settings", "Geodata:</b> off in settings"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in\n%s", want, text)
		}
	}
	text = MaintenanceAlert(MaintenanceReport{ReviewStarted: true, Recommended: true, Applied: "not-attempted", Geodata: "completed"}).text
	if !strings.Contains(text, "recommendation left for Apply in the panel") || !strings.Contains(text, "✅") {
		t.Fatal("recommend-only night", text)
	}
	text = MaintenanceAlert(MaintenanceReport{ReviewStarted: true, Recommended: true, RecommendationCurrent: true, Applied: "not-attempted", Geodata: "completed"}).text
	if !strings.Contains(text, "the running pool is already the best; nothing to apply") || strings.Contains(text, "left for Apply") {
		t.Fatal("unchanged recommendation reported as pending Apply", text)
	}
	// A review that started but did not complete is never a green night.
	text = MaintenanceAlert(MaintenanceReport{ReviewStarted: true, Applied: "not-attempted", Geodata: "completed"}).text
	if !strings.Contains(text, "⚠️") || strings.Contains(text, "recommendation left") {
		t.Fatal("failed review reported green", text)
	}
}

func TestRecoveryAlertNamesTheRemovedMemberEscaped(t *testing.T) {
	at := time.Date(2026, 10, 16, 10, 40, 0, 0, time.FixedZone("MSK", 3*3600))
	alert := RecoveryAlert(RecoveryReport{At: at, Removed: "🇫🇮 <i>x</i>", Pool: []string{"🇳🇱 a & b", "🇩🇪 c"}})
	text := alert.text
	if !alert.html || len(text) > maxHTMLBytes {
		t.Fatal("recovery alert is not a bounded HTML alert", len(text))
	}
	for _, want := range []string{"🛠 <b>Pool recovered</b>", "16 Oct 10:40 MSK", "<b>🇫🇮 &lt;i&gt;x&lt;/i&gt;</b> passed Xray's health check but failed 3 targeted probes", "keep their measured ranking", "1. <b>🇳🇱 a &amp; b</b>", "2. <b>🇩🇪 c</b>"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in\n%s", want, text)
		}
	}
	if text = RecoveryAlert(RecoveryReport{Replaced: true, Provisional: true, Pool: []string{"n"}}).text; strings.Contains(text, "⛔") || !strings.Contains(text, "a provisional pool of nodes that answered was applied") {
		t.Fatal("provisional recovery text", text)
	}
	// Removing a member from an older provisional pool is not "measured".
	if text = RecoveryAlert(RecoveryReport{Removed: "n", Provisional: true}).text; strings.Contains(text, "measured ranking") || !strings.Contains(text, "stay in the provisional pool") {
		t.Fatal("removal from a provisional pool", text)
	}
}
