package nativequality

import (
	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/xkeen"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
	"strings"
	"testing"
	"time"
)

func TestRecommendationPreservesNativeStrategyAndSettings(t *testing.T) {
	for _, kind := range []string{"leastLoad", "leastPing"} {
		original := `{"routing":{"balancers":[{"selector":["proxy-"],"strategy":{"type":"` + kind + `","settings":{"maxRTT":"10s","expected":2,"future":9007199254740993}}}]}}`
		out, err := replaceRecommendation(original, 0, []c1.NativeQualityCost{{Match: "^proxy-a$", Value: 1}}, []string{"proxy-a", "proxy-b"})
		if err != nil {
			t.Fatal(err)
		}
		for _, keep := range []string{`"maxRTT":"10s"`, `"expected":2`, `"future":9007199254740993`, `"type":"` + kind + `"`} {
			if !strings.Contains(string(out), keep) {
				t.Fatal(string(out), keep)
			}
		}
		if strings.Contains(string(out), `"costs"`) != (kind == "leastLoad") {
			t.Fatal("changed native strategy", string(out))
		}
	}
}

func TestConfiguredFreshnessDoesNotInventHealthOutsideObservationSet(t *testing.T) {
	now := time.Now()
	snapshot := xrayapi.Snapshot{APIReachable: true, RoutingReachable: true, ObservatoryReachable: true}
	pool := []string{"proxy-a", "proxy-b", "proxy-c", "proxy-stale", "proxy-future"}
	for _, tag := range pool {
		snapshot.OutboundHealth = append(snapshot.OutboundHealth, xrayapi.OutboundHealth{Tag: tag, Alive: true, DelayMS: 2000, LastTry: now.Add(-3 * time.Minute)})
	}
	snapshot.OutboundHealth[3].LastTry = now.Add(-10 * time.Minute)
	snapshot.OutboundHealth[4].LastTry = now.Add(time.Minute)
	c := observationCriteria{maxRTT: 10000, freshness: 6 * time.Minute, observed: map[string]bool{"proxy-a": true, "proxy-b": true, "proxy-stale": true, "proxy-future": true}}
	g, err := prepareWithCriteria(snapshot, pool, 1, 1, now, c)
	if err != nil || len(g.Candidates) != 2 || g.Candidates[0].RTTMS != 2000 {
		t.Fatal(g, err)
	}
	c.maxRTT = 750
	if _, err := prepareWithCriteria(snapshot, pool, 1, 1, now, c); err == nil {
		t.Fatal("ignored configured RTT")
	}
}

func TestCriteriaUseConfiguredRTTAndWholeSequentialCycle(t *testing.T) {
	routing := `{"routing":{"balancers":[{"strategy":{"type":"leastLoad","settings":{"maxRTT":"10s"}}}]}}`
	targets := []xkeen.ConfigTarget{{Tag: "proxy-a", Kind: "outbound"}, {Tag: "proxy-b", Kind: "outbound"}}
	for _, tt := range []struct {
		mode string
		want time.Duration
	}{{"true", 65 * time.Second}, {"false", 100 * time.Second}} {
		c, err := readCriteria(routing, `{"observatory":{"subjectSelector":["proxy-"],"probeInterval":"30s","enableConcurrency":`+tt.mode+`}}`, 0, targets)
		if err != nil || c.maxRTT != 10000 || c.freshness != tt.want {
			t.Fatal(c, err)
		}
	}
	for _, bad := range []string{`"bad"`, `null`, `10000`, `"0s"`, `"2h"`} {
		_, err := readCriteria(strings.Replace(routing, `"10s"`, bad, 1), `{"observatory":{"subjectSelector":["proxy-"],"probeInterval":"30s"}}`, 0, targets)
		if err == nil {
			t.Fatal("accepted invalid maxRTT", bad)
		}
	}
	if _, err := readCriteria(routing, `{"observatory":{"subjectSelector":["proxy-"],"probeInterval":"15m"}}`, 0, targets); err == nil {
		t.Fatal("unbounded stale horizon")
	}
}
