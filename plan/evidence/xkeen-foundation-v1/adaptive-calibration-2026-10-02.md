# Offline adaptive score calibration

Scope: Issue #121 TASK-020 scoring-only implementation. This is working-tree
iteration evidence on base `f61a361727400934f316b0b6b6ca40e8676211ce`, not final
exact-HEAD qualification. No router, live transfer, activation or native SB
comparison was performed. TASK-020 remains partial; exploration, initial run,
mode transitions and independent override expiry remain separate work.

The score is
`[(Rmin/R)^0.35 * (D/Dmax)^0.45 * (U/Umax)^0.20]^0.40`.
Production evaluates weighted differences of logarithms followed by `exp`,
avoiding intermediate underflow for finite positive rates. Normalization is
dimensionless. Pairwise ratios do not depend on the rate unit or an unrelated
candidate's normalization maxima. Existing RTT guards, dwell, hysteresis,
admission, transport and generation budgets are unchanged.

## Reproduction and observed results

Run in the repository's Docker/Linux development environment:

```powershell
docker compose -f docker-compose.dev.yml run --rm -T dev go test -count=1 -run '^TestAdaptiveCalibration' -v ./internal/c1
docker compose -f docker-compose.dev.yml run --rm -T dev go test -count=1 ./internal/c1
```

RED: before changing production code, focused tests exited 1. Equal-RTT 2x
throughput yielded score ratio `1.027831034` and Apply retained the incumbent
with reason `hysteresis`; unit-invariance and calibrated RTT tradeoff tests
also failed. The new all-axes noise calibration interval differed from the old
formula, although the old formula also avoided switches in that noise trace.

GREEN: after the change, all six focused calibration tests passed (exit 0).

| Synthetic scenario | Observed result |
| --- | --- |
| Equal RTT, 2x download/upload | Score improvement 19.747870%; Apply switches after dwell |
| Equal RTT, 10x download/upload | Score improvement 81.970086%; Apply switches after dwell |
| All 64 corners of independent +/-10% noise, forward/reverse replay | 128 generations; zero switches; maximum improvement 8.357773% |
| Rate scales 1e-9, 1, MiB, 1e100; unrelated extreme guarded candidate | Pairwise score ratio invariant within 1e-12 |
| Smallest positive float versus maximum finite float | Scores remain finite and positive |
| Invalid download/upload and exact RTT guard boundaries | Invalid/guarded challenger cannot win |

The complete `internal/c1` package then passed once (exit 0, reported test time
0.332s), including existing freshness/unique-observation, manual/native,
stale-generation, dwell/hysteresis, cancellation and budget tests. `gofmt` and
`git diff --check` passed. No full repository gate, native SB replay, independent
review or hardware result is claimed.

Tested file SHA-256:

- `internal/c1/adaptive.go`: `20331e8a0a382f79e4a9fd20d1bd76dd4b6e9c2747dcc7c19fbb2d68aaa15581`
- `internal/c1/adaptive_calibration_test.go`: `b5eaabaf80000a993811b84ec7d939e0947cccf48d96692b3ad50e9a3dc69a75`

The synthetic observations establish bounded score behavior, not actual network
capacity. Endpoint caps, loss and native SB behavior require further corpus and
bounded qualification. Native leastPing remains the active safe mode; this
change provides no independent stale-override expiry owner.

## Exploration and initial-run follow-up

Commit `603347e` adds one RAM stable-ID exploration cursor. With more eligible
challengers than the configured cap, the last RTT shortlist position rotates
through the remaining eligible nodes; the fastest preceding positions and
current node stay included. A cap of one rotates all eligible challengers.
Removal, reordering and readdition do not reset traversal to the fastest node.
Freezing a generation advances the cursor, even if that generation is later
cancelled. No additional candidate or transfer budget is added.

Existing supervisor tick notifications may admit one initial quality generation
after at least three independent fresh, alive, positive RTT observations. The
opportunity is consumed before transfer, including a failed/cancelled generation;
subsequent runs use the normal cadence. Busy, manual, stale, duplicate, failed or
zero-RTT evidence cannot trigger transfers. There is no extra network poller or
persistent scheduling write. A process restart starts a new RAM evidence window.

Meaningful pre-fix failures reproduced permanent tail starvation, dependence on
RTT order rather than stable IDs, waiting the full cadence despite fresh samples,
and acceptance of failed/zero RTT samples. Author package tests and focused race
tests passed; independent root verification ran the entire `internal/c1` race
suite successfully (4.522s). Independent source review approved all six changed
files. This is source evidence, not a live optimizer comparison.

The 144 MiB / 180 second generation ceilings are unchanged. Native Xray remains
the deployed selection owner and panel adaptive stays disabled. Traffic/day UI,
economical preset, native SB comparison and independent stale-override expiry
remain separate unfinished work.
