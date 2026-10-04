# Node loading, measurement, selection and failover

Current native-shell implementation, checked against source and router on2026-10-04.
Panel-managed profiles live in the private registry; stock XKeen remains unmodified.

## Loading and updating

Manual VLESS/REALITY links or an HTTPS subscription are parsed into the registry.
Subscription downloads have15s timeout,1MiB body limit, redirect and public-IP/SSRF
checks. Mixed lists import supported profiles and ignore other protocols. Profiles
are validated/deduplicated; stable source identity matches existing subscription
members, updates their credentials/name, adds new members and removes absent ones.
Other subscriptions and unmanaged native outbounds are preserved.

New WL-name profiles are disabled by default (case-insensitive separate word);
RU/BY country defaults also apply, including manual imports. Defaults affect only
new members: matching profiles retain every saved enabled/disabled choice on refresh,
with a disabled subscription gating its members. Explicitly enabled WL/RU/BY members
are not forcibly disabled by refresh. A removed and later reintroduced identity is
a new member and receives defaults again.

Manual changes use Preview then one Apply against its exact registry baseline;
missing-member removals in a manual refresh require explicit acceptance.
Automatic subscription refresh is sequential: startup5min plus deterministic0..5min
jitter, then6h after completion; registry rescan5min. Busy/preview/drift defers at
5/15/30min; fetch/content/activation failures do not immediately replay an operation.
Scheduler status is RAM-only and starts anew when the panel restarts.

Unchanged content is a no-op, with no restart. Changed content renders only managed
04_outbounds, preserves unrelated native files, validates the complete Xray config,
saves one previous registry/outbounds pair, invokes stock S05xkeen restart on and
verifies readiness, selectors and native API state. A known activation failure
restores the pair and verifies restoration. Unknown lifecycle or failed recovery
keeps the pending marker and blocks panel writers; do not replay Apply blindly.
Panel operations serialize each other; external native CLI/cron are not locked.

2026-10-04 correction: the inventory verifier previously imposed leastPing after
successful native restart. Native leastLoad therefore caused activation and rollback
verification to fail. Strategy compatibility now belongs to full Xray validation;
selector membership and native runtime checks remain. Node update does not change
native strategy, routing, DNS or health settings.

## Continuous health checks

The installed core is Xray26.9.30. Its Observatory performs an HTTP request through
each enabled proxy to https://www.google.com/generate_204. This checks actual proxy
reachability and response delay, not ICMP ping or bandwidth. Alive/delay/time/error
observations feed native balancing. Installed enableConcurrency=true and interval30s:
a parallel probe cycle finishes, then waits30s; slow probes increase revisit time.
It is not an exact30s failover guarantee. The panel displays these native results.

## Throughput comparison

The Performance action can start a comparison manually. A RAM-only scheduler also
runs after10min startup and every6h. Successful manual/automatic subscription refresh
(including no-op) requests a comparison after2min; notifications coalesce and no
automatic start happens within6h of any previous manual/automatic comparison start.
Busy, pending or unavailable conditions defer10min; an admitted failed measurement
still consumes the6h slot. Maximum automatic transfer budget576MiB/day in a continuous
run; panel restarts reset the RAM schedule. No automatic Stage, Apply, native restart
or override: results are a recommendation, applied explicitly through the editor.
Eligible nodes need
fresh native observations (<=2min), alive and RTT<=750ms. Current native target is
first, then up to four lowest-RTT alternatives and one rotating exploration candidate.
This is a shortlist, not a measurement of every node or proof of the global best.

Existing bounded diagnostics temporarily target each candidate and clean their
owned temporary diagnostic state; they do not replace the production selection.
Download stages1/3/4/8MiB (max16), upload1/3/4MiB (max8),8s per stage,30s per node;
each direction stops once a complete stage lasts>=1s. Aggregate complete byte/time
rates require>=250ms; incomplete transfers fail. Whole comparison max6nodes,
144MiB/180s plus3s cleanup. Download then upload; do not infer packet loss from
HTTP failures. Unique retained native observations contribute failure-frequency and
median absolute RTT-deviation penalties after sufficient successful observations.

Display score includes RTT/download/upload/health. Applying a recommendation uses
separate native costs, avoiding double RTT weighting:
Q = (down/bestDown)^0.75 * (up/bestUp)^0.25 / healthPenalty;
cost = clamp(1/Q^2,1,100). Unmeasured/failed members get100, remain eligible backups.
At least two valid measurements and age<=30min are required. Stage consumes the
recommendation, rejects baseline/pool drift, saves only bal-proxy.strategy through
the same validated native editor. Explicit Apply restarts once. Stage alone does
not select a node or activate saved config.

## Native selection and switching

Installed strategy is leastLoad, expected1, maxRTT750ms,53 exact tag costs, fallback
block; no runtime override. Native Xray excludes dead/noncandidate/too-slow nodes
and orders eligible nodes by its RTT-deviation metric multiplied by sqrt(cost),
with average RTT and health tie-breaks. Ordinary Observatory supplies delay as that
metric; burst HealthPing data, if configured, supplies its deviation. Lowest eligible
candidate handles new connections. This is native weighted choice, not a panel
hysteresis/dwell loop; old supervisor constants do not govern this operating mode.

After a failed probe, future connections use another eligible node. Existing TCP
sessions cannot be migrated; applications reconnect. If all candidates are unusable,
block fallback prevents VPN-only traffic leaking directly. Direct routing rules
remain direct. Recovery probes make nodes eligible again. Native health/selection
continue if the panel stops; subscription refresh and new throughput comparisons do
not. Manual API override can bypass native health selection: remove it to return to
automatic balancing. No automatic override is created by quality recommendations.

Persisted costs do not expire or continuously remeasure speed. New/changed profiles
can make them stale; newly added tags without matching cost use Xray default1 until
a fresh comparison is applied. Exact tag weights cover the measured pool at Stage,
not future subscription members. These limits matter when interpreting best-node UI.

Actual stock-core isolated failure/recovery fixture passed; its2s/loopback timing
is not a production/LAN guarantee. Independent LAN DNS/outage/failover timing is
not yet qualified. Current PC uses Karing and cannot establish that evidence.

Sources for pinned native semantics:
[leastLoad](https://raw.githubusercontent.com/XTLS/Xray-core/v26.9.30/app/router/strategy_leastload.go),
[Observatory](https://raw.githubusercontent.com/XTLS/Xray-core/v26.9.30/app/observatory/observer.go).
Panel sources: internal/nodes/refresher.go, operations.go, transaction.go;
internal/nativequality/service.go; internal/c1/adaptive.go and native_quality.go.
