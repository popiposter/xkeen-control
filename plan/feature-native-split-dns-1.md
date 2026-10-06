---
goal: Synchronize installed independent LAN DNS with stock XKeen and panel configuration
version: 1
date_created: 2026-10-06
last_updated: 2026-10-06
owner: xkeen-control
status: 'Completed'
tags: [feature, dns, native]
---

# Introduction

![Status: Completed](https://img.shields.io/badge/status-Completed-green)

Issue #125 integrates the already-installed standard mosdns resolver. XKeen remains unmodified; the panel derives DNS domain data from native configuration and installed geodata. Source baseline is main `8140c9cda51ca9bf703d3a7965f4595f2c69445a`. Earlier live resolver qualification is recorded in Draft PR124; this plan does not replay its installation or outage test.

## 1. Requirements & Constraints

- **REQ-001**: Preserve first-match unconditional native domain DIRECT/VPN/BLOCK decisions, default outbound, geosite attributes and node endpoint bootstrap exceptions. Count conditional traffic rules without claiming DNS equivalence.
- **REQ-002**: Follow native `panel-dns-vpn` DoH resolvers and the existing loopback SOCKS route. Preserve installed DIRECT DoH addresses; never add VPN-to-DIRECT fallback.
- **REQ-003**: Validate derivation before Save/Apply, synchronize after successful native commands/verified Apply and once/minute detect external native CLI/cron changes while panel is running.
- **REQ-004**: Project complete stock selective LAN scope from verified `redirect`/`tproxy` followRedirect tunnel inputs. Ignore inert `ruleTag`; treat `tcp,udp` as unconstrained transport. Unknown scoped domain policies must fail rather than produce empty all-DIRECT DNS.
- **SEC-001**: Fixed paths, authenticated status, CSRF-bound synchronization, secretless DTOs/logs, regular files and bounded parsing. Router credentials remain outside Git/containers.
- **CON-001**: No native script/init/hooks modification, second updater, automatic installation, firmware/firewall mutation or native command replay. Ordinary panel lease does not lock external CLI/cron.
- **CON-002**: Pending/error native history prevents synchronization. Effective unchanged rules do not restart DNS. Preserve previous DNS generation; interrupted restart survives panel restart and is inspect-only.
- **CON-003**: At most 256 native rules/categories per geodata file, 64 MiB per input geodata file, 500000 exported/derived entries and 16 MiB aggregate derived lists. Bounded 40-second sync, 25-second DNS init restart, 5-second readiness check.

## 2. Implementation Steps

### Implementation Phase 1

- GOAL-001: Implement one coherent source cohort before qualification.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Add `geodatareader.Reader.ExportDomains` streaming export and `splitdns.Compile` ordered native projection with bounded lists and source hashes. | Yes | 2026-10-06 |
| TASK-002 | Add `splitdns.Service` generation staging, exact input rechecks, fixed S06mosdns restart, no-op detection, inspect-only interrupted receipt and minute observer. Depends on TASK-001. | Yes | 2026-10-06 |
| TASK-003 | Wire `ConfigEditor.ValidateDerived`, native `Jobs.AfterCommand`, authenticated `/api/v1/dns/split` status/sync and DNS-section shadcn card. Depends on TASK-002. | Yes | 2026-10-06 |
| TASK-004 | Qualify focused export/compiler/service/API/UI fixtures and native callback behavior; update embedded frontend. Depends on TASK-003. | Yes | 2026-10-06 |

### Implementation Phase 2

- GOAL-002: Qualify and deliver one new panel candidate without replaying router operations.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-005 | Commit exact clean candidate; run one `scripts/dev-check.ps1 -Full`, verify actual ARM64 artifact and update one Draft PR/Issue125 evidence. Depends on TASK-004. | Yes | 2026-10-06 |
| TASK-006 | Take bounded current panel/DNS rollback snapshots, deliver unique new panel binary, independently verify native configs/registry/auth/core/init preservation and DNS generation/process/readiness. Depends on TASK-005 PASS. | Yes | 2026-10-06 |
| TASK-007 | Check live DNS card and unchanged synchronization without restart; fresh DIRECT/VPN queries, sanitize results. Do not repeat prior Xray Stop/Start. Record limitations and Draft review state. Depends on TASK-006. | Yes | 2026-10-06 |

## 3. Alternatives

- **ALT-001**: Modify native cron/hooks or install a second geodata downloader: rejected because stock XKeen owns updates.
- **ALT-002**: Continue static exported lists: rejected because routing/geodata updates would leave stale DNS policy.
- **ALT-003**: Send all names through VPN: rejected because it breaks direct DNS availability during Xray failure.

## 4. Dependencies

- **DEP-001**: Existing optional mosdns v5.3.4 installation, exact loopback/LAN listener and Keenetic profile; panel never installs these automatically.
- **DEP-002**: Native JSON configuration, `geodatareader`, ordinary `authority.Lease`, stock native commands, current shadcn components.

## 5. Files

- **FILE-001**: `internal/splitdns/` compiler/service and fixtures; `internal/geodatareader/export.go` and export fixtures.
- **FILE-002**: `internal/xkeen/config.go`, `config_set.go`, `jobs.go`, `cmd/xkeen-control/main.go` shared integration boundaries.
- **FILE-003**: `internal/httpapi/server.go`, `split_dns.go`, `split_dns_test.go`; `web/src/split-dns.jsx`, `main.jsx`, `web/tests/split-dns.spec.js`; generated `internal/webassets/dist/`.
- **FILE-004**: `docs/NATIVE-XKEEN.md`, `docs/CONTROL-PLANE.md`, `docs/OPERATIONS.md` exact DNS ownership/status semantics.

## 6. Testing

- **TEST-001**: Export all four domain kinds, attribute/inverse-attribute selection, missing categories and unsafe files.
- **TEST-002**: Native order/default/catch-all/conditional rules, unsupported targets/resolvers, unsafe list expressions and directory, unchanged semantics no restart, source drift rejection, pending guard, durable interrupted restart inspection without replay.
- **TEST-003**: HTTP session/CSRF/request bounds and DNS UI pending/failed/synchronized presentation.
- **TEST-005**: Verify stock transparent-scoped domain rules with ruleTag and full-transport catch-all; reject unrecognized scopes without resolver mutation.
- **TEST-004**: One clean exact-HEAD full Linux gate and ARM64 artifact; bounded new live delivery plus independent native preservation and DNS no-op readback. Prior outage evidence stays distinct.

## 7. Risks & Assumptions

- **RISK-001**: External CLI/cron can race the panel. Recheck bytes/hashes before activation; no claim of external writer locking or zero race window.
- **RISK-002**: A DNS restart can fail after config replacement. Retain owned previous configuration, show failure and inspect receipt; do not replay automatically or claim native command failure.
- **RISK-003**: DNS questions cannot represent rules conditional on IP/protocol/port/inbound. Show this boundary; such conditions remain native traffic decisions.
- **ASSUMPTION-001**: Fixed installed listener/forwarder tags are the operator-qualified integration. Unknown layouts must report unsupported rather than be redesigned silently.
- **ASSUMPTION-002**: Panel stopped means last successful DNS generation continues; startup resynchronizes external changes. No instant update claim while panel is stopped.

## 8. Related Specifications / Further Reading

- [Issue125](https://github.com/popiposter/xkeen-control/issues/125)
- [Native XKeen contract](../docs/NATIVE-XKEEN.md)
- [Native shell v2](architecture-native-shell-v2.md)
- [Operations](../docs/OPERATIONS.md)
- [mosdns v5.3.4 domain matcher](https://github.com/IrineSistiana/mosdns/blob/v5.3.4/pkg/matcher/domain/matcher.go)

## Completion evidence

Issue125 completed in reviewed PR126. Exact c861 FULL106/ARM and independent development-router DNS/native-preservation/no-op acceptance passed. Stable v0.3.1 subsequently published from exact6e62d620630f5994b00acb2dfb60cbd690b44bcd after reviewed build/test corrections, final FULL and protected run37427370603; fresh public asset verification passed. Signed release installation, reboot/IPv6/all-provider acceptance are not claimed. See docs/RELEASES.md and Issue125/ledger4 public evidence.
