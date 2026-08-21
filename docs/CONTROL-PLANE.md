# Control plane

`xkeen-control` is the lightweight management process around XKeen + Xray. Xray remains the traffic data plane; the panel owns typed local operations, safe projections, stable selection and bounded coordination.

This document describes the **current production-qualified runtime**. Planned releases/bootstrap/local appliance state/component updates are summarized separately and are not yet active behavior.

## Current runtime shape

Production currently installs one pre-built Linux ARM64 binary and init script:

```text
/opt/sbin/xkeen-control
/opt/etc/init.d/S99xkeen-control
```

Current local state:

```text
/opt/etc/xkeen-control/auth/password.bcrypt
/opt/etc/xkeen-control/listen-address             optional exact private LAN bind
/opt/etc/xkeen-control/secrets/nodes.json         authoritative VPN/subscription registry
/opt/etc/xkeen-control/state/                     small bounded persistent runtime state
/opt/etc/xkeen-control/previous/                  bounded rollback material
/opt/etc/xkeen-control/previous/panel/            exactly one previous panel generation
/opt/etc/xkeen-control/state/installed-release.json last successful panel release
/opt/etc/xkeen-control/state/update-policy.json  bounded update policy
/opt/libexec/xkeen-control-updater                fixed panel rollback helper
/tmp/xkeen-control/                               transient work
/tmp/xkeen-control/panel-update/                 release candidate assets
```

The binary contains the React/Vite UI. Go/Node toolchains stay off-router.

## Security boundary

Default listener:

```text
127.0.0.1:8787
```

One exact private LAN address may be configured. Wildcard/public/hostname binds fail closed. Direct WAN exposure is not supported.

Authentication uses a local bcrypt hash, random RAM sessions, HttpOnly/SameSite cookies, same-origin/CSRF checks on mutations, in-memory throttling and security headers. `/healthz` is the only unauthenticated endpoint and returns generic process health.

There is no generic shell, PTY, arbitrary command endpoint, filesystem API or raw configuration editor.

Public/API projections are allowlisted. They may show safe operator fields such as display name, endpoint host/port, source, canonical tag/state and runtime telemetry. They must never expose UUIDs, REALITY key material, short IDs, subscription URLs/tokens, raw VLESS strings or complete secret-bearing registry/outbound objects.

## Data sources

The current service reads bounded structured state from:

1. Xray `RoutingService.GetBalancerInfo("bal-proxy")`;
2. Xray `ObservatoryService.GetOutboundStatus()`;
3. `nodes.json` through typed registry code and safe projections;
4. current non-secret Xray/XKeen policy summaries;
5. C.1 selection/benchmark state.

## Current API domains

Session/runtime endpoints include:

```text
POST /api/v1/session/login
POST /api/v1/session/logout
GET  /api/v1/session
GET  /api/v1/status
GET  /api/v1/nodes
GET  /api/v1/performance
GET  /api/v1/config-summary
GET  /healthz
GET  /api/v1/update
POST /api/v1/update/check
POST /api/v1/update/policy
POST /api/v1/update/apply
POST /api/v1/update/rollback
POST /api/v1/session/password
```

Typed node/subscription mutations include preview/cancel/apply operations for import, replacement, subscription refresh, node enable/disable/remove and manual selection. The exact endpoint list lives in code/tests; this document describes domain boundaries rather than duplicating every route.

Preview candidates are RAM-only, session-bound, bounded, expiring and one-shot. Apply receives a server-issued preview token rather than posting the secret payload again.

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

The supervisor separates 60-second active liveness failover, bounded Observatory RTT quality decisions and once/day sustained throughput evidence.

Temporary targeted probes use typed append-only RoutingService rules on the dedicated loopback `probe` inbound. Probe cleanup is gating: cleanup failure prevents a benchmark-driven switch and further unsafe probe reuse.

Benchmark working state stays in RAM/`/tmp`; one compact completed-run snapshot may persist. Legacy XKeen Speed Balancer and watchdog writers are disabled.

## Shared runtime coordinator

Explicit lifecycle operations must not race selection/probe/benchmark work. Current node Apply uses the shared coordinator to preempt benchmark work, drain relevant supervisor/probe activity, hold the mutation critical section through Xray activation/rollback, then trigger immediate reconciliation.

Future panel/component/import operations must reuse or extend this same maintenance ownership model rather than start independent mutation goroutines.

Issue #2 panel update and rollback are typed lifecycle mutations backed by the same coordinator. They operate only on the panel binary/init/helper and do not install or repair XKeen/Xray or rewrite routing, DNS, Observatory or the node registry. Missing components are reported in the authenticated setup projection as `missing` while the panel remains usable.

## Write model

Normal polling and runtime telemetry cause no persistent writes.

Persistent writes are purpose-specific and bounded, currently including auth/listener changes, explicit `nodes.json` mutations plus generated active outbounds, real stable-selection changes, one compact completed benchmark snapshot and bounded rollback generations.

No SQLite/Redis/Prometheus/Grafana/growing revision history belongs on the router.

## Planned product capabilities — not yet deployed

### #2 — releases/bootstrap/panel self-update

This public repository becomes the signed software distribution channel. The panel will gain signed release discovery, one-command bootstrap, setup mode and transactional self-update with one previous binary generation. Normal panel install/update will not require a GitHub credential.

### #3 — local appliance state / backup

A schema-versioned local `appliance.json` will become the non-secret supported settings authority. Portable safe export excludes VPN/subscription secrets by default; optional secret-bearing backup is encrypted. This file does not exist as authority yet.

### #4 — component lifecycle

Typed/version-aware XKeen/Xray/geodata inventory/update/rollback will share the maintenance coordinator. It will not expose a shell or generic package manager.

### #5 — visual configuration

Supported routing, DNS, XKeen/Xray, performance and panel settings will be rendered from typed local state into complete runtime candidates with preview/validation/apply/rollback. No raw JSON editor.

## Authorities

- Current system architecture: [`ARCHITECTURE.md`](ARCHITECTURE.md)
- Sequencing: [`ROADMAP.md`](ROADMAP.md)
- Build/test: [`DEVELOPMENT.md`](DEVELOPMENT.md)
- Production operations: [`OPERATIONS.md`](OPERATIONS.md)
- Fresh install/restore: [`FRESH-KEENETIC.md`](FRESH-KEENETIC.md)
- Security: [`../SECURITY.md`](../SECURITY.md)
