# Releases and panel updates

## Current release

Signed stable [v0.4.4](https://github.com/popiposter/xkeen-control/releases/tag/v0.4.4), published 2026-10-09T09:41:53Z, from independently reviewed source `27daa9abdfec946e150a2bb934f96ddd7aec3125`, tree `ee178ad47d3bb9da3ebf978971063c36c695ce98`, source epoch 1791538428.

Protected [run37912340620](https://github.com/popiposter/xkeen-control/actions/runs/37912340620) completed exact-source hosted FULL and signing/publication. Fresh unauthenticated downloads verified exactly ten public assets with the source-built verifier: pinned Ed25519 key, both signatures/manifests, sizes/hashes, nine SHA256SUMS entries, static ELF/soft-float and the published MIPS version under qemu.

| Binary | Bytes | SHA256 |
| --- | ---: | --- |
| linux/arm64 | 16,515,232 | `051208a443148f32339feb230afa62ef20106636072112bd858549d571d17008` |
| linux/mipsle soft-float | 19,071,167 | `1521f54d68fea16c8ffdb95c1f9d0f93a85b3e85ae3ad18075ff592afad09490` |

[PR163](https://github.com/popiposter/xkeen-control/pull/163) delivers [#162](https://github.com/popiposter/xkeen-control/issues/162): durable update receipts, bounded readiness, checked file commits/rollback and verified RAM maintenance delivery for older helpers. Independent source review and local FULL bind `93cd2b617a2b8886d5be015e25feb69d5fe5593a`; the released merge tree is identical. Frozen-source review and protected hosted FULL separately bind the released merge SHA. Later documentation commits do not change that source.

Signed 0.4.4 hardware installation, node recovery, controlled resource comparison and independent client/DNS/failure acceptance remain **NOTRUN**. Issues [#157](https://github.com/popiposter/xkeen-control/issues/157), [#158](https://github.com/popiposter/xkeen-control/issues/158) and [#162](https://github.com/popiposter/xkeen-control/issues/162) remain open. The failed 0.4.3 update and observed rollback to 0.4.2 remain separate historical evidence below. Publication does not settle an interrupted operation or authorize replay; follow [maintenance delivery and inspection](OPERATIONS.md#durable-update-outcomes-162).

## Installation

Package targets:`linux/arm64` and `linux/mipsle` soft-float with confirmed Entware `mipsel-3.4`/`mipsel-3.4_kn`. For an existing stock XKeen/Xray with Entware, install only the panel. Existing KN1810 preparation and hardware limits are described in [MIPS Keenetic](MIPS-KEENETIC.md):

```sh
xkeen_installer=$(curl -fsSL https://github.com/popiposter/xkeen-control/releases/download/v0.4.4/install.sh) && sh -c "$xkeen_installer"
```

For a genuinely new supported Ultra KN1811/5.01.C.6.0-1, one private IPv4 bridge/one WAN and no host/custom DNS exceptions, use the explicit guided mode:

```sh
xkeen_installer=$(curl -fsSL https://github.com/popiposter/xkeen-control/releases/download/v0.4.4/install.sh) && sh -c "$xkeen_installer" -- --setup
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

### Stable 0.4.3 publication and failed installation

Signed stable [v0.4.3](https://github.com/popiposter/xkeen-control/releases/tag/v0.4.3), published 2026-10-09T08:14:04Z, from independently reviewed source `a98f090b2611f90a450458476383c62defafe966`, tree `1d4ec798795a2dea1db4e03fd0420cf20170da5f`, source epoch 1791533192.

Protected [run37903102185](https://github.com/popiposter/xkeen-control/actions/runs/37903102185) completed exact-source hosted FULL and signing/publication. Fresh unauthenticated downloads verified exactly ten public assets using a verifier built from the released source: pinned Ed25519 key, both signatures/manifests, asset identity/compatibility/size/hash and all nine SHA256SUMS entries. Static ELF/soft-float and published MIPS version startup under qemu passed.

| Binary | Bytes | SHA256 |
| --- | ---: | --- |
| linux/arm64 | 16,515,232 | `341f77bed61ab75d9635d00bd0b5daa34cd0c5d61027a11c20eb6017b7f5c501` |
| linux/mipsle soft-float | 19,005,631 | `b100eabd1c87c9e14a6ba9c57e2d8c8ec932b3572ed0bc7f155de49748a53e35` |

[PR159](https://github.com/popiposter/xkeen-control/pull/159) delivers hardware-derived diagnostic budgets, resource pressure cancellation, native benchmark conflict reporting and preservation of native routing criteria. [PR160](https://github.com/popiposter/xkeen-control/pull/160) adds explicit offline recovery of coherent interrupted node transactions, with existing process-lock exclusion and durable no-replay fencing. Both received independent source review and local FULL. Final PR160 qualification binds `16900459f250339a352d9480370ada36e366a4f2`; the merged release tree is identical. Frozen-source review and protected hosted FULL separately bind the released merge SHA. Later documentation commits do not change the immutable release source.

One authorized KN1810 update attempt reached the signed 0.4.3 candidate and then the existing helper rolled back to signed 0.4.2. Installation acceptance **FAILED**; the terminal cause is unknown because the old helper did not retain a durable outcome. Fresh readback confirmed the restored binary and preserved native/configuration state. A transient candidate version is not installation proof. [#162](https://github.com/popiposter/xkeen-control/issues/162) addresses durable outcomes and bounded readiness; do not replay the old update command.

Actual node recovery, controlled resource comparison and independent client/DNS/failure acceptance remain pending. Issues [#157](https://github.com/popiposter/xkeen-control/issues/157) and [#158](https://github.com/popiposter/xkeen-control/issues/158) remain open for hardware acceptance. Publication/emulation does not establish router success.

### Stable 0.4.2 publication record

Signed stable [v0.4.2](https://github.com/popiposter/xkeen-control/releases/tag/v0.4.2), published2026-10-09T06:18:10Z, from independently reviewed source `48c0f11e127edb6c7463c003f235467938ffa958`, tree `5c5e06c19bf956fa757e249f4fe92eaf36657ff1`, source epoch1791525924.

Protected [run37891859918](https://github.com/popiposter/xkeen-control/actions/runs/37891859918) completed exact-source hosted FULL and signing/publication. Fresh unauthenticated download verified the exact ten public assets with a verifier built from the released source: pinned Ed25519 key, both signatures/manifests, complete identity/compatibility/assets and all nine SHA256SUMS entries. Static ELF/soft-float and published MIPS version startup under qemu passed.

| Binary | Bytes | SHA256 |
| --- | ---: | --- |
| linux/arm64 | 16,449,696 | `92910693cc103a17501c36dc3316fa0e2bd8d6732d63daebce452a58991f7eb7` |
| linux/mipsle soft-float | 19,005,631 | `4d21b277b681ad47c262f3c103ca44eb8d09b7d0a5d5924ac60d7a60d0e483fa` |

[PR155](https://github.com/popiposter/xkeen-control/pull/155) adds MIPS120s full validation,125s configured preparation,375s node transactions and narrow150s UI waits. Bootstrap checks stat metadata and jq regex capabilities before placement. Independent source review and local FULL (11 Node tests,108 browser tests, Go/vet/race/helpers/embed/audit0 and both builds/emulation) bind `59bd1798a8bf1c322af059ddbff4a0f2767101ea`; the merged/released tree is identical. Frozen merged-source review and protected hosted FULL separately bind48c0f11. Later documentation commits are not the release source.

Public verification/emulation does not establish signed0.4.2 hardware installation, native validation/Apply, or independent LAN/IPv6/failure acceptance; these remain NOTRUN. Published0.4.1 from `68d7ed356980897fc921001375b24cf2af10b90c` remains immutable; its original [publication evidence](https://github.com/popiposter/xkeen-control/issues/4#issuecomment-6069426076) is separate.


### Earlier releases

Stable v0.3.0:source `8140c9cda51ca9bf703d3a7965f4595f2c69445a`, protected run37239198359 and independent public verification passed. Stable0.3.1 added optional LAN DNS synchronization and build/test fixes: source `6e62d620630f5994b00acb2dfb60cbd690b44bcd`, protected run37427370603 and independent public verification PASS. Stable0.4.0 adds the restricted guided fresh installer. Legacy v0.2.0/D.1 qualification remains historical; [prior detailed release record](archive/RELEASES-before-0.3.1-cleanup.md) retains its separate appliance contract. Immutable released sources never become later documentation HEADs.

Stable [v0.4.0](https://github.com/popiposter/xkeen-control/releases/tag/v0.4.0) was published2026-10-08T20:16:53Z from source `2cda1d36712fa4b4dcf80ef9966358b45323c2d8`, tree `bb978a8e40d78e62a27003636a7626f722b1ba21`, epoch1791490118. Protected [run37837549187](https://github.com/popiposter/xkeen-control/actions/runs/37837549187) completed hosted FULL/signing/publication; independent source-built verification of seven assets passed. ARM64 binary16,384,160bytes SHA256 `6459c41ae72d0f163439a668eddbf8b10569655979a226e29616e04734ae501e`. [Immutable0.4.0 evidence](https://github.com/popiposter/xkeen-control/issues/4#issuecomment-6068324258). Local FULL separately binds PR147 HEAD `bd5d290772e788091c4847c02442e6ca1a308d46`; its tree equals that released tree. Hardware installation/fresh/LAN/IPv6/failure acceptance was not claimed by that publication.
