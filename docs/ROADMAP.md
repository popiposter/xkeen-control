# Roadmap

This is the sequencing/status authority. The active GitHub issue is the detailed architecture/acceptance contract for the slice being implemented.

## Current production baseline

Slices A/B/C/C.1 and D are production-qualified. The validated fresh-source migration baseline merged as #7, the canonical Go module/import identity cleanup merged as #9 / Issue #8, and Slice D completed through Issue #2 with historical signed stable release `v0.1.1` from source `8f15246099538426ef08163b832c3aa6f73e8265` plus bounded live Keenetic adoption → rollback → re-adoption qualification. D.1 / Issue #3 is also production-qualified in signed stable `v0.2.0` from exact source `f170cdb0a9531cb8f4e08c95c0ba9bc8fe3dfd86`.

Current runtime facts:

- `/opt/etc/xkeen-control/secrets/nodes.json` is the authoritative local VPN/subscription registry;
- active `04_outbounds.json` is generated from that registry;
- enabled nodes use canonical `proxy-*` tags in one `bal-proxy` pool;
- `xkeen-control` is one pre-built Go binary with embedded React UI, auth and typed node/subscription operations;
- node changes use complete candidate validation, bounded activation and rollback;
- `xkeen-control` is the only managed stable `bal-proxy` override writer; native Xray `leastPing` is emergency fallback;
- 60-second active liveness is independent from RTT quality switching;
- the control-plane-owned sustained benchmark runs once/day with bounded whole-run traffic and RAM/`/tmp` high-churn state;
- node Apply, supervisor/selection and benchmark work share one runtime coordinator;
- signed GitHub Releases are the software distribution authority for the qualified `linux/arm64` panel;
- the public installer and panel rollback path are production-qualified without changing node/Xray/XKeen/routing/DNS/Observatory state;
- after successful typed `appliance adopt`, `/opt/etc/xkeen-control/config/appliance.json` is the local authority for supported non-secret appliance policy;
- managed `02_dns.json`, `05_routing.json` and `07_observatory.json` derive deterministically from that authority, while `04_outbounds.json` remains generated from `nodes.json`;
- before adoption, an existing router retains the explicit repository-derived/legacy policy boundary; adoption is compatibility-gated, not implicit, and unknown/manual drift fails closed.

## Repository authority

`popiposter/xkeen-control` is the public source, CI and release authority. It was initialized on 2026-08-21 from the validated secretless tree of historical `popiposter/xkeen-keenetic` commit `a1b8c3ce4e7f1914312b23b52c3b96269865e90e` using fresh Git history.

The historical repository remains private quarantine/history only. Do not import its commits, branches, PR refs, cached views, releases, Actions artifacts or other historical Git objects into this repository.

Router-specific settings and secrets never enter this repository or release assets.

The active Go module/import identity is canonical: `github.com/popiposter/xkeen-control`. Historical `popiposter/xkeen-keenetic` references that remain in documentation describe quarantine/history only.

## Product direction after D.1

Per-router settings are not synchronized from Git.

```text
public source + signed GitHub Releases (#2, done)
        ↓
local typed appliance state + portable backup (#3, done / v0.2.0)
        ↓
managed XKeen / Xray / geodata lifecycle (#4, active; beta.2 admission PASS; exact install authorization next)
        ↓
visual typed configuration + transactional render/apply (#5, planned)
```

## Delivery sequence

| Slice | Status | Authority | Outcome |
| --- | --- | --- | --- |
| A — secretless unified-pool foundation | Done | validated historical baseline | Secretless software tree, unified pool, transactional deploy foundation |
| B — control-plane skeleton | Done | validated historical baseline | Go binary, embedded UI, auth, runtime projection, Docker qualification |
| C — node/subscription management | Done | validated historical baseline | Local `nodes.json`, canonical tags, typed preview/apply, rollback, operator UI |
| C.1 — stable selection + sustained benchmark | Done | validated source snapshot `a1b8c3c…` | Sticky stable override, independent liveness, bounded benchmark, shared coordinator |
| Pre-D — canonical Go module/import identity | Done | Issue #8 / PR #9 | Canonical `github.com/popiposter/xkeen-control` provenance; no runtime behavior change |
| D — releases/bootstrap/panel self-update | Done | Issue #2 / `v0.1.1` | Public signed Releases, CI/release pipeline, one-command bootstrap, setup mode, transactional panel update/rollback |
| D.1 — appliance state + backup/import/export | Done / production-qualified | Issue #3 / `v0.2.0` | Local schema-versioned settings, safe export, encrypted secret backup, typed restore |
| D.2 — component lifecycle | **Active — beta.2 admission PASS; exact beta install/original-return authorization next** | Issue #4 / `v0.3.0-beta.2` | Signed beta.2 is public and independently verified; read-only install admission passed; production router remains `v0.2.0` until separately authorized exact Apply |
| D.3 — visual configuration | Planned | Issue #5 | Planned typed routing/DNS/XKeen/Xray/panel/performance UI and deterministic render/apply; not deployed |
| E — notifications/security hardening | Planned after D.3 | master issue #1 | Outbound alerts, management-VPN guidance, final attack-surface hardening |

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

## D.2 / Issue #4 — active; beta.2 admission PASS; exact beta install authorization next

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

PR #48 then refreshed the one immutable qualified XKeen catalog entry to the September 15 upstream automated build under completed Issue #47. PR #51 subsequently parallelized ordinary PR/main CI without changing runtime or protected Release workflow semantics. PR #52 corrected D.2 sequencing so a new beta.2 publication and independent host verification precede router install admission. PR #53 removed self-referential exact-source SHA wording from ROADMAP, #50 / PR #54 closed the two known scheduler-sensitive node timing false negatives with test-only changes, and PR #55 closed the repository prerequisites. The protected Release workflow then published signed prerelease `v0.3.0-beta.2`, and independent public-host verification passed against the exact frozen source, seven public assets, source-pinned key, signature, manifest-bound hashes/sizes and `SHA256SUMS`. Exact source/run/asset/report evidence remains in Issue #4 rather than being duplicated here.

Issue #60 / PR #61 then repaired the Q1 scheduler-provenance contract for implicit `crond` defaults without changing runtime behavior. The corrected live writer-containment requalification passed, followed by a completely fresh beta.2 read-only install admission. Report `5683192737` proves stable identity/health, D.1 validate/verify, pending-state settlement, unchanged update policy, intentional `manual-override`, exact beta.2 Check, 20 bounded resource samples and final writer/stable re-check. No Apply/Rollback or other production mutation occurred.

The current [Issue #4](https://github.com/popiposter/xkeen-control/issues/4) remains the Q1 scope/acceptance authority and `docs/OPERATIONS.md` remains the exact bounded operator protocol. Completed Q0 authority is PR #41 / review `5122799862`; completed F2 authority is PR #39 / review `5121493878`. Gate 1 authority is report `5584461861`; beta.2 Gate 2 host authority is report `5681478283`; beta.2 install-admission authority is report `5683192737`.

The simplicity decisions in [master issue #1](https://github.com/popiposter/xkeen-control/issues/1) remain in force: one Go process, embedded UI, distinct component gate → runtime Coordinator → authority lease boundaries, fixed trust adapters and bounded journals/rollback. Q1 adds no generic transaction/form/pipeline framework, job queue/history, database, metrics agent, new API or runtime dependencies. Existing F2 behavior is the subject of qualification, not a reason to expand the product.

**Next is explicit authorization for one exact beta.2 panel install plus the eventual original stable panel return**, not another admission pass. Production remains signed stable `v0.2.0` until the exact Apply is separately authorized and independently verified.

Before any authorized Apply, refresh settlement-consistent stable identity/resources/pending/writer state and repeat the exact beta.2 Check. Use the existing explicit-version signed update path and do not call `/update/policy`. HTTP 202 is handoff acceptance only: independently verify exact running `0.3.0-beta.2` version/source/channel, PID/executable path/hash and health, plus unchanged protected policy/appliance/node/auth/listener state. The trial rollback authority is the updater-created fresh snapshot of the then-current signed `v0.2.0` generation, not historical previous-panel contents. Unknown/lost outcome stops without replay or opportunistic rollback.

After verified beta.2 installation, Gate 3 remains bounded read-only F2 smoke plus exact candidate proof. Later gates separately authorize Xray update/verified rollback → complete geodata update/verified rollback → XKeen update/verified rollback, restoring each component baseline before the next class and settling all component state before original-panel return.

Then refresh F3 bounded `off|notify|manual` check-only policy/scheduler and G supported Setup Mode installation against evidence and concrete need. F3/G remain planned, not canceled or deployed, and are not prerequisites for manual qualification. Issue #4 stays open until its explicitly retained scope is qualified and closed out; #5 and stable promotion require separate decisions.

## D.3 / Issue #5 — planned, not deployed

Make `appliance.json + nodes.json` the supported local configuration authority. UI edits typed domains (routing, DNS, supported XKeen/Xray options, performance, panel settings), renders a complete candidate, validates it, previews semantic impact and applies transactionally. No raw JSON/YAML editor, shell, PTY or arbitrary file manager.

## After D.3

Slice E adds outbound notifications and remote-management/security hardening without creating a remote command plane. The panel remains trusted-LAN/management-VPN only.

## Maintenance rule

After every merge, update this file only when status/sequencing changed, refresh master issue #1 if stale, and remove contradictory planning evidence. Do not duplicate detailed active-issue architecture here.
