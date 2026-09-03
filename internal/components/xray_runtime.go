package components

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/popiposter/xkeen-control/internal/nodes"
)

// CommandXrayCandidateProbe is the fixed version-only probe used for both
// staged and active Xray binaries. It does not accept a subcommand or args.
type CommandXrayCandidateProbe struct {
	Binary string
}

func (p CommandXrayCandidateProbe) ProbeXrayCandidate(ctx context.Context, binary string) XrayVersionResult {
	if binary == "" {
		binary = p.Binary
	}
	if binary == "" {
		return XrayVersionResult{ExitCode: -1, Err: errXrayBinaryInvalid}
	}
	return commandXrayVersionProbe{binary: binary}.ProbeXrayVersion(ctx)
}

// CommandXrayCandidateValidator owns the one fixed candidate validation
// command. Output is bounded and discarded; neither output nor a command
// error is returned to an external caller.
type CommandXrayCandidateValidator struct {
	Binary string
}

func (v CommandXrayCandidateValidator) ValidateXrayCandidate(ctx context.Context, binary, configDir, assetDir string) error {
	if binary == "" {
		binary = v.Binary
	}
	if binary == "" || configDir == "" || assetDir == "" {
		return errXrayCandidateConfigInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}
	limit := &outputLimit{remaining: MaxXrayProbeOutput}
	stdout := &boundedOutput{limit: limit}
	stderr := &boundedOutput{limit: limit}
	command := exec.CommandContext(ctx, binary, "run", "-test", "-confdir", configDir)
	command.Dir = configDir
	command.Env = componentXrayEnvironment(assetDir)
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil || limit.exceeded() {
		return errXrayCandidateConfigInvalid
	}
	return nil
}

// CommandXrayRuntime adapts the existing typed XKeen/Xray activator and C.1
// reachability probe to the Phase C runtime contract. No lifecycle command is
// configurable through this adapter.
type CommandXrayRuntime struct {
	Activator      nodes.Activator
	ActiveBinary   string
	ConfigDir      string
	AssetDir       string
	ProbeReachable func(context.Context) bool
}

func (r CommandXrayRuntime) ValidateActiveConfig(ctx context.Context) error {
	if r.ActiveBinary == "" || r.ConfigDir == "" || r.AssetDir == "" {
		return errXrayCandidateConfigInvalid
	}
	return (CommandXrayCandidateValidator{}).ValidateXrayCandidate(ctx, r.ActiveBinary, r.ConfigDir, r.AssetDir)
}

func (r CommandXrayRuntime) Restart(ctx context.Context) error {
	if r.Activator == nil {
		return errors.New("Xray runtime is unavailable")
	}
	return r.Activator.Restart(ctx)
}

func (r CommandXrayRuntime) WaitReady(ctx context.Context) error {
	if r.Activator == nil {
		return errors.New("Xray runtime is unavailable")
	}
	return r.Activator.WaitReady(ctx)
}

func (r CommandXrayRuntime) Verify(ctx context.Context, expectedTags []string) error {
	if r.Activator == nil || r.ProbeReachable == nil || len(expectedTags) == 0 {
		return errors.New("Xray runtime verification is unavailable")
	}
	if err := r.Activator.VerifyOutboundTags(ctx, expectedTags); err != nil {
		return err
	}
	if !r.ProbeReachable(ctx) {
		return errors.New("Xray C.1 probe is unavailable")
	}
	return nil
}

func componentXrayEnvironment(assetDir string) []string {
	environment := os.Environ()
	if assetDir == "" {
		return environment
	}
	const assetPrefix = "XRAY_LOCATION_ASSET="
	for index, entry := range environment {
		if strings.HasPrefix(entry, assetPrefix) {
			environment[index] = assetPrefix + assetDir
			return environment
		}
	}
	return append(environment, assetPrefix+assetDir)
}
