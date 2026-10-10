package benchmarkpolicy

import (
	"testing"
)

func TestPolicyRejectsUnboundedValues(t *testing.T) {
	for _, raw := range []string{
		`{"xkeen":{"xray":{"speed_balancer":{"max_nodes":129,"max_time":10,"test_url":"https://speed.example/down?bytes=20971520"}}}}`,
		`{"xkeen":{"xray":{"speed_balancer":{"max_nodes":128,"max_time":11,"test_url":"https://speed.example/down?bytes=20971520"}}}}`,
		`{"xkeen":{"xray":{"speed_balancer":{"max_nodes":128,"max_time":10,"test_url":"https://speed.example/down?bytes=20971521"}}}}`,
	} {
		if got := Parse([]byte(raw)); got != (Policy{}) {
			t.Fatalf("unbounded policy accepted: %+v", got)
		}
	}
}
