# Releases, bootstrap and panel updates

> **Native-shell release:** [stable `v0.3.0`](https://github.com/popiposter/xkeen-control/releases/tag/v0.3.0) is published.
> Use [Native XKeen contract](NATIVE-XKEEN.md) for the new generation: install
> stock XKeen first, then the panel alongside it. Historical Setup/appliance
> adoption and component repair below are not native installation procedures.
> Compatibility with older panel generations is outside the supported contract.
> Panel replacement/rebind excludes active native panel jobs/config writes;
> external CLI/cron still requires operator quiescence and independent readback.

Invoking the installer on an existing managed development panel delegates to its
installed signed self-update command. Stop starting panel jobs and inspect their
terminal state before using this external CLI path: an independent CLI process
does not share the serving panel's in-memory lease. Unknown operations require
readback, not another installer/update attempt. Development metadata acceptance
does not waive installed-marker matching, helper/layout or signed-candidate checks.

Slice D / Issue #2 remains production-qualified. Public signed releases, bounded first-install bootstrap, historical C.1 adoption and panel-only self-update/rollback are current behavior for the qualified `linux/arm64` target. Historical stable release `v0.1.1` is the first release qualified through the complete legacy adoption → rollback → re-adoption production sequence. D.1 / Issue #3 is production-qualified in signed stable `v0.2.0` from exact source `f170cdb0a9531cb8f4e08c95c0ba9bc8fe3dfd86`.

## Stable 0.3.0 publication � 2026-10-05

PR #122 merged with operator approval to exact source
`8140c9cda51ca9bf703d3a7965f4595f2c69445a`, tree
`5c4fa862e996f192dda0f7fe6b6dfc52a0657f59` (identical to reviewed PR HEAD).
Protected [Release run 37239198359](https://github.com/popiposter/xkeen-control/actions/runs/37239198359)
passed both build and protected publish. Independent unauthenticated public
seven-asset download passed source-pinned Ed25519 key/signature, exact
`0.3.0` / `stable` / source / Linux ARM64 manifest, asset sizes/hashes and
all six `SHA256SUMS` entries. The checksum file itself was hashed separately.

| Public asset | Bytes | SHA256 |
| --- | ---: | --- |
| S99xkeen-control | 1991 | `94f34f9ce05725525f446e2382ddd37fc420cf6b912af4a01c608bed7d77b9fd` |
| SHA256SUMS | 515 | `c3e85e61c40f2530b2cecb8868b1730bfffe7bf1b294185c60f4b03a4eb4b941` |
| install.sh | 14926 | `1f5b10dbadb1712aff7f124719cbe7db4ab7544986cf6d0879b31a6dcbc0d9b4` |
| release-manifest.json | 834 | `b13856bc539e68f88930559f57e6a823538e6364976063782590a47a9358c989` |
| release-manifest.sig | 89 | `2541645cba5c909ee86723e15fe3e4eede7f425ac976821ef09731dde826f770` |
| xkeen-control-linux-arm64 | 15859872 | `3c44c9f26d2e7875989c58b8cc09f2de31935971c7b5d067b4d83797e4d4f08c` |
| xkeen-control-updater | 22015 | `88acc265d8fa8d1a80f627dc1d88433a046afc06a2a7e54de8f31ce11298e027` |

Publication is independent of router installation. The operator router currently
runs development source `92d49ec`: fresh broad speed qualification measured
12 candidates, obtained 11 valid results and applied a six-node native pool.
Signed stable installation has not been qualified. Unproxied LAN/outage, real
Telegram credentials and second-router transfer acceptance remain unqualified.

## Release authority

Only a GitHub Release from `popiposter/xkeen-control` is an install/update source. Raw branches, branch archives, Actions artifacts and arbitrary URLs are not accepted. The supported artifact is `linux/arm64` and every release contains exactly:

```text
xkeen-control-linux-arm64
S99xkeen-control
xkeen-control-updater
install.sh
release-manifest.json
release-manifest.sig
SHA256SUMS
```

The manifest binds exact public metadata, source commit, source epoch, artifact sizes/hashes and compatibility flags. The installer verifies internal consistency before first installation. An installed binary uses the source-pinned production Ed25519 public key for normal checks and updates; release/API hosts, redirects, body sizes and channels are bounded.

The production private signing key is confined to the protected GitHub `release` environment. Tests use synthetic keys only. Release publication fails closed unless the protected public key matches the source-pinned trust anchor and the exact manifest is signed successfully.

## Protected publication flow

Stable/beta publication is manual and source-pinned. The release workflow:

1. checks out the exact operator-supplied full `main` SHA;
2. re-checks that remote `main` still equals that SHA;
3. runs full release qualification;
4. builds deterministic unsigned release inputs;
5. hands only secretless assets to the protected publish job;
6. verifies the protected public key matches the source-pinned trust anchor;
7. signs the exact manifest with the protected private key;
8. creates a non-public draft release;
9. re-downloads the draft and verifies the exact seven-file set, `SHA256SUMS`, manifest signature and manifest-bound asset names/sizes/hashes;
10. re-checks remote `main` again and only then publishes the verified draft.

Release `v0.1.1` completed this flow on source `8f15246099538426ef08163b832c3aa6f73e8265`.

## Bootstrap boundary

`scripts/install.sh` requires root, `/opt`, Entware `opkg`, `linux/arm64` and bounded free space. It installs only explicitly missing prerequisites, never performs blanket `opkg upgrade`, never changes node/Xray/XKeen/routing/DNS/Observatory policy and never automates the upstream interactive component installer. Missing XKeen/Xray/configuration is a healthy Setup Mode state.

The currently qualified release-specific invocation is:

```sh
sh -c "$(curl -fsSL https://github.com/popiposter/xkeen-control/releases/download/v0.2.0/install.sh)"
```

The first panel password is generated by the Go binary with `crypto/rand`, stored only as a bcrypt hash under root-only permissions and printed once to the invoking terminal. A rerun preserves the existing credential/listener/state. Authenticated password replacement clears the setup marker and invalidates sessions.

## Existing managed install

A valid managed install never downgrades to bootstrap-only trust on installer rerun. The installer validates the installed binary identity and any release marker, then delegates to the installed binary's pinned-signature `self-update` path. Partial/corrupt managed layouts fail before download/delegation/mutation.

After successful typed D.1 `appliance adopt`, the local appliance authority governs supported non-secret policy. Before adoption, an existing router retains the explicit repository-derived/legacy policy boundary; adoption is not implicit and unknown/manual drift fails closed.

## Historical C.1 adoption

The known pre-#2 C.1 manual panel has a narrow fingerprint-gated adoption path. The installer accepts only the fixed historical binary/init SHA-256 pair and the exact legacy helper/marker absence; fingerprints are release-owned constants, not operator inputs.

The staged updater uses an explicit `adopt` operation. Before legacy stop it creates a private bounded node recovery journal. Post-stop recovery does not infer commit from file coherence:

- unchanged node snapshot: no node generation crossed the handoff;
- incoherent node/outbounds pair: restore the validated snapshot because Xray activation could not yet have started;
- coherent changed pair: use typed `nodes reconcile-runtime` to validate the complete Xray candidate, restart Xray with bounded budgets, wait for API readiness and verify expected outbound inventory;
- failed coherent reconciliation: restore the validated journal snapshot and explicitly reconcile its runtime before panel replacement.

Rollback can represent a previous generation where the helper was absent, restoring the exact legacy layout and permitting later re-adoption.

## Panel update boundary

The typed update API uses fixed release policy (`manual`, `notify`, or `auto-stable`), authenticated session + origin + CSRF checks, `/tmp/xkeen-control/panel-update` candidates and one persistent previous panel generation. The external helper has only fixed panel lifecycle operations (`install`, `adopt`, `rollback`) and fixed panel paths. It cannot execute arbitrary commands or accept arbitrary destination paths/URLs.

Normal managed update/rollback coordinates through the control-plane lifecycle barrier. The helper survives panel replacement, swaps only the fixed panel binary/init/helper/marker paths, verifies generic health plus exact local build identity and supports automatic/explicit restore of the one previous panel generation.

The legacy adoption recovery path may perform the narrowly typed Xray runtime reconciliation described above solely to converge an interrupted pre-#2 node transaction; it does not alter unrelated routing/DNS/Observatory policy or node authority semantics.

## Qualification

Development qualification uses:

```sh
bash scripts/test-release.sh
```

Fast iteration and final full qualification use:

```powershell
pwsh -NoProfile -File scripts/dev-check.ps1
pwsh -NoProfile -File scripts/dev-check.ps1 -Full
```

Production qualification completed for `v0.1.1` on the exact released source. The bounded live sequence was:

```text
legacy exact -> v0.1.1 adoption -> exact legacy rollback -> v0.1.1 re-adoption
```

Every completed transition passed generic health, exact version/source/channel and single PID-file-backed process checks. Bounded non-secret fingerprints for auth/listener/node/Xray/XKeen/selection/benchmark state remained unchanged. No blanket package upgrade, reboot, credential rotation, component install/repair, KeeneticOS update or sustained benchmark was used.

D.1 production qualification completed for signed stable `v0.2.0` from exact source `f170cdb0a9531cb8f4e08c95c0ba9bc8fe3dfd86`. The protected release flow passed the accepted D.1 qualification floor, and the published release contains the exact seven-file release set with a signed manifest. Bounded live qualification verified the signed panel, typed appliance adoption/validation, safe and encrypted export boundaries, settings-only restore preview/apply equivalence, and appliance/node/generated-policy/runtime coherence. Evidence is recorded with sanitized versions, bounded counts, state transitions and non-secret hashes; secret-bearing backups and live node material are excluded.

The qualified D.1 release does not add component lifecycle controls or visual typed configuration. Later #4/#5 functionality exists in source but remains outside the deployed production-qualified generation; `docs/ROADMAP.md` owns current sequencing.
