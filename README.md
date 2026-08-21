# xkeen-control

**A lightweight control panel for Keenetic routers running XKeen + Xray.**

Manage VPN nodes and subscriptions, understand what Xray is doing, keep the active proxy stable, run bounded performance checks, and apply changes transactionally — from one small Go binary with an embedded web UI.

> **Status:** the current C.1 generation is production-validated on Keenetic `linux/arm64`. Signed GitHub Releases, one-command installation and panel self-update are the active next slice ([#2](../../issues/2)); do not treat those planned features as shipped yet.

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
- one pre-built Go binary + embedded React UI, with no Go/Node toolchain on the router;
- loopback or one exact trusted-LAN/management-VPN listener, never wildcard/WAN.

## Current authority split

```text
/opt/etc/xkeen-control/secrets/nodes.json   authoritative VPN/subscription secrets
/opt/etc/xray/configs/04_outbounds.json     generated runtime artifact
repository config/xray + config/xkeen       current non-secret policy source
RAM + /tmp                                  high-churn runtime/preview/benchmark state
```

See [Architecture](docs/ARCHITECTURE.md) for the exact routing, DNS, selection and transaction semantics.

## Current capabilities

Node import/replace, named subscription management, enable/disable/remove and explicit subscription refresh are preview-first. Normal API/UI responses never expose UUIDs, REALITY keys, short IDs or subscription URLs.

`xkeen-control` is the only managed writer of the stable `bal-proxy` override. Active liveness, Observatory RTT quality evidence and the bounded daily sustained benchmark remain separate mechanisms coordinated through one runtime mutation boundary.

Node changes build a complete candidate, validate Xray, snapshot the previous logical generation, activate atomically, wait for RoutingService/inventory readiness and roll back on failure.

## Installation

The current source baseline still uses the developer/operator deployment path documented in [Operations](docs/OPERATIONS.md) and [Fresh Keenetic](docs/FRESH-KEENETIC.md).

The active [Slice D / #2](../../issues/2) will replace that distribution path with signed GitHub Releases and a safe one-command bootstrap. The final public install command must not be advertised as available until the release manifest, signature verification, installer fixtures and updater rollback are qualified.

The historical repository `popiposter/xkeen-keenetic` is private quarantine/history only. `popiposter/xkeen-control` is the public source/CI/release authority; old Git history must never be imported here.

## Product roadmap

| Slice | Goal |
| --- | --- |
| **D / #2 — next** | Public signed releases, one-command bootstrap, setup mode, transactional panel self-update/rollback |
| **D.1 / #3** | Local typed appliance state, portable backup/import/export, optional encrypted VPN-secret backup |
| **D.2 / #4** | XKeen/Xray/geodata versions, compatibility-aware update policies, schedules and rollback |
| **D.3 / #5** | Visual typed configuration for routing, DNS, XKeen/Xray, performance and panel settings |
| **E** | Notifications, management-VPN guidance and final attack-surface hardening |

The authoritative sequence is always [ROADMAP.md](docs/ROADMAP.md).

## Security model

This repository and future public releases are **secretless**. Router-specific credentials remain local. Never put production VLESS URLs, UUIDs, REALITY key material, subscription tokens, passwords, SSH credentials or secret-bearing backups into issues, PRs, logs or CI artifacts.

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
| Build/test | [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) |
| Production operations | [docs/OPERATIONS.md](docs/OPERATIONS.md) |
| Fresh router / restore | [docs/FRESH-KEENETIC.md](docs/FRESH-KEENETIC.md) |
| Security | [SECURITY.md](SECURITY.md) |
| Agent workflow | [AGENTS.md](AGENTS.md) |

Detailed implementation architecture lives in the active GitHub issue rather than being duplicated across every document.
