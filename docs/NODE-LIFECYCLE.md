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
(including no-op) requests a detailed comparison after2min; notifications coalesce and no
automatic start happens within1h of any previous manual/automatic comparison start.
Busy, pending or unavailable conditions defer10min; an admitted failed measurement
still consumes the1h slot. A regular four-run day has a ceiling3456MiB; frequent
subscription refreshes can request up to24runs/day, ceiling20736MiB. Actual transfer
use is usually smaller; panel restarts reset the RAM schedule. No automatic Stage, Apply, native restart
or override: results are a recommendation. Manual Apply recommendation uses the
same editor and native restart job directly from the test screen.
Eligible nodes need fresh native observations (<=2min), alive and RTT<=750ms.
Detailed tests additionally require RTT <= max(300ms, twice the lowest fresh RTT),
capped at750ms. Sample all enabled managed outbounds, not only the current selected
pool: up to12successful measurements /18attempts in fresh RTT/tag order. Failed
measurements use the next eligible candidate. Button and automatic tests share
the same detailed measurement path. This bounded sample is not proof of
the global best among unmeasured nodes.

Existing bounded diagnostics temporarily target each candidate and clean their
owned temporary diagnostic state; they do not replace the production selection.
Detailed diagnostics warm up download and upload with1MiB each, then measure8 idle
HTTP response delays. Download repeats4MiB three times, then16MiB twice; upload
repeats2MiB three times, then8MiB twice. After three measurements a direction stops
if the latest transfer lasted>=1s. A candidate has a72MiB reservation and60s deadline;
the generation has864MiB/720s plus existing3s cleanup. Failed transfers reserve the
entire requested payload, including upload bytes that cannot be confirmed. Replacements
stop at that ceiling; at least two valid results may form an explicitly partial
recommendation. The action is named Run speed test. Download then upload; do not infer packet loss from
HTTP failures. One concurrent sampler per transfer measures loaded latency, up to32
attempts and200ms spacing, joined before diagnostic route cleanup. Timings are HTTP
response delays through each outbound, not ICMP. A per-candidate HTTP keep-alive
session avoids repeated handshakes after warm-up. Median/p95/jitter and sample counts
are retained for idle/download/upload latency; throughput median/p10/p90 use repeats
of the largest completed size, with all size/duration/rate samples available in Performance. Short
transfer flags are retained. Missing loaded metrics stay unavailable. Unique retained native observations contribute failure-frequency and
median absolute RTT-deviation penalties after sufficient successful observations.

Rank up to six valid measured nodes by RTT * sqrt(cost), matching ordinary native
leastLoad's tradeoff. Costs avoid double RTT weighting:
Detailed throughput saturates at100Mbps down/30Mbps up. Let failure be the failed
HTTP request fraction, growth=max(loaded p95)-idle median, jitter the largest
observed jitter, variation=max((p90-p10)/median) across throughput directions.
measurementPenalty=1+min(9,5*failure+max(0,growth)/200+jitter/100+variation/2).
Q = (down/bestDown)^0.75 * (up/bestUp)^0.25 / healthPenalty / measurementPenalty;
cost = clamp(1/Q^2,1,100). Performance displays quality100/sqrt(cost), a relative
sample score rather than Cloudflare AIM, and expandable full metrics for each node.
Ranking also uses measured idle median RTT. The selected six receive exact tag costs and become the
balancer selector; other enabled nodes remain loaded and broadly observed for the
next test. Reject prefix collisions with any other outbound. At least two valid
measurements and age<=30min are required. Stage consumes the recommendation,
rejects baseline/inventory drift or unrelated pending changes, saves only
bal-proxy.strategy and selector through the same validated native editor.
Apply recommendation follows with one existing native restart job. HTTP202 is
acceptance; completed plus configurationState applied proves application. Failed
or ambiguous restart leaves ordinary pending config/console for inspection, with
no automatic retry or rollback. Unsaved GUI edits block this action and survive
navigation. The Stage API alone remains save-only.

## Native selection and switching

The recommendation uses leastLoad, expected1, maxRTT750ms, up to six exact tag
costs and selectors, preserving the existing fallback; no automatic override.
Native Xray excludes dead/noncandidate/too-slow nodes
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
can make them stale; newly added tags remain outside a restricted active pool until
a fresh recommendation is applied, but are eligible for subsequent speed tests.
Node/subscription verification recognizes the exact weighted quality subset without
requiring all enabled outbounds in the active selector. It still verifies the full
enabled artifact and native API. These limits matter when interpreting best-node UI.

Actual stock-core isolated failure/recovery fixture passed; its2s/loopback timing
is not a production/LAN guarantee. Independent LAN DNS/outage/failover timing is
not yet qualified. Current PC uses Karing and cannot establish that evidence.

Sources for pinned native semantics:
[leastLoad](https://raw.githubusercontent.com/XTLS/Xray-core/v26.9.30/app/router/strategy_leastload.go),
[Observatory](https://raw.githubusercontent.com/XTLS/Xray-core/v26.9.30/app/observatory/observer.go).
Panel sources: internal/nodes/refresher.go, operations.go, transaction.go;
internal/nativequality/service.go; internal/c1/adaptive.go and native_quality.go.


Metadata-only node/subscription commits retain the same intent, drift check and
rollback snapshot. When parsed native outbounds are unchanged, save only the
registry: no Xray validation, outbounds rewrite or service restart. Display names,
subscription labels/URLs/cadence and disabled-profile edits can qualify; actual
runtime changes still require full validation and normal activation.
