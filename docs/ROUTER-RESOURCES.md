# Router resource limits

Issue [#157](https://github.com/popiposter/xkeen-control/issues/157) delivers
conservative diagnostic limits in signed 0.4.3. These limits await hardware
qualification, not measured router throughput capacity.

| Profile | Single node | Comparison | Automatic comparisons |
| --- | --- | --- | --- |
| MIPS, RAM at most 256 MiB, or unknown total RAM | 4 MiB / 20 seconds | 3 successful nodes, 4 attempts, 24 MiB / 90 seconds per batch | Bounded sequential sweep under #188; hardware acceptance pending |
| Other supported routers | 48 MiB / 45 seconds | Manual: 12 successful / 18 attempts, 288 MiB / 360 seconds; automatic: 6 / 12, 144 MiB / 180 seconds | At most every 6 hours |

Wall ceilings include the existing three-second probe cleanup reserve. Failed
and partial transfers count toward traffic limits; stages can stop early. These
tests measure short transfers and should not be presented as line-rate capacity.

Issue [#188](https://github.com/popiposter/xkeen-control/issues/188) replaces
the MIPS automatic-disabled policy with a bounded sequential review. Each batch
keeps the constrained limits above; an automatic review may use at most six
batches, 24 attempts, 144 MiB and 30 minutes. The rolling 24-hour automatic
ceiling is two reviews and 288 MiB, including failed transfers. Refreshes are
coalesced and a review never starts within six hours of another comparison.
The private quota receipt also preserves the last comparison start and an
inspection-required fence across panel restarts. An uncertain native outcome
cannot be retried simply by restarting the panel. Manual comparisons have a
separate per-run 24 MiB ceiling rather than drawing from the automatic quota;
the panel shows both limits and the rolling automatic reservation usage.
After an uncertain result, the panel's explicit inspection action checks the
complete pending-free native workspace, lifecycle receipt/process, Xray runtime,
active pool and probe rules. It reconciles only the panel's known temporary
probe rules through their shared owner and verifies its in-memory gate cleared;
it never reruns Apply or the speed test. If any
readback remains uncertain, the fence persists for operator investigation.
Only fresh, healthy, RTT-eligible nodes are candidates. An automatic pool change
requires a complete review with every frozen eligible candidate attempted,
at least 80% valid fresh measurements, unchanged registry/configuration and one
verified native Apply. A partial three-node manual sample does not meet this
condition. The current native pool remains in effect on incomplete or uncertain
outcomes. These source limits do not establish live hardware acceptance.

The existing measurement owners share a read-only hardware profile and an
on-demand `/proc` sampler. No new daemon, persistent settings authority or timer
is installed. Admission uses two CPU deltas: both at least 85% busy refuses work.
Available memory below 32 MiB (constrained) / 64 MiB (standard), or swap-out above
1 MiB/s, also refuses admission. During work, three consecutive one-second
pressure readings (CPU at least 90%, low memory or rapid swap-out) cancel work.
Unknown telemetry refuses/stops testing explicitly. Historical swap occupancy
alone does not establish current pressure. Cleanup retains its own context and
owner; the sampler is joined before the panel operation lease is released.

A configured native periodic speed test blocks panel speed tests. This bounded
cron inspection is not exclusion of external CLI/cron. An operator must inspect
and quiesce competing work before testing; the panel never rewrites native cron
or silently stops native jobs. Missing cron is treated as absent; unreadable or
oversized cron is unknown. Recheck actual native activity during live acceptance.

Recommendation staging preserves native strategy and settings, including
`maxRTT`, `expected` and unrelated values. It updates costs only for `leastLoad`;
`leastPing` retains its strategy and receives only the measured selector list.
Configured positive RTT up to one minute bounds eligibility. Omitted RTT uses a
labelled 10-second diagnostic ceiling without writing it into native settings.
Manual comparisons additionally restrict candidates to twice the lowest fresh
latency, with a 300 ms floor, within that ceiling.

Freshness derives from ordinary Observatory selectors, concurrency and interval.
Concurrent cycle allowance is interval + 5 seconds; sequential allowance applies
that amount to each matched outbound. Add 30 seconds scheduling margin and refuse
horizons over 15 minutes. The default interval is 10 seconds. These semantics
follow [Xray Observatory](https://github.com/XTLS/Xray-core/blob/main/app/observatory/observer.go).
Only currently selected, freshly observed nodes enter comparisons. Other stored
nodes remain available for explicit single-node tests, without invented health.

Existing runtime cache/singleflight, hidden-page polling pause and DNS no-op
synchronization remain unchanged. No broader benchmark from #138 is enabled.

## Hardware evidence and current blocker

An ARM64 40.7-second observation found CPU average 5.8%, maximum 24.8%; the main
configuration remains the control. A prior KN1810 39.3-second observation found
27.2% average and 99.4% peak with Xray bursts. Concurrent TLS probes are a
hypothesis; a controlled configuration comparison has not run.

The combined 180-second observer failed before collecting frames. Independent
short observations do not make that failed run pass. On KN1810, 52 actual enabled
proxy outbounds were observed; 59 was the registry inventory including disabled
entries. A smaller five-target cohort was prepared but not saved: the editor
rejected it while an interrupted node transaction retained its pending marker.
Cron was restored unchanged. Recovery is separately specified in
[#158](https://github.com/popiposter/xkeen-control/issues/158). Do not delete the
marker, restart to clear the lock, bypass the editor or replay native activation.
The signed0.4.9 MIPS manual speed test completed; automatic sweep, controlled
load comparison and client acceptance remain pending.
