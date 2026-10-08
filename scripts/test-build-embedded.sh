#!/usr/bin/env bash
set -euo pipefail
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/scripts" "$work/internal/webassets/dist" "$work/bin"
cp "$ROOT/scripts/build-control-plane.sh" "$work/scripts/"
printf 'synthetic embedded bytes\n' > "$work/internal/webassets/dist/index.html"
before=$(sha256sum "$work/internal/webassets/dist/index.html")
cat > "$work/bin/go" <<'GO'
#!/bin/sh
test "$CGO_ENABLED" = 0 && test "$GOOS" = linux
case "$GOARCH" in arm64) ;; mipsle) test "$GOMIPS" = softfloat ;; *) exit 72 ;; esac
while [ "$#" -gt 0 ]; do
    if [ "$1" = -o ]; then shift; printf 'synthetic binary' > "$1"; exit 0; fi
    shift
done
exit 1
GO
printf '#!/bin/sh\nexit 73\n' > "$work/bin/npm"
chmod +x "$work/bin/"*
PATH="$work/bin:$PATH" VERSION=test COMMIT=synthetic CHANNEL=development sh "$work/scripts/build-control-plane.sh" --embedded
test -s "$work/dist/xkeen-control-linux-arm64"
PATH="$work/bin:$PATH" ARCHITECTURE=mipsle VERSION=test COMMIT=synthetic CHANNEL=development sh "$work/scripts/build-control-plane.sh" --embedded
test -s "$work/dist/xkeen-control-linux-mipsle"
if PATH="$work/bin:$PATH" ARCHITECTURE=mips sh "$work/scripts/build-control-plane.sh" --embedded >/dev/null 2>&1; then
    echo 'unsupported build architecture accepted' >&2; exit 1
fi
test "$before" = "$(sha256sum "$work/internal/webassets/dist/index.html")"
if PATH="$work/bin:$PATH" sh "$work/scripts/build-control-plane.sh" --unknown >/dev/null 2>&1; then
    echo 'unknown build mode accepted' >&2; exit 1
fi
echo 'Embedded-only assembly fixture passed (no npm, unchanged embedded bytes)'
