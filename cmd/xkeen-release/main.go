package main

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/popiposter/xkeen-control/internal/release"
)

type assetsFlag map[string]string

func (value *assetsFlag) String() string { return "name=path" }

func (value *assetsFlag) Set(raw string) error {
	name, path, ok := strings.Cut(raw, "=")
	if !ok || name == "" || path == "" {
		return errors.New("asset must be name=path")
	}
	if *value == nil {
		*value = make(assetsFlag)
	}
	if _, exists := (*value)[name]; exists {
		return errors.New("duplicate asset")
	}
	(*value)[name] = path
	return nil
}

func main() {
	if len(os.Args) < 2 {
		fatal("usage: xkeen-release {manifest|sign|verify|verify-assets|verify-pinned-key}")
	}
	switch os.Args[1] {
	case "manifest":
		manifestCommand(os.Args[2:])
	case "sign":
		signCommand(os.Args[2:])
	case "verify":
		verifyCommand(os.Args[2:])
	case "verify-assets":
		verifyAssetsCommand(os.Args[2:])
	case "verify-pinned-key":
		verifyPinnedKeyCommand(os.Args[2:])
	default:
		fatal("unsupported release operation")
	}
}

func manifestCommand(args []string) {
	flags := flag.NewFlagSet("manifest", flag.ExitOnError)
	output := flags.String("output", "", "manifest output")
	version := flags.String("version", "", "semantic version")
	commit := flags.String("commit", "", "full source commit")
	channel := flags.String("channel", "", "stable or beta")
	epoch := flags.Int64("source-date-epoch", 0, "deterministic source epoch")
	var values assetsFlag
	flags.Var(&values, "asset", "required release asset name=path")
	_ = flags.Parse(args)
	if *output == "" || *version == "" || *commit == "" || *channel == "" || len(values) != len(release.RequiredArtifacts) {
		fatal("manifest arguments are incomplete")
	}
	manifest, err := release.BuildManifest(*version, *commit, *channel, *epoch, values)
	if err != nil {
		fatal(err.Error())
	}
	contents, err := manifest.MarshalDeterministic()
	if err != nil {
		fatal("manifest encoding failed")
	}
	if err := os.WriteFile(*output, contents, 0o600); err != nil {
		fatal("manifest write failed")
	}
}

func signCommand(args []string) {
	flags := flag.NewFlagSet("sign", flag.ExitOnError)
	manifestPath := flags.String("manifest", "", "manifest path")
	keyPath := flags.String("key-file", "", "protected key path")
	output := flags.String("output", "", "signature output")
	_ = flags.Parse(args)
	if *manifestPath == "" || *keyPath == "" || *output == "" {
		fatal("sign arguments are incomplete")
	}
	manifest, err := os.ReadFile(*manifestPath)
	if err != nil {
		fatal("manifest read failed")
	}
	if _, err := release.ParseManifest(manifest); err != nil {
		fatal("manifest is invalid")
	}
	keyContents, err := os.ReadFile(*keyPath)
	if err != nil {
		fatal("signing key is unavailable")
	}
	privateKey, err := release.DecodePrivateKey(keyContents)
	if err != nil {
		fatal("signing key is invalid")
	}
	signature, err := release.Sign(manifest, privateKey)
	if err != nil {
		fatal("manifest signing failed")
	}
	if err := os.WriteFile(*output, signature, 0o600); err != nil {
		fatal("signature write failed")
	}
}

func verifyCommand(args []string) {
	flags := flag.NewFlagSet("verify", flag.ExitOnError)
	manifestPath := flags.String("manifest", "", "manifest path")
	signaturePath := flags.String("signature", "", "signature path")
	keyPath := flags.String("public-key-file", "", "public key path")
	_ = flags.Parse(args)
	if *manifestPath == "" || *signaturePath == "" || *keyPath == "" {
		fatal("verify arguments are incomplete")
	}
	manifest, err := os.ReadFile(*manifestPath)
	if err != nil {
		fatal("manifest read failed")
	}
	if _, err := release.ParseManifest(manifest); err != nil {
		fatal("manifest is invalid")
	}
	signature, err := os.ReadFile(*signaturePath)
	if err != nil {
		fatal("signature read failed")
	}
	publicKey := readPublicKey(*keyPath)
	if err := release.Verify(manifest, signature, publicKey); err != nil {
		fatal("signature verification failed")
	}
}

func verifyAssetsCommand(args []string) {
	flags := flag.NewFlagSet("verify-assets", flag.ExitOnError)
	manifestPath := flags.String("manifest", "", "manifest path")
	assetDir := flags.String("asset-dir", "", "directory containing release assets")
	_ = flags.Parse(args)
	if *manifestPath == "" || *assetDir == "" {
		fatal("verify-assets arguments are incomplete")
	}
	manifestBytes, err := os.ReadFile(*manifestPath)
	if err != nil {
		fatal("manifest read failed")
	}
	manifest, err := release.ParseManifest(manifestBytes)
	if err != nil {
		fatal("manifest is invalid")
	}
	assets := make(map[string][]byte, len(manifest.Artifacts))
	for _, item := range manifest.Artifacts {
		contents, err := os.ReadFile(filepath.Join(*assetDir, item.Name))
		if err != nil {
			fatal("release asset is unavailable")
		}
		assets[item.Name] = contents
	}
	if err := release.VerifyCandidate(release.Candidate{Manifest: manifest, Assets: assets}); err != nil {
		fatal("release assets do not match signed manifest metadata")
	}
}

func verifyPinnedKeyCommand(args []string) {
	flags := flag.NewFlagSet("verify-pinned-key", flag.ExitOnError)
	keyPath := flags.String("public-key-file", "", "public key path")
	_ = flags.Parse(args)
	if *keyPath == "" {
		fatal("verify-pinned-key arguments are incomplete")
	}
	provided := readPublicKey(*keyPath)
	pinned, err := release.DecodePublicKey([]byte(release.StablePublicKeyHex))
	if err != nil || !bytes.Equal(pinned, provided) {
		fatal("source-pinned production public key is missing or does not match release environment")
	}
}

func readPublicKey(path string) ed25519.PublicKey {
	keyContents, err := os.ReadFile(path)
	if err != nil {
		fatal("public key is unavailable")
	}
	publicKey, err := release.DecodePublicKey(keyContents)
	if err != nil {
		fatal("public key is invalid")
	}
	return publicKey
}

func fatal(message string) {
	_, _ = fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
