# xkeen-control

**A lightweight control panel for Keenetic routers running XKeen + Xray.**

Manage VPN nodes and subscriptions, understand what Xray is doing, keep the active proxy stable, run bounded performance checks, and apply changes transactionally — from one small Go binary with an embedded web UI.

> **Active development:** Issue #121 uses stock XKeen commands and native configuration editors. The development panel is installed and qualified on the operator router; signed `v0.2.0` below describes the historical generation. See [Native contract](docs/NATIVE-XKEEN.md) and [ROADMAP](docs/ROADMAP.md).

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

## Native ownership and capabilities

XKeen owns its installation, service, components and native update schedules. The panel invokes allowlisted native commands, showing their console output and prompts when needed; it does not patch XKeen code or replace its cron engine.

Native JSON/JSONC editors provide text and graphical modes, drafts, full Xray validation, a shared pending configuration indicator, explicit restart, discard and optional previous configuration restore. Routing includes installed geodata categories, membership search and first-match examples. DNS configuration is editable; independent LAN DNS/outage behavior still requires qualification.

`/opt/etc/xkeen-control/secrets/nodes.json` stores VPN nodes and subscriptions privately. Only managed outbounds are generated from it; unrelated native fields and outbounds are preserved. Enabled subscription refresh uses the existing panel scheduler. WL nodes default to disabled. Native leastPing remains the current selection owner; panel adaptive overrides are disabled.

Portable encrypted configuration transfer supports validation, explicit interface mapping and staging without automatic restart. Telegram notifications and restricted single-user native commands are optional and disabled until configured. Actual bot credentials and second-router transfer acceptance are separate from local fixture evidence.

The panel keeps authentication, origin/CSRF protection, bounded private data handling and its signed self-updater. No Go/Node toolchain runs on the router.

## Installation

For an Entware/Open Package-ready `linux/arm64` Keenetic, the currently production-qualified release-specific installer is:

```sh
sh -c "$(curl -fsSL https://github.com/popiposter/xkeen-control/releases/download/v0.2.0/install.sh)"
```

The installer is bounded: it never performs blanket `opkg upgrade`, never installs/repairs XKeen or Xray, and preserves existing auth/listener/node/Xray/XKeen/routing/DNS/Observatory state. Missing XKeen/Xray/configuration is reported as Setup Mode rather than triggering an opaque upstream installer.

Existing managed installs use the installed binary's pinned-signature self-update path. The qualified legacy C.1 install has a narrow fingerprint-gated adoption path; historical `v0.1.1` was production-qualified through legacy → adoption → exact rollback → re-adoption. Current signed `v0.2.0` adds the qualified D.1 typed appliance adoption and backup/restore boundary.

See [Releases](docs/RELEASES.md), [Operations](docs/OPERATIONS.md) and [Fresh Keenetic](docs/FRESH-KEENETIC.md).

The historical repository `popiposter/xkeen-keenetic` is private quarantine/history only. `popiposter/xkeen-control` is the public source/release authority; ordinary qualification runs locally and the protected manual GitHub workflow is reserved for releases. Old Git history must never be imported here.

## Product roadmap

| Slice | Goal |
| --- | --- |
| **D / #2 — done** | Public signed releases, one-command bootstrap, setup mode, transactional panel self-update/rollback |
| **D.1 / #3 — done** | Production-qualified local typed appliance state, portable backup/import/export, optional encrypted VPN-secret backup |
| **D.2 / #4 — source-only** | Component lifecycle, bounded policy and typed Setup source delivered; not deployed |
| **D.3 / #5 — done source-only** | Routing, DNS/Observatory, bounded Performance, System/Panel and final Dashboard integration are complete in source; not deployed |
| **E / #99 — active source work** | Outbound notifications, management-VPN guidance and final private-management attack-surface hardening |

The authoritative sequence is always [ROADMAP.md](docs/ROADMAP.md).

## Security model

This repository and public releases are **secretless**. Router-specific credentials remain local. Never put production VLESS URLs, UUIDs, REALITY key material, subscription tokens, passwords, SSH credentials or secret-bearing backups into issues, PRs, qualification logs or release artifacts.

The panel is for trusted management access only; direct WAN exposure and generic shell/file-manager APIs are out of scope.

Read [SECURITY.md](SECURITY.md) before production or release work.

## Development

The fast proportional local check is:

```powershell
pwsh -NoProfile -File scripts/dev-check.ps1
```

The final exact-HEAD local gate for code/build changes is:

```powershell
pwsh -NoProfile -File scripts/dev-check.ps1 -Full
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

## Private management

Use an operator-managed VPN to the router or an SSH tunnel to the loopback
listener for remote administration. A management VPN/private interface must use
one exact server-listed private IPv4 or ULA IPv6 address. Connect with that numeric
address and the listener port; bracket IPv6. `localhost` with the same port is
accepted only for a loopback listener (for example, an SSH tunnel on port 8787).
An omitted HTTP port is valid only when the listener actually uses port 80.

Never bind directly to WAN or open a WAN firewall rule. Hostname/wildcard binds
are unavailable. VPN, firewall and DDNS configuration remain operator-managed;
the panel offers read-only guidance and no automation for these facilities.
This source hardening does not change the production-qualified `v0.2.0` baseline.
