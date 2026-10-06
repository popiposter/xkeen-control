---
goal: Detailed bounded node quality measurements and post-refresh scheduling
version: 2
date_created: 2026-10-06
last_updated: 2026-10-06
owner: xkeen-control
status: In progress
tags: [feature, quality, native]
---

# Introduction

![Status: In progress](https://img.shields.io/badge/status-In_progress-yellow)

Measure each diagnostic candidate through its fixed Xray probe route. Stock XKeen remains unmodified. Scheduled and button runs use the same detailed methodology; results require explicit Apply.

## 1. Requirements & Constraints

- REQ-001: Sample up to 12 successful enabled candidates in fresh Observatory RTT order, within max(300ms,2*bestRTT), capped at750ms; allow18 attempts and select6.
- REQ-002: Measure idle median/p95/jitter, loaded download/upload latency/jitter, repeated throughput median/p10/p90, request failures and sample counts. Missing metrics are unavailable, never zero loss.
- REQ-003: Exclude warm-up from throughput; use repeated size ramp-up. Per-node ceiling72MiB/60s; whole run864MiB/720s plus3s cleanup. Failed transfers consume budget.
- REQ-004: Run detailed diagnostics every6h and after successful subscription refresh, including no-op. Coalesce refreshes for2min; minimum1h between automatic starts, including manual starts. Busy/pending retries10min.
- REQ-005: Penalize failures, jitter, loaded-delay growth and throughput variability. Throughput benefit saturates above100Mbps down/30Mbps up. Keep native leastLoad selection/failover and explicit Apply.
- SEC-001: Fixed provider URLs, isolated probe route, bounded RAM results; no raw errors, secrets, arbitrary endpoints, UDP-loss claim or live service restart during measurement.
- CON-001: Preserve Draft PR137 separately; do not deploy a candidate that removes the installed Telegram fix. No installer/update/recovery replay.

## 2. Implementation Steps

### Implementation Phase 1

- GOAL-001: Implement detailed measurements, ranking and scheduling as one source cohort.

| Task | Description | Completed | Date |
| --- | --- | --- | --- |
| TASK-001 | Add internal/c1/detailed_quality.go with bounded repeated transfers and concurrent latency probes; integrate AdaptiveRunner only for native broad samples | No | |
| TASK-002 | Extend secretless candidate metrics and native_quality.go costs | No | |
| TASK-003 | Change internal/nativequality/schedule.go refresh coalescing and detailed scheduled runs | No | |
| TASK-004 | Update web/src/native-quality.jsx metrics, budget, schedule and explanatory details | No | |

### Implementation Phase 2

- GOAL-002: Qualify the coherent candidate and record exact evidence.

| Task | Description | Completed | Date |
| --- | --- | --- | --- |
| TASK-005 | Offline fixtures for distributions, failure/cancellation/budget cleanup, rankings and scheduler; proportional checks then one final FULL | No | |
| TASK-006 | Publish dedicated Draft PR with exact evidence; live acceptance only from a candidate retaining installed Telegram behavior, fresh intent and bounded diagnostics | No | |

## 3. Alternatives

- ALT-001: Browser Cloudflare engine cannot isolate each router outbound and includes client/Wi-Fi path; reuse methodology in Go instead.
- ALT-002: TURN UDP packet-loss measurement requires extra server/protocol qualification; defer, report HTTP request failures precisely.

## 4. Dependencies

- DEP-001: Existing c1 ProbeRouter, native Observatory, panel lease and native config editor.
- DEP-002: PR137 contains the installed Telegram fix; this branch is based on its exact2b517f2 HEAD so a later panel delivery retains bot connectivity. PR137 remains independently review-pending; no merge authorization is inferred.

## 5. Files

- FILE-001: internal/c1/adaptive.go, detailed_quality.go, manual.go, native_quality.go and focused fixtures.
- FILE-002: internal/nativequality/service.go, schedule.go and fixtures.
- FILE-003: web/src/native-quality.jsx, performance browser fixtures, docs/NODE-LIFECYCLE.md.

## 6. Testing

- TEST-001: Synthetic stable/variable/slow/failing candidates, throughput saturation and missing-metric handling.
- TEST-002: Loaded sampler cancellation/join before route cleanup and bounded bytes/time including failed transfers.
- TEST-003: Refresh bursts, cadence/manual throttling, busy deferral and cancellation.
- TEST-004: UI metric availability, schedule explanation, recommendation Apply unchanged; final Linux FULL and ARM64 artifact.

## 7. Risks & Assumptions

- RISK-001: Diagnostics compete with user traffic; hard ceilings and visible budgets limit disruption, results remain point-in-time/provider-specific.
- RISK-002: Very fast paths can remain too short at byte ceiling; label limited samples rather than claim line rate.
- ASSUMPTION-001: Native Xray owns production selection; measurements never automatically apply config.

## 8. Related Specifications / Further Reading

- [Native contract](../docs/NATIVE-XKEEN.md)
- [Node lifecycle](../docs/NODE-LIFECYCLE.md)
- [Cloudflare methodology](https://github.com/cloudflare/speedtest)
