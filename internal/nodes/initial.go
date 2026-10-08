package nodes

import (
	"context"
	"errors"
	"strings"
)

var ErrInitialSource = errors.New("initial subscription or node is invalid or unavailable")

// PrepareInitialSource uses the same parser/fetcher and subscription candidate
// owner as normal node operations. It returns private data in RAM, never saves
// or activates a node and never accepts a preexisting registry.
func PrepareInitialSource(ctx context.Context, source string, fetcher SubscriptionFetcher) (Registry, error) {
	if len(source) == 0 || len(source) > MaxProfileInput || strings.ContainsAny(source, "\r\n\x00") {
		return Registry{}, ErrInitialSource
	}
	if strings.HasPrefix(strings.ToLower(source), "vless://") {
		p, err := ParseProfile(source)
		if err != nil {
			return Registry{}, ErrInitialSource
		}
		n, err := NewNode(p.VLESS, p.Name, Source{Type: "manual"})
		if err != nil {
			return Registry{}, ErrInitialSource
		}
		r := NewRegistry()
		r.Nodes = append(r.Nodes, n)
		return r, nil
	}
	if fetcher == nil {
		fetcher = HTTPSubscriptionFetcher{}
	}
	if validateSubscriptionURL(source) != nil {
		return Registry{}, ErrInitialSource
	}
	b, err := fetcher.Fetch(ctx, source)
	if err != nil {
		return Registry{}, ErrInitialSource
	}
	p, err := ParseSubscriptionBody(b)
	if err != nil {
		return Registry{}, ErrInitialSource
	}
	id, err := randomIdentifier()
	if err != nil {
		return Registry{}, ErrInitialSource
	}
	r, err := buildSubscriptionCandidate(NewRegistry(), Subscription{ID: id, Name: "Initial subscription", URL: source, Enabled: true}, p)
	if err != nil || len(r.Nodes) == 0 || r.Validate() != nil {
		return Registry{}, ErrInitialSource
	}
	return r, nil
}
