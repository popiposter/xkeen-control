# Releases and panel updates

## Current release

Signed stable [v0.4.0](https://github.com/popiposter/xkeen-control/releases/tag/v0.4.0), published2026-10-08T20:16:53Z, from independently reviewed source `2cda1d36712fa4b4dcf80ef9966358b45323c2d8`, tree `bb978a8e40d78e62a27003636a7626f722b1ba21`, source epoch1791490118.

Protected [run37837549187](https://github.com/popiposter/xkeen-control/actions/runs/37837549187) completed exact-source hosted FULL (Go/vet/race/helpers/frontend/embed/audit0, unit10/10, Chromium106/106) and signing/publication. Fresh unauthenticated download verified the exact seven public assets with a verifier built from the released source: pinned Ed25519 key/signature, complete manifest identity/compatibility/assets and actual `sha256sum -c SHA256SUMS`. ARM64 binary:16,384,160bytes, SHA256 `6459c41ae72d0f163439a668eddbf8b10569655979a226e29616e04734ae501e`. [Sanitized publication evidence](https://github.com/popiposter/xkeen-control/issues/4#issuecomment-6068324258).

The merged release tree equals the independently approved PR147 tree; its prior local FULL remains bound to PR HEAD `bd5d290772e788091c4847c02442e6ca1a308d46`, separately from hosted FULL on the released SHA. Later documentation commits are not the release source. Public verification does not establish signed installation or fresh hardware/LAN/IPv6/failure-fallback acceptance; these remain NOTRUN for0.4.0.

## Installation

Supported package target:`linux/arm64`. For an existing stock XKeen/Xray with Entware, install only the panel:

```sh
xkeen_installer=$(curl -fsSL https://github.com/popiposter/xkeen-control/releases/download/v0.4.0/install.sh) && sh -c "$xkeen_installer"
```

For a genuinely new supported Ultra KN1811/5.01.C.6.0-1, one private IPv4 bridge/one WAN and no host/custom DNS exceptions, use the explicit guided mode:

```sh
xkeen_installer=$(curl -fsSL https://github.com/popiposter/xkeen-control/releases/download/v0.4.0/install.sh) && sh -c "$xkeen_installer" -- --setup
```

It privately imports the source, prepares stock2.1 native components, panel, mosdns and a validated reference generation, then assigns HOME last. [Fresh prerequisites, ordering and interrupted setup](FRESH-KEENETIC.md) govern this restricted route; it never patches native code or reinstalls existing components. Other firmware/model/topology support remains unavailable.

Initial panel password is printed once; existing authority is preserved by panel-only installation. Its default listener is loopback8787; guided setup chooses the exact discovered trusted HOME address. No WAN/wildcard listener. See [quick start](QUICKSTART-RU.md).

Existing managed panel updates use the installed binary's pinned-signature self-update path. Installed marker, helper/layout, resources, lifecycle quiescence and exact candidate checks remain mandatory. No supported migration contract from historical appliance generations; no repair by copying individual secret/runtime files.

## Distribution trust

The published ARM64 releases through0.4.0 have exactly:

```text
xkeen-control-linux-arm64
S99xkeen-control
xkeen-control-updater
install.sh
release-manifest.json
release-manifest.sig
SHA256SUMS
```

Issue150's subsequent dual-platform candidate retains all those names and the
ARM64 schema1 manifest, adding exactly `xkeen-control-linux-mipsle`,
`release-manifest-mipsle.json`, `release-manifest-mipsle.sig` (ten public files).
Each platform has its own signed four-artifact manifest; init/installer/updater
are shared. Both signatures, exact assets and equal version/channel/source/epoch/
compatibility are verified before publication. Existing ARM64 clients retain
their manifest URLs and artifact contract; new MIPS clients use the fixed
suffixed URLs and refuse ARM64 manifests/assets. Global SHA256SUMS covers nine
payload/manifest/signature files. Bootstrap downloads/verifies only its current
platform's six files while validating the complete checksum-name set.
This candidate is not published0.4.0 and does not authorize a MIPS install yet.

Only protected manual Release publishes: explicit version/channel/current-reviewed-main SHA, full read-only build gate, deterministic handoff, source-pinned public-key match, protected signing, verified draft re-download and final main recheck. Signing keys/router credentials never enter build qualification. Actions artifacts/raw main are not install authority. Panel Ed25519 trust does not imply signing of upstream native XKeen/component updates.

Independent host verification builds `cmd/xkeen-release` from exact released source, runs verify-pinned-key, verify, verify-assets and `sha256sum -c SHA256SUMS` on fresh exact seven downloads. Bound retrieval to10minutes/128MiB; compare source/version/channel/epoch/linux/arm64/compatibility. Never bypass signature or manually repair failed publication.

## History

Stable v0.3.0:source `8140c9cda51ca9bf703d3a7965f4595f2c69445a`, protected run37239198359 and independent public verification passed. Stable0.3.1 added optional LAN DNS synchronization and build/test fixes: source `6e62d620630f5994b00acb2dfb60cbd690b44bcd`, protected run37427370603 and independent public verification PASS. Stable0.4.0 adds the restricted guided fresh installer. Legacy v0.2.0/D.1 qualification remains historical; [prior detailed release record](archive/RELEASES-before-0.3.1-cleanup.md) retains its separate appliance contract. Immutable released sources never become later documentation HEADs.
