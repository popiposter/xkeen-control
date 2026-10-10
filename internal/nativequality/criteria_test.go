package nativequality

import (
	"github.com/popiposter/xkeen-control/internal/c1"
	"strings"
	"testing"
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

func TestCriteriaReadNativeMaxRTTAndIgnoreTheObservedSet(t *testing.T) {
	routing := `{"routing":{"balancers":[{"strategy":{"type":"leastLoad","settings":{"maxRTT":"10s"}}}]}}`
	// A narrowed 07 whose members all vanished still admits a review.
	for _, observatory := range []string{`{"observatory":{"subjectSelector":["proxy-gone-a","proxy-gone-b"],"probeInterval":"10s"}}`, `{"observatory":{}}`} {
		if c, err := readCriteria(routing, observatory, 0); err != nil || c.maxRTT != 10000 || c.latencySource != "native-max-rtt" {
			t.Fatal(observatory, c, err)
		}
	}
	for _, bad := range []string{`"bad"`, `null`, `10000`, `"0s"`, `"2h"`} {
		if _, err := readCriteria(strings.Replace(routing, `"10s"`, bad, 1), `{"observatory":{}}`, 0); err == nil {
			t.Fatal("accepted invalid maxRTT", bad)
		}
	}
	if _, err := readCriteria(routing, `{"burstObservatory":{}}`, 0); err == nil {
		t.Fatal("accepted a missing observatory object")
	}
}
