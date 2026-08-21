# Roadmap

This is the sequencing/status authority. The active GitHub issue is the detailed architecture/acceptance contract for the slice being implemented.

## Current production baseline

Slices A/B/C/C.1 are production-qualified in the validated source snapshot migrated into this repository.

Current runtime facts:

- `/opt/etc/xkeen-control/secrets/nodes.json` is the authoritative local VPN/subscription registry;
- active `04_outbounds.json` is generated from that registry;
- enabled nodes use canonical `proxy-*` tags in one `bal-proxy` pool;
- `xkeen-control` is one pre-built Go binary with embedded React UI, auth and typed node/subscription operations;
- node changes use complete candidate validation, bounded activation and rollback;
- `xkeen-control` is the only managed stable `bal-proxy` override writer; native Xray `leastPing` is emergency fallback;
- 60-second active liveness is independent from RTT quality switching;
- the control-plane-owned sustained benchmark runs once/day with bounded whole-run traffic and RAM/`/tmp` high-churn state;
- node Apply, supervisor/selection and benchmark work share one runtime coordinator;
- current routing/DNS/Observatory policy is still repository-derived until D.1/D.3 migrate it into local appliance state.

## Repository authority

`popiposter/xkeen-control` is the public source, CI and release authority. It was initialized on 2026-08-21 from the validated secretless tree of historical `popiposter/xkeen-keenetic` commit `a1b8c3ce4e7f1914312b23b52c3b96269865e90e` using fresh Git history.

The historical repository remains private quarantine/history only. Do not import its commits, branches, PR refs, cached views, releases, Actions artifacts or other historical Git objects into this repository.

Router-specific settings and secrets never enter this repository or release assets.

## Product direction after C.1

Per-router settings are not synchronized from Git.

```text
public source + signed GitHub Releases
        ↓
one-command bootstrap + panel self-update
        ↓
local typed appliance state + portable backup
        ↓
managed XKeen / Xray / geodata lifecycle
        ↓
visual typed configuration + transactional render/apply
```

## Delivery sequence

| Slice | Status | Authority | Outcome |
| --- | --- | --- | --- |
| A — secretless unified-pool foundation | Done | validated historical baseline | Secretless software tree, unified pool, transactional deploy foundation |
| B — control-plane skeleton | Done | validated historical baseline | Go binary, embedded UI, auth, runtime projection, Docker qualification |
| C — node/subscription management | Done | validated historical baseline | Local `nodes.json`, canonical tags, typed preview/apply, rollback, operator UI |
| C.1 — stable selection + sustained benchmark | Done | validated source snapshot `a1b8c3c…` | Sticky stable override, independent liveness, bounded benchmark, shared coordinator |
| D — releases/bootstrap/panel self-update | **Next** | Issue #2 | Public signed Releases, CI/release pipeline, one-command bootstrap, setup mode, transactional panel update/rollback |
| D.1 — appliance state + backup/import/export | Planned | Issue #3 | Local schema-versioned settings, safe export, encrypted secret backup, typed restore |
| D.2 — component lifecycle | Planned | Issue #4 | XKeen/Xray/geodata inventory, compatibility-aware updates, schedules, rollback |
| D.3 — visual configuration | Planned | Issue #5 | Typed routing/DNS/XKeen/Xray/panel/performance UI and deterministic render/apply |
| E — notifications/security hardening | Planned after D.3 | master issue #1 | Outbound alerts, management-VPN guidance, final attack-surface hardening |

## D / Issue #2 — next

#2 owns software distribution and panel lifecycle. Key decisions:

- use this repository's GitHub Releases;
- stable/beta releases come from reviewed exact commits/tags, never mutable `main` downloads;
- release artifacts carry full source SHA, architecture, checksums and a signed compatibility manifest;
- first supported release target is qualified `linux/arm64`; additional architectures require explicit qualification;
- the one-command installer uses bounded prerequisites and never blanket `opkg upgrade`;
- first install generates a one-time setup credential and binds only loopback/exact private LAN;
- panel update downloads to `/tmp`, verifies signature/hash/architecture, retains one previous panel generation, health-checks the new process and rolls back automatically on failure;
- no router settings or VPN credentials are distributed through Releases.

CI/release workflows use standard GitHub-hosted runners unless a later architecture decision requires otherwise.

## D.1 / Issue #3

Move router-specific supported settings into local schema-versioned `appliance.json`, while keeping `nodes.json` as the separate secret authority. Safe export excludes VPN/subscription secrets by default; secret-bearing export is explicit and encrypted. Import is bounded, preview-first and transactional, and hardware-local settings cannot strand a restored router.

## D.2 / Issue #4

Add one typed maintenance plane for XKeen, Xray and geodata. Do not expose upstream CLI as shell passthrough. Updates are capability/version/platform gated, use bounded snapshots and post-update verification, and default to notify/manual until real hardware rollback is qualified.

## D.3 / Issue #5

Make `appliance.json + nodes.json` the supported local configuration authority. UI edits typed domains (routing, DNS, supported XKeen/Xray options, performance, panel settings), renders a complete candidate, validates it, previews semantic impact and applies transactionally. No raw JSON/YAML editor, shell, PTY or arbitrary file manager.

## After D.3

Slice E adds outbound notifications and remote-management/security hardening without creating a remote command plane. The panel remains trusted-LAN/management-VPN only.

## Maintenance rule

After every merge, update this file only when status/sequencing changed, refresh master issue #1 if stale, and remove contradictory planning evidence. Do not duplicate detailed active-issue architecture here.
