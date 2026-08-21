# Architecture

This document describes the **currently production-qualified architecture** represented by the migrated C.1 source baseline. Planned product evolution is called out separately and must not be treated as deployed behavior. Detailed design for active work lives in the active GitHub issue.

## System goal

`xkeen-control` is a lightweight management layer for Keenetic + XKeen + Xray. The data plane remains Xray/XKeen; the panel adds safe local operations, visibility, stable proxy selection and bounded lifecycle coordination without turning the router into a general-purpose server.

Current routing policy is selective: ordinary traffic remains DIRECT while explicitly blocked/geo-sensitive services use one unified proxy pool.

```text
trusted LAN clients
      ↓
Keenetic / XKeen Hybrid interception
      ↓
Xray routing + DNS
      ↓
DIRECT or bal-proxy

xkeen-control
  ├─ local node registry + renderer
  ├─ typed preview/apply transactions
  ├─ runtime/Observatory projection
  ├─ stable-selection supervisor
  └─ bounded benchmark / lifecycle coordinator
```

## Current authority split

```text
/opt/etc/xkeen-control/secrets/nodes.json   authoritative VPN/subscription secrets
/opt/etc/xray/configs/04_outbounds.json     generated runtime artifact
config/xray/*.json                          current non-secret Xray policy source
config/xkeen/xkeen.json                     current non-secret XKeen policy source
RAM + /tmp/xkeen-control                    preview/runtime/high-churn state
```

`nodes.json` is schema-versioned and root-only. Generated outbounds are never restored independently over a different registry.

The planned D.1/D.3 local `appliance.json` model does **not** exist yet; until that migration is implemented, routing/DNS/Observatory policy remains repository-derived.

## Data plane

LAN clients are intercepted by XKeen in Hybrid mode:

- TCP → Xray `redirect` inbound on port `61219`;
- UDP → Xray `tproxy` inbound on port `61219`;
- local administrative SOCKS → `127.0.0.1:1080`;
- sniffing for HTTP/TLS/QUIC with `routeOnly=true`.

IPv6 is currently disabled to avoid bypass around the IPv4 transparent-routing path.

## Unified proxy pool

There is one managed Xray balancer:

```text
bal-proxy
```

Canonical managed node tags use the neutral `proxy-` namespace. Historical `proxy-main-*` / `proxy-us-*` values are migration-only inputs; the renderer emits stable neutral tags.

Native Xray `leastPing` remains the emergency selector when no valid runtime override exists.

## Local node/subscription management

Node import/replace, enable/disable/remove and named subscription refresh are preview-first.

A mutation follows one logical transaction:

```text
RAM preview
   ↓
/tmp registry + rendered outbounds candidate
   ↓
complete Xray candidate validation
   ↓
bounded previous-generation snapshot
   ↓
atomic registry/outbounds activation
   ↓
foreground XKeen/Xray restart
   ↓
RoutingService readiness + expected bal-proxy inventory
   ↓
success
   or
explicit restore + restart + readiness/inventory verification
```

Node operations do not rewrite routing, DNS or Observatory policy. Unknown transports/fields fail closed. Subscription networking uses a public-routable address policy to reject special-use/private destinations and unsafe redirects.

## Routing and DNS

Xray is first-match. Explicit proxy exceptions must beat broad DIRECT classification. Current policy keeps private networks/domains and ordinary traffic DIRECT, while explicit blocked/geo-sensitive domains/IPs and selected realtime traffic use `bal-proxy`.

Explicit proxy-policy domains use redundant DoH through `bal-proxy`. Unmatched domains fall through to Keenetic/system DNS. `disableFallbackIfMatch=true` prevents explicitly proxied domains from silently leaking to local/direct DNS when both policy resolvers fail; `serveStale=true` provides bounded cache resilience.

## Stable selection and liveness

`xkeen-control` is the only managed stable `bal-proxy` override writer. Legacy XKeen Speed Balancer/watchdog writers are disabled.

Three mechanisms stay separate:

```text
Observatory RTT quality       slow quality evidence
active 1 KiB probe / 60 s     fast effective-node liveness
sustained benchmark           once/day throughput evidence
```

The supervisor uses bounded unique Observatory samples, materiality thresholds, persistence and dwell to avoid healthy-node flapping. Two consecutive active-probe failures clear a stale override so native `leastPing` can provide fallback before a replacement is actively validated.

## Sustained benchmark

Current control-plane-owned policy:

```text
schedule                 04:17 router-local, once/day, no catch-up
eligible nodes           all enabled canonical nodes, max 256
payload/node             min(20 MiB, floor(2 GiB / N))
minimum planned payload  4 MiB
per-node timeout         10 s
execution                sequential, fresh transport per node
```

Targeted samples use the dedicated loopback `probe` inbound and typed Xray `RoutingService.AddRule(..., shouldAppend=true)`. Temporary rules are cleaned on every path. Payload is streamed to discard; high-churn state remains in RAM/`/tmp`; only one compact completed-run snapshot is persisted.

Benchmark work never owns the fast health plane. Explicit lifecycle mutations preempt/cancel benchmark work through the shared coordinator.

## Control-plane process and UI

Production currently installs:

```text
/opt/sbin/xkeen-control
/opt/etc/init.d/S99xkeen-control
```

It is a pre-built static Linux ARM64 Go binary with embedded React/Vite assets. No Go/Node toolchain is installed on Keenetic.

Default listener is `127.0.0.1:8787`; one exact private LAN address may be configured. Wildcard/public/hostname binds fail closed. Direct WAN exposure is out of scope.

Authentication uses a local bcrypt password hash, RAM sessions, same-origin/CSRF mutation protection, login throttling and security headers. There is no generic shell, PTY, arbitrary command runner, filesystem API or raw JSON editor.

## Resource / flash model

RAM or `/tmp` holds sessions/rate limits, status caches, RTT/liveness windows, preview candidates, per-node benchmark work/results and temporary render/validation trees.

Persistent writes are tied to real explicit state changes and bounded generations: auth/listener settings, `nodes.json`, generated active outbounds, stable-selection changes, one compact benchmark snapshot and bounded rollback material. No growing metrics database belongs on the router.

## Planned product evolution — not current runtime

Current sequence is:

```text
#2 public signed releases + bootstrap + panel self-update
 ↓
#3 local appliance state + portable backup/import/export
 ↓
#4 managed XKeen/Xray/geodata lifecycle
 ↓
#5 typed visual routing/DNS/XKeen/Xray/panel configuration
```

After #3/#5, per-router supported non-secret configuration will move from repository-derived policy into local typed appliance state and runtime files will become deterministic generated artifacts. Until those slices merge and are qualified, the current authority model above remains in force.

## Architecture invariants

- public source/releases/issues/CI are secretless;
- local `nodes.json` remains the node/subscription secret authority until an explicit migration;
- generated outbounds are never an independent authority;
- management access remains private;
- mutations are typed, bounded and rollback-aware;
- high-churn state avoids flash;
- no generic remote execution/file-editing surface;
- native Xray fallback remains available when the control plane cannot safely hold an override.
