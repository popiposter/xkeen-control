package xkeen

import (
	"context"
	"testing"
)

func TestRoutingExampleFirstMatchAndUncertainty(t *testing.T) {
	tests := []struct {
		name, text string
		sample     RoutingSample
		state      string
		rule       int
		target     string
	}{
		{"first", `{"routing":{"rules":[{"type":"field","domain":["domain:example.com"],"outboundTag":"vpn"},{"type":"field","domain":["full:www.example.com"],"outboundTag":"direct"}]}}`, RoutingSample{Domain: "www.example.com"}, "matched", 1, "vpn"},
		{"all conditions", `{"routing":{"rules":[{"type":"field","domain":["domain:example.com"],"network":"udp","outboundTag":"vpn"},{"type":"field","domain":["example.com"],"port":"443,8000-9000","outboundTag":"direct"}]}}`, RoutingSample{Domain: "www.example.com", Network: "tcp", Port: 8443}, "matched", 2, "direct"},
		{"uncertain earlier", `{"routing":{"rules":[{"type":"field","domain":["example.com"],"protocol":["tls"],"outboundTag":"vpn"},{"type":"field","domain":["example.com"],"outboundTag":"direct"}]}}`, RoutingSample{Domain: "example.com"}, "unknown", 1, ""},
		{"known false earlier", `{"routing":{"rules":[{"type":"field","domain":["full:else.test"],"protocol":["tls"],"outboundTag":"vpn"},{"type":"field","domain":["example.com"],"balancerTag":"pool"}]}}`, RoutingSample{Domain: "example.com"}, "matched", 2, "pool"},
		{"geo missing", `{"routing":{"rules":[{"type":"field","domain":["geosite:example"],"outboundTag":"vpn"}]}}`, RoutingSample{Domain: "example.com"}, "unknown", 1, ""},
		{"ipv6", `{"routing":{"rules":[{"type":"field","ip":["2001:db8::/32"],"outboundTag":"direct"}]}}`, RoutingSample{IP: "2001:db8::123"}, "matched", 1, "direct"},
		{"inverse conjunction", `{"routing":{"rules":[{"type":"field","ip":["!10.0.0.0/8","!192.168.0.0/16"],"outboundTag":"vpn"}]}}`, RoutingSample{IP: "192.168.1.2"}, "default", 0, ""},
		{"inverse alternatives", `{"routing":{"rules":[{"type":"field","ip":["!10.0.0.0/8","!192.168.0.0/16","192.168.1.0/24"],"outboundTag":"vpn"}]}}`, RoutingSample{IP: "192.168.1.2"}, "matched", 1, "vpn"},
		{"second DNS pass", `{"routing":{"domainStrategy":"IPIfNonMatch","rules":[]}}`, RoutingSample{Domain: "example.com"}, "unknown", 0, ""},
		{"invalid regex", `{"routing":{"rules":[{"type":"field","domain":["regexp:["],"outboundTag":"vpn"}]}}`, RoutingSample{Domain: "example.com"}, "unknown", 1, ""},
		{"native dotless regex remains uncertain", `{"routing":{"rules":[{"type":"field","domain":["dotless:ab+"],"outboundTag":"vpn"},{"type":"field","domain":["full:abbb"],"outboundTag":"direct"}]}}`, RoutingSample{Domain: "abbb"}, "unknown", 1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PreviewRouting(context.Background(), tt.text, tt.sample, nil)
			if err != nil || got.State != tt.state || got.Rule != tt.rule || got.Target != tt.target {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
	if _, err := PreviewRouting(context.Background(), `{}`, RoutingSample{IP: "not-an-ip"}, nil); err == nil {
		t.Fatal("invalid example accepted")
	}
}
