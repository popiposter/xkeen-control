# ADR-001 — Secretless control plane foundation

Status: **Accepted foundation; distribution/config-authority direction evolved after C.1.**

Slice A was production-validated before the fresh-source repository migration. The foundational decisions below remain active. The original idea that router-specific non-secret desired state would later be applied from Git was superseded by Issues #2–#5: public software Releases + local typed appliance state + portable backup + managed component lifecycle + visual configuration.

The filename is retained for history/link stability.

## Context

The router is intentionally specialized around DIRECT as the normal/default path, one unified VLESS/REALITY proxy pool for explicit proxy-required traffic, Xray native `leastPing` fallback, a purpose-built `xkeen-control` process, explicit DNS/routing precedence, low-frequency sustained benchmarking separated from lightweight liveness/RTT and bounded persistent flash writes.

The historical private repository once tracked live VPN material. That made clones credential backups and is why this public repository uses fresh Git history rather than importing the old history.

## Foundational decisions that remain active

### A. Source / GitHub surfaces are secretless

Production VPN/subscription secrets live locally:

```text
/opt/etc/xkeen-control/secrets/nodes.json
```

`nodes.json` is root-only and authoritative. Active `04_outbounds.json` is generated runtime output. VLESS URLs, UUIDs, REALITY key material, short IDs, subscription URLs/tokens, passwords and secret-bearing backups never belong in source, issues, PRs, CI or releases.

`popiposter/xkeen-control` was initialized from the validated secretless snapshot `a1b8c3ce4e7f1914312b23b52c3b96269865e90e` into fresh Git history. The former `popiposter/xkeen-keenetic` repository remains private quarantine/history only and its commits/PR refs must not be imported here.

### B. One managed proxy pool

The managed Xray balancer is `bal-proxy`. Canonical managed node tags use neutral `proxy-*` identity. Historical MAIN/US tags are migration-only compatibility inputs, not routing tiers.

### C. Health, latency quality and throughput are different planes

Current C.1 implementation separates lightweight Xray Observatory RTT/health telemetry, 60-second targeted active-node liveness and once/day bounded sustained throughput benchmark.

Native `leastPing` remains emergency fallback. The control plane owns the normal stable runtime override and legacy competing XKeen Speed Balancer/watchdog writers are disabled. High-churn evidence stays in RAM/`/tmp`; only bounded summary state may persist.

### D. Mutations are candidate-based and transactional

Changes affecting Xray build a complete candidate, validate before activation, snapshot the previous logical generation, swap through a bounded lifecycle, verify RoutingService/inventory, and restore/re-verify on failure.

Node mutations render generated outbounds from the local secret registry and do not silently rewrite unrelated routing/DNS/Observatory policy.

### E. Flash writes are exceptional

Runtime caches, previews, RTT/liveness samples, benchmark working state and downloads belong in RAM or `/tmp`. Persistent state is purpose-specific, explicit and bounded. No growing metrics/revision database belongs on Keenetic.

### F. UI/API is purpose-built

The product exposes typed operations and safe projections, not a generic shell, PTY, command runner, filesystem browser or raw JSON editor. Management access is loopback / exact private LAN / management VPN only, never direct WAN/wildcard.

## Architecture evolution after C.1

The replacement direction is:

```text
public source + signed software Releases
        ↓
one-command install + transactional panel update
        ↓
local schema-versioned appliance state
        ↓
portable safe/encrypted backup
        ↓
managed XKeen/Xray/geodata lifecycle
        ↓
typed visual configuration -> deterministic render/apply
```

### Software authority — Issue #2

This public GitHub repository is software source/CI/release authority. Routers will consume signed immutable releases, not mutable Git configuration. Signed release/bootstrap/update behavior remains planned until #2 is implemented and qualified.

### Router configuration authority — Issues #3 and #5

Supported per-router non-secret settings move into local schema-versioned appliance state. `nodes.json` remains separate secret authority. Runtime Xray/XKeen files become deterministic generated/applied artifacts.

This migration is **planned**, not current C.1 behavior: until D.1/D.3 merge, routing/DNS/Observatory policy remains repository-derived.

### Component lifecycle — Issue #4

XKeen/Xray/geodata updates become typed, capability/version/platform-aware maintenance operations with bounded snapshot/verification/rollback. No generic package manager or shell passthrough.

## Consequences

Benefits include public secretless source/release distribution, future workstation-independent installation/updating, portable local settings and bounded recoverable component/settings mutations. High-churn state continues to avoid flash.

Costs/migration: current production still uses repository-derived policy until local appliance-state migration; historical pre-cleanup copies remain secret material; backup/component/visual-config features require explicit later schema and qualification work.

## Current sequencing

See `ROADMAP.md`. Current active program is #2 → #3 → #4 → #5, followed by notifications/security hardening.
