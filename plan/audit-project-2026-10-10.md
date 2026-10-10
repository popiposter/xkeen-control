---
goal: Audit of the node-quality target, project size, development process, tests and releases
version: 1.1
date_created: 2026-10-10
last_updated: 2026-10-10
status: 'In progress'
tags: [audit, simplification, process, release, nodes]
---

# Project simplification audit (2026-10-10)

Source: branch `codex/target-node-architecture` at `6f022b8` (PR #200), compared
with `origin/main` `c20235a`. All counts below come from the repository. Commands
were run on the Windows host, which does not replace Docker/Linux qualification.
No router was accessed.

The short verdict: the product core is sound. Over **40%** of Go production
code (≈26k of ≈60k lines, excluding generated protobuf), and the tests,
scripts and docs that serve it, belong to retired generations: the appliance, the component lifecycle, the C.1
supervisor and the legacy benchmark. The binary never executes it, but every
FULL run tests it and every agent has to read past it. Process overhead also
grew faster than the product. Recent changes needed a ~10k-character issue
contract, an independent review, a local FULL, a hosted FULL, a stable release
and then a separate docs-only "evidence" PR.

## Status (2026-10-10)

| Item | State |
| --- | --- |
| §1 A1–A9 | Corrected in `architecture-node-quality-v1.md` (this PR). |
| §1 A5: #194, #198 | Closed as superseded by #199 on operator decision. |
| §2 rows 1–2, 4–6: `components`, `appliance`, legacy `backup`, `performancepolicy`, unused routes, legacy scripts | Done in #202 (`b3cb737`): −36k lines; independent review approved, FULL passed. |
| §2 row 3: `c1` supervisor/benchmark generation | #199 Phase 0 (TASK-000), in progress. It changes the `/api/v1/status` projection, so it was not part of a behaviour-neutral PR. |
| §2 row 7: `splitdns` | Open; needs an operator decision. |
| §3: double Go pass, Playwright duplication | Done in #203 (`72ee184`). FULL takes ~170 s end to end, previously ~6–7 min. See §3.1. |
| §4 process changes, §5 step 3 docs pruning | Open. |

## 1. Node-quality target (`plan/architecture-node-quality-v1.md`)

The direction is right: one scheduler, decision and Apply path for both CPUs.
Observatory covers only the ≤6-member pool, and no-op refreshes cost zero.
The findings below are corrected in the plan in the same PR. Their severity
is the severity they would have had during implementation.

| ID | Severity | Finding | Resolution in the plan |
| --- | --- | --- | --- |
| A1 | P1 | REQ-008 ceilings cannot be met by the existing ladder. ARM64 worst case is 24 MiB per candidate (down 1/3/4/8, up 1/3/4), so 24 candidates need up to 576 MiB against a 288 MiB cap. MIPS is 6 MiB × 12 = 72 MiB against 48 MiB. Measurement is sequential (`ProbeRouter` lease size 1, up to 30 s per node): 24 × 30 s = 12 min against 10 min. MIPS adds three 3-minute batch pauses (`sweepBatchPause`). Fast links stop the ladder *later*, so the reviews most worth applying would hit the cap and fail the 80% rule. | Ceilings are derived from the ladder. Both profiles review **12** candidates. ARM64: 288 MiB / 8 min. MIPS: 72 MiB / 12 min, in batches of three with a 1-minute pause. An invariant `bytes ≥ candidates × ladder worst case` is tested. |
| A2 | P1 | REQ-004 introduces a second measurement subsystem: a daily 64-node discovery pass with its own cursor, 24-hour evidence store and durable cleanup fence. REQ-007 already requires a fresh targeted RTT for every frozen candidate, and REQ-009 already needs on-demand probing during an outage. | The daily inventory pass is removed. Targeted RTT probes run only (a) as the review pre-phase for the frozen candidates, reusing the review's fair cursor, and (b) during outage recovery. There is no separate evidence store or cursor. |
| A3 | P2 | REQ-004 requires a *durable* probe-rule fence. Probe rules exist only in Xray memory and match only the loopback `probe` inbound, so a leftover rule cannot affect client traffic. The real gap is elsewhere: the only startup `ProbeRouter.Reconcile` call is in `c1.Supervisor.Start`, and `Coordinator.Start`, which would start it, is never called (§2). | The durable fence is replaced with "reconcile every managed probe tag at panel start and before each probe run; skip the probe on failure". This adds the missing live startup call. |
| A4 | P2 | SEC-001 requires proof that the requested outbound carried the probe, but the code has no such check. The probe rule is appended (`AddRule(..., true)`), so any earlier `05` rule without an `inboundTag` restriction could route probe traffic to `bal-proxy` and measure the wrong node. The shipped reference is safe only because all of its rules are inbound-scoped. | Admission now runs a static check: every rule before the appended probe rule must have an `inboundTag` set that excludes `probe`. Otherwise the review refuses with `probe-route-shadowed`. No stats counters or new state are needed. |
| A5 | P2 | #194 (orphan selector after subscription churn, 9.7k-character contract with a degraded-provenance receipt) and #198 (durable one-time "imported pool initialization intent") are listed as dependencies. Both treat symptoms of stale broad-Observatory evidence, which this target removes. | Both fold into #199. **Rule 1:** an exact selector without an enabled matching outbound is an *unhealthy member*. The node transaction keeps `05` byte-for-byte, and zero healthy members triggers REQ-009. **Rule 2:** the first review from any non-target selector (broad, imported or orphaned) is the same initialization path, with fresh RTT for incumbents, and needs no intent record. Closing #194/#198 as superseded needs operator approval. |
| A6 | P3 | REQ-006 says both "one automatic review per rolling 24 h" and "≥6 h between starts". For automatic reviews the second rule is redundant. It is unclear whether a change-triggered review may exceed the daily limit. | One rule: one automatic review per rolling 24 h. A change waits for the next allowed slot. The 6-hour gap applies only between a manual test and an automatic review. |
| A7 | P3 | REQ-007's "improves by at least 15%" does not say whether it is measured per member or per pool. | Defined per pool: rank all valid results, take the top six, and apply only when that pool's aggregate score is ≥15% better or an incumbent is unhealthy. |
| A8 | P3 | `resourcepolicy.ForPlatform` has a third profile: ARM64 with ≤256 MiB is `constrained` with `Automatic=false`. The plan names only two. | The plan states that low-memory ARM64 uses the MIPS limits with the same automatic policy. |
| A9 | P3 | TASK-004 refactors `nativequality` around a `c1` package that is half dead (§2). Unifying first means preserving dead seams. | A Phase 0 is added: remove the dead `c1` scheduler, supervisor and benchmark before TASK-001. |

These were checked and are **not** findings. The `leastLoad` and `observatory`
semantics match the pinned Xray sources cited in `docs/NODE-LIFECYCLE.md`. The
fresh reference ships `leastPing` and a broad selector, which the first quality
Apply converts. The ladder's early stop (a stage ≥1 s) is consistent with the
throughput model.

## 2. Dead and retired code

"Reachable" means called from `cmd/xkeen-control` at runtime. Line counts
exclude tests unless stated otherwise.

| Area | Size | Evidence | Action |
| --- | --- | --- | --- |
| `internal/components` | 21,068 + 8,687 test | The only external use is two constants, `components.DefaultXrayBinary` and `DefaultXrayAssetDir` (`cmd/xkeen-control/main.go:275,330`). | Move the two constants to `internal/xkeen` and delete the package, `scripts/test-components.sh` and its DEVELOPMENT.md section. |
| `internal/appliance` | 1,766 + 505 test | Imported only by `components` and the never-constructed `backup.Service`. | Delete. |
| `internal/backup` legacy half | ~550 | `backup.New`/`NewService`/`ParseBundle`/`OpenEncrypted` have no non-test callers. `nativebackup` uses only the envelope (`SealProduced`/`OpenProduced`/`ValidatePassphrase`/errors). | Keep the envelope (~200 lines) and delete the bundle/Service code. |
| `c1` supervisor, schedules and benchmark | ~1,800 (`supervisor.go`, schedule parts of `coordinator.go`, `benchmark.go`, `performance_policy.go`, `state.go`) | `Coordinator.Start` is never called, so the supervisor loop, adaptive schedule and nightly benchmark never run. The live parts are the `ProbeRouter`, the lease/maintenance, the manual and adaptive runners, and `SetManualOverride`. | Keep the live parts and delete the rest (also covered by A9). |
| `internal/performancepolicy` + `/api/v1/performance/policy{,/preview,/apply,/cancel}` | 682 + routes | It only configures the dead adaptive cadence. The UI never calls it; only Playwright fixtures mock it. | Delete. |
| Unused routes | — | `/api/v1/benchmark/run` (dead runner), `/api/v1/config-summary` (no UI caller), and `/api/v1/backup/export`, whose handler is `nativebackup.Export`, which always returns `ErrUnavailable`. | Delete them and their fixtures. |
| Legacy router scripts | ~70 + fixtures | `speed-failover-watchdog.sh`, `install-watchdog.sh`, `install-performance-schedule.sh`, `disable-legacy-speed-balancer.sh`, `run-bounded-speed-benchmark.sh` and `install-control-plane.sh` edit native XKeen cron/config. That contradicts the 2026-10-03 rule to leave XKeen unmodified, and none of them ships in a release. `verify.sh` and `update-geodata.sh` have no references. `test-c1.sh` and `test-backup.sh` are thin wrappers. | Delete them, plus `test-benchmark-policy.sh`. Replace the mention in `docs/MIPS-KEENETIC.md` with a one-line manual instruction. |
| `internal/splitdns` + `/api/v1/dns/split/sync` + `split-dns.jsx` | 1,212 + 317 test | Fresh setup no longer uses mosdns (#181). A one-minute loop and the config-validation hook stay active for existing deployments. | One explicit slice: inspect the routers that still own mosdns, retire them, then delete the package. This needs an operator decision. |

Where to stop: `nodes` (6.5k), `xkeen` (4.2k), `setup` (2.2k), `update` (1.7k)
and `httpapi` are live and proportionate to what they do. Simplify them only
through ordinary feature work.

## 3. Tests

- Go tests total ~33k lines. About 10k test the dead packages above. On the
  host, the dead packages alone take ~22 s without `-race`. FULL runs every Go
  test twice: `go test -count=1 ./...` and then `go test -race ./...`
  (`scripts/dev-check.sh:90,93`). The race run already executes every test.
  **Run only `-race` in FULL** and keep `go vet`.
- The Playwright fixtures (`web/tests/fixtures/feature-complete-model.js`,
  `performance-ui.spec.js`, and the `config-summary` mocks in five specs) mock
  endpoints the UI no longer calls. Delete them with the routes. Keep the
  119-test suite otherwise. It is the only end-to-end UI evidence.
- Several shell fixtures assert *absences* with grep (for example
  `test-components.sh` checks for forbidden routes and commands). Once the code
  is gone, so is the risk. Keep only `test-public-hygiene.sh` as the generic
  guard.
- The Release workflow runs `dev-check.sh --full` and then
  `release-build.sh` rebuilds both binaries. That rebuild is cheap and keeps
  byte provenance, so leave it.

### 3.1 Measured FULL breakdown and outcome (#203)

Measured in the Docker gate on a cold Go test cache, before #203:

- **`go test -race ./...`: 117 s.** `internal/auth` alone took 109 s, because its fixtures hashed passwords at the production bcrypt cost 12.
- **Playwright: 106 s,** with 2 workers on 16 available CPUs.
- **`test-updater.sh`: 54 s,** of which ~35 s was real `sleep 1` readiness waits.
- **Plain `go test -count=1 ./...`: 21 s,** a repeat of the race run.
- **Everything else: ~35 s.**

The local race pass was usually served from the Go test cache, so it did not
rerun at all.

After #203:
- FULL runs a single uncached `go test -race -count=1 ./...`.
- The `auth` fixtures use `bcrypt.MinCost`, and a test pins the production cost at 12.
- The updater fixture uses an instant `sleep`.
- Playwright runs six local workers; the hosted release keeps two.
- Fact matrices moved to Node unit tests.
- Duplicated responsive sweeps were dropped, keeping the only populated overflow pass.

Result: 106 browser tests plus 20 new unit cases, and FULL takes ~170 s.

## 4. Development process

What works: secretless public evidence, exact-HEAD review, the fast
proportional `dev-check`, and protected signing with independent asset
verification. Keep all of them.

What costs more than it returns:

1. **Issue contracts are specifications in prose.** #194 is 9.7k characters
   and #198 is 8.2k, with 5–7 numbered clauses each. They are dense enough that
   contradictions (A1, A5) survive review. Keep a template of at most one
   screen: goal, decision, non-goals, acceptance, tests. Detail belongs in code
   and fixtures.
2. **A stable release for every hardware iteration.** Six stable releases
   (0.4.7 to 0.4.12) shipped within about 12 hours on 2026-10-09/10, each a
   follow-up fix to MIPS quality behaviour. The workflow already supports the
   `beta` channel. Iterate on hardware with `beta` and promote one stable build
   once acceptance passes.
3. **A docs-only "record evidence" PR after every release** (#175, #178, #187,
   #190, #193, #197). Generate the evidence into the GitHub Release body
   (source SHA, run ID, asset hashes, which the workflow already knows).
   `RELEASES.md` then keeps only the trust contract and a link. Update
   `ROADMAP.md` in the feature PR when status actually changes.
4. **Several agent-instruction files.** `AGENTS.md`,
   `.github/copilot-instructions.md`, `docs/CHATGPT-PROJECT-INSTRUCTIONS.md`
   and now `CLAUDE.md` overlap. The Copilot file says agents "never merge
   without explicit operator authorization", which contradicts the standing
   authority in AGENTS.md §6. Keep `AGENTS.md` as the source and make the
   others one-line pointers.
5. **Documentation volume.** There are 30 docs, 23 plan files and 184 KB of
   archive. `DEVELOPMENT.md` (396 lines) carries per-issue sections (#99 B,
   #2, #4, Issue150). Move per-issue fixtures into their issue or test names.
   Delete completed plans and audits from `plan/`, since Git history keeps
   them. Keep `docs/archive/` only if someone still reads it.
6. **The README carries release narrative** (KN1810 sweep counts and NOTRUN
   paths). That belongs in the release notes. The README should state
   capability and installation only.

## 5. Proposed sequence

Each step is an ordinary Draft PR. Steps 1–3 change no runtime behaviour, so
they need focused tests plus one FULL, and no hardware run.

1. **Delete dead Go code and routes.** Done in #202, except the `c1`
   generation, which moved to #199 Phase 0.
2. **Make the process changes.** The FULL speed-up is done (#203). Still open: generate release
   notes in the workflow, use beta for hardware iteration, add a short issue
   template, and turn the instruction files into pointers. Docs-only, except
   for `dev-check.sh` and `release.yml`.
3. **Prune the docs.** Covers `DEVELOPMENT.md`, `RELEASES.md`, the README
   narrative and `plan/` cleanup.
4. **Implement #199 with the corrected plan.** Starts with Phase 0, the `c1`
   cleanup, which is in progress. #194/#198 are closed.
5. **Retire splitdns.** Needs an operator decision about routers that still
   own mosdns.

Rollback for steps 1–3 is a Git revert. Nothing touches the router.
