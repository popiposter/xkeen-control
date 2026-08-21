#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
GO_BIN="${GO_BIN:-go}"
NPM_BIN="${NPM_BIN:-npm}"
VERSION="${VERSION:-$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo dev)}"
COMMIT="${COMMIT:-$(git -C "$ROOT" rev-parse HEAD 2>/dev/null || echo dev)}"
CHANNEL="${CHANNEL:-development}"
ASSET_DIR="$ROOT/internal/webassets/dist"
WEB_DIR="$ROOT/web"
OUTPUT="${OUTPUT:-$ROOT/dist/xkeen-control-linux-arm64}"

command -v "$GO_BIN" >/dev/null 2>&1 || { echo "ERROR: Go toolchain is required off-router" >&2; exit 1; }
command -v "$NPM_BIN" >/dev/null 2>&1 || { echo "ERROR: Node/npm toolchain is required off-router" >&2; exit 1; }

cd "$WEB_DIR"
"$NPM_BIN" ci
"$NPM_BIN" run build

mkdir -p "$ASSET_DIR"
find "$ASSET_DIR" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
cp -R "$WEB_DIR/dist/." "$ASSET_DIR/"
# Vite preserves CRLF from index.html on Windows bind mounts. Embedded assets
# are normalized so repository hygiene and arm64 builds stay deterministic.
find "$ASSET_DIR" -type f -exec sed -i 's/\r$//' {} +

mkdir -p "$(dirname -- "$OUTPUT")"
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 "$GO_BIN" build \
  -trimpath -buildvcs=false -ldflags="-s -w -X github.com/popiposter/xkeen-control/internal/buildinfo.Version=$VERSION -X github.com/popiposter/xkeen-control/internal/buildinfo.Commit=$COMMIT -X github.com/popiposter/xkeen-control/internal/buildinfo.Channel=$CHANNEL" \
  -o "$OUTPUT" "$ROOT/cmd/xkeen-control"

file "$OUTPUT" 2>/dev/null || true
ls -lh "$OUTPUT"
