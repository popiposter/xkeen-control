#!/usr/bin/env bash
set -euo pipefail
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"
binary=dist/xkeen-control-linux-mipsle
command -v qemu-mipsel >/dev/null
command -v readelf >/dev/null
header=$(od -An -tx1 -N20 "$binary" | tr -d ' \n')
[[ "$header" == 7f454c460101* && "${header:36:4}" == 0800 ]] || {
    echo 'MIPS binary must be ELF32 little-endian EM_MIPS' >&2; exit 1;
}
if readelf -l "$binary" | grep -q INTERP; then
    echo 'MIPS binary must be static' >&2; exit 1
fi
metadata=$(go version -m "$binary")
grep -Fq 'GOARCH=mipsle' <<< "$metadata"
grep -Fq 'GOMIPS=softfloat' <<< "$metadata"
grep -Fq 'CGO_ENABLED=0' <<< "$metadata"
qemu-mipsel "$binary" version --json | jq -e '.product == "xkeen-control" and (.version | type == "string") and (.sourceCommit | type == "string")' >/dev/null
# The real CLI must refuse missing admission without creating persistent state.
# Positive descriptor/capability behavior is exercised below on synthetic paths.
test ! -e /opt/var/lock/xkeen-control
test ! -L /opt/var/lock/xkeen-control
capability_output=$(mktemp)
if qemu-mipsel "$binary" self-update inspect-capabilities >"$capability_output" 2>/dev/null; then
    rm -f "$capability_output"
    echo 'Missing setup admission was accepted' >&2; exit 1
fi
jq -e '.ready == false and .reason == "setup-admission-unavailable" and .sync == "not-run" and .timeout == "not-run" and .flock == "not-run"' "$capability_output" >/dev/null
rm -f "$capability_output"
test ! -e /opt/var/lock/xkeen-control
test ! -L /opt/var/lock/xkeen-control
# These fixtures exercise actual 32-bit crypto/JSON/platform clients under
# emulation; they contain no native/router processes or private credentials.
CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat go test -count=1 -exec qemu-mipsel ./internal/release ./internal/buildinfo ./internal/validationbudget
# Execute the fixed capability diagnostic logic on MIPS, using synthetic RAM
# files and real host lock utilities. This is ABI/process evidence, not proof
# of any firmware shell or installed router generation.
CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat go test -count=1 -exec qemu-mipsel \
    -run 'Test(DirectFlockSequenceAndCleanup|FlockRejectsNonConflictFailures|FlockUsesOneDeadlineAndCleansOnTimeout|DroppedShellTailDefectDoesNotAffectDirectProbe|DiagnosticSharedChecksOnlyTouchRAM|InspectNormalNeverCreatesAndReusesSharedAdmission|CapabilityInspectionReleasesAdmissionAndReportsFailures|UpdateInspectionAllowlistIsExact)$' \
    ./internal/update ./internal/setup ./cmd/xkeen-control
echo 'MIPS static soft-float ELF/startup and emulated platform fixtures passed (hardware NOTRUN)'
