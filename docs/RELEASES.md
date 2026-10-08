# Releases and panel updates

## Current release

Signed stable [v0.4.1](https://github.com/popiposter/xkeen-control/releases/tag/v0.4.1), published2026-10-08T21:27:57Z, from independently reviewed source `68d7ed356980897fc921001375b24cf2af10b90c`, tree `226c4c7195116650d545462c1986f8c6ebab3b4e`, source epoch1791494452.

Protected [run37846297734](https://github.com/popiposter/xkeen-control/actions/runs/37846297734) completed exact-source hosted FULL (Go/vet/race/helpers/frontend/embed/audit0, unit10/10, Chromium106/106), static ARM64/MIPS soft-float builds, emulated MIPS startup/platform fixtures and signing/publication. Fresh unauthenticated download verified the exact ten public assets with a verifier built from the released source: pinned Ed25519 key, both signatures/manifests, complete identity/compatibility/assets and all nine `SHA256SUMS` entries. Published MIPS version startup under qemu reports the exact release provenance. [Sanitized publication evidence](https://github.com/popiposter/xkeen-control/issues/4#issuecomment-6069426076).

| Binary | Bytes | SHA256 |
| --- | ---: | --- |
| linux/arm64 | 16,384,160 | `c3427bd99e7c658469525ce84bc22fa06373b69ebc4aaf06c8f57c4f0cd58257` |
| linux/mipsle soft-float | 18,874,559 | `3d16a7975ba0d52b9d6f7081189af8f3915282efb60ed0be247dddc5892bf9b5` |

Merged PR151 content equals independently approved PR HEAD `94cd622f852df4621135d47b93efc5fa03976cac`; local FULL remains bound to that SHA. Docs-only PR152 and exact frozen source were independently reviewed before publication; protected hosted FULL separately binds the released SHA. Later documentation commits are not the release source. Public verification/emulation does not establish signed hardware installation, RSS/auth/PTY/native preservation or independent LAN/IPv6/failure acceptance; these remain NOTRUN for0.4.1.

## Installation

Package targets:`linux/arm64` and `linux/mipsle` soft-float with confirmed Entware `mipsel-3.4`/`mipsel-3.4_kn`. For an existing stock XKeen/Xray with Entware, install only the panel. Existing KN1810 preparation and hardware limits are described in [MIPS Keenetic](MIPS-KEENETIC.md):

```sh
xkeen_installer=$(curl -fsSL https://github.com/popiposter/xkeen-control/releases/download/v0.4.1/install.sh) && sh -c "$xkeen_installer"
```

For a genuinely new supported Ultra KN1811/5.01.C.6.0-1, one private IPv4 bridge/one WAN and no host/custom DNS exceptions, use the explicit guided mode:

```sh
xkeen_installer=$(curl -fsSL https://github.com/popiposter/xkeen-control/releases/download/v0.4.1/install.sh) && sh -c "$xkeen_installer" -- --setup
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

Published0.4.1 from Issue150/PR151 retains all those names and the
ARM64 schema1 manifest, adding exactly `xkeen-control-linux-mipsle`,
`release-manifest-mipsle.json`, `release-manifest-mipsle.sig` (ten public files).
Each platform has its own signed four-artifact manifest; init/installer/updater
are shared. Both signatures, exact assets and equal version/channel/source/epoch/
compatibility are verified before publication. Existing ARM64 clients retain
their manifest URLs and artifact contract; new MIPS clients use the fixed
suffixed URLs and refuse ARM64 manifests/assets. Global SHA256SUMS covers nine
payload/manifest/signature files. Bootstrap downloads/verifies only its current
platform's six files while validating the complete checksum-name set.
Published0.4.0 remains ARM64-only and immutable. MIPS0.4.1 is panel-only; guided fresh setup capability is not expanded.

Only protected manual Release publishes: explicit version/channel/current-reviewed-main SHA, full read-only build gate, deterministic handoff, source-pinned public-key match, protected signing, verified draft re-download and final main recheck. Signing keys/router credentials never enter build qualification. Actions artifacts/raw main are not install authority. Panel Ed25519 trust does not imply signing of upstream native XKeen/component updates.

Independent host verification builds `cmd/xkeen-release` from exact released source, runs verify-pinned-key, then verify and verify-assets for each published architecture, and `sha256sum -c SHA256SUMS` on fresh downloads of the exact public set. For a dual-platform release this means ten downloads, both signatures/four-artifact manifests, equal source/version/channel/epoch/compatibility and linux/arm64 plus linux/mipsle identity. Bound retrieval to10minutes/128MiB. Historical ARM64 releases through0.4.0 require their seven files. Never bypass signature or manually repair failed publication.

## History

Stable v0.3.0:source `8140c9cda51ca9bf703d3a7965f4595f2c69445a`, protected run37239198359 and independent public verification passed. Stable0.3.1 added optional LAN DNS synchronization and build/test fixes: source `6e62d620630f5994b00acb2dfb60cbd690b44bcd`, protected run37427370603 and independent public verification PASS. Stable0.4.0 adds the restricted guided fresh installer. Legacy v0.2.0/D.1 qualification remains historical; [prior detailed release record](archive/RELEASES-before-0.3.1-cleanup.md) retains its separate appliance contract. Immutable released sources never become later documentation HEADs.

Stable [v0.4.0](https://github.com/popiposter/xkeen-control/releases/tag/v0.4.0) was published2026-10-08T20:16:53Z from source `2cda1d36712fa4b4dcf80ef9966358b45323c2d8`, tree `bb978a8e40d78e62a27003636a7626f722b1ba21`, epoch1791490118. Protected [run37837549187](https://github.com/popiposter/xkeen-control/actions/runs/37837549187) completed hosted FULL/signing/publication; independent source-built verification of seven assets passed. ARM64 binary16,384,160bytes SHA256 `6459c41ae72d0f163439a668eddbf8b10569655979a226e29616e04734ae501e`. [Immutable0.4.0 evidence](https://github.com/popiposter/xkeen-control/issues/4#issuecomment-6068324258). Local FULL separately binds PR147 HEAD `bd5d290772e788091c4847c02442e6ca1a308d46`; its tree equals that released tree. Hardware installation/fresh/LAN/IPv6/failure acceptance was not claimed by that publication.
