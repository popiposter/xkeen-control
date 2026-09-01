# Control plane

`xkeen-control` is the lightweight management process around XKeen + Xray. Xray remains the traffic data plane; the panel owns typed local operations, safe projections, stable selection, signed panel lifecycle and bounded coordination.

This document describes the **current production-qualified runtime** after Slice D / Issue #2. D.1 / Issue #3 is the current product slice and remains planned until its implementation and production qualification complete.

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
/opt/etc/xkeen-control/secrets/nodes.json              authoritative VPN/subscription registry
/opt/etc/xkeen-control/state/installed-release.json    bounded installed-release marker
/opt/etc/xkeen-control/state/update-policy.json        bounded panel update policy
/opt/etc/xkeen-control/state/                          other small bounded operational state
/opt/etc/xkeen-control/previous/panel/                 one previous panel generation
/opt/etc/xkeen-control/previous/                       bounded node rollback material
/tmp/xkeen-control/                                    transient preview/update/runtime work
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

Public/API projections are allowlisted. They may show safe operator fields such as display name, endpoint host/port, source, canonical tag/state, release/build identity and runtime telemetry. They must never expose UUIDs, REALITY key material, short IDs, subscription URLs/tokens, raw VLESS strings or complete secret-bearing registry/outbound objects.

## Current authority split

```text
GitHub Releases                                      software distribution authority
/opt/etc/xkeen-control/secrets/nodes.json           node/subscription secret authority
/opt/etc/xray/configs/04_outbounds.json             generated from nodes.json
config/xray/*.json + config/xkeen/xkeen.json        current non-secret policy source
RAM + /tmp/xkeen-control                            high-churn preview/update/runtime state
```

Until D.1 adoption succeeds, routing/DNS/Observatory policy remains repository-derived. The planned local `/opt/etc/xkeen-control/config/appliance.json` authority does not exist yet in current production behavior.

## Data sources

The current service reads bounded structured state from:

1. Xray `RoutingService.GetBalancerInfo("bal-proxy")`;
2. Xray `ObservatoryService.GetOutboundStatus()`;
3. `nodes.json` through typed registry code and safe projections;
4. current non-secret Xray/XKeen policy summaries;
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
GET  /api/v1/config-summary
GET  /healthz
```

Typed node/subscription mutations include preview/cancel/apply operations for import, replacement, subscription refresh, node enable/disable/remove and manual selection. Preview candidates are RAM-only, session-bound, bounded, expiring and one-shot. Apply receives a server-issued preview token rather than posting the secret payload again.

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

The supervisor separates 60-second active liveness failover, bounded Observatory RTT quality decisions and once/day sustained throughput evidence.

Temporary targeted probes use typed append-only RoutingService rules on the dedicated loopback `probe` inbound. Probe cleanup is gating: cleanup failure prevents a benchmark-driven switch and further unsafe probe reuse.

Benchmark working state stays in RAM/`/tmp`; one compact completed-run snapshot may persist. Legacy XKeen Speed Balancer and watchdog writers are disabled.

## Shared runtime coordinator

Explicit lifecycle operations must not race selection/probe/benchmark work. Node Apply, manual selection mutation and panel update/rollback share the coordinator lifecycle barrier. The barrier gives explicit operator mutations priority, drains/cancels managed work, holds the mutation critical section through activation/rollback and triggers immediate reconciliation after release.

D.1 import Apply and later component lifecycle must reuse this same maintenance ownership model rather than start independent mutation goroutines.

## Signed panel release / update boundary

Slice D / Issue #2 is production-qualified. `v0.1.1` completed protected publication and bounded live historical C.1 adoption → exact legacy rollback → re-adoption qualification.

Normal managed update uses the installed binary's source-pinned Ed25519 trust anchor, fixed GitHub release discovery, bounded HTTPS/redirect/body policy and manifest-bound artifact hashes/sizes. Candidate assets stay under `/tmp/xkeen-control/panel-update`.

The fixed external updater owns only panel lifecycle operations and fixed panel paths. It verifies generic health plus exact local version/source/channel and PID/path. One previous panel generation is retained. It is not a generic command runner or file manager.

The known pre-#2 C.1 layout has a release-owned fingerprint-gated adoption path. Unknown/partial layouts fail closed. The adoption bridge includes bounded recovery for an interrupted legacy node transaction; it does not create a generic Xray mutation surface.

Panel install/update does not install or repair XKeen/Xray and does not rewrite routing, DNS, Observatory or node authority state. Missing components remain a typed Setup Mode state.

## Write model

Normal polling, update checks and runtime telemetry cause no persistent writes unless the operator deliberately changes policy or applies a release/state mutation.

Persistent writes are purpose-specific and bounded, including auth/listener changes, explicit `nodes.json` mutations plus generated active outbounds, real stable-selection changes, one compact completed benchmark snapshot, compact panel release/update markers and bounded rollback generations.

No SQLite/Redis/Prometheus/Grafana/growing revision history belongs on the router.

## D.1 / Issue #3 — current product slice, not yet deployed

D.1 introduces a schema-versioned local non-secret appliance authority and portable backup/import/restore. The detailed contract lives in Issue #3.

Planned authority after successful D.1 adoption:

```text
/opt/etc/xkeen-control/config/appliance.json        portable supported non-secret policy
/opt/etc/xkeen-control/secrets/nodes.json           separate VPN/subscription secret authority
active supported Xray policy files                  deterministic generated artifacts
```

The first D.1 phase is intentionally zero-runtime-mutation adoption: strict-parse current supported DNS/routing/Observatory policy, prove fixed companion files and generated outbounds are compatible, validate a complete rendered candidate, then atomically write only `appliance.json` after equivalence is proven.

Phase B adds a safe export with no VPN/subscription secrets by default and an explicitly re-authenticated encrypted secret-bearing export. Phase C will add bounded session-bound preview/apply restore. Secret-bearing backups must never be uploaded to public GitHub evidence.

The Phase B source implementation on the Issue #3 Draft PR (not yet deployed or production-qualified) adds these routes:

```text
GET  /api/v1/backup/export
POST /api/v1/backup/export-secret
```

The safe export requires an authenticated same-origin session and contains only typed `appliance` state. The secret export additionally requires the session CSRF token and one request body containing the current password and a 12–256-byte passphrase; it returns a bounded Argon2id/XChaCha20-Poly1305 envelope. Neither route writes backup material to persistent storage. Phase B has no import/apply or UI surface.

D.1 does not expose raw JSON/Xray/XKeen editing, does not clone panel auth/listener/update state and does not install/repair XKeen/Xray. Component lifecycle remains #4; broad visual typed configuration remains #5.

## Planned later capabilities

### #4 — component lifecycle

Typed/version-aware XKeen/Xray/geodata inventory/update/rollback will share the maintenance coordinator. It will not expose a shell or generic package manager.

### #5 — visual configuration

Supported routing, DNS, XKeen/Xray, performance and panel settings will be edited as typed domains and rendered into complete runtime candidates with preview/validation/apply/rollback. No raw JSON editor.

## Authorities

- Current system architecture: [`ARCHITECTURE.md`](ARCHITECTURE.md)
- Sequencing: [`ROADMAP.md`](ROADMAP.md)
- Active D.1 contract: [Issue #3](https://github.com/popiposter/xkeen-control/issues/3)
- Build/test: [`DEVELOPMENT.md`](DEVELOPMENT.md)
- Production operations: [`OPERATIONS.md`](OPERATIONS.md)
- Security: [`../SECURITY.md`](../SECURITY.md)
