package nodes

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestInitialSourceUsesExistingPrivateImportAndRegistry(t *testing.T) {
	for _, source := range []string{syntheticProfile, "https://subscriptions.example.com/private-fixture"} {
		fetch := &fakeFetcher{body: []byte(syntheticProfile + "\n" + syntheticProfileTwo)}
		r, e := PrepareInitialSource(context.Background(), source, fetch)
		if e != nil || r.Validate() != nil {
			t.Fatal(e)
		}
		want := 1
		if strings.HasPrefix(source, "https") {
			want = 2
			if len(r.Subscriptions) != 1 || r.Subscriptions[0].URL != source {
				t.Fatal("private subscription not authoritative")
			}
		}
		if len(r.Nodes) != want {
			t.Fatal("duplicate or missing nodes")
		}
		for _, n := range r.Nodes {
			if !n.Enabled || n.Stale || n.Missing {
				t.Fatal("unusable initial node")
			}
		}
	}
}
func TestInitialSourceRejectsDuplicateSubscriptionIdentity(t *testing.T) {
	if _, e := PrepareInitialSource(context.Background(), "https://subscriptions.example.com/private-fixture", &fakeFetcher{body: []byte(syntheticProfile + "\n" + syntheticProfile)}); !errors.Is(e, ErrInitialSource) {
		t.Fatal("duplicate identity admitted")
	}
}
func TestInitialSourceErrorsAreSecretlessAndEmptyNeverAdmitted(t *testing.T) {
	for _, source := range []string{"", syntheticProfile + "\n", "https://subscriptions.example.com/private-fixture"} {
		r, e := PrepareInitialSource(context.Background(), source, &fakeFetcher{err: errors.New("private bearer fixture")})
		if !errors.Is(e, ErrInitialSource) || len(r.Nodes) != 0 || strings.Contains(e.Error(), "bearer") {
			t.Fatal("unsafe initial error")
		}
	}
	if _, e := PrepareInitialSource(context.Background(), "https://subscriptions.example.com/private-fixture", &fakeFetcher{body: []byte("")}); !errors.Is(e, ErrInitialSource) {
		t.Fatal("empty source accepted")
	}
}
