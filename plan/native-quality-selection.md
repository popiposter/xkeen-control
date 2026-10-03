# Native quality selection and failover

Issue121 refinement, requested 2026-10-03. Stock XKeen code stays unchanged.

## Initial audit baseline

Installed panel source0d6a, audit sourceabeeb9a. Main does not start the old C1 supervisor or expose override writes. Live read-only status: native/effective target present, no override, Observatory interval5m,43healthy/60observed at snapshot. This is configuration evidence, not a measured failover time or LAN success.

Xray26.9.30 leastPing excludes observed-dead nodes. Existing flows are not migrated; new flows select a healthy alternative after observation updates. A five-minute interval is too slow for this use case. Inspect all-unavailable fallback before changing configuration; do not silently send VPN-only traffic directly.

Sources: [override bypass](https://raw.githubusercontent.com/XTLS/Xray-core/v26.9.30/app/router/balancing.go), [native health/cost selection](https://raw.githubusercontent.com/XTLS/Xray-core/v26.9.30/app/router/strategy_leastload.go), [cost matching](https://raw.githubusercontent.com/XTLS/Xray-core/v26.9.30/app/router/weight.go).

## Decisions

1. Keep calibrated download/upload/RTT comparison,10% hysteresis and dwell, but remove fastest-node-relative RTT veto. A20ms/1Mbps node must not exclude60ms/100Mbps. Keep absolute750ms challenger ceiling. Unique retained Observatory failures and median absolute RTT deviation penalize instability. HTTP probe failure is not described as packet loss.
2. Keep existing transfer ceilings:6candidates/144MiB/180s, no sustained benchmark. Measure current target first, retain rotating exploration and report shortlist scope, never global best among unmeasured nodes.
3. No persistent API override for automatic quality: it bypasses native health and has no TTL. Do not revive old supervisor, add watchdogs or patch native code. Translate quality evidence into ordinary native leastLoad costs, preserving selectors/fallback/all backups. Xray remains sole selection/failover owner if panel stops.
4. Costs weight throughput and observed health, not RTT twice. Exact anchored escaped tag match; bounded1..100. Failed/unmeasured nodes are not removed; all explicit pool members receive exact costs; unmeasured/failed backups receive conservative100 and remain eligible. Proposals need nonzero generation, ordered bounded start/completion, exact shortlist count, pool membership,2valid measurements and age<=30m. This partial recommendation cannot guarantee global best. Persisted costs may become stale but do not pin dead nodes.

## Implementation and acceptance

- CohortA: scorer/health/native-cost proposal and counterexample/finiteness/stale/matching tests; source-only, no selection writes.
- CohortB: reuse bounded measurement job/lease, show measured recommendation in GUI; SAME pending config editor for explicit leastLoad-cost Apply. No new transaction owner or arbitrary endpoint. Native expected1 keeps a single chosen healthy target; backups remain eligible.
- CohortC: installed-core compatibility, pending/rollback/config baseline, NEW native editor change for faster health checks (start30s), full Xray validation, one bounded Apply and independent readback. Preserve stock code/auth/registry/selective routing/DNS. Never replay old Apply.
- CohortD: bounded failover proof with panel stopped, failed selected proxy/healthy backup, all unavailable, recovery; distinguish source fixtures/core/router API/LAN traffic. Karing is not LAN evidence. No provider manipulation or Keenetic firewall rewrite to manufacture failure.

At the initial source checkpoint, scorer/native-cost builder and comparison/GUI were source-only. Installed acceptance is recorded below. Old adaptive override remains disabled.

## Recorded implementation boundary

CohortA scorer/native costs/current-first and focused entire c1 Go+vet PASS; independent source review approved corrected runtime. Failed old tests asserted the superseded relativeRTT/current-last policy; updated those expectations while retaining exploration/caps/noise/security cases. Review invalid-health/unknown-backup findings fixed and regressed. No browser change or browser rerun.

NEW live interval-only native editor Save/Apply completed once and independent fresh API/SSH readback PASS:30s instead of5m, no override, native executable/newPID/API and unchanged routing/DNS/outbounds/registry/auth/init/core hashes. Config comparison proves only probeInterval changed; pending absent. Snapshot40healthy/60observed, provider health varies. No forced node failure or LAN-flow switch measurement. Never replay quality-liveness-20261003-new helper/Apply.

Before delivery, quality core, bounded comparison and GUI cost proposal were source-only. The coherent candidate and native runtime acceptance are now recorded below. No additional global updater/transaction/writer/watchdog. One final FULL before that coherent delivery, proportional Go-only iterations before it.

## Comparison/editor cohort

Explicit RAM-only comparison uses the existing coordinator lifecycle and fixed
adaptive transfer runner, with the SAME panel lease held through cleanup. Current
native target first, four fastest eligible alternatives, one rotating alternative;
six candidates/144MiB/180s ceilings. Fresh native observations required, no override.
The existing policy engine retains unique Observatory timestamps from ordinary
collector reads, with no new poller or automatic selection loop. Three retained
successes permit failure/jitter penalties; a single observation is not presented
as a stability window. Native leastLoad also retains its own health/deviation logic.

Authenticated GUI shows measured down/up and RTT. Save consumes the fresh
recommendation, rejects config/registry drift and incomplete target projections,
replaces only the native balancer strategy through the SAME lossless JSONC editor,
validates the full candidate and creates an ordinary pending change. It does not
restart or select. Existing Apply/discard/previous/diagnostics remain the sole
configuration owner. No automatic bandwidth test, cadence or override write;
native selection uses persisted costs and live health until another explicit
comparison. Old inactive adaptive-policy GUI and its obsolete browser suite retired.

NEW three-node fixed-provider diagnostics completed once, with independent
readback of unchanged config digest, no pending/no override, completed manual
state and no temporary diagnostic rules. Down/up Mbps respectively: current
150.62/49.20, lowest-RTT alternative136.27/51.49, exploration27.51/27.41.
Total transferred116MiB, within144MiB; no global ranking/LAN/failover-time claim.


## Installed acceptance: immutable source61f4946

Exact61f49469627de85375a1b4cbb37e7f23b9ded2ac FULL PASS Go/vet/race,
helpers,101browser/frontend/embed/audit0. ARM64 artifact15,663,264bytes SHA256
`d4fc147c5d12a9dbe852bc3b602ae60316d1593c8b580eae3599a59d03191521`.
ONE panel-only delivery independently PASS, preserving native process/files.
Development identity by tested artifact hash, not a signed release.

ONE NEW bounded comparison completed,5valid of6sampled; download Mbps
100.19,74.25,45.32,failed,5.64,53.35. ONE recommendation Stage and ONE typed
native Apply completed. Independent fresh API/SSH/fullXrayvalidation PASS:
only bal-proxystrategy changed to native leastLoad(expected1,maxRTT750ms),
53exact costs1.295..100, selectors/rules/blackholefallback preserved, no override
or temporary diagnostic rule, native PID/executable/hash and unaffected
core/init/registry/auth/configs verified;60nodes53enabled2subscriptionsWL0,
pendingabsent. Observatory30s. This enables native automatic health/RTT-weighted
selection from the measured costs. Repeated throughput comparisons are explicit
GUI actions; no automatic speed-test schedule or panel override loop is started.

Isolated actual installed stock-core fixture: two synthetic loopback outbounds,
leastLoad(cost1/100),2sObservatory; successful SOCKS flow, preferredfailure ->
healthybackup with successful flow, bothdead -> blocked flow, recovery -> successful
flow, no override. Remote fixture execution PASS; localwrapper postcheck reused
launch free-memory threshold and failed before identity output. Separate fresh
cleanup/process/file readback PASS without replay, proving no fixture processes
and unchanged production PID/files. Owned temporary helper binary removed.
Synthetic switch956ms is ONLY the2s loopback fixture, not production30s or LAN
failover timing. Independent LAN/Karing-free client DNS/routing/outage acceptance
remains NOTRUN. Stock XKeen code unchanged.
