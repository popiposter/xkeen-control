# Releases and panel updates

## Current release

Signed stable [v0.4.9](https://github.com/popiposter/xkeen-control/releases/tag/v0.4.9), published 2026-10-09T21:28:51Z, from independently reviewed source `771df6c35e303fa8842fb5c76503db62e1eec238`, tree `2190d50526fba595fc0f926d261fc407738fe778`, source epoch 1791580884.

Protected [run37993030915](https://github.com/popiposter/xkeen-control/actions/runs/37993030915) passed hosted FULL and publication. Fresh unauthenticated downloads verified exactly ten assets, both pinned signatures/manifests, sizes/hashes, static ELF identity, MIPS soft-float and emulated version. Earlier run37991358440 failed before checkout because Docker Hub rate-limited the job image; it did not publish a tag or assets and was not replayed. [PR186](https://github.com/popiposter/xkeen-control/pull/186) pins the official Node image through the public ECR mirror; exact source review and local FULL bind `c3600cc8fdf8c8c2accb6df9bb2c9f60ea1cad82`, whose tree matches the released merge.

| Binary | Bytes | SHA256 |
| --- | ---: | --- |
| linux/arm64 | 16,580,768 | `60f6b6b451e610009800ce1def4c94ea15de9c00e11025114a3419c02527b88d` |
| linux/mipsle soft-float | 19,071,167 | `f30de1e938541927f31903355b78080f550b1de518745f2a969d2ce0840365ae` |

[PR184](https://github.com/popiposter/xkeen-control/pull/184) fixes the MIPS manual speed test when the kernel omits `pswpout`, preserving fail-closed CPU/memory/swap pressure checks. Exact source review, local FULL and pre-release MIPS hardware telemetry admission passed. Signed0.4.9 installation on KN1810 is independently verified (`installed-verified`, matching binary/source and preserved Xray configuration). One bounded 4 MiB manual test completed all seven stages in 9.602 s without an error; Xray/API remained healthy and cron was restored. This establishes the tested node/path, not all-node throughput or fresh-install acceptance.

The release also includes [PR182](https://github.com/popiposter/xkeen-control/pull/182): fresh setup now uses compact selective routing and ordinary Keenetic DNS without a mosdns install. Existing router DNS retirement is a separate operator action and is not performed by a panel update.

## Previous stable 0.4.8

Signed stable [v0.4.8](https://github.com/popiposter/xkeen-control/releases/tag/v0.4.8), published 2026-10-09T14:40:11Z, from independently reviewed source `6796dbc3e4b914ba89a29ef9019cff9cb1a7f7c3`, tree `95870ad4fb5dadf07df7f3ac0a97708986002a40`, source epoch 1791556209.

Protected [run37945046129](https://github.com/popiposter/xkeen-control/actions/runs/37945046129) passed exact-source hosted FULL and publication. Independent unauthenticated downloads verified all ten assets, both pinned signatures/manifests, hashes/sizes, static ABI and published MIPS soft-float startup under emulation.

| Binary | Bytes | SHA256 |
| --- | ---: | --- |
| linux/arm64 | 16,646,304 | `0e4805d06e496019f9868690aa00da8fe4c184f906307a9020a6004e12e059ea` |
| linux/mipsle soft-float | 19,202,239 | `99679eaa7892131d42c57ba3f051a463f928212681c8a9f3238b8f3d2fb9ee26` |

[PR177](https://github.com/popiposter/xkeen-control/pull/177) delivers [#176](https://github.com/popiposter/xkeen-control/issues/176): bounded in-process ListRule readiness without extra Xray processes, bounded transport establishment and truthful durable failure classes. Exact source review and local FULL bind `39637508529c03fea609eb2ec08b21705dfbc172`; the released merge tree matches. Later documentation changes do not alter released source.

Signed0.4.8 installation and a new controlled subscription transaction remain **NOTRUN**. Signed0.4.7 installation and its separately verified recovery settlement passed, but a later automatic subscription transaction retained a new previous-branch readiness failure. The source repair and publication do not establish its cause or settle that attempt. Controlled resource/client acceptance remains separate; no failed operation may be replayed.

## Previous stable 0.4.7

Signed stable [v0.4.7](https://github.com/popiposter/xkeen-control/releases/tag/v0.4.7), published 2026-10-09T13:32:11Z, from independently reviewed source `2a92e949031850906eb22ca869fcb14a804b7fd4`, tree `534a5c06ca7bb29fd058a9d5a358ce3c1b4db2b2`, source epoch 1791552177.

Protected [run37936693765](https://github.com/popiposter/xkeen-control/actions/runs/37936693765) passed hosted FULL and publication. Independent unauthenticated downloads verified all ten assets, both pinned signatures/manifests, hashes/sizes, static ABI and published MIPS soft-float startup under emulation.

| Binary | Bytes | SHA256 |
| --- | ---: | --- |
| linux/arm64 | 16,646,304 | `fe71f42fcbaea5325bccaad08abe235436da21a33199b52cf9bc50daf6abc472` |
| linux/mipsle soft-float | 19,202,239 | `a67cbf996e7d194c46e09a165a7a221498fb3fdca36bd4e9b72a677ba3b7bef5` |

[PR172](https://github.com/popiposter/xkeen-control/pull/172) delivers #171 branch-specific node transaction proof and durable recovery outcomes. [PR174](https://github.com/popiposter/xkeen-control/pull/174) fixes two asynchronous qualification fixtures without changing production scheduling. Exact source review and local FULL bind `849c0a5b3a75f37a0a96dbe6d957ce69242d3916`; the released merge tree matches. Earlier run37934676894 failed before packaging/publication and remains failed. Later documentation changes do not alter released source.

Signed0.4.7 installation and separately authorized recovery settlement subsequently passed [hardware readback](https://github.com/popiposter/xkeen-control/issues/171#issuecomment-6082322961). A later automatic subscription transaction retained a new previous-branch readiness failure. Old completion evidence cannot settle that marker. Controlled resource/client acceptance remains pending; no failed operation may be replayed.

## Previous stable 0.4.6

Signed stable [v0.4.6](https://github.com/popiposter/xkeen-control/releases/tag/v0.4.6), published 2026-10-09T11:33:07Z, from independently reviewed source `9d1c516cb3744ba7cb4fa4e4585f6e795e51d72e`, tree `762b6bfd56894ab11cce2eedad40edba3c1f4e3f`, source epoch 1791545029.

Protected [run37923703720](https://github.com/popiposter/xkeen-control/actions/runs/37923703720) completed hosted FULL and signing/publication. Independent unauthenticated downloads verified exactly ten assets, both pinned signatures/manifests, all sizes/hashes, nine SHA256SUMS entries, static ABI and published MIPS soft-float startup under qemu.

| Binary | Bytes | SHA256 |
| --- | ---: | --- |
| linux/arm64 | 16,646,304 | `59d17e402b54d8ac6aa900f585a529e2b49fc9ad7e25076bf6cb30dc63781f5d` |
| linux/mipsle soft-float | 19,136,703 | `a18701d11107e71d63a31d11e89f56d5a99af5dbe8f3c1693efab5c83780320c` |

[PR169](https://github.com/popiposter/xkeen-control/pull/169) delivers [#168](https://github.com/popiposter/xkeen-control/issues/168): explicit [verify-existing settlement](NODE-RECOVERY.md) without another lifecycle command, durable completion fencing and sanitized failure stages. Independent review/local FULL bind `f09e26614677001c1f232fdeb361d2d757088808`; the released merge tree is identical. Later documentation commits do not change the immutable release source.

Signed0.4.6 installation and its original verify-existing settlement subsequently passed [hardware readback](https://github.com/popiposter/xkeen-control/issues/168#issuecomment-6080313195). A later automatic subscription transaction has a different unresolved marker. Resource/client acceptance remains separate.

## Previous stable 0.4.5

Signed stable [v0.4.5](https://github.com/popiposter/xkeen-control/releases/tag/v0.4.5), published 2026-10-09T10:33:06Z, from independently reviewed source `5cc498f79d337fb723f5ac90fd21f6dcce3f0eeb`, tree `8dd2b8365aec76d3d32a0808a5468bf505cf20d0`, source epoch 1791541537.

Protected [run37917684760](https://github.com/popiposter/xkeen-control/actions/runs/37917684760) completed exact-source hosted FULL and signing/publication. Fresh unauthenticated downloads verified exactly ten public assets with the source-built verifier: pinned Ed25519 key, both signatures/manifests, sizes/hashes, nine SHA256SUMS entries, static ELF/soft-float and the published MIPS version under qemu.

| Binary | Bytes | SHA256 |
| --- | ---: | --- |
| linux/arm64 | 16,515,232 | `900dd6f02d3af440f784078b0ed8dde4911675fffa73239b688ca8effff4f69f` |
| linux/mipsle soft-float | 19,136,703 | `fffe6620d38fced79081657feca6d99e8f728ddf5560488d6ef49d25c9161f53` |

[PR166](https://github.com/popiposter/xkeen-control/pull/166) delivers [#165](https://github.com/popiposter/xkeen-control/issues/165): direct flock descriptor checks and the fixed read-only `self-update inspect-capabilities` command. Independent source review and local FULL bind `6c406f1aef8d9c931f24261d1f178feaf35da243`; the released merge tree is identical. Frozen-source review and protected hosted FULL separately bind the released merge SHA. Later documentation commits do not change that source.

Subsequent signed0.4.5 hardware capability checks and installation passed; node recovery failed and retained activation-intent. Controlled resource comparison and independent client/failure acceptance remain incomplete. The failed 0.4.3 update and 0.4.4 preflight refusal remain separate historical evidence below. Publication does not settle an interrupted operation or authorize replay; follow [maintenance delivery and inspection](OPERATIONS.md#durable-update-outcomes-162).

## Installation

Package targets:`linux/arm64` and `linux/mipsle` soft-float with confirmed Entware `mipsel-3.4`/`mipsel-3.4_kn`. For an existing stock XKeen/Xray with Entware, install only the panel. Existing KN1810 preparation and hardware limits are described in [MIPS Keenetic](MIPS-KEENETIC.md):

```sh
xkeen_installer=$(curl -fsSL https://github.com/popiposter/xkeen-control/releases/download/v0.4.9/install.sh) && sh -c "$xkeen_installer"
```

For a genuinely new supported Ultra KN1811/5.01.C.6.0-1, one private IPv4 bridge/one WAN and no host/custom DNS exceptions, use the explicit guided mode:

```sh
xkeen_installer=$(curl -fsSL https://github.com/popiposter/xkeen-control/releases/download/v0.4.9/install.sh) && sh -c "$xkeen_installer" -- --setup
```

It privately imports the source, prepares stock2.1 native components, the panel and compact selective routing with ordinary Keenetic DNS, then assigns HOME last. [Fresh prerequisites, ordering and interrupted setup](FRESH-KEENETIC.md) govern this restricted route; it never patches native code or reinstalls existing components. Other firmware/model/topology support remains unavailable.

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

### Stable 0.4.4 publication and preflight refusal

Signed stable [v0.4.4](https://github.com/popiposter/xkeen-control/releases/tag/v0.4.4), published 2026-10-09T09:41:53Z, from independently reviewed source `27daa9abdfec946e150a2bb934f96ddd7aec3125`, tree `ee178ad47d3bb9da3ebf978971063c36c695ce98`, source epoch 1791538428.

Protected [run37912340620](https://github.com/popiposter/xkeen-control/actions/runs/37912340620) completed exact-source hosted FULL and signing/publication. Fresh unauthenticated downloads verified exactly ten public assets with the source-built verifier: pinned Ed25519 key, both signatures/manifests, sizes/hashes, nine SHA256SUMS entries, static ELF/soft-float and the published MIPS version under qemu.

| Binary | Bytes | SHA256 |
| --- | ---: | --- |
| linux/arm64 | 16,515,232 | `051208a443148f32339feb230afa62ef20106636072112bd858549d571d17008` |
| linux/mipsle soft-float | 19,071,167 | `1521f54d68fea16c8ffdb95c1f9d0f93a85b3e85ae3ad18075ff592afad09490` |

[PR163](https://github.com/popiposter/xkeen-control/pull/163) delivers [#162](https://github.com/popiposter/xkeen-control/issues/162): durable update receipts, bounded readiness, checked file commits/rollback and verified RAM maintenance delivery for older helpers. Independent source review and local FULL bind `93cd2b617a2b8886d5be015e25feb69d5fe5593a`; the released merge tree is identical. Frozen-source review and protected hosted FULL separately bind the released merge SHA. Later documentation commits do not change that source.

One 0.4.4 KN1810 maintenance attempt refused before reservation, helper launch or payload placement: firmware `/bin/sh -c` dropped the positional arguments used by the Go flock probe. Direct flock and script-file forwarding passed independent diagnosis. Installed signed 0.4.2 and native configuration/runtime remained unchanged; prior panel/cron services were restored. This is a failed preflight, not successful installation, and must not be replayed. #165 addresses the narrow Go probe dependency; no native code or helper/init patches.


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
