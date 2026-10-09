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
# These fixtures exercise actual 32-bit crypto/JSON/platform clients under
# emulation; they contain no native/router processes or private credentials.
CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat go test -count=1 -exec qemu-mipsel ./internal/release ./internal/buildinfo ./internal/validationbudget
echo 'MIPS static soft-float ELF/startup and emulated platform fixtures passed (hardware NOTRUN)'
