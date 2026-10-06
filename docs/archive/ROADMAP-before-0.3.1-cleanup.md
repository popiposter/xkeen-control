> Historical snapshot retained for evidence and link continuity. Not a current installation or implementation contract. See [current documentation](../README.md).

# Roadmap

This is the sequencing/status authority. The active GitHub issue is the detailed architecture/acceptance contract for the slice being implemented.

## Current direction — native XKeen implementation

### Active revision — 2026-10-06

The panel is a graphical shell over **unmodified XKeen code**. The
[native contract](../NATIVE-XKEEN.md), [source audit](../../plan/audit-native-shell-2026-10-03.md)
and [v2 plan](../../plan/architecture-native-shell-v2.md) supersede invasive v1
admission/profile workers. Stock XKeen owns components, interception and cron.
The [command matrix](../../plan/xkeen-command-inventory-v2.md) maps native commands to
forms/buttons and a private command-bound console.

Stable `v0.3.0` was published from `8140c9c`; stable
[v0.3.1](https://github.com/popiposter/xkeen-control/releases/tag/v0.3.1) is now
published from exact reviewed `6e62d620630f5994b00acb2dfb60cbd690b44bcd`.
PR122 and PR126 are merged; Issue125 DNS integration is completed. PR128 fixed
a transitive build dependency advisory; PR130 fixed inconsistent mock readback.
The final exact-main FULL passed 106 browser cases, Go/race/helpers,
frontend/embed, audit zero and an actual ARM64 artifact. Protected run37427370603
build and publish passed, followed by independent fresh seven-public-asset
pinned-key/signature/manifest/checksum verification. See [release evidence](../RELEASES.md).

Native v2 includes command jobs/console, Form/Text config editors with shared
pending/Apply/optional previous restore, installed-geodata routing search,
subscriptions, balanced speed-pool recommendation, encrypted native transfer
and optional Telegram control. Native Xray owns selection/failover; the retired
panel selection override stays disabled. A fresh broad sample and six-node
recommendation Apply were independently verified before stable0.3.0; earlier
stale recommendation rejection remains separate historical evidence.

The operator router runs the accepted development DNS integration: 87,663 domain
entries, four ordered decisions and ten conditional traffic rules; native files,
auth and process were preserved. Fresh DIRECT/VPN/local/ad queries and unchanged
Sync without DNS/Xray restart passed. A separate earlier bounded Xray outage test
proved direct/local DNS availability without VPN-to-DIRECT fallback. Signed
v0.3.1 installation is not claimed by development delivery or publication.

Issue121 stays open for remaining acceptance boundaries: real Telegram requires
operator credentials, second-router transfer needs another router, and reboot,
IPv6, all-provider outage and broader LAN failover remain unqualified. Optional
mosdns is separately installed; the panel derives data from native routing and
installed geodata, without a second downloader or native script patch.

### Historical 2026-10-02 direction and checkpoints

After the 2026-10-02 audit the operator authorized full ordered implementation and qualification on clean Entware. The [native implementation contract](../NATIVE-XKEEN.md) governs this work: native XKeen first, panel alongside it, no compatibility with older panel installations. Service restarts and local SSH key setup are authorized; no router reboot is planned.

The [audit](../../plan/audit-xkeen-foundation-2026-10-02.md) binds findings to source `8adff1e00e89515b37aac1d7d6e7e1df924a9143`. [Issue #121](https://github.com/popiposter/xkeen-control/issues/121) and the [implementation plan](../../plan/architecture-xkeen-foundation-v1.md) return install/update/cron/lifecycle/interception ownership to native XKeen, retain the panel updater and node management, and schedule config/geodata UX, reliability, portable backup and Telegram work in separate slices. Implementation is in progress. Independent plan review reproduced the audit counterexamples and required minimal config onboarding before subscription Apply, plus a real shared lock for any CLI/cron concurrency claim. These corrections are incorporated in the plan; runtime acceptance is pending.

Beta.5 publication/public verification and prior panel-only installation are historical PASS. The subsequent authorized reset/native-file reconstruction did not reach integrated subscription/LAN/quality acceptance. The old router state is no longer a reusable baseline after the operator's planned opkg reset. Preserve unknown/failed operation receipts; do not replay them. Earlier sequencing below is historical except where explicitly retained by the new plan.

## Native development installation — 2026-10-02

Issue #121 now has a working native installation with the development panel from
`c805a551846b30f2e8ae7fc423723d39ecbc6db3`. Its full local Linux gate passed,
including 149 browser cases. Independent live readback verified the installed
and running ARM64 executable hash, authenticated health, 59 profiles (52 enabled),
two subscriptions and no enabled WL profiles. The panel-only replacement kept
Xray process identity, native configuration, registry and authentication intact;
no configuration transaction is pending. See the [live evidence](https://github.com/popiposter/xkeen-control/issues/121#issuecomment-5957677528).

This is a hash-bound development build, not a signed release. Native Xray remains
the selection owner; panel adaptive scheduling and automatic subscription writes
are disabled while shared native admission is unfinished. The source-only native
admission candidates remain fenced and must not be installed. LAN-client traffic,
late-crash behavior, native update/cron concurrency and the remaining plan stages
are not yet qualified. The earlier installation/import/recovery operations must
not be replayed.

HTTP fixture optimization in `f97fac50f5ee59e804af241d7878bde5f4b1e5b4` preserves
production authentication code and real authorization tests. An uncached HTTP
package race run fell from 296.465s to 22.025s by reducing bcrypt cost only in
ordinary test fixtures; one production-cost HTTP smoke and password-rotation
tests remain. This proportional result is not a new full-gate claim.

Native admission source now includes foreground init/hook borrowing, a bounded
elected NDM caller with current-ready reconciliation and protected RAM bootstrap.
Independent focused review/checks passed; generated candidates remain fenced and
uninstalled. Exact `22f02c7` FAST passed Go/helpers/ARM64 and 149 browser cases;
newer `36596a8` FAST passed Go/helpers/ARM64 without browsers. These are source
iteration results, not native update/cron, boot, kernel or LAN acceptance. Native
update preservation and target qualification remain prerequisites to activation.

The exact clean `5c2ca61` event/bootstrap checkpoint subsequently passed the
full local gate (Go/vet/race, native helpers131, browser149, frontend/audit and
ARM64 artifact) in 3m58s. Later native registration-template and installer staging
experiments have independent focused review and remain source-only; their
authenticated updater handoff and target qualification are still pending.
The full checkpoint is not reused as qualification of these newer edits.

The clean `721a0c1` checkpoint passed FULL in 3m42s (helpers138,
browser149, Go/vet/race, frontend/audit and ARM64). Subsequent source work adds
exclusive updater-body binding and a one-use post-exec proof of the fixed native
interpreter/path/argument vector. Independent focused review passed; the public
native-source catalogue passed 94 fixtures separately from the default gate.
These proofs do not enable an updater: staged profile decoration, entry/finish
integration, update readback and all native writer/cron coverage remain pending.
All generated native candidates remain fenced and uninstalled.

The updater generation proof checkpoint `a967ea2` passed FULL in 4m07s
(helpers154, browser149, Go/vet/race, frontend/audit and ARM64). Later `82fd985`
passed FAST Go/helpers159/ARM64 without browsers, race or audit. A complete
72-file pinned public archive profile and fixed authenticated staging worker now
have independent source review. Extracted native-installer integration preserves
the old live files on refusal and promotes the three decorated overlays plus all
unchanged modules on success. The explicit public-source catalogue passed110
cases; this is separate from the default FULL gate and hardware acceptance.
Full updater entry/finish, post-update verification, writer/cron coverage and
target qualification remain prerequisites to enabling native admission.

The clean `aa6c924` primitive checkpoint passed FULL in 3m44s (helpers190,
browser149, Go/vet/race, frontend/audit0 and ARM64). Subsequent source work
composes the update-specific pre/post verifier from the reviewed profile,
init/package/environment/core/kernel checks. Its pinned-source fixtures use
synthetic runtime callbacks; native executor integration and target acceptance
remain pending. This does not enable or install any native candidate, and the
prior FULL is not reused after edits.

The exact `1f9d339` verifier checkpoint subsequently passed FULL in 3m37s
(helpers190, browser149, Go/vet/race, frontend/audit0 and ARM64). The subsequent
`a80911a` forced update-to-init borrowing seam passed independent review and FAST
Go/helpers197/ARM64 without Chromium. `6182dd3` additionally checks all nine native
update dependencies, preserves opkg configuration and refuses stopped hook
cleanup drift. Independent focused checks passed 17 groups; the separate pinned
public-source catalogue passed 153 cases. These are source iteration results.
Native updater entry,
terminal/cleanup/drain wiring and hardware qualification remain pending; this
does not enable the native backend or adaptive writer.

The exact `d6e3666` borrower/prerequisite checkpoint passed FULL in 3m40s
(helpers197, browser149, Go/vet/race, frontend/audit0 and ARM64). Subsequent
source work checks the actual native package reload before both update phases,
preserves its classifier, prepares four fenced overlays and removes only the
script-update path's unused Entware feed refresh. Independent focused review
passed; updater entry, terminal, cleanup/drain and target qualification remain
pending. This newer source is not covered by the prior FULL result.

The exact `194d3e6` prefix/cache checkpoint passed FULL in 3m41s
(helpers197, browser149, Go/vet/race, frontend/audit0 and ARM64), with independent
source-slice approval. The next fenced source connects the fixed updater entry,
same-body exec, verified cleanup and owner-only dirty drain. Its real native
successful terminal remains unwired pending inner-writer error propagation,
cron/cache handling, safe destinations and archive verification before extraction.
No new updater candidate is installed or enabled; prior FULL does not qualify
these newer changes.

## Historical production baseline

Slices A/B/C/C.1 and D remain production-qualified. The validated fresh-source migration baseline merged as #7, the canonical Go module/import identity cleanup merged as #9 / Issue #8, and Slice D completed through Issue #2 with historical signed stable release `v0.1.1` from source `8f15246099538426ef08163b832c3aa6f73e8265` plus bounded live Keenetic adoption → rollback → re-adoption qualification. D.1 / Issue #3 is also production-qualified in signed stable `v0.2.0` from exact source `f170cdb0a9531cb8f4e08c95c0ba9bc8fe3dfd86`. Issue #78 Slice F is present on source main as source-qualified-only work and is not a production-qualification or release claim.

Current runtime facts:

- `/opt/etc/xkeen-control/secrets/nodes.json` is the authoritative local VPN/subscription registry;
- active `04_outbounds.json` is generated from that registry;
- enabled nodes use canonical `proxy-*` tags in one `bal-proxy` pool;
- `xkeen-control` is one pre-built Go binary with embedded React UI, auth and typed node/subscription operations;
- node changes use complete candidate validation, bounded activation and rollback;
- `xkeen-control` is the only managed stable `bal-proxy` override writer; native Xray `leastPing` is emergency fallback;
- source-main active liveness defaults to 60 seconds / 2 failures and PR #92 only permits more conservative 60–300 second / 2–5 failure settings;
- the production-qualified D baseline's control-plane-owned sustained benchmark runs once/day with bounded whole-run traffic and RAM/`/tmp` high-churn state;
- source-main adaptive quality defaults to the three-hour generation; PR #92 permits only less-frequent 180–1440 minute cadence, 1–5 challengers plus current, 30–1440 minute dwell and 10–50% hysteresis while hard traffic/time ceilings remain source-owned; the legacy daily benchmark is explicit-only there;
- node Apply, supervisor/selection and benchmark work share one runtime coordinator;
- signed GitHub Releases are the software distribution authority for the qualified `linux/arm64` panel;
- the public installer and panel rollback path are production-qualified without changing node/Xray/XKeen/routing/DNS/Observatory state;
- after successful typed `appliance adopt`, `/opt/etc/xkeen-control/config/appliance.json` is the local authority for supported non-secret appliance policy;
- managed `02_dns.json`, `05_routing.json` and `07_observatory.json` derive deterministically from that authority, while `04_outbounds.json` remains generated from `nodes.json`;
- before adoption, an existing router retains the explicit repository-derived/legacy policy boundary; adoption is compatibility-gated, not implicit, and unknown/manual drift fails closed.

## Repository authority

`popiposter/xkeen-control` is the public source and release authority. Development qualification is local; protected GitHub Actions is reserved for manual releases. The repository was initialized on 2026-08-21 from the validated secretless tree of historical `popiposter/xkeen-keenetic` commit `a1b8c3ce4e7f1914312b23b52c3b96269865e90e` using fresh Git history.

The historical repository remains private quarantine/history only. Do not import its commits, branches, PR refs, cached views, releases, Actions artifacts or other historical Git objects into this repository.

Router-specific settings and secrets never enter this repository or release assets.

The active Go module/import identity is canonical: `github.com/popiposter/xkeen-control`. Historical `popiposter/xkeen-keenetic` references that remain in documentation describe quarantine/history only.

## Historical product direction after D.1 — superseded by the audit plan

Per-router settings are not synchronized from Git.

```text
public source + signed GitHub Releases (#2, done)
        ↓
local typed appliance state + portable backup (#3, done / v0.2.0)
        ↓
managed XKeen / Xray / geodata lifecycle (#4 trial stopped; architecture under audit)
        ↓
visual typed configuration + transactional render/apply (#5 source delivery complete through #91 F; trial panel installed, integrated acceptance pending)
        ↓
notifications + private-management security hardening (#99 source delivered; included in trial panel)
        ↓
feature freeze + exact-main integrated qualification (#106 complete; reviewed successor corrections delivered through #117)
```

## Delivery sequence

| Slice | Status | Authority | Outcome |
| --- | --- | --- | --- |
| A — secretless unified-pool foundation | Done | validated historical baseline | Secretless software tree, unified pool, transactional deploy foundation |
| B — control-plane skeleton | Done | validated historical baseline | Go binary, embedded UI, auth, runtime projection, Docker qualification |
| C — node/subscription management | Done | validated historical baseline | Local `nodes.json`, canonical tags, typed preview/apply, rollback, operator UI |
| C.1 — stable selection + sustained benchmark | Done | validated source snapshot `a1b8c3c…` | Sticky stable override, independent liveness, bounded benchmark, shared coordinator |
| Pre-D — canonical Go module/import identity | Done | Issue #8 / PR #9 | Canonical `github.com/popiposter/xkeen-control` provenance; no runtime behavior change |
| D — releases/bootstrap/panel self-update | Done | Issue #2 / `v0.1.1` | Public signed Releases, protected release pipeline, one-command bootstrap, setup mode, transactional panel update/rollback |
| D.1 — appliance state + backup/import/export | Done / production-qualified | Issue #3 / `v0.2.0` | Local schema-versioned settings, safe export, encrypted secret backup, typed restore |
| D.2 — component lifecycle | **Trial stopped by operator; native-XKeen architecture plan pending** | Issue #4 + 2026-10-02 audit | Source work through #117 and beta.5 publication/panel installation remain historical. Integrated convergence/quality was not completed. Do not resume the previous successor-release/Setup sequence |
| D.3 — foundation A-F (#46) | **Source delivered through Issue #78; integrated acceptance pending** | Issue #46 / #68 / #70 / #72 / #74 / #76 / #78 | Selection-first Nodes/batch mutations, subscription reconciliation/refresh and one manual/adaptive quality owner are in the trial panel; source delivery alone does not prove integrated live acceptance |
| D.3/#5 — visual configuration | **Source delivered; integrated live acceptance pending** | Issue #5 / #91 + ledger #4 | Routing, DNS/Observatory, bounded Performance, System/Panel, and final Dashboard integration are in the trial panel. Production-qualified stable remains `v0.2.0` |
| E — notifications/security hardening | **Source delivered; stable qualification unchanged** | Issue #99 / PRs #100/#101 | Fixed-host notifications, stable-notify discovery and private-management/auth protections are in the trial panel; integrated live acceptance remains separate |
| Feature freeze / candidate qualification | **#106 completed; successor gates active in #4** | Issue #108 / #117 / master #1 | Reviewed source corrections require a fresh immutable candidate and exact local/protected/public/live gates; old release identities are preserved |

## Pre-D / Issue #8 — complete

The repository identity cleanup merged as PR #9. `go.mod`, active in-module imports and protobuf Go package metadata use `github.com/popiposter/xkeen-control`; dependency versions and runtime behavior were unchanged. No further pre-D maintenance is scheduled.

## D / Issue #2 — complete

#2 established the public software distribution and panel lifecycle boundary:

- this repository's signed GitHub Releases are the software distribution authority;
- stable/beta releases come from reviewed exact commits through the protected `release` environment;
- release artifacts carry full source SHA, architecture, checksums and a signed compatibility manifest;
- the qualified release target is `linux/arm64`; additional architectures require explicit qualification;
- the one-command installer uses bounded prerequisites and never blanket `opkg upgrade`;
- first install can generate a one-time setup credential and binds only loopback/exact private LAN;
- existing managed installs use the source-pinned signature path;
- the historical pre-#2 C.1 panel has a fingerprint-gated adoption bridge with bounded node/runtime recovery;
- panel update downloads to `/tmp`, retains one previous panel generation, health/version/PID-path verifies the new process and supports rollback;
- releases do not distribute router settings or VPN credentials.

Production qualification completed on `v0.1.1`: legacy adoption, exact legacy rollback including helper absence, and re-adoption all passed while bounded non-secret fingerprints for auth/listener/node/Xray/XKeen/selection/benchmark state remained unchanged.

## D.1 / Issue #3 — complete

`v0.2.0` is the signed stable D.1 release, production-qualified on `linux/arm64` from exact source `f170cdb0a9531cb8f4e08c95c0ba9bc8fe3dfd86`. Successful typed `appliance adopt` establishes local schema-versioned `appliance.json` as the authority for supported non-secret policy, while `nodes.json` remains the separate VPN/subscription secret authority. Managed DNS/routing/Observatory policy is generated deterministically from the appliance authority; active outbounds remain generated from `nodes.json`.

Safe export excludes secrets by default; secret-bearing export is explicit and encrypted. Import is authenticated, bounded, preview-first and transactional, with typed validation, authority coordination and interrupted-import recovery. Equivalent settings-only restore is a no-op and does not rewrite node/generated/runtime state or restart Xray/XKeen. D.1 preserves the #2 release/update trust and rollback boundary.

Pre-adoption compatibility is explicit: routers without a successful typed `appliance adopt` retain their existing repository-derived/legacy policy. Adoption is not implicit and unknown/manual drift fails closed.

## D.2 / Issue #4 — historical trial; continuation stopped

Phase A merged via PR #23 to `main` `bda9dd0cc7bb142a4cb1468811fff9b5146b1e8e`; source main gained the bounded read-only component inventory and authenticated `GET /api/v1/components` for panel, XKeen, Xray, geodata, KeeneticOS and Entware.

Phase B merged via PR #25 to `main` `d6a6c81f47c9b50dcead8cf95c70906660af87b2`; source main now also has authenticated same-origin/CSRF-bound `POST /api/v1/components/check` for bounded trusted metadata checks of Xray, XKeen and the fixed product geodata catalog. Check state is RAM-only; artifact bytes are not downloaded and component/router state is not mutated.

Phase C merged via PR #27 to `main` `94ef0878e743a1c3355042132102dd97b4151508`; post-merge CI #73 / run `33776705482` completed successfully on that exact revision. Source main now also contains the internal transactional Xray update/rollback core with fresh exact candidate re-resolution/download, complete adopted-D.1 candidate validation, shared Coordinator → authority-lease ownership, one bounded previous Xray generation, durable shared component journal and local-only startup recovery. Phase C deliberately adds no browser/API mutation controls.

Phase D merged via PR #29 to `main` `6091b3f28e9f5e2285732e6bdc5118fca3e896b9`. Source main now also contains the internal transactional complete six-file geodata update/rollback core, shared Xray/geodata component mutation and startup recovery arbitration, one bounded previous whole geodata generation, unrelated-asset preservation and post-runtime exact generation re-verification. Phase D deliberately adds no browser/API mutation controls.

Phase E0 merged via PR #31 to `main` `c320df478e33482a61b78e44abcee83a1093180e`. Source main now describes the actual supported XKeen development authority: fixed `jameszeroX/XKeen` / `dev`, current `S05xkeen` lifecycle layout with `S24xray` treated only as legacy/mixed state, and a GitHub-verified exact build/source/blob metadata projection for fixed `test/xkeen.tar.gz`. E0 remains read-only/informational: it downloads no archive bytes and exposes no install/update/rollback authorization.

Phase E1 merged via PR #33 to `main` `527b8b75b6a1f9126b636583be202be23ef58ae8`. Source main now also contains the internal transactional XKeen update/rollback core: fixed exact jameszeroX dev build/tree/blob resolution and transport, strict GNU-tar/file-only qualification, canonical managed `xkeen + .xkeen` generation/marker, preserved `S05xkeen` runtime convergence, shared `xray|geodata|xkeen` component journal/recovery arbitration, one previous XKeen generation and rollback/startup recovery. Candidate `xkeen` is not executed for convergence, and D.1/Xray/geodata/config/opkg/cron/panel/unrelated Entware state remains outside XKeen ownership.

Phase E2 merged via PR #35 to `main` `6766a5618e1e4eaa9dbca5994bd5b4f02d9ecad9` from reviewed exact HEAD `53e3effa8ef51a39e81cc26bd1d174595fc717ad`. Source main now contains exactly one fully qualified installable `jameszeroX/XKeen` dev catalog entry: exact immutable build/source/blob identity, independently reproduced archive SHA-256, complete 72-member GNU-tar file-only manifest, source-parent content-equivalence proof and canonical installed-generation digest. Read-only Check marks only that exact moving build eligible and still does not authorize mutation by cache.

Phase F1 merged via PR #37 to `main` `db034f277278788dfb6ec90950bc97648922b54d` from approved exact HEAD `8b5aee28eaff10b5f718ee0b66f444487408156e`. Post-merge CI #103 / run `33961698349` completed successfully on that main revision. Source main now has authenticated, same-origin/CSRF-bound component Preview/Apply/Rollback/Cancel over the existing transaction cores: fresh exact intent, bounded RAM/session one-shot tokens, separate component admission, stale rollback-target rejection, verified-restoration error classification and a recovery-inclusive synchronous HTTP response window. F1 has no mutation UI, scheduler or policy persistence.

Phase F2 merged via PR #39 to `main` `125c48612cf5812816d8972ecafdd36434329abe` from approved exact HEAD `536a130a1eaed152813e49a0b94c56c9ee2b993a` (review `5121493878`). Both have tree `d9d29db07ad4ad953570c5231cab1744ca10a339`. Post-merge CI #109 / run `33982591217` completed successfully on that main revision. Source now includes the first-class Components/Updates UI, stable component error codes, Coordinator lifecycle hints and 29 synthetic browser cases, including late-session completion and missing/unknown presence regressions. Inventory stays outside five-second polling; transaction cores and source trust boundaries remain unchanged.

These source-main capabilities are not a new stable release or production-qualification claim. The last documented production-qualified baseline remains D.1 `v0.2.0`. Gate 1 live evidence confirmed the appliance was coherently running that signed baseline. `v0.3.0-beta.1` was then published and independently verified, but after that release the qualified XKeen catalog was deliberately refreshed; beta.1 therefore remains historical evidence and is no longer the live-trial candidate.

Q0 merged via PR #41 to `main` `f71e59fa72a4181ef7249046630b4dae8d9dd72c` from approved HEAD `ef7ab4e4d8ffa0703238b7182475c846b0030816` (review `5122799862`). Both have tree `11b8b11d95b1f95919ac700cfcbf1e10196ca7ce`. Pre-merge CI #113 / run `33988598752` and exact-main post-merge CI #114 / run `33989694840` completed successfully. The unprivileged Release build now runs the existing pinned browser suite before unsigned assembly, protected by a narrow workflow regression. The bounded Q1 runbook is merged, including the corrected sibling geodata activation path. Q0 changed no runtime, dependency, catalog or signing permissions and performed no live qualification.

PR #43 then merged the Gate 1 metadata portability correction to `main` `5a7b6d3676e3cd5e651306a0674ae657c7381787`; exact-main CI #118 / run `34166927949` completed successfully. Gate 1 completed **PASS / eligible to prepare Gate 2** in sanitized report `5584461861` after bounded prerequisite remediation. The two proven orphan node-candidate scratch directories were removed by exact path, and the single external 02:00 geodata writer was reversibly contained and remains disabled through Q1 while `crond` stays running. Fresh identity, health, D.1 coherence, pending-state, writer, protected-fingerprint and idle-resource checks passed. The operator-owned `manual-override` selection is intentional dynamic C.1 state and was not normalized. `/proc/<pid>/io` remained unavailable, so only a bounded block-device delta was observed; this is not evidence of zero writes or physical flash wear.

PR #44 merged the Gate 1 closeout/status handoff as source `e7b4673e6f3d8db79916aef03c8b5ba1d63e845c`, tree `44f7153428dcc2b70d97403047aa5adee9b43f22`; exact-main CI #120 completed successfully. Protected Release run `34241228068` then published signed prerelease `v0.3.0-beta.1` targeted exactly at that source. The release workflow re-downloaded and verified its signed seven-asset draft before publication. Independent post-publication host verification in report `5591023511` downloaded exactly the seven public assets within bounds and passed pinned-key, signature, asset hash/size and `SHA256SUMS` verification with the exact frozen manifest/compatibility tuple.

PR #48 then refreshed the one immutable qualified XKeen catalog entry to the September 15 upstream automated build under completed Issue #47. PR #51 subsequently parallelized the then-current ordinary PR/main CI without changing runtime or protected Release workflow semantics; that automatic CI was later retired in favor of local qualification while the protected manual Release workflow remained. PR #52 corrected D.2 sequencing so a new beta.2 publication and independent host verification precede router install admission. PR #53 removed self-referential exact-source SHA wording from ROADMAP, #50 / PR #54 closed the two known scheduler-sensitive node timing false negatives with test-only changes, and PR #55 closed the repository prerequisites. The protected Release workflow then published signed prerelease `v0.3.0-beta.2`, and independent public-host verification passed against the exact frozen source, seven public assets, source-pinned key, signature, manifest-bound hashes/sizes and `SHA256SUMS`. Exact source/run/asset/report evidence remains in Issue #4 rather than being duplicated here.

Issue #60 / PR #61 then repaired the Q1 scheduler-provenance contract for implicit `crond` defaults without changing runtime behavior. The corrected live writer-containment requalification passed, followed by a completely fresh beta.2 read-only install admission. Report `5683192737` proves stable identity/health, D.1 validate/verify, pending-state settlement, unchanged update policy, intentional `manual-override`, exact beta.2 Check, 20 bounded resource samples and final writer/stable re-check. No Apply/Rollback or other production mutation occurred.

The separately authorized exact beta.2 panel install then completed **VERIFIED BETA** in report `5693384881`: one Apply handoff was accepted with no replay, the exact signed beta generation is running and healthy, protected state and saved policy were preserved, post-install writer containment passed, and a fresh exact original `v0.2.0` panel rollback snapshot is retained. This is a trial-panel qualification state, not stable promotion.

The current [Issue #4](https://github.com/popiposter/xkeen-control/issues/4) remains the Q1 scope/acceptance authority and `docs/OPERATIONS.md` remains the exact bounded operator protocol. Completed Q0 authority is PR #41 / review `5122799862`; completed F2 authority is PR #39 / review `5121493878`. Gate 1 authority is report `5584461861`; beta.2 Gate 2 host authority is report `5681478283`; beta.2 install-admission authority is report `5683192737`; verified beta-panel install authority is report `5693384881`.

The simplicity decisions in [master issue #1](https://github.com/popiposter/xkeen-control/issues/1) remain in force: one Go process, embedded UI, distinct component gate → runtime Coordinator → authority lease boundaries, fixed trust adapters and bounded journals/rollback. Q1 adds no generic transaction/form/pipeline framework, job queue/history, database, metrics agent or runtime dependencies; F3 adds only its two typed authenticated policy endpoints. Existing F2 behavior is the subject of qualification, not a reason to expand the product.

**Gate 3 and the initial Xray live pair are complete historical evidence.** The Xray pair stopped at deterministic pre-commit `candidate-rejected`; PR #65 delivered the #64 diagnostic source correction. The feature-complete source-development pause below is historical. Later separately authorized beta.3/beta.4 publication and panel installation are recorded in #108/#4. Production-qualified stable remains `v0.2.0`; trial panel installation does not promote components or the integrated product to stable qualification.

F3 is delivered by PR #67: bounded persisted `off|notify|manual` component policy with a check-only scheduler/notification hook. It must not introduce automatic component mutation. #46 A-F are delivered by PRs #69/#71/#73/#75 plus Issues #76/#78. D.2 G is delivered source-only by Issue #81 / PR #82: typed Setup fresh/takeover convergence using qualified component primitives, with sole-writer retirement and source-owned Hybrid interception, without generic package/command/file surfaces or blanket `opkg upgrade`.

The completed source sequence is **#46 A-F foundation → D.2 G Setup Mode (#82) → #5 Routing/DNS/Observatory (#84/#86/#88/#90) → #91 Performance/System/integration (#92/#97/#98) → #99 notifications/security (#100/#101) → #102 workspace/#104 defaults → #106 integration freeze**. Later reviewed release/Setup/import corrections are delivered through #117. Source slices do not independently authorize publication or live mutation; the operator separately authorized the beta successor, signed panel installation, bounded reference convergence and quality path recorded in #4. Historical component update/rollback qualification and stable-return decisions remain separate from this pending integrated acceptance.

## D.3 / Issue #46 foundation — Slices A-F delivered; integrated acceptance pending

PR #69 delivered Slice A (selection-first Nodes and atomic batch mutations). PR #71 delivered Slice B (exact subscription membership reconciliation). PR #73 delivered Slice C (bounded automatic subscription refresh with non-preemptive background admission). PR #75 delivered Slice D (fixed manual-node down/up diagnostics and bounded progress). Issue #76 delivers Slice E (fresh RTT shortlist, fixed down/up quality generation, deterministic scoring, hysteresis/dwell and the one Coordinator performance owner). Issue #78 delivers Slice F (integrated automatic-quality Overview/Nodes presentation, closed state/reason vocabulary, view-scoped performance polling, legacy-trigger retirement and source qualification). Their source qualification did not imply live acceptance. They are included in the separately installed trial panel; final integrated qualification remains pending in #4.

## D.3 / Issue #5 — source complete; integrated acceptance pending

Issues #83/#85 and PRs #84/#86 deliver Routing broker/UI. Issue #87 / PR #88
deliver the shared managed-policy envelope plus closed DNS/Observatory backend,
Issue #89 / PR #90 deliver the visual DNS + Observatory workspace, and PR #92
delivers the bounded Performance policy/UI with closed persisted-authority drift
handling. Issue #91 E / PR #97 delivers source-aware private listener management,
password/session UX and truthful signed panel release controls over existing
owners. Issue #91 F completes the final Dashboard integration/drift and
feature-complete UX gate. The integrated browser suite verifies lazy settings
reads, narrow Routing/DNS uncertainty coordination, lifecycle exclusion,
cross-domain draft retention, System handoff semantics, session turnover and
safe browser projections. #5 source delivery is complete and included in the
trial panel; integrated live acceptance is still pending in #4.
Stable `v0.2.0` remains the production-qualified baseline.

Product Slice E is delivered under Issue #99 after PRs #100/#101.
The later operator workspace redesign (#102 / PR #103) and RU/BY subscription
default policy (#104 / PR #105) are also merged. Their historical integration
checkpoint is `d4a78d99a54bd7dcc24e04b16ccaf43f3148c385`; production-qualified
stable remains `v0.2.0`. The later trial panel contains this source behavior,
while integrated runtime/component acceptance remains pending in #4.

Issue #106 is completed by PR #107. Reviewed release repairs #109/#110 and
#115/#116 subsequently qualified protected publication. Immutable signed
beta.3 source is `8b76a9696995b6edb6d62bc425876cd894260e57`; immutable signed
beta.4 source is `2c4d4f97ade2f968a1d2f0e1e27f480f12c9f735`. Beta.4 public
verification and its explicitly authorized manual signed-file installation
passed in [ledger #4](https://github.com/popiposter/xkeen-control/issues/4#issuecomment-5938849350).

Issue #117 / PR #118 is completed: Keenetic absent-writer resource admission,
exact default-route spelling and hook compatibility, cold System state, and
the requested standalone-WL new-node disabled default are reviewed and locally
qualified. Beta.5 from `8adff1e00e89515b37aac1d7d6e7e1df924a9143` later passed
protected publication, independent public verification and panel installation.
The subsequent reset/convergence work did not complete integrated acceptance.
The operator stopped further installation on 2026-10-02 and requested the
native-XKeen audit/plan above. No new release or Setup retry is the current next
step. Issues #4/#81 remain historical/reconciliation references; #80 reliability
requirements must be reconciled with the new native lifecycle before implementation.

## Maintenance rule

After every merge, update this file only when status/sequencing changed, refresh master issue #1 if stale, and remove contradictory planning evidence. Do not duplicate detailed active-issue architecture here.


### Native cleanup delivery — 2026-10-03

Development artifact from `0d6a0d6763947a21bc7804492e29d624fefe2dff` is installed. Panel-only replacement independently verified executable, health and preserved native process/configuration. Current 60 nodes, 53 enabled, two subscriptions, WL enabled zero, no pending config. Old component/Setup/appliance import routes return JSON404; native conditional updates expose a terminal from startup. XKeen code remains stock.

Qualification is combined exact-source evidence: original FULL passed Go/race/helpers/frontend/embed/audit and 110/111 browser cases; the single login-heading timeout passed 5/5 unchanged isolated repetitions, then proportional artifact build passed. Cause remains unproven. Do not label this a single successful FULL invocation. Previous 6dba FULL remains bound to that source.

Source/live implementation is delivered for native commands, editors, geodata, subscriptions, encrypted transfer and optional Telegram, with hardware boundaries explicit: unproxied LAN DNS/DIRECT/proxy/outage and client policy, second-router Stage/Apply, real configured bot and native quality comparison remain NOT RUN. Adaptive override remains disabled; native selection is current. No new install feature or native repair protocol is required to run those acceptance checks.
