# Node loading, measurement, selection and failover

Historical native-shell snapshot, checked against source and router on2026-10-04.
For the proposed shared ARM64/MIPS lifecycle, see the
[target architecture](../plan/architecture-node-quality-v1.md) and
[#199](https://github.com/popiposter/xkeen-control/issues/199). This older
description does not establish current installed probe intervals or automatic
Apply behavior.
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

The Performance action can start a comparison manually. Subscription refreshes
(including successful no-op refreshes) notify the panel's RAM scheduler; a
failed fetch does not. The standard-router schedule only measures and leaves
Apply to the operator. Issue [#188](https://github.com/popiposter/xkeen-control/issues/188)
adds a constrained-router review that tests fresh eligible nodes in sequential
small batches, then applies one verified native pool only after complete coverage.
The first review waits at least10min after startup, refresh notifications coalesce
for2min, and starts are separated by at least6h. MIPS periodic cadence is12h.
Each constrained batch allows3valid/4attempts,24MiB/90s; a review is limited
to6batches/24attempts/144MiB/30min and two reviews/288MiB per rolling24h.
If eligibility exceeds18 nodes, coverage or 80% validity fails, evidence expires,
or resource/configuration state is uncertain, the running pool is preserved.
Manual Apply still uses the same editor and native restart job from this screen.
Source after #205 (not yet released): the constrained review no longer uses
Observatory for eligibility or incumbent health. It freezes every incumbent
plus a fair rotation of the other enabled nodes, probes each with one targeted
zero-byte RTT request (10s, in chunks of six under the panel lease), and only
then reserves quota and speed-tests the candidates that answered within native
maxRTT. A candidate that does not answer, or is slower than maxRTT, is
unhealthy evidence for the pool decision. Reviews refuse with
probe-route-shadowed when any routing rule lacks an inbound restriction. The
next paragraph describes the released Observatory-based behaviour.
Source after #207 and the shared-automatic PR (unreleased): every review on
both profiles uses one pipeline and policy. Limits come from
`resourcepolicy.Profile.Review()`: up to 12 RTT-probed candidates; ARM64 batches
of six without pause within 288 MiB/8 min, MIPS (and ARM64 with 256 MiB or
less) batches of three with a one-minute pause within 72 MiB/12 min. One
automatic review per rolling 24 hours may apply a verified pool on either
profile; it is due daily and pulled forward only by committed node changes
(not no-op refreshes), never sooner than six hours after any start. The manual
speed test only measures and leaves Apply to the operator. The Observatory RTT
history and its health penalty are no longer used. Applying a pool (automatic or
the manual recommendation) writes 05_routing.json and 07_observatory.json in one
validated save: native Observatory then observes exactly the pool members every
10s, concurrently on the standard profile and sequentially on constrained
profiles (MIPS, and ARM64 with 256 MiB or less or unknown memory). Unchanged members with a
stale 07 are repaired by the same joint Apply.
No-healthy-member recovery (unreleased, REQ-009): every 10 minutes the panel
checks whether native Observatory reports every pool member down, or every
member has vanished from the registry. A member without an Observatory record is
unknown, never an outage. Recovery then probes up to 12 candidates (the members
and a rotating share of the other enabled nodes) with the same targeted RTT
probe, spends no speed-test quota and runs no transfer. If a member answers, it
stops. Otherwise it applies up to six answering nodes, fastest RTT first, as a
labelled provisional pool with equal anchored costs through the same validated
05+07 Apply and readback; an unknown outcome sets the inspection fence. At most
one recovery Apply runs per hour, so a failing provisional pool cannot become a
restart loop. An active manual override defers recovery. If nothing answers,
the configuration is left intact and proxied destinations keep the block
fallback. The next complete automatic review replaces a provisional pool with its
measured top six and clears the label. The Nodes table labels nodes without an
Observatory record "Not monitored" rather than failed.
Eligible nodes need fresh, alive native observations within the configured finite
RTT criterion. Manual tests additionally require RTT no more than twice the
lowest fresh RTT, with a300ms floor. Comparison candidates come from all enabled
managed outbounds, not only the current selected pool. A bounded sample does not
prove the global best among nodes outside the RTT criterion.

Existing bounded diagnostics temporarily target each candidate and clean their
owned temporary diagnostic state; they do not replace the production selection.
Download stages1/3/4/8MiB (max16), upload1/3/4MiB (max8),8s per stage,30s per node;
each direction stops once a complete stage lasts>=1s. Aggregate complete byte/time
rates require>=250ms; incomplete transfers fail. On a standard router, manual
comparisons have288MiB/360s and automatic comparisons144MiB/180s; MIPS keeps the
smaller per-batch limits above. Cleanup is included in each wall ceiling. All failed bytes count. Replacements
stop at that ceiling; at least two valid results may form an explicitly partial
recommendation. The action is named Run speed test. Download then upload; do not infer packet loss from
HTTP failures. Unique retained native observations contribute failure-frequency and
median absolute RTT-deviation penalties after sufficient successful observations.

Rank up to six valid measured nodes by RTT * sqrt(cost), matching ordinary native
leastLoad's tradeoff. Costs avoid double RTT weighting:
Q = (down/bestDown)^0.75 * (up/bestUp)^0.25 / healthPenalty;
cost = clamp(1/Q^2,1,100). The selected six receive exact tag costs and become the
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

The recommendation preserves the configured native strategy, expected count and
maxRTT while selecting up to six exact tags and costs; it keeps the existing
fallback and creates no automatic override.
Native Xray excludes dead/noncandidate/too-slow nodes
and orders eligible nodes by its RTT-deviation metric multiplied by sqrt(cost),
with average RTT and health tie-breaks. Ordinary Observatory supplies delay as that
metric; burst HealthPing data, if configured, supplies its deviation. Lowest eligible
candidate handles new connections. This is native weighted choice, not a panel
hysteresis/dwell loop; the never-started C.1 supervisor and its constants were removed (#199 Phase 0).

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
