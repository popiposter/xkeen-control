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
		Eligible: 46, Answered: 41, SpeedTested: 18, Valid: 15, ReviewStarted: true, Decision: "best-six", Applied: "applied", Geodata: "completed"}
	report.Pool = []MaintenanceNode{
		{Name: "<b>evil</b> & co", Country: "de", RTTMS: 48, DownBPS: 116e6 / 8, UpBPS: 34e6 / 8, Measured: true, Active: true},
		{Name: strings.Repeat("x", 60) + "\x07", Country: "1!", Measured: false},
	}
	alert := MaintenanceAlert(report)
	text := alert.text
	if !alert.html || len(text) > maxHTMLBytes {
		t.Fatal("report is not a bounded HTML alert", len(text))
	}
	for _, want := range []string{"✅ <b>Nightly maintenance completed</b>", "12 Oct 04:00 – 04:38 MSK", "2 refreshed, 1 changed", "41 of 46 nodes answered", "15 of 18 valid", "best six applied · applied and verified", "Geodata:</b> updated", "1. 🇩🇪 <b>&lt;b&gt;evil&lt;/b&gt; &amp; co</b>", "48 ms · ↓ 116 Mbps · ↑ 34 Mbps", "— <i>not measured</i>"} {
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
