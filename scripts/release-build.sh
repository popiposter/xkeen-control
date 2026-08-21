#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
VERSION="${VERSION:?VERSION is required}"
CHANNEL="${CHANNEL:?CHANNEL is required}"
COMMIT="$(git -C "$ROOT" rev-parse HEAD)"
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git -C "$ROOT" show -s --format=%ct HEAD)}"
OUT="${OUT:-$ROOT/dist/release}"
KEY_FILE="${RELEASE_SIGNING_KEY_FILE:-}"
[ "$CHANNEL" = stable ] || [ "$CHANNEL" = beta ] || { echo "invalid channel" >&2; exit 2; }
[ -n "$KEY_FILE" ] || { echo "protected release signing key file is required" >&2; exit 2; }

rm -rf "$OUT"
mkdir -p "$OUT"
VERSION="$VERSION" CHANNEL="$CHANNEL" COMMIT="$COMMIT" ./scripts/build-control-plane.sh
cp "$ROOT/dist/xkeen-control-linux-arm64" "$OUT/xkeen-control-linux-arm64"
cp "$ROOT/packaging/S99xkeen-control" "$OUT/S99xkeen-control"
cp "$ROOT/scripts/xkeen-control-updater" "$OUT/xkeen-control-updater"
sed "s/^STABLE_RELEASE_VERSION=\"\"$/STABLE_RELEASE_VERSION=\"$VERSION\"/" "$ROOT/scripts/install.sh" > "$OUT/install.sh"
chmod 755 "$OUT"/xkeen-control-linux-arm64 "$OUT"/S99xkeen-control "$OUT"/xkeen-control-updater "$OUT"/install.sh

go run ./cmd/xkeen-release manifest --output "$OUT/release-manifest.json" --version "$VERSION" --commit "$COMMIT" --channel "$CHANNEL" --source-date-epoch "$SOURCE_DATE_EPOCH" \
	--asset "xkeen-control-linux-arm64=$OUT/xkeen-control-linux-arm64" \
	--asset "S99xkeen-control=$OUT/S99xkeen-control" \
	--asset "xkeen-control-updater=$OUT/xkeen-control-updater" \
	--asset "install.sh=$OUT/install.sh"
go run ./cmd/xkeen-release sign --manifest "$OUT/release-manifest.json" --key-file "$KEY_FILE" --output "$OUT/release-manifest.sig"
(cd "$OUT" && sha256sum xkeen-control-linux-arm64 S99xkeen-control xkeen-control-updater install.sh release-manifest.json release-manifest.sig > SHA256SUMS)
