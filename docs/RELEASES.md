# Releases and panel updates

## Current release

Signed stable [v0.3.1](https://github.com/popiposter/xkeen-control/releases/tag/v0.3.1), published2026-10-06, from independently reviewed source `6e62d620630f5994b00acb2dfb60cbd690b44bcd`, tree `7858938c560051453a1d3346eb5aa586a7e6eda9`.

Exact-main FULL106browser/Go/race/helpers/frontend/embed/audit0/ARM passed. Protected [run37427370603](https://github.com/popiposter/xkeen-control/actions/runs/37427370603) completed build and publish. Fresh unauthenticated download verified the exact seven public assets, pinned Ed25519 key/signature, manifest identity/compatibility/assets and actual `sha256sum -c SHA256SUMS`. ARM64 binary:15,990,944bytes, SHA256 `49b77af3269dc1d2d786ea4de0e618f3c765acd8f6c4aaec516b9cb848b54dc6`.

Public verification is not signed-release installation evidence. The operator router's accepted development build and historical DNS outage proof are separate. Earlier run37424360277 failed a synthetic DNS fixture, never published and was not rerun; reviewed fixes preceded the new exact-source freeze.

## Installation

Stock XKeen with Xray and Entware must already be installed. The panel installer does not patch/install/repair native components. Supported package target:`linux/arm64`.

```sh
sh -c "$(curl -fsSL https://github.com/popiposter/xkeen-control/releases/download/v0.3.1/install.sh)"
```

Initial panel password is generated and printed once; existing authority is preserved. Default listener is loopback8787; use a private tunnel or one explicit trusted LAN address. No WAN/wildcard listener. See [quick start](QUICKSTART-RU.md).

Existing managed panel updates use the installed binary's pinned-signature self-update path. Installed marker, helper/layout, resources, lifecycle quiescence and exact candidate checks remain mandatory. No supported migration contract from historical appliance generations; no repair by copying individual secret/runtime files.

## Distribution trust

Each release has exactly:

```text
xkeen-control-linux-arm64
S99xkeen-control
xkeen-control-updater
install.sh
release-manifest.json
release-manifest.sig
SHA256SUMS
```

Only protected manual Release publishes: explicit version/channel/current-reviewed-main SHA, full read-only build gate, deterministic handoff, source-pinned public-key match, protected signing, verified draft re-download and final main recheck. Signing keys/router credentials never enter build qualification. Actions artifacts/raw main are not install authority. Panel Ed25519 trust does not imply signing of upstream native XKeen/component updates.

Independent host verification builds `cmd/xkeen-release` from exact released source, runs verify-pinned-key, verify, verify-assets and `sha256sum -c SHA256SUMS` on fresh exact seven downloads. Bound retrieval to10minutes/128MiB; compare source/version/channel/epoch/linux/arm64/compatibility. Never bypass signature or manually repair failed publication.

## History

Stable v0.3.0:source `8140c9cda51ca9bf703d3a7965f4595f2c69445a`, protected run37239198359 and independent public verification passed. Stable0.3.1 adds optional LAN DNS synchronization and reviewed build/test fixes. Legacy v0.2.0/D.1 qualification remains historical; [prior detailed release record](archive/RELEASES-before-0.3.1-cleanup.md) retains its separate appliance contract. Immutable released sources never become later documentation HEADs.
