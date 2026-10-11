package notifications

import (
	"fmt"
	"html"
	"strings"
	"time"
	"unicode"
)

// MaintenanceReport is the closed summary of one nightly maintenance window.
// It carries counts, closed state codes and the running pool by display name
// and measured figures only: no endpoint, credential, URL or native output.
type MaintenanceReport struct {
	StartedAt, FinishedAt time.Time
	// Subscriptions counts refresh attempts in the window.
	Subscriptions, SubscriptionsUpdated, SubscriptionsFailed int
	// Eligible nodes were probed; Answered replied; SpeedTested were measured
	// and Valid of them produced a usable result.
	Eligible, Answered, SpeedTested, Valid int
	ReviewStarted                          bool
	// SubscriptionsOff and ReviewOff mark steps the operator turned off.
	SubscriptionsOff, ReviewOff bool
	// Recommended marks a recommend-only review that completed and left its
	// recommendation for Apply in the panel; RecommendationCurrent marks one
	// whose members already form the running pool.
	Recommended, RecommendationCurrent bool
	// Decision, Applied and Geodata are closed codes; anything else renders
	// as unknown.
	Decision, Applied, Geodata string
	Pool                       []MaintenanceNode
}

// MaintenanceNode is one running pool member. Name must be a report label
// that never derives from the endpoint (nodes.ReportLabel).
type MaintenanceNode struct {
	Name           string
	RTTMS          int64
	DownBPS, UpBPS float64
	Measured       bool
}

const maxMaintenanceNodes = 8

var maintenanceDecisions = map[string]string{
	"best-pool":                  "best measured pool applied",
	"pool-unchanged":             "unchanged, no restart needed",
	"observatory-repair":         "kept; health checks narrowed to it",
	"insufficient-valid-results": "kept: too few valid results",
}

var maintenanceApplied = map[string]string{
	"applied":             "applied and verified",
	"no-op":               "no restart needed",
	"not-applied":         "not applied",
	"inspection-required": "needs inspection in the panel",
}

var maintenanceGeodata = map[string]string{
	"completed":   "updated",
	"failed":      "update failed",
	"refused":     "refused (configuration pending or busy)",
	"unknown":     "outcome unknown; inspect the panel",
	"skipped":     "skipped",
	"unavailable": "unavailable",
	"canceled":    "cancelled",
	"disabled":    "off in settings",
}

// MaintenanceAlert renders the nightly report as Telegram HTML. Every value
// from outside the package is escaped; node names are bounded.
func MaintenanceAlert(r MaintenanceReport) Alert {
	reviewOK := r.ReviewOff || r.ReviewStarted && (r.Applied == "applied" || r.Applied == "no-op" || r.Recommended || r.Decision == "pool-unchanged")
	ok := reviewOK && r.SubscriptionsFailed == 0 && (r.Geodata == "completed" || r.Geodata == "disabled")
	var b strings.Builder
	if ok {
		b.WriteString("✅ <b>Nightly maintenance completed</b>\n")
	} else {
		b.WriteString("⚠️ <b>Nightly maintenance needs attention</b>\n")
	}
	if !r.StartedAt.IsZero() && !r.FinishedAt.IsZero() {
		fmt.Fprintf(&b, "<i>%s – %s</i>\n", r.StartedAt.Format("2 Jan 15:04"), r.FinishedAt.Format("15:04 MST"))
	}
	b.WriteString("\n")
	if r.SubscriptionsOff {
		b.WriteString("📥 <b>Subscriptions:</b> off in settings\n")
	} else {
		fmt.Fprintf(&b, "📥 <b>Subscriptions:</b> %d refreshed", r.Subscriptions)
		if r.SubscriptionsUpdated > 0 {
			fmt.Fprintf(&b, ", %d changed", r.SubscriptionsUpdated)
		}
		if r.SubscriptionsFailed > 0 {
			fmt.Fprintf(&b, ", %d failed", r.SubscriptionsFailed)
		}
		b.WriteString("\n")
	}
	if r.ReviewOff {
		b.WriteString("📡 <b>Review:</b> off in settings\n")
	} else if r.ReviewStarted {
		fmt.Fprintf(&b, "📡 <b>Probe:</b> %d of %d nodes answered\n", r.Answered, r.Eligible)
		fmt.Fprintf(&b, "⚡ <b>Speed test:</b> %d of %d valid\n", r.Valid, r.SpeedTested)
		if r.Recommended && r.RecommendationCurrent {
			b.WriteString("🔁 <b>Pool:</b> the running pool is already the best; nothing to apply")
		} else if r.Recommended {
			b.WriteString("🔁 <b>Pool:</b> recommendation left for Apply in the panel (12 h)")
		} else {
			fmt.Fprintf(&b, "🔁 <b>Pool:</b> %s", closedText(maintenanceDecisions, r.Decision))
		}
		if r.Applied != "" && !r.Recommended && r.Decision != "pool-unchanged" {
			fmt.Fprintf(&b, " · %s", closedText(maintenanceApplied, r.Applied))
		}
		b.WriteString("\n")
	} else {
		b.WriteString("📡 <b>Review:</b> did not run; the running pool was kept\n")
	}
	fmt.Fprintf(&b, "🌍 <b>Geodata:</b> %s\n", closedText(maintenanceGeodata, r.Geodata))
	if len(r.Pool) > 0 {
		b.WriteString("\n🏆 <b>Running pool</b>\n")
		for i, node := range r.Pool {
			if i == maxMaintenanceNodes {
				fmt.Fprintf(&b, "… and %d more\n", len(r.Pool)-i)
				break
			}
			fmt.Fprintf(&b, "%d. <b>%s</b>", i+1, html.EscapeString(boundedName(node.Name)))
			if node.Measured {
				fmt.Fprintf(&b, "\n    <code>%d ms · ↓ %s · ↑ %s</code>", node.RTTMS, mbps(node.DownBPS), mbps(node.UpBPS))
			} else {
				b.WriteString(" — <i>not measured</i>")
			}
			b.WriteString("\n")
		}
	}
	return Alert{text: strings.TrimRight(b.String(), "\n"), html: true}
}

func closedText(table map[string]string, code string) string {
	if text, ok := table[code]; ok {
		return text
	}
	return "unknown"
}

// boundedName keeps a display name printable and at most 40 runes.
func boundedName(name string) string {
	var out []rune
	for _, r := range name {
		if unicode.IsControl(r) {
			continue
		}
		out = append(out, r)
	}
	if len(out) > 40 {
		out = append(out[:39], '…')
	}
	if len(out) == 0 {
		return "unnamed"
	}
	return string(out)
}

func mbps(bps float64) string {
	if bps <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f Mbps", bps*8/1e6)
}
