package nodes

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
)

type outboundDocument struct {
	Outbounds []json.RawMessage `json:"outbounds"`
}

type fixedOutbound struct {
	Tag      string `json:"tag"`
	Protocol string `json:"protocol"`
}

type vlessOutbound struct {
	Tag            string              `json:"tag"`
	Protocol       string              `json:"protocol"`
	Settings       vlessSettings       `json:"settings"`
	StreamSettings vlessStreamSettings `json:"streamSettings"`
}

type vlessSettings struct {
	VNext []vlessServer `json:"vnext"`
}

type vlessServer struct {
	Address string      `json:"address"`
	Port    int         `json:"port"`
	Users   []vlessUser `json:"users"`
}

type vlessUser struct {
	ID         string `json:"id"`
	Encryption string `json:"encryption"`
	Flow       string `json:"flow,omitempty"`
}

type vlessStreamSettings struct {
	Network         string          `json:"network"`
	Security        string          `json:"security"`
	RealitySettings realitySettings `json:"realitySettings"`
	WSSettings      *wsSettings     `json:"wsSettings,omitempty"`
	XHTTPSettings   *xhttpSettings  `json:"xhttpSettings,omitempty"`
	GRPCSettings    *grpcSettings   `json:"grpcSettings,omitempty"`
	FinalMask       *finalMask      `json:"finalmask,omitempty"`
}

type realitySettings struct {
	ServerName  string `json:"serverName"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"publicKey"`
	ShortID     string `json:"shortId"`
	SpiderX     string `json:"spiderX,omitempty"`
}

type wsSettings struct {
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
}

type grpcSettings struct {
	ServiceName string `json:"serviceName,omitempty"`
	Mode        string `json:"mode,omitempty"`
}

type xhttpSettings struct {
	Path string `json:"path"`
	Host string `json:"host,omitempty"`
	Mode string `json:"mode,omitempty"`
}

type finalMask struct {
	TCP []finalMaskItem `json:"tcp"`
}

type finalMaskItem struct {
	Type     string                    `json:"type"`
	Settings finalMaskFragmentSettings `json:"settings"`
}

type finalMaskFragmentSettings struct {
	Packets  string `json:"packets"`
	Length   string `json:"length"`
	Delay    string `json:"delay"`
	MaxSplit string `json:"maxSplit,omitempty"`
}

// Render produces the complete generated 04_outbounds artifact. Fixed
// direct/block/api/dns fallbacks preserve the existing routing contract; only
// enabled registry nodes are projected as VLESS outbounds.
func Render(registry Registry) ([]byte, error) {
	if err := registry.Validate(); err != nil {
		return nil, err
	}
	items := make([]json.RawMessage, 0, len(registry.Nodes)+4)
	for _, fixed := range []fixedOutbound{
		{Tag: "api", Protocol: "freedom"},
		{Tag: "block", Protocol: "blackhole"},
		{Tag: "direct", Protocol: "freedom"},
		{Tag: "dns-out", Protocol: "dns"},
	} {
		raw, err := json.Marshal(fixed)
		if err != nil {
			return nil, errors.New("unable to render fixed outbound")
		}
		items = append(items, raw)
	}
	nodes := registry.SortedNodes()
	for _, node := range nodes {
		if !node.Enabled {
			continue
		}
		raw, err := renderNode(node)
		if err != nil {
			return nil, err
		}
		items = append(items, raw)
	}
	// Fixed outbounds are stable by contract; node entries are sorted by
	// canonical tag before appending. Keep this explicit as a determinism guard.
	sort.SliceStable(items[4:], func(i, j int) bool {
		var left, right struct {
			Tag string `json:"tag"`
		}
		_ = json.Unmarshal(items[4+i], &left)
		_ = json.Unmarshal(items[4+j], &right)
		return left.Tag < right.Tag
	})
	document, err := json.MarshalIndent(outboundDocument{Outbounds: items}, "", "  ")
	if err != nil {
		return nil, errors.New("unable to render outbound document")
	}
	return append(document, '\n'), nil
}

func renderNode(node Node) ([]byte, error) {
	profile := node.VLESS
	stream := vlessStreamSettings{
		Network:  profile.Network,
		Security: profile.Security,
		RealitySettings: realitySettings{
			ServerName:  profile.ServerName,
			Fingerprint: profile.Fingerprint,
			PublicKey:   profile.PublicKey,
			ShortID:     profile.ShortID,
			SpiderX:     profile.SpiderX,
		},
	}
	switch profile.Network {
	case "ws", "http":
		stream.WSSettings = &wsSettings{Path: profile.Path}
		if profile.HostHeader != "" {
			stream.WSSettings.Headers = map[string]string{"Host": profile.HostHeader}
		}
	case "xhttp", "splithttp":
		stream.XHTTPSettings = &xhttpSettings{Path: profile.Path, Host: profile.HostHeader, Mode: profile.Mode}
	case "grpc":
		stream.GRPCSettings = &grpcSettings{ServiceName: profile.ServiceName, Mode: profile.Mode}
	}
	if profile.FinalMask != nil {
		fragment := profile.FinalMask.Fragment
		stream.FinalMask = &finalMask{TCP: []finalMaskItem{{
			Type: "fragment",
			Settings: finalMaskFragmentSettings{
				Packets: fragment.Packets, Length: fragment.Length, Delay: fragment.Delay, MaxSplit: fragment.MaxSplit,
			},
		}}}
	}
	outbound := vlessOutbound{
		Tag:      node.OutboundTag,
		Protocol: "vless",
		Settings: vlessSettings{VNext: []vlessServer{{
			Address: profile.Host,
			Port:    profile.Port,
			Users:   []vlessUser{{ID: profile.UUID, Encryption: profile.Encryption, Flow: profile.Flow}},
		}}},
		StreamSettings: stream,
	}
	encoded, err := json.Marshal(outbound)
	if err != nil {
		return nil, errors.New("unable to render VLESS outbound")
	}
	return bytes.Clone(encoded), nil
}
