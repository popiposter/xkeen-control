#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
VERSION="${VERSION:?VERSION is required}"
CHANNEL="${CHANNEL:?CHANNEL is required}"
COMMIT="$(git -C "$ROOT" rev-parse HEAD)"
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git -C "$ROOT" show -s --format=%ct HEAD)}"
OUT="${OUT:-$ROOT/dist/release}"
MODE="${RELEASE_BUILD_MODE:-signed}"
KEY_FILE="${RELEASE_SIGNING_KEY_FILE:-}"
[ "$CHANNEL" = stable ] || [ "$CHANNEL" = beta ] || { echo "invalid channel" >&2; exit 2; }
[ "$MODE" = unsigned ] || [ "$MODE" = signed ] || { echo "invalid release build mode" >&2; exit 2; }
if [ "$MODE" = signed ]; then
	[ -n "$KEY_FILE" ] || { echo "protected release signing key file is required" >&2; exit 2; }
fi

# The full gate already built and checked these bytes. Release assembly must
# neither install npm dependencies again nor rewrite its qualified Go embed input.
git -C "$ROOT" diff --exit-code HEAD -- "$ROOT/internal/webassets/dist"
bash "$ROOT/scripts/verify-webassets.sh"
rm -rf "$OUT"
mkdir -p "$OUT"
cp "$ROOT/packaging/S99xkeen-control" "$OUT/S99xkeen-control"
cp "$ROOT/scripts/xkeen-control-updater" "$OUT/xkeen-control-updater"
sed "s/^STABLE_RELEASE_VERSION=\"\"$/STABLE_RELEASE_VERSION=\"$VERSION\"/" "$ROOT/scripts/install.sh" > "$OUT/install.sh"
chmod 755 "$OUT/S99xkeen-control" "$OUT/xkeen-control-updater" "$OUT/install.sh"
for architecture in arm64 mipsle; do
    VERSION="$VERSION" CHANNEL="$CHANNEL" COMMIT="$COMMIT" ARCHITECTURE="$architecture" ./scripts/build-control-plane.sh --embedded
    binary="xkeen-control-linux-$architecture"
    cp "$ROOT/dist/$binary" "$OUT/$binary"
    manifest=release-manifest
    if [ "$architecture" = mipsle ]; then manifest=release-manifest-mipsle; fi
    go run ./cmd/xkeen-release manifest --output "$OUT/$manifest.json" --architecture "$architecture" --version "$VERSION" --commit "$COMMIT" --channel "$CHANNEL" --source-date-epoch "$SOURCE_DATE_EPOCH" \
        --asset "$binary=$OUT/$binary" \
        --asset "S99xkeen-control=$OUT/S99xkeen-control" \
        --asset "xkeen-control-updater=$OUT/xkeen-control-updater" \
        --asset "install.sh=$OUT/install.sh"
    if [ "$MODE" = signed ]; then
        go run ./cmd/xkeen-release sign --manifest "$OUT/$manifest.json" --key-file "$KEY_FILE" --output "$OUT/$manifest.sig"
    fi
done
git -C "$ROOT" diff --exit-code HEAD -- "$ROOT/internal/webassets/dist"
if [ "$MODE" = signed ]; then
    (cd "$OUT" && sha256sum xkeen-control-linux-arm64 xkeen-control-linux-mipsle S99xkeen-control xkeen-control-updater install.sh release-manifest.json release-manifest.sig release-manifest-mipsle.json release-manifest-mipsle.sig > SHA256SUMS)
fi
