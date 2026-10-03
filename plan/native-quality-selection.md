# Native quality selection and failover

Issue121 refinement, requested 2026-10-03. Stock XKeen code stays unchanged.

## Factual baseline

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

Current scorer/native-cost builder is source-only. GUI recommendation/apply and actual native cost/failover acceptance remain pending. Old adaptive override remains disabled; finishing unsafe pinning is unnecessary.

## Recorded implementation boundary

CohortA scorer/native costs/current-first and focused entire c1 Go+vet PASS; independent source review approved corrected runtime. Failed old tests asserted the superseded relativeRTT/current-last policy; updated those expectations while retaining exploration/caps/noise/security cases. Review invalid-health/unknown-backup findings fixed and regressed. No browser change or browser rerun.

NEW live interval-only native editor Save/Apply completed once and independent fresh API/SSH readback PASS:30s instead of5m, no override, native executable/newPID/API and unchanged routing/DNS/outbounds/registry/auth/init/core hashes. Config comparison proves only probeInterval changed; pending absent. Snapshot40healthy/60observed, provider health varies. No forced node failure or LAN-flow switch measurement. Never replay quality-liveness-20261003-new helper/Apply.

Quality core and cost proposal remain source-only, not active native leastLoad. Next wire a bounded comparison/recommendation job without old override supervisor, then GUI same-editor native costs and compatible runtime qualification. No additional global updater/transaction/writer/watchdog. One final FULL before that coherent delivery, proportional Go-only iterations before it.
