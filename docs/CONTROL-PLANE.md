# Control plane

`xkeen-control` is the lightweight management process around XKeen + Xray. Xray remains the traffic data plane; the panel owns typed local operations, safe projections, stable selection, signed panel lifecycle and bounded coordination.

This document describes the **current production-qualified runtime** after D.1 / Issue #3. Signed stable `v0.2.0` is the qualified `linux/arm64` release. Later #4/#5 work exists in source but is not deployed; `docs/ROADMAP.md` owns current source sequencing.

## Current runtime shape

Production installs pre-built Linux ARM64 panel artifacts from signed GitHub Releases:

```text
/opt/sbin/xkeen-control
/opt/etc/init.d/S99xkeen-control
/opt/libexec/xkeen-control-updater
```

Current local state includes:

```text
/opt/etc/xkeen-control/auth/password.bcrypt
/opt/etc/xkeen-control/listen-address                  optional exact private LAN bind
/opt/etc/xkeen-control/config/appliance.json            local typed non-secret authority after adoption
/opt/etc/xkeen-control/secrets/nodes.json              authoritative VPN/subscription registry
/opt/etc/xkeen-control/state/installed-release.json    bounded installed-release marker
/opt/etc/xkeen-control/state/update-policy.json        bounded panel update policy
/opt/etc/xkeen-control/state/                          other small bounded operational state
/opt/etc/xkeen-control/previous/panel/                 one previous panel generation
/opt/etc/xkeen-control/previous/                       bounded node rollback material
/tmp/xkeen-control/                                    transient preview/update/runtime work
```

The binary contains the React/Vite UI. Go/Node toolchains stay off-router.

The source-only operator workspace redesign in Issue #102 uses a compact dark
sidebar, dense paginated Nodes and task-first editors with optional facts behind
native disclosures. [UI-DESIGN.md](UI-DESIGN.md) records the approved density and
workflow coverage; [design-qa.md](../design-qa.md) records synthetic visual QA.
This presentation layer retains the existing typed operation owners and does
not change the production-qualified runtime described below.

## Security boundary

Default listener:

```text
127.0.0.1:8787
```

One exact private LAN address may be configured. Wildcard/public/hostname binds fail closed. Direct WAN exposure is not supported.

Authentication uses a local bcrypt hash, random RAM sessions, HttpOnly/SameSite cookies, same-origin/CSRF checks on mutations, in-memory throttling and security headers. `/healthz` is the only unauthenticated endpoint and returns generic process health.

There is no generic shell, PTY, arbitrary command endpoint, filesystem API or raw configuration editor.

Public/API projections are allowlisted. They may show safe operator fields such as display name, endpoint host/port, source, canonical tag/state, release/build identity and runtime telemetry. They must never expose UUIDs, REALITY key material, short IDs, subscription URLs/tokens, raw VLESS strings or complete secret-bearing registry/outbound objects.

## Current authority split

```text
GitHub Releases                                      software distribution authority
/opt/etc/xkeen-control/config/appliance.json         local typed non-secret authority after adoption
/opt/etc/xkeen-control/secrets/nodes.json             node/subscription secret authority
/opt/etc/xray/configs/02_dns.json                     generated from appliance authority
/opt/etc/xray/configs/05_routing.json                 generated from appliance authority
/opt/etc/xray/configs/07_observatory.json             generated from appliance authority
/opt/etc/xray/configs/04_outbounds.json               generated from nodes.json
config/xray 01/03/06/08 + config/xkeen/xkeen.json      fixed D.1 compatibility templates
RAM + /tmp/xkeen-control                              high-churn preview/update/runtime state
```

Before successful typed D.1 `appliance adopt`, an existing router retains the explicit repository-derived/legacy policy boundary. Adoption is not implicit; fixed companion and generated-policy compatibility must be proven, and unknown/manual drift fails closed.

## Data sources

The current service reads bounded structured state from:

1. Xray `RoutingService.GetBalancerInfo("bal-proxy")`;
2. Xray `ObservatoryService.GetOutboundStatus()`;
3. `nodes.json` through typed registry code and safe projections;
4. local typed appliance policy after adoption, with the fixed compatibility path before adoption;
5. C.1 selection/benchmark state;
6. current panel release/update state.

## Current API domains

Session/runtime endpoints include:

```text
POST /api/v1/session/login
POST /api/v1/session/logout
POST /api/v1/session/password
GET  /api/v1/session
GET  /api/v1/status
GET  /api/v1/nodes
GET  /api/v1/performance
GET  /api/v1/performance/policy
POST /api/v1/performance/policy/preview
POST /api/v1/performance/policy/apply
POST /api/v1/performance/policy/cancel
GET  /api/v1/config-summary
GET  /api/v1/components
POST /api/v1/components/check
GET  /api/v1/components/policy
POST /api/v1/components/policy
POST /api/v1/setup/preview
POST /api/v1/setup/apply
POST /api/v1/setup/cancel
GET  /api/v1/appliance/policy
POST /api/v1/appliance/policy/preview
POST /api/v1/appliance/policy/apply
POST /api/v1/appliance/policy/cancel
GET  /healthz
```

Typed node/subscription mutations include preview/cancel/apply operations for import, replacement, subscription refresh, node enable/disable/remove and manual selection. Preview candidates are RAM-only, session-bound, bounded, expiring and one-shot. Apply receives a server-issued preview token rather than posting the secret payload again.

Subscription refresh initially disables new nodes whose local country hints
identify Russia (RU) or Belarus (BY). Known provider flags in the name take
precedence over tokenized name/hostname aliases; this is metadata inference,
not verified geographic location. Unknown countries retain existing defaults.
RU/BY name codes require uppercase ISO tokens or a numbered provider marker
(for example BY-1, edge-by-01, edge_ru_01); hostname code tokens remain
case-insensitive. Country names/Russian aliases, BLR/RUS and flags remain
recognized. Ordinary name text such as Hosted by Provider or Powered by Example
is unknown and keeps the enabled default; lowercase prose "by" is not a code.
Repeated manual/automatic refresh preserves saved RU/BY per-node enabled choices,
subject to the parent subscription's disabled gate. Existing enabled members are
not migrated or forcibly disabled. Explicit node/subscription enable and manual
profile import retain their existing semantics. The registry remains the sole
saved state authority; no geolocation request or scheduler is added.

The source-main Slice A batch node preview routes are:

```text
POST /api/v1/nodes/batch/state/preview
POST /api/v1/nodes/batch/remove/preview
```

They accept only a bounded, duplicate-free list of safe node IDs plus the
typed desired state where applicable. The Nodes workspace selects across the
complete filtered result, including pages beyond the visible 25-row page, and
uses one selection toolbar for batch state/remove previews and single-node
manual override or profile replacement. These routes reuse the existing
session-bound Preview, BaseDigest, token-only Apply/Cancel, Coordinator,
authority lease and full-registry transaction boundary. Slice A is source-qualified
behavior; it is not production-qualified or deployed.

Source-main Slice B / Issue #70 makes the existing explicit subscription
refresh an exact reconciliation of one complete parsed provider snapshot. A
matched `subscriptionSourceKey` keeps its node ID and canonical tag while
refreshing material and clearing legacy `stale/missing`; new members are
appended deterministically; target members absent from the successful snapshot
are removed in the same candidate. Manual nodes and other subscriptions keep
their committed placement and state. Reordered equivalent snapshots are a
no-op, and fetch/parse/identity/cardinality/candidate/activation failures keep
the last committed generation. The existing refresh Preview, token-only
Apply/Cancel, Coordinator, authority lease and full-registry transaction remain
the only mutation boundary. The safe preview lists ordinary added/updated/
removed changes; exact-refresh removals identify the provider snapshot in the
UI, while the manual-delete reappearance warning remains limited to explicit
manual node deletion. Slice B is source-qualified behavior; it is not deployed or
production-qualified.

Source-main Slice C / Issue #72 adds one purpose-built, in-process refresher
for enabled saved subscriptions. It uses a fixed six-hour cadence, a
five-minute startup wait plus deterministic safe-ID jitter, no catch-up or
persistent scheduler state, one attempt at a time, and a bounded five-minute
read-only registry rescan. New subscriptions discovered after startup use the
normal cadence; a tracked disabled-to-enabled subscription receives the fresh
startup delay. Disabled or removed subscriptions have no automatic due state,
and the attempt path re-reads the authoritative registry before fetching and
before committing.

Provider fetch and Slice B candidate construction happen outside lifecycle and
authority ownership. A changed candidate first takes the Coordinator's
non-preemptive `TryBeginManagedApply` admission and then the shared authority
lease's immediate `TryAcquire`; either busy result defers without waiting or
cancelling benchmark/supervisor work. Under both admissions the exact base
digest is rechecked before one existing full-registry node transaction. Manual
Preview tokens are untouched. Busy/stale work retries only at 5, 15 and 30
minutes before returning to the normal cadence; content and activation failures
also retain the committed generation and schedule the normal cadence.

The existing authenticated `GET /api/v1/nodes` response carries only bounded
RAM status (`waiting`, `running`, `deferred`, `failed` or `disabled`) with safe
timestamps, `updated`/`noop` result and an allowlisted error code, including
`operator-preview` when an operator's live node Preview defers a changed
automatic commit. Authoritative `enabled: false` projects `disabled` immediately
even if the refresher's bounded rescan has not observed the change. The UI is
status-only: explicit Refresh/Edit/Enable/Disable/Remove controls remain, with
no cadence setting, scheduler-run endpoint or extra polling loop. Slice C is
source-qualified only and does not imply release, router access, provider access or
production qualification.

Source-main Slice D / Issue #74 (PR #75) adds one fixed `manual-node`
diagnostic for exactly one current enabled canonical node. The existing
Coordinator performance owner and ProbeRouter lease run three zero-byte
latency requests, then sequential 1/3/8/20 MiB download and 1/3/4/8 MiB
upload stages through the fixed loopback probe transport, with 32 MiB down,
16 MiB up and 45-second whole-run ceilings. Progress and the last result stay
RAM-only; the existing authenticated performance response overlays that
projection on cached historical data so active polling does not force heavy
Xray/XKeen/config reads. The browser posts only a safe node ID, and the
server resolves the current canonical tag. Manual diagnostics never change
selection, manual override or the persisted legacy benchmark. Slice D is
source-qualified only and is not deployed or production-qualified.

Source-main Slice E / Issue #76 adds the only healthy automatic quality-switch
path: a fixed three-hour, process-local adaptive generation fed by the existing
Supervisor/PolicyEngine Observatory RTT evidence. It freezes the current
managed target plus the lowest-five fresh eligible RTT candidates (appending
the current target when needed, hard maximum six), then reuses Slice D's fixed
loopback-proxy down/up transport with 16 MiB download, 8 MiB upload, 30-second
per-candidate and 180-second/144 MiB generation ceilings. The deterministic
same-generation score, RTT guard, 30-minute dwell and 10% hysteresis are
applied through the existing selection transaction; only a real target change
writes `selection.json`. The Coordinator remains the sole performance owner
for `adaptive`, `manual-node` and explicit `legacy-full` modes, and ProbeRouter
cleanup remains gating. Healthy Tick retains RTT evidence but no longer
switches on latency alone; active liveness, manual override and native
`leastPing` fallback remain independent. Legacy `04:17` scheduling is no
longer automatic and explicit `/api/v1/benchmark/run` remains a snapshot-only
compatibility diagnostic. Adaptive status is bounded/RAM-only on the existing
authenticated performance response; there is no new URL, configuration or
run-now surface. Slice E is source-qualified only and is not deployed or
production-qualified.

Source-main Slice F / Issue #78 completes the #46 foundation presentation and
integration boundary without adding another runtime policy or operation owner.
Overview now presents automatic quality from the existing selection and
adaptive projections, including manual-override, native/fallback, waiting,
running, terminal and safe reason states. Nodes presents at most six current
or last-generation candidates with display-name resolution, safe canonical-tag
fallback, RTT/down/up/quality evidence and explicit current/actual-switched
markers. The legacy full benchmark remains an authenticated explicit
compatibility route and readable snapshot, but its primary UI trigger and
daily/next-run presentation are removed; historical throughput is not shown as
adaptive quality.

The existing `GET /api/v1/performance` response remains the only high-frequency
performance telemetry projection path. While adaptive work is running, the UI
polls that path about once per second only on mounted Overview or Nodes views;
manual diagnostic polling remains Nodes-only, and terminal/navigation
transitions stop the extra poll. Slice F itself added no new endpoint,
persistent state, browser storage, scheduler, traffic budget, selection
algorithm, Coordinator/ProbeRouter ownership or mutation API.

Issue #91 D / PR #92 later adds a separate **lazy configuration** projection at
`GET /api/v1/performance/policy` plus session-bound Preview/Apply/Cancel. It
persists only six conservative knobs in
`/opt/etc/xkeen-control/state/performance-policy.json`: active probe interval
60–300 seconds, failure threshold 2–5, adaptive cadence 180–1440 minutes,
challenger limit 1–5 plus the current target, minimum dwell 30–1440 minutes,
and quality hysteresis 10–50%. The existing transfer sizes, generation byte/time
ceilings, transport identity, RTT guard and scoring remain source-owned.

The Performance owner records the exact authority generation that initialized
or successfully converged the active C.1 RAM snapshot. Post-start out-of-band
file replacement/removal/invalidation is projected as closed
`drift-detected`; GET never silently adopts it, and Preview/Apply remain
blocked until authority/runtime coherence is restored deliberately. Policy
Apply neither runs a benchmark, writes selection state nor restarts the runtime.
The Performance workspace loads this settings projection only on entry/explicit
Refresh and preserves the conservative one-shot/unknown-outcome behavior used
by the other policy editors. PR #92 is source-qualified only and is not deployed
or production-qualified.

Current panel lifecycle endpoints are:

```text
GET  /api/v1/update
POST /api/v1/update/check
POST /api/v1/update/policy
POST /api/v1/update/apply
POST /api/v1/update/rollback
```

They use fixed product release policy rather than arbitrary URLs and preserve the same session/origin/CSRF/body-limit boundary as other mutations.

## Node activation transaction

`nodes.json` is authoritative; active `04_outbounds.json` is generated.

```text
RAM preview
   ↓
/tmp registry + rendered outbounds candidate
   ↓
complete Xray validation
   ↓
bounded previous logical generation
   ↓
atomic registry/outbounds replacement
   ↓
foreground XKeen/Xray lifecycle
   ↓
RoutingService readiness + expected bal-proxy inventory
   ↓
success or verified restore/restart/readiness
```

Node activation does not regenerate routing, DNS or Observatory policy.

## Selection / performance

C.1 makes the stable runtime override the normal managed selection policy; native `leastPing` remains emergency fallback.

Source-main E separates active liveness/recovery, existing Observatory RTT
evidence and the single adaptive quality generation. PR #92 keeps the current
60-second / 2-failure liveness values and three-hour adaptive cadence as exact
defaults, but allows only **more conservative** bounded policy: liveness may be
slower or require more failures, adaptive checks may be less frequent/use fewer
challengers, and dwell/hysteresis may be increased. Healthy automatic switching
is still owned only by that adaptive generation; explicit legacy throughput
remains a compatibility diagnostic and no longer selects a healthy target.
Native Xray `leastPing` remains the emergency fallback.

Temporary targeted probes use typed append-only RoutingService rules on the dedicated loopback `probe` inbound. Probe cleanup is gating: cleanup failure prevents a quality-driven switch and further unsafe probe reuse.

Benchmark working state stays in RAM/`/tmp`; one compact completed-run snapshot may persist. Legacy XKeen Speed Balancer and watchdog writers are disabled.

## Shared runtime coordinator

Explicit lifecycle operations must not race selection/probe/benchmark work. Node Apply, manual selection mutation and panel update/rollback share the coordinator lifecycle barrier. The barrier gives explicit operator mutations priority, drains/cancels managed work, holds the mutation critical section through activation/rollback and triggers immediate reconciliation after release.

D.1 import Apply reuses this same maintenance ownership model. Component lifecycle in #4 must also reuse it rather than start independent mutation goroutines. The Issue #81 Setup source boundary uses the same ComponentMutationGate → Coordinator → authority order and one shared component/setup recovery arbiter.

## Signed panel release / update boundary

Slice D / Issue #2 remains production-qualified. Historical `v0.1.1` completed protected publication and bounded live C.1 adoption → exact legacy rollback → re-adoption qualification. D.1 / Issue #3 is production-qualified in signed stable `v0.2.0` from exact source `f170cdb0a9531cb8f4e08c95c0ba9bc8fe3dfd86`.

Normal managed update uses the installed binary's source-pinned Ed25519 trust anchor, fixed GitHub release discovery, bounded HTTPS/redirect/body policy and manifest-bound artifact hashes/sizes. Candidate assets stay under `/tmp/xkeen-control/panel-update`.

The fixed external updater owns only panel lifecycle operations and fixed panel paths. It verifies generic health plus exact local version/source/channel and PID/path. One previous panel generation is retained. It is not a generic command runner or file manager.

The known pre-#2 C.1 layout has a release-owned fingerprint-gated adoption path. Unknown/partial layouts fail closed. The adoption bridge includes bounded recovery for an interrupted legacy node transaction; it does not create a generic Xray mutation surface.

Panel install/update does not install or repair XKeen/Xray and does not rewrite routing, DNS, Observatory or node authority state. Missing components remain a typed Setup Mode state.

## Write model

Normal polling, update checks and runtime telemetry cause no persistent writes unless the operator deliberately changes policy or applies a release/state mutation.

Persistent writes are purpose-specific and bounded, including auth/listener changes, explicit typed `appliance.json` adoption/restore changes, explicit `nodes.json` mutations plus generated active outbounds, the authenticated component policy at `/opt/etc/xkeen-control/state/component-policy.json`, explicit bounded Performance policy changes at `/opt/etc/xkeen-control/state/performance-policy.json`, real stable-selection changes, one compact completed legacy benchmark snapshot, compact panel release/update markers and bounded rollback generations. The Issue #81 Setup source boundary additionally commits the empty authority pair, complete managed config/geodata, exact candidate generations, fixed `S05xkeen` and one bounded shared journal only during explicit Apply; its plan/token remains in RAM. The Issue #83 custom-routing preview remains RAM-only and dispatches Apply through that same D.1 settings journal/recovery owner; it never writes `nodes.json` or independently owns generated outbounds. Adaptive generations, manual progress/results and component scheduler timestamps, status, notification dedupe and failures remain in RAM.

No SQLite/Redis/Prometheus/Grafana/growing revision history belongs on the router.

## D.1 / Issue #3 — production-qualified current runtime

D.1 is production-qualified in signed stable `v0.2.0` on `linux/arm64`. It provides a schema-versioned local non-secret appliance authority and portable typed backup/import/restore while keeping `nodes.json` as the separate VPN/subscription secret authority.

Successful typed `appliance adopt` is intentionally zero-runtime-mutation authority creation: the service strictly parses supported DNS/routing/Observatory policy, proves fixed companion files and generated outbounds are compatible, validates a complete rendered candidate and atomically writes only `appliance.json` after equivalence is proven. After adoption, managed `02_dns.json`, `05_routing.json` and `07_observatory.json` derive deterministically from the appliance authority; `04_outbounds.json` remains generated from `nodes.json`.

The authenticated D.1 backup routes are:

```text
GET  /api/v1/backup/export
POST /api/v1/backup/export-secret
```

The production import routes are:

```text
POST /api/v1/backup/import/preview?mode=settings-only|replace-registry|merge-registry
POST /api/v1/backup/import/apply
POST /api/v1/backup/import/cancel
```

The safe export requires an authenticated same-origin session and contains only typed `appliance` state. The secret export additionally requires the session CSRF token and one request body containing the current password and a 12–256-byte passphrase; it returns a bounded Argon2id/XChaCha20-Poly1305 envelope. Neither route writes backup material to persistent storage.

Import preview uses strict bounded multipart parsing with one in-flight preview admission and returns only a session-bound, expiring server token plus safe change metadata. Apply and cancel accept that token rather than a replacement mode or candidate payload; the mode and candidate are fixed by preview. Restore Apply is preview-first, typed, authority-coordinated and journaled for interrupted-import recovery. An equivalent settings-only restore is a no-op: it preserves node/generated/runtime state and does not restart Xray/XKeen. Secret-bearing backups must never be uploaded to public GitHub evidence.

D.1 does not expose raw JSON/Xray/XKeen editing, does not clone panel auth/listener/update state and does not install/repair XKeen/Xray. Before successful adoption, the explicit repository-derived/legacy compatibility boundary remains in force; unknown/manual drift fails closed. Component lifecycle and typed visual policy work remain source-only outside the deployed D.1 generation.

## Issue #83 — custom-routing policy broker source boundary

Issue #83 adds a backend/API-only typed projection over the adopted appliance
v1 policy. The authenticated routes are:

```text
GET  /api/v1/appliance/policy
POST /api/v1/appliance/policy/preview
POST /api/v1/appliance/policy/apply
POST /api/v1/appliance/policy/cancel
```

The projection edits only server-owned custom client rules in the single
region immediately before the protected final direct catch-all. Protected
routing rules, the `bal-proxy` selector/fallback/leastPing strategy, inbound
tags and generated Xray fields are never request data. Proxy custom domains
are added to both source-owned proxy-DNS resolvers from the embedded
ProductDefault baseline; direct and block domains are not added. The API
rejects protected/manual drift instead of adopting it, returns only bounded
semantic facts, and performs Preview candidate validation before storing a
session-bound one-shot RAM token. Apply reuses the existing Coordinator,
authority lease, complete render/validation, previous generation, journal,
restart/readiness, rollback and startup recovery path. Issue #85 adds the
source-only visual Routing workspace over these unchanged routes. Its
Dashboard-scoped controller loads the policy lazily on Routing entry and by
explicit refresh only; it does not add policy polling to the dashboard
collector. The workspace exposes only typed custom-rule editing plus
read-only protected/DNS/Observatory facts, semantic Preview, and token-only
Apply/Cancel. It adds no backend route, broker/transaction/authority
semantics, raw configuration surface, DNS or Observatory mutation, or
browser-storage state. This remains source-qualified only: it is not deployed and
makes no release, router-access or live-qualification claim.

## Issue #87 — shared DNS + Observatory broker source boundary

Issue #87 adds the backend/API-only DNS and Observatory projection over the
same adopted appliance v1 authority:

```text
GET  /api/v1/appliance/dns-observatory
POST /api/v1/appliance/dns-observatory/preview
POST /api/v1/appliance/dns-observatory/apply
POST /api/v1/appliance/dns-observatory/cancel
```

The authenticated projection exposes only ProductDefault-derived opaque
resolver IDs and safe labels, typed fallback/cache/stale/parallel settings,
bounded proxy-domain counts and a 1..5 whole-minute Observatory cadence.
Resolver addresses, tags, resolver domains, raw policy/configuration and node
or subscription material are never request or response data. The shared
classifier preserves supported DNS/Observatory state across Routing previews
and preserves custom Routing rules across DNS/Observatory previews. Preview
renders and validates a complete candidate before storing a session-bound,
one-shot RAM token; Apply uses the existing Coordinator, authority lease,
previous generation, journal, restart/readiness, rollback and startup recovery
path. This remains source-qualified only and does not add a visual DNS workspace,
performance settings, a new persistence owner or a live-router qualification.

## Issue #89 — visual DNS + Observatory workspace source boundary

Issue #89 adds the Dashboard-scoped visual DNS workspace over the unchanged #87
routes. DNS is loaded lazily on first entry and by explicit refresh; it is not
part of the five-second Dashboard polling loop. The browser edits only ordered
opaque resolver selections, fallback/cache/stale/parallel settings and the
bounded Observatory cadence. Locked query strategy, leak prevention, the fixed
system-fallback object and bounded proxy-domain counts remain read-only facts.

Preview posts the complete typed DNS + Observatory DTO and renders only the
server semantic diff. Apply and Cancel post only the one-shot RAM token. A
Dashboard-owned controller preserves an in-flight Apply across navigation,
never replays an uncertain request, and retains unknown/unproven outcomes until
the affected controller completes a successful fresh read. Routing and DNS
controllers only coordinate by invalidating the peer's completed or in-flight
Preview before Apply and again after an unknown/unproven Apply outcome. While
that outcome awaits its fresh read, both workspaces block new Preview/Apply;
the resolving read updates safe source-owned facts without rebasing either
workspace draft or dirty state. Drafts are not merged, submitted or discarded;
explicit Refresh/Discard retains its rebasing behavior. The backend shared
authority digest remains the final stale-prevention boundary.

The workspace adds no browser storage, backend field/route, persistence,
transaction/recovery owner, performance/selection control, release, router
access or live qualification.

Phase A of #4 adds the separate read-only component inventory foundation: an authenticated `GET /api/v1/components` returns a bounded typed projection for panel, XKeen, Xray, geodata, KeeneticOS and Entware. It performs no network discovery, persistence, coordinator/lease work, lifecycle mutation or panel-update-policy changes; later mutation policy remains a separate typed boundary.

The Phase B source-main boundary adds an authenticated, same-origin/CSRF-bound `POST /api/v1/components/check` for explicit trusted metadata checks of only Xray, XKeen and the fixed product geodata catalog. Results are bounded and RAM-only; no artifact bytes are downloaded, no component or router state is changed, and no production-release or live-qualification claim follows from the source implementation.

Phase F1 adds the backend-only manual component mutation broker on source `main`:

```text
POST /api/v1/components/preview
POST /api/v1/components/apply
POST /api/v1/components/rollback
POST /api/v1/components/cancel
```

The broker accepts only typed Xray/geodata `stable` updates, XKeen `dev`
updates, and typed rollback intents. Preview uses fresh uncached metadata and
RAM-only session-bound tokens; Apply and Rollback dispatch the stored identity
to the existing transaction services, which independently re-resolve, stage,
validate, coordinate, journal, activate and recover. F1 has no Components UI,
policy, scheduler, automatic update, Setup Mode or release surface, and these
routes are not production-qualified or deployed.

Phase F2 adds the source-main Components / Updates UI over those unchanged F1
routes. Inventory is loaded lazily on first entry and by explicit refresh, not
through the five-second dashboard collector. Check and Preview remain explicit
one-component requests; confirmed Apply/Rollback sends only the consumed
server preview token and is never replayed after an error or lost response.
The authenticated status projection now includes Coordinator-observed
`lifecycle.maintenance` and `lifecycle.applying` hints. They add no new owner,
journal or persistence, and an unavailable projection fails closed in the UI.
Component errors expose a stable allowlisted `code` alongside the existing safe
message without changing their HTTP statuses. An Xray `candidate-rejected`
error may additionally carry one closed sanitized `reasonCode` for the
pre-commit class (`artifact-download`, `artifact-integrity`, `archive-extract`,
`binary-probe`, `candidate-render`, `candidate-config-validation`, `staging-io`
or `candidate-validation`); raw errors and candidate details never cross the
HTTP/UI boundary. F2 adds no policy, scheduler, automatic install,
operation-history endpoint, production deployment or live qualification.

## Phase G — typed Setup takeover/convergence source boundary

Issue #81 adds one closed Setup Mode flow for fresh installation and recognized
managed/legacy takeover convergence. It accepts only authenticated,
same-origin/CSRF-bound `POST` requests to
`/api/v1/setup/preview`, `/api/v1/setup/apply` and `/api/v1/setup/cancel`.
Preview accepts only `{}` and resolves the server-owned Xray stable, complete
six-file geodata and qualified XKeen dev identities without downloading bodies
or writing durable state. Apply and Cancel accept only a one-shot,
session-bound RAM token.

Setup is not component Install/repair. It prepares the ProductDefault typed
authority plus an empty canonical node registry for fresh Setup, or preserves
valid `nodes.json`, supported appliance policy, panel-local state and strictly
migrates reviewed legacy profiles for takeover. It renders the complete staged
candidate, validates Xray against that staged config and geodata, qualifies the
XKeen generation and fixed source-owned `S05xkeen`, and converges one typed
source-owned Keenetic Hybrid interception generation with IPv4-only, LAN-input
`br0`/non-LOCAL policy-scoped TCP redirect and UDP TProxy before readiness;
IPv6 remains disabled under the current product policy. Native verification
proves the exact owned chain shape and fwmark/table-111 routing. Reviewed legacy
NDM/netfilter/ipset ownership and automatic writers are retired only through
fixed typed operations; unrelated router state is preserved. One shared `setup`
journal/recovery path snapshots the previous generation's typed interception
subset in the root-only rollback payload, commits the fixed setup-owned paths,
starts/proves the runtime and interception owner, and restores both on failure
or crash.
Partial, mixed, manual or uncertain layouts are blocked; there is no generic
repair or command surface. Ordinary component update/rollback contracts remain
unchanged, including their non-empty outbound verification.

This is source-qualified behavior only. It is not a release, router-install,
production-candidate or live-Setup qualification claim.

## Phase F3 — bounded component policy and check-only scheduler

F3 adds the authenticated, same-origin/CSRF-bound policy surface:

```text
GET  /api/v1/components/policy
POST /api/v1/components/policy
```

The persisted policy is the exact schema-versioned object at
`/opt/etc/xkeen-control/state/component-policy.json`:
`{schemaVersion:1, mode:"manual", checkCadenceMinutes:1440}`. An absent file
means `manual`; present malformed, oversized, non-regular, symlinked or
permission-unsafe input fails closed to effective `off` with a bounded reason.
Only `manual`, `notify` and `off` are accepted, with a 60-minute to 7-day
cadence. `manual` leaves Check and typed Preview/Apply explicit. `notify`
adds a sequential, cached, bounded background Check-only cycle over the fixed
Xray stable, geodata stable and XKeen dev tuples; it never previews, applies,
rolls back or downloads mutation bodies. `off` disables discovery and update
admission while inventory and rollback Preview/Rollback/Cancel remain usable.

The scheduler starts its first cycle only after a full cadence, skips during
unavailable/maintenance/applying lifecycle states, uses no catch-up loop or
persistent status writes, and may call only a typed in-process notification
hook with safe projected fields. Policy changes invalidate existing update
previews below the HTTP/UI layer. F3 is source-qualified only: it adds no release
dispatch, router access, production mutation, live qualification or external
notification transport.

## Product Slice E / Issue #99 — active source boundary

Issue #99 A adds a fixed-host outbound-only Telegram sender, wired through the
existing F3 component scheduler/hook. Only component update/change, panel stable
update-available and explicit operator test alerts exist. Messages contain fixed
product labels, safe channel/state/version, check time and a short candidate
digest. There is no generic event bus, webhook URL, inbound bot command or
automatic mutation surface.

The separate panel-local authority is
`/opt/etc/xkeen-control/secrets/notifications.json`: schema v1, fixed provider
`telegram`, enabled boolean, bot token and numeric chat ID. The complete strict
JSON object is at most 4 KiB, mode 0600 and root-owned under a root-only 0700
secrets directory. Absent means unconfigured; malformed, unsafe or changed files
fail closed without read-side repair or network. Explicit configure/enable/
disable/clear writes are atomic and synced. GET/API errors/status never return
the credentials or upstream content. These credentials are excluded from D.1
safe export and encrypted node backup. They are separately reconfigurable after
reinstall; rollback to an older binary leaves this file ignored without changing
node/appliance/auth authorities.

Authenticated same-origin/CSRF mutations have exact bodies:

```text
GET  /api/v1/notifications                    safe state only
POST /api/v1/notifications/configure          {botToken,chatId}; defaults disabled
POST /api/v1/notifications/enabled            {enabled}
POST /api/v1/notifications/test               {}; one fixed test, even disabled
POST /api/v1/notifications/clear              {}
```

System / Panel contains the Notifications card. Both credential inputs are
password-style browser RAM values, cleared on submission/session/navigation,
never stored in localStorage/sessionStorage or read back. Delivery timestamps,
closed error codes and scheduler/dedupe state are bounded and RAM-only. Component
send attempts occur once per fixed tuple per normal F3 cycle; successful delivery
dedupes the fingerprint, while failure may retry only at the next cadence.
F3 retains one hard 105-second cycle bound: three 30-second metadata Checks plus
three bounded five-second deliveries. Both component and panel schedulers admit
and launch delivery under their policy owner's mutation mutex. A successful
policy change revokes old-epoch attempts still awaiting admission; an already
admitted delivery may finish. Policy, scheduler and lifecycle ownership are not
held across network delivery or its result wait.

The sender uses only HTTPS `api.telegram.org:443` / fixed `sendMessage`, plain
text, no redirect/proxy configuration, a five-second whole-request/connect cap
and a 16 KiB response cap. Connect-time DNS checks reject private/special-use
answers and dial checked numeric IPs with the fixed TLS ServerName. Native
token-in-path errors and provider bodies never reach public projections.

The update owner now has a separate read-only `DiscoverStable` path and one
panel notify scheduler. It verifies signed manifest identity/compatibility,
downloads no artifact bodies and never calls explicit `Manager.Check`, changes
`m.latest`/operator check status or authorizes `ApplyChecked`. It sends only for
a compatible stable version strictly newer than the running installed version.
Persisted stable `notify` waits a full 60-minute..7-day cadence before first run
and after policy changes, observes policy with a bounded read-only rescan, skips
unavailable/maintenance/applying lifecycle states, and has no catch-up burst.
One check runs at a time. Successful dedupe and next-cadence-only failure retries
stay RAM-only. The safe update status includes the scheduler projection, without
placing its discovery result into the operator's checked candidate fields.
Beta notify is `unsupported-channel`; auto-stable is `unsupported-mode`; neither
does background discovery/download/install. Explicit Check remains the only
normal UI path to arm checked Apply.

Issue #99 B hardens the existing private-management boundary. RAM sessions are
capped at 32 (expired-first, oldest expiry/token eviction); remote attempt entries
at 256 (expired-first, deterministic non-locked eviction, all-locked fail closed).
Login/Reauthenticate/CredentialState share a protected <=256-byte bcrypt reader
with Linux root ownership, real 0700 parent, regular non-symlink hash without
group/world bits, no-follow open and pre/open/post identity checks. Unsafe state
is unavailable and is never repaired on read.

Password replacement retires the Manager's RAM credential generation and
invalidates sessions under a separate credential write lock. Login and
Reauthenticate compare bcrypt outside that lock, but stale generations cannot
complete success/admission after rotation. Ordinary session/CSRF work retains
its existing boundary. Valid-length replacement attempts also retire the old
generation on writer/marker errors, which can occur after hash commit; invalid
lengths leave sessions unchanged. No persistent auth state is added.

An early Host guard uses only the accepted socket's http.LocalAddrContextKey.
Exact numeric private/ULA/loopback IP plus effective port is required, bracketed
IPv6 without zones. The sole hostname exception is localhost on loopback; an
omitted port means 80 only for a port-80 listener. Missing/invalid local context
and proxy-host substitutions fail closed before API/assets/health dispatch.
Numeric updater/rebind health probes remain compatible.

Only login {password}, password replacement {newPassword} and secret backup
export {currentPassword,passphrase} receive the strict password-object boundary:
one exact application/json Content-Type, no query, bounded bodies, case-sensitive
required strings, no duplicate/unknown/missing/null/trailing data. The small
password replacement bound is 16 KiB; secret export retains its existing bound.
Security headers retain CSP/nosniff/DENY/no-referrer/Permissions-Policy/API no-store
and add COOP/CORP same-origin, without COEP. System / Panel's Private management
card uses already-loaded listener facts and adds no endpoint, polling or write.
Remote administration uses operator-managed VPN or an SSH tunnel to loopback;
WAN, hostname/wildcard binds and VPN/firewall/DDNS automation remain out of scope.

Issue #99 remains source-only. It does not dispatch a Release, access the live
router or change the production-qualified `v0.2.0` baseline.

## Planned later capabilities

### #4 — component lifecycle production qualification

The source F1 broker, F2 operator UI and F3 bounded policy/check-only scheduler are the typed/version-aware XKeen/Xray/geodata inventory/update/rollback surface. A future production/release qualification must prove the live component paths, rollback/recovery behavior and operator controls before any #4 mutation route is deployed. It will not expose a shell or generic package manager.

### #5 — visual configuration

Issue #5 is complete source-only through Issue #91 F; it is not deployed. The
Dashboard composes the final navigation sequence `Overview → Nodes → Routing →
DNS → Performance → Components / Updates → Backup & Restore → System / Panel`.
Its five-second collector remains limited to status, nodes and performance
telemetry. Routing, DNS/Observatory, Performance policy, component inventory
and System settings load lazily on entry or explicit refresh.

Routing and DNS/Observatory share the existing appliance authority and a narrow
peer Preview invalidation/unknown-outcome fresh-read gate. Performance policy,
Components, Backup & Restore, listener/auth and signed panel updates remain
under their purpose-specific owners. There is no global mutation bus or new
persistent settings authority. Lifecycle-unavailable, maintenance and applying
states disable new mutation initiation across the workspaces. System release
Check and read-only facts remain available; panel update Apply and Rollback
require a known idle lifecycle state.

System / Panel uses the active listener projection resolved at process startup
from environment, listener file or default. Later file drift is shown without
being silently adopted. A typed listener rebind uses the fixed updater and a
202 response means the handoff started; reconnect and verify the active bind.
Signed release Check remains explicit, Apply uses the checked version, and a
202 update/rollback response does not prove the final outcome. Stable `notify` performs the separate check-only discovery described above.
Beta notify and `auto-stable` remain unsupported without background mutation. Components and Backup & Restore retain their own
existing forms and transaction owners. No raw JSON editor is exposed.

The existing listener handoff API remains typed and purpose-specific:

```text
GET  /api/v1/panel/listener
POST /api/v1/panel/listener/preview
POST /api/v1/panel/listener/apply
POST /api/v1/panel/listener/cancel
```

The `202` listener Apply response only confirms that the handoff started. A
same-session refresh cannot establish completion; the operator reconnects to
the target and verifies the active listener projection.

The source-only feature-complete suite verifies these cross-domain boundaries.
The production-qualified baseline remains signed stable `v0.2.0`; no #5 source
change implies a release, router access or live qualification.

## Authorities

- Current system architecture: [`ARCHITECTURE.md`](ARCHITECTURE.md)
- Sequencing: [`ROADMAP.md`](ROADMAP.md)
- Completed D.1 implementation/qualification contract: [Issue #3](https://github.com/popiposter/xkeen-control/issues/3)
- Active source completion contract: [Issue #99](https://github.com/popiposter/xkeen-control/issues/99)
- Live D.2 operational ledger: [Issue #4](https://github.com/popiposter/xkeen-control/issues/4)
- Build/test: [`DEVELOPMENT.md`](DEVELOPMENT.md)
- Production operations: [`OPERATIONS.md`](OPERATIONS.md)
- Security: [`../SECURITY.md`](../SECURITY.md)
