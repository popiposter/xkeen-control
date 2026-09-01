package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/appliance"
	"github.com/popiposter/xkeen-control/internal/buildinfo"
	"github.com/popiposter/xkeen-control/internal/nodes"
)

type testApplianceSource struct {
	value appliance.Appliance
	err   error
}

func (s testApplianceSource) Snapshot() (appliance.Appliance, error) {
	return s.value, s.err
}

type testRegistrySource struct {
	value nodes.Registry
	err   error
}

func (s testRegistrySource) Snapshot(context.Context) (nodes.Registry, error) {
	return s.value, s.err
}

type incrementingReader struct{ next byte }

func (r *incrementingReader) Read(value []byte) (int, error) {
	for index := range value {
		value[index] = r.next
		r.next++
	}
	return len(value), nil
}

func testBuild() buildinfo.Info {
	return buildinfo.Info{
		Product: "xkeen-control", Version: "1.2.3",
		SourceCommit: strings.Repeat("a", 40), Channel: "stable",
	}
}

func testAppliance(t *testing.T) appliance.Appliance {
	t.Helper()
	value := appliance.Appliance{
		SchemaVersion: appliance.SchemaVersion,
		DNS: appliance.DNSPolicy{
			Servers:       []appliance.DNSServer{{Address: "localhost"}},
			QueryStrategy: "UseIPv4", ServeStale: true, ServeExpiredTTL: 3600,
			DisableFallbackIfMatch: true, EnableParallelQuery: true, UseSystemHosts: true,
		},
		Routing: appliance.RoutingPolicy{
			DomainStrategy: "IPIfNonMatch", DomainMatcher: "hybrid",
			Rules: []appliance.RoutingRule{
				{Type: "field", InboundTag: []string{"api"}, Action: appliance.RuleAction{OutboundTag: "api"}},
				{Type: "field", InboundTag: []string{"tproxy"}, Action: appliance.RuleAction{BalancerTag: "bal-proxy"}},
			},
			Balancers: []appliance.Balancer{{
				Tag: "bal-proxy", Selector: []string{"proxy-node-11111111"},
				FallbackTag: "block", Strategy: appliance.BalancerStrategy{Type: "leastPing"},
			}},
		},
		Observatory: appliance.ObservatoryPolicy{
			SubjectSelector: []string{"proxy-node-11111111"}, ProbeInterval: "5m",
		},
	}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	return value
}

func testRegistry(t *testing.T) nodes.Registry {
	t.Helper()
	profile := nodes.VLESS{
		UUID: "11111111-1111-4111-8111-111111111111", Host: "node.example.com", Port: 443,
		Encryption: "none", Security: "reality", ServerName: "node.example.com", Fingerprint: "chrome",
		PublicKey: "AAAAAAAAAAAAAAAA", ShortID: "0123456789abcdef", Network: "tcp",
	}
	const subscriptionID = "sub-11111111"
	node, err := nodes.NewNodeWithID(profile, "Synthetic node", nodes.Source{Type: "subscription", SubscriptionID: subscriptionID}, "node-11111111")
	if err != nil {
		t.Fatal(err)
	}
	registry := nodes.NewRegistry()
	registry.Subscriptions = []nodes.Subscription{{
		ID: subscriptionID, Name: "Synthetic provider", URL: "https://subscription.example/synthetic-token", Enabled: true,
	}}
	registry.Nodes = []nodes.Node{node}
	if err := registry.Validate(); err != nil {
		t.Fatal(err)
	}
	return registry
}

func testService(t *testing.T, random *incrementingReader, derive KeyDeriver) *Service {
	t.Helper()
	config := Config{
		Appliance: testApplianceSource{value: testAppliance(t)},
		Nodes:     testRegistrySource{value: testRegistry(t)},
		Build:     testBuild(), Now: func() time.Time { return time.Unix(1_750_000_000, 0).UTC() },
		Random: random, DeriveKey: derive, GOOS: "linux", GOARCH: "arm64",
	}
	return NewService(config)
}

func TestSafeBundleIsDeterministicAndSecretless(t *testing.T) {
	service := testService(t, &incrementingReader{}, nil)
	first, err := service.Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("safe export changed for fixed state and clock")
	}
	bundle, err := ParseBundle(first)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Manifest.ContainsSecrets || bundle.Nodes != nil || len(bundle.Manifest.Sections) != 1 || bundle.Manifest.Sections[0].Name != "appliance" {
		t.Fatalf("safe bundle shape = %+v", bundle.Manifest)
	}
	applianceBytes, err := appliance.MarshalCanonical(bundle.Appliance)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(applianceBytes)
	section := bundle.Manifest.Sections[0]
	if section.Size != int64(len(applianceBytes)) || section.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("safe section metadata = %+v", section)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(first, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["nodes"]; ok {
		t.Fatal("safe export contains a nodes section")
	}
	for _, marker := range []string{
		"11111111-1111-4111-8111-111111111111", "AAAAAAAAAAAAAAAA", "0123456789abcdef",
		"https://subscription.example/synthetic-token", "synthetic-auth-password", "synthetic-auth-hash", "127.0.0.1:8787",
	} {
		if bytes.Contains(first, []byte(marker)) {
			t.Fatalf("safe export contains secret marker %q", marker)
		}
	}
}

func TestSafeExportRequiresStoredApplianceAuthority(t *testing.T) {
	service := NewService(Config{
		Appliance: testApplianceSource{err: errors.New("authority missing")},
		Build:     testBuild(), Now: func() time.Time { return time.Unix(1_750_000_000, 0).UTC() },
	})
	if _, err := service.Export(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing authority error = %v", err)
	}
}

func TestBackupExportDoesNotCreateFilesOrEchoSensitiveInputs(t *testing.T) {
	temporary := t.TempDir()
	before, err := os.ReadDir(temporary)
	if err != nil {
		t.Fatal(err)
	}
	const passphrase = "correct synthetic passphrase"
	service := testService(t, &incrementingReader{}, nil)
	if _, err := service.ExportSecret(context.Background(), passphrase); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadDir(temporary)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("backup export created %d filesystem entries", len(after)-len(before))
	}

	currentPassword := "synthetic-current-password"
	registryMarker := "https://subscription.example/synthetic-token"
	failed := NewService(Config{
		Appliance: testApplianceSource{err: errors.New(currentPassword + " " + passphrase + " " + registryMarker)},
		Nodes:     testRegistrySource{err: errors.New(registryMarker)},
		Build:     testBuild(), Now: func() time.Time { return time.Unix(1_750_000_000, 0).UTC() },
	})
	if _, err := failed.ExportSecret(context.Background(), passphrase); err == nil || strings.Contains(err.Error(), currentPassword) || strings.Contains(err.Error(), passphrase) || strings.Contains(err.Error(), registryMarker) {
		t.Fatalf("sensitive export error = %v", err)
	}
}

func TestEncryptedRoundTripUsesBoundedEnvelopeAndFreshRandomness(t *testing.T) {
	service := testService(t, &incrementingReader{}, nil)
	const passphrase = "correct synthetic passphrase"
	first, err := service.ExportSecret(context.Background(), passphrase)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.ExportSecret(context.Background(), passphrase)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("secret exports did not change salt/nonce")
	}
	if len(first) > MaxEncryptedEnvelope {
		t.Fatalf("encrypted envelope size = %d", len(first))
	}
	var envelope encryptedEnvelope
	if err := json.Unmarshal(first, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Format != EncryptedFormat || envelope.EnvelopeVersion != EnvelopeVersion || envelope.KDF.Name != KDFName || envelope.KDF.Version != Argon2Version || envelope.KDF.MemoryKiB != Argon2MemoryKiB || envelope.KDF.Iterations != Argon2Iterations || envelope.KDF.Parallelism != Argon2Parallelism || envelope.KDF.KeyBytes != Argon2KeyBytes || envelope.Cipher.Name != "XChaCha20-Poly1305" {
		t.Fatalf("encrypted envelope parameters = %+v", envelope)
	}
	if salt, ok := decodeRawURL(envelope.KDF.Salt, Argon2SaltBytes); !ok {
		t.Fatal("encrypted envelope salt is not the fixed length")
	} else {
		clearBytes(salt)
	}
	if nonce, ok := decodeRawURL(envelope.Cipher.Nonce, XChaCha20NonceBytes); !ok {
		t.Fatal("encrypted envelope nonce is not the fixed length")
	} else {
		clearBytes(nonce)
	}
	for _, marker := range []string{
		"11111111-1111-4111-8111-111111111111", "AAAAAAAAAAAAAAAA", "0123456789abcdef",
		"https://subscription.example/synthetic-token", "synthetic-auth-password", "synthetic-auth-hash", "127.0.0.1:8787",
	} {
		if bytes.Contains(first, []byte(marker)) {
			t.Fatalf("encrypted envelope contains plaintext marker %q", marker)
		}
	}
	bundle, err := OpenEncrypted(first, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Nodes == nil || !bundle.Manifest.ContainsSecrets || len(bundle.Manifest.Sections) != 2 || bundle.Manifest.Sections[1].Name != "nodes" {
		t.Fatalf("secret bundle shape = %+v", bundle.Manifest)
	}
	if !reflect.DeepEqual(*bundle.Nodes, testRegistry(t)) {
		t.Fatal("encrypted roundtrip changed the typed registry")
	}
}

func TestEncryptedOpenRejectsWrongPassphraseAndTamperingBeforeReturningPlaintext(t *testing.T) {
	derive := func(password, salt []byte, _, _ uint32, _ uint8, keyBytes uint32) []byte {
		input := append(append([]byte(nil), password...), salt...)
		digest := sha256.Sum256(input)
		return append([]byte(nil), digest[:keyBytes]...)
	}
	service := testService(t, &incrementingReader{}, derive)
	const passphrase = "correct synthetic passphrase"
	original, err := service.ExportSecret(context.Background(), passphrase)
	if err != nil {
		t.Fatal(err)
	}
	var envelope encryptedEnvelope
	if err := json.Unmarshal(original, &envelope); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		data []byte
		pass string
	}{
		{name: "wrong passphrase", data: original, pass: "wrong synthetic passphrase"},
		{name: "ciphertext tamper", data: tamperEnvelope(t, envelope, func(value *encryptedEnvelope) {
			ciphertext, ok := decodeRawURL(value.Ciphertext, 0)
			if !ok {
				t.Fatal("fixture ciphertext did not decode")
			}
			ciphertext[0] ^= 1
			value.Ciphertext = base64.RawURLEncoding.EncodeToString(ciphertext)
		}), pass: passphrase},
		{name: "AAD nonce tamper", data: tamperEnvelope(t, envelope, func(value *encryptedEnvelope) {
			value.Cipher.Nonce = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x7f}, XChaCha20NonceBytes))
		}), pass: passphrase},
		{name: "unknown outer field", data: addUnknownEnvelopeField(t, original), pass: passphrase},
		{name: "malformed base64", data: tamperEnvelope(t, envelope, func(value *encryptedEnvelope) { value.KDF.Salt = "%%%" }), pass: passphrase},
		{name: "alternate KDF parameter", data: tamperEnvelope(t, envelope, func(value *encryptedEnvelope) { value.KDF.MemoryKiB++ }), pass: passphrase},
		{name: "alternate cipher", data: tamperEnvelope(t, envelope, func(value *encryptedEnvelope) { value.Cipher.Name = "AES-GCM" }), pass: passphrase},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			opened, err := openEncrypted(test.data, test.pass, derive)
			if err == nil || !reflect.DeepEqual(opened, Bundle{}) {
				t.Fatalf("tampered open = %+v, %v", opened, err)
			}
		})
	}
}

func TestSecretEncryptionIsSingleFlight(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var calls atomic.Int32
	derive := func(_ []byte, _ []byte, _, _ uint32, _ uint8, keyBytes uint32) []byte {
		calls.Add(1)
		once.Do(func() { close(started) })
		<-release
		return bytes.Repeat([]byte{0x42}, int(keyBytes))
	}
	service := testService(t, &incrementingReader{}, derive)
	firstResult := make(chan error, 1)
	go func() {
		_, err := service.ExportSecret(context.Background(), "correct synthetic passphrase")
		firstResult <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first KDF did not start")
	}
	if _, err := service.ExportSecret(context.Background(), "second synthetic passphrase"); !errors.Is(err, ErrBusy) {
		t.Fatalf("second concurrent export = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("KDF calls = %d", calls.Load())
	}
	close(release)
	select {
	case err := <-firstResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first export did not finish")
	}
}

func tamperEnvelope(t *testing.T, source encryptedEnvelope, mutate func(*encryptedEnvelope)) []byte {
	t.Helper()
	mutated := source
	mutate(&mutated)
	contents, err := json.Marshal(mutated)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func addUnknownEnvelopeField(t *testing.T, contents []byte) []byte {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(contents, &fields); err != nil {
		t.Fatal(err)
	}
	fields["unexpected"] = json.RawMessage(`true`)
	result, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
