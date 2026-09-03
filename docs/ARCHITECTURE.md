# Architecture

This document describes the **currently production-qualified architecture**. Slices A/B/C/C.1/D/D.1 are qualified; #4 and #5 are planned product evolution and must not be treated as deployed behavior. Detailed design for active work lives in the active GitHub issue.

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
  ├─ bounded benchmark / lifecycle coordinator
  └─ signed release/bootstrap/panel-update client
```

## Current authority split

```text
GitHub Releases                                      software distribution authority
/opt/etc/xkeen-control/config/appliance.json         local typed non-secret authority after adoption
/opt/etc/xkeen-control/secrets/nodes.json             authoritative VPN/subscription secrets
/opt/etc/xray/configs/02_dns.json                     generated managed policy
/opt/etc/xray/configs/05_routing.json                 generated managed policy
/opt/etc/xray/configs/07_observatory.json             generated managed policy
/opt/etc/xray/configs/04_outbounds.json               generated from nodes.json
config/xray 01/03/06/08 + config/xkeen/xkeen.json      fixed D.1 compatibility templates
RAM + /tmp/xkeen-control                              preview/runtime/update/high-churn state
```

`nodes.json` is schema-versioned and root-only. Generated outbounds are never restored independently over a different registry.

After successful typed D.1 `appliance adopt`, `appliance.json` is the local authority for supported non-secret policy and the managed `02_dns.json`, `05_routing.json` and `07_observatory.json` files are deterministic generated artifacts. Before successful adoption, an existing router retains the explicit repository-derived/legacy policy boundary. Adoption is not implicit: fixed companion and generated-policy compatibility must be proven, and unknown/manual drift fails closed.

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

## Local appliance state and backup/restore

D.1 uses a schema-versioned local `appliance.json` for supported non-secret appliance policy and keeps `nodes.json` as the separate VPN/subscription secret authority. Successful typed adoption is intentionally zero-runtime-mutation authority creation: the service strictly parses the supported policy, proves fixed companion files and generated outbounds are compatible, validates a complete rendered candidate, and writes only `appliance.json` after equivalence is established.

After adoption, supported DNS/routing/Observatory files are generated deterministically from the appliance authority; active outbounds remain generated from `nodes.json`. Node-only mutations do not rewrite unrelated appliance policy.

Safe export contains typed appliance state without node/subscription secrets by default. Explicit secret-bearing export requires re-authentication and a passphrase, uses a bounded authenticated envelope and is not persisted by the panel. Restore is authenticated, same-origin/CSRF protected, session-bound, bounded and preview-first; Apply uses typed validation, an authority lease, a transaction journal and interrupted-import recovery. An equivalent settings-only restore is a no-op that preserves node/generated/runtime state without restarting Xray/XKeen. No raw JSON, filesystem, archive or command surface is exposed.

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

## Signed releases, bootstrap and panel lifecycle

`popiposter/xkeen-control` GitHub Releases are the software distribution authority. The first production-qualified stable release after Slice D completion was historical `v0.1.1`, built from exact source `8f15246099538426ef08163b832c3aa6f73e8265`. Current D.1 production qualification is provided by signed stable `v0.2.0`, built from exact source `f170cdb0a9531cb8f4e08c95c0ba9bc8fe3dfd86`.

The protected release flow:

```text
reviewed exact main SHA
        ↓
full hosted qualification
        ↓
deterministic linux/arm64 assets + manifest
        ↓
source-pinned Ed25519 public-key check
        ↓
protected manifest signing
        ↓
draft release upload
        ↓
re-download + exact asset/hash/signature verification
        ↓
final main-SHA check
        ↓
publish
```

Public first install starts from GitHub HTTPS plus exact release-internal consistency. Once a trusted panel is installed, normal update checks/apply use the compiled source-pinned Ed25519 trust anchor. The router needs no GitHub write credential.

Panel candidates live under `/tmp/xkeen-control/panel-update`. One previous panel generation is retained under `/opt/etc/xkeen-control/previous/panel`. The fixed external updater owns process stop/swap/start/health/version/PID-path verification and rollback; it is not a generic command runner or file API.

The historical pre-#2 C.1 panel is adopted only when fixed release-owned binary/init fingerprints match. Because the old process could be interrupted during a node Apply, the adoption bridge journals the authoritative node registry plus generated outbounds and deterministically handles unchanged, incoherent and coherent-post-write states. Coherent changed state must pass typed Xray runtime reconciliation; a failed reconcile restores the validated journal snapshot and converges runtime before panel replacement.

Production qualification for `v0.1.1` proved legacy → adoption → exact legacy rollback including helper absence → re-adoption while bounded non-secret fingerprints for auth/listener/node/Xray/XKeen/selection/benchmark state remained unchanged.

## Control-plane process and UI

Production installs:

```text
/opt/sbin/xkeen-control
/opt/etc/init.d/S99xkeen-control
/opt/libexec/xkeen-control-updater
```

It is a pre-built static Linux ARM64 Go binary with embedded React/Vite assets. No Go/Node toolchain is installed on Keenetic.

Default listener is `127.0.0.1:8787`; one exact private LAN address may be configured. Wildcard/public/hostname binds fail closed. Direct WAN exposure is out of scope.

Authentication uses a local bcrypt password hash, RAM sessions, same-origin/CSRF mutation protection, login throttling and security headers. There is no generic shell, PTY, arbitrary command runner, filesystem API or raw JSON editor.

## Resource / flash model

RAM or `/tmp` holds sessions/rate limits, status caches, RTT/liveness windows, preview candidates, per-node benchmark work/results, release/update downloads and temporary render/validation trees.

Persistent writes are tied to real explicit state changes and bounded generations: auth/listener settings, typed `appliance.json` adoption/restore changes, `nodes.json`, generated active outbounds, stable-selection changes, one compact benchmark snapshot, one previous panel generation and compact panel release/update policy markers. No growing metrics or update-history database belongs on the router.

## Planned product evolution — not current runtime

Current sequence after completed D.1 is:

```text
#3 local appliance state + portable backup/import/export (done / v0.2.0)
 ↓
#4 managed XKeen/Xray/geodata lifecycle (current/next; planned)
 ↓
#5 typed visual routing/DNS/XKeen/Xray/panel configuration (planned)
```

#4 component lifecycle and #5 visual configuration remain planned rather than deployed. The current authority model above is in force for adopted routers; the explicit repository-derived/legacy compatibility boundary remains in force before successful adoption.

## Architecture invariants

- public source/releases/issues/CI are secretless;
- GitHub Releases are software authority, never router configuration authority;
- local `nodes.json` remains the node/subscription secret authority until an explicit migration;
- after successful typed appliance adoption, local `appliance.json` is the authority for supported non-secret policy; pre-adoption routers retain the explicit repository-derived/legacy compatibility boundary;
- managed `02_dns.json`, `05_routing.json` and `07_observatory.json` are generated from appliance authority, while `04_outbounds.json` is generated from `nodes.json`;
- generated outbounds are never an independent authority;
- management access remains private;
- mutations are typed, bounded and rollback-aware;
- high-churn state avoids flash;
- no generic remote execution/file-editing surface;
- native Xray fallback remains available when the control plane cannot safely hold an override.
