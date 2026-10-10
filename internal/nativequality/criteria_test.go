package nativequality

import (
	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/xkeen"
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
