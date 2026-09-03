# xkeen-control

**A lightweight control panel for Keenetic routers running XKeen + Xray.**

Manage VPN nodes and subscriptions, understand what Xray is doing, keep the active proxy stable, run bounded performance checks, and apply changes transactionally — from one small Go binary with an embedded web UI.

> **Status:** D.1 / Issue #3 is production-qualified on Keenetic `linux/arm64` in signed stable release `v0.2.0`, built from exact source `f170cdb0a9531cb8f4e08c95c0ba9bc8fe3dfd86`. Historical `v0.1.1` remains the first Slice D release; #4 is the next/current product slice, while its component lifecycle and all #5 visual configuration remain planned rather than deployed.

## Why this project

XKeen and Xray are powerful, but operating a real router usually means editing files over SSH, remembering command flags, interpreting raw runtime state and being careful not to break routing during updates.

`xkeen-control` adds a purpose-built management layer without turning Keenetic into a general-purpose server:

- one unified proxy pool with Xray `leastPing` as emergency fallback;
- stable node selection that avoids flapping on small RTT changes;
- fast active liveness failover independent from slower quality decisions;
- typed VPN node and subscription management with preview/apply/rollback;
- bounded sustained throughput benchmarking with high-churn state in RAM/`/tmp`;
- clear runtime visibility for native, override and effective selection;
- transactional Xray activation with validation, readiness checks and rollback;
- signed public releases with bounded bootstrap and panel update/rollback;
- one pre-built Go binary + embedded React UI, with no Go/Node toolchain on the router;
- loopback or one exact trusted-LAN/management-VPN listener, never wildcard/WAN.

## Current authority split

```text
/opt/etc/xkeen-control/config/appliance.json local typed non-secret authority after successful adoption
/opt/etc/xkeen-control/secrets/nodes.json     authoritative VPN/subscription secrets
/opt/etc/xray/configs/02_dns.json             generated managed policy
/opt/etc/xray/configs/05_routing.json         generated managed policy
/opt/etc/xray/configs/07_observatory.json     generated managed policy
/opt/etc/xray/configs/04_outbounds.json       generated from nodes.json
config/xray 01/03/06/08 + config/xkeen        fixed D.1 compatibility templates
RAM + /tmp                                    high-churn runtime/preview/benchmark/update state
```

Pre-adoption compatibility boundary: routers without a successful typed `appliance adopt` retain their existing repository-derived/legacy policy. Adoption is not implicit; unknown or manually drifted layouts fail closed.

See [Architecture](docs/ARCHITECTURE.md) for the exact routing, DNS, selection and transaction semantics.

## Current capabilities

Node import/replace, named subscription management, enable/disable/remove and explicit subscription refresh are preview-first. Normal API/UI responses never expose UUIDs, REALITY keys, short IDs or subscription URLs.

`xkeen-control` is the only managed writer of the stable `bal-proxy` override. Active liveness, Observatory RTT quality evidence and the bounded daily sustained benchmark remain separate mechanisms coordinated through one runtime mutation boundary.

Node changes build a complete candidate, validate Xray, snapshot the previous logical generation, activate atomically, wait for RoutingService/inventory readiness and roll back on failure.

Signed releases are built from an exact reviewed `main` SHA, use a source-pinned Ed25519 trust anchor and are re-downloaded/reverified before publication. Panel update candidates stay under `/tmp`; one previous panel generation supports bounded rollback.

D.1 adds typed local appliance authority, safe export, explicit re-authenticated encrypted secret export and bounded authenticated preview-first restore. Safe export excludes node/subscription secrets; encrypted export remains protected operator material.

## Installation

For an Entware/Open Package-ready `linux/arm64` Keenetic, the currently production-qualified release-specific installer is:

```sh
sh -c "$(curl -fsSL https://github.com/popiposter/xkeen-control/releases/download/v0.2.0/install.sh)"
```

The installer is bounded: it never performs blanket `opkg upgrade`, never installs/repairs XKeen or Xray, and preserves existing auth/listener/node/Xray/XKeen/routing/DNS/Observatory state. Missing XKeen/Xray/configuration is reported as Setup Mode rather than triggering an opaque upstream installer.

Existing managed installs use the installed binary's pinned-signature self-update path. The qualified legacy C.1 install has a narrow fingerprint-gated adoption path; historical `v0.1.1` was production-qualified through legacy → adoption → exact rollback → re-adoption. Current signed `v0.2.0` adds the qualified D.1 typed appliance adoption and backup/restore boundary.

See [Releases](docs/RELEASES.md), [Operations](docs/OPERATIONS.md) and [Fresh Keenetic](docs/FRESH-KEENETIC.md).

The historical repository `popiposter/xkeen-keenetic` is private quarantine/history only. `popiposter/xkeen-control` is the public source/CI/release authority; old Git history must never be imported here.

## Product roadmap

| Slice | Goal |
| --- | --- |
| **D / #2 — done** | Public signed releases, one-command bootstrap, setup mode, transactional panel self-update/rollback |
| **D.1 / #3 — done** | Production-qualified local typed appliance state, portable backup/import/export, optional encrypted VPN-secret backup |
| **D.2 / #4 — current/next** | Planned XKeen/Xray/geodata versions, compatibility-aware update policies, schedules and rollback; not deployed |
| **D.3 / #5 — planned** | Planned visual typed configuration for routing, DNS, XKeen/Xray, performance and panel settings; not deployed |
| **E** | Notifications, management-VPN guidance and final attack-surface hardening |

The authoritative sequence is always [ROADMAP.md](docs/ROADMAP.md).

## Security model

This repository and public releases are **secretless**. Router-specific credentials remain local. Never put production VLESS URLs, UUIDs, REALITY key material, subscription tokens, passwords, SSH credentials or secret-bearing backups into issues, PRs, logs or CI artifacts.

The panel is for trusted management access only; direct WAN exposure and generic shell/file-manager APIs are out of scope.

Read [SECURITY.md](SECURITY.md) before production or release work.

## Development

The supported full qualification command is:

```powershell
pwsh -NoProfile -File scripts/dev-check.ps1
```

It covers the current Go tests/vet/race checks, frontend install/check/build/audit, Linux `arm64` build and host diff hygiene. See [Development](docs/DEVELOPMENT.md).

Agents should start at [AGENTS.md](AGENTS.md).

## Documentation

| Need | Read |
| --- | --- |
| Architecture / invariants | [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) |
| Roadmap / sequencing | [docs/ROADMAP.md](docs/ROADMAP.md) |
| Control-plane runtime/API | [docs/CONTROL-PLANE.md](docs/CONTROL-PLANE.md) |
| Releases/bootstrap/update | [docs/RELEASES.md](docs/RELEASES.md) |
| Build/test | [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) |
| Production operations | [docs/OPERATIONS.md](docs/OPERATIONS.md) |
| Fresh router / restore | [docs/FRESH-KEENETIC.md](docs/FRESH-KEENETIC.md) |
| Security | [SECURITY.md](SECURITY.md) |
| Agent workflow | [AGENTS.md](AGENTS.md) |

Detailed implementation architecture lives in the active GitHub issue rather than being duplicated across every document.
