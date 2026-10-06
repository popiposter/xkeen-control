> Historical operator evidence, not a current automated-install procedure. Static domain exports are superseded by [Issue125](https://github.com/popiposter/xkeen-control/issues/125) and stable0.3.1 DNS synchronization. No old operation is authorized for replay.

---
goal: Independent LAN split DNS with native Keenetic and unmodified XKeen
version: 1
date_created: 2026-10-05
last_updated: 2026-10-05
owner: operator
status: Completed
tags: [infrastructure, dns, native]
---

# Introduction

![Status: Completed](https://img.shields.io/badge/status-Completed-brightgreen)

Issue #121 operator qualification: keep Keenetic DNS on port 53 and use its custom DNS profile to forward external queries to standard mosdns v5.3.4. DIRECT queries use independent public DoH; VPN queries use DoH through a loopback-only Xray SOCKS inbound and the existing native balancer. No native XKeen code changes.

## 1. Requirements & Constraints

- **REQ-001**: DIRECT DNS remains usable when Xray stops. VPN DNS never falls back to DIRECT; unavailable tunnel produces failure or existing cached answers only.
- **REQ-002**: Preserve local Keenetic name handling, node registry, six-node balancer, authentication, panel binary and native updater/cron.
- **SEC-001**: Bind mosdns only to explicit loopback/trusted LAN addresses; Xray SOCKS only to loopback. No WAN listeners or firewall changes.
- **CON-001**: No dns-override, second Xray process, native source patches, watchdog or parallel component updater.
- **CON-002**: Persist only bounded configuration, executable and a rollback snapshot; logs/cache remain in RAM. Retain operator credentials and raw receipts outside Git.
- **CON-003**: DNS rules are a pinned export of installed geosite categories and exact active routing selectors. Preserve full/domain/regexp/keyword semantics and precedence. Geodata changes require an explicit refreshed export; no implicit automatic synchronization claim.

## 2. Implementation Steps

### Implementation Phase 1

- GOAL-001: Establish architecture and prerequisite identity without changing live routing.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Verify model/firmware documentation and prove a temporary host-specific Keenetic profile with one unavailable upstream has no DIRECT fallback; remove exact temporary settings. | Yes | 2026-10-05 |
| TASK-002 | Download official mosdns v5.3.4 ARM64 asset; verify published GitHub asset SHA256 and record extracted executable hash. | Yes | 2026-10-05 |
| TASK-003 | Export installed geosite snapshots with before/after SHA256; create lossless VPN domain-set from native 05_routing.json and a manifest outside Git. | Yes | 2026-10-05 |

### Implementation Phase 2

- GOAL-002: Qualify a standard independent service before LAN DNS switches. Depends on Phase 1.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-004 | Stage mosdns executable/config/domain-set; start on alternate TCP/UDP port 15354. DIRECT forward uses TLS-verified Cloudflare/Google DoH by IP; VPN forward uses only loopback SOCKS port 5310. Measure RSS and cold-query behavior with unavailable SOCKS. | Yes | 2026-10-05 |
| TASK-005 | Through typed native config SaveSet/Apply, add loopback SOCKS inbound tag panel-dns-tunnel in 03_inbounds.json and its balancer rule before catchall in 05_routing.json. Validate full installed Xray config and independently read back health, PID and preserved files. | Yes | 2026-10-05 |
| TASK-006 | Prove VPN/direct A, AAAA and HTTPS queries, ad-rule precedence and unavailable-tunnel behavior on the alternate listener. Verify local names on the retained Keenetic listener in Phase 3. | Yes | 2026-10-05 |

### Implementation Phase 3

- GOAL-003: Switch LAN through native profile and prove outage behavior. Depends on Phase 2.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-007 | Create one custom Keenetic profile with only mosdns LAN:15354 upstream, assign the current private LAN segment after checking host overrides, and enable public DNS engine. Preserve local router DNS; no upstream loop through port 53. Save native settings only after acceptance. | Yes | 2026-10-05 |
| TASK-008 | Perform one bounded native Xray Stop/Start with durable intent receipts; prove fresh DIRECT queries work and VPN queries fail without DIRECT fallback during stop. Independently restore health and six-node configuration. | Yes | 2026-10-05 |
| TASK-009 | Install conventional mosdns Entware init script with startup order after Entware availability, RAM log and explicit config path; verify script/config, process executable and listener state without router reboot. Publish sanitized results and rollback instructions. | Yes | 2026-10-05 |

## 3. Alternatives

- **ALT-001**: Xray replacing Keenetic port 53 via dns-override fails ordinary DNS when Xray is stopped; rejected.
- **ALT-002**: Domain-specific Keenetic name-server entries returned fresh answers despite unavailable selected upstream; strict behavior not qualified.
- **ALT-003**: dnsmasq cannot preserve all full/regexp/keyword geosite semantics; rejected for this installed policy.
- **ALT-004**: A second Xray resolver conflicts with native singleton process discovery; rejected.

## 4. Dependencies

- **DEP-001**: Stock installed Xray/Keenetic, supported custom DNS profile commands, existing balancer tag from active routing.
- **DEP-002**: Official mosdns v5.3.4 ARM64 binary; no router build toolchain.
- **DEP-003**: Installed geosite files and exact current routing selectors; private operator access and typed panel config APIs.

## 5. Files

- **FILE-001**: Native 03_inbounds.json and 05_routing.json only for loopback SOCKS routing; preserve all other native configs.
- **FILE-002**: /opt/etc/mosdns/config.json, vpn.txt and source-manifest.json; root-owned bounded data.
- **FILE-003**: /opt/sbin/mosdns and /opt/etc/init.d/S06mosdns; independent standard DNS service.
- **FILE-004**: This plan and sanitized Issue #121 evidence. Operator execution scripts/credentials/receipts remain outside Git.

## 6. Testing

- **TEST-001**: Temporary exclusive-profile unavailable-upstream test and exact rollback already passed.
- **TEST-002**: Binary digest, lossless export types/counts, pinned input/output hashes and real native startup/RSS checks.
- **TEST-003**: Full installed Xray candidate validation, one typed Apply, independent file/node/auth/service readback.
- **TEST-004**: Fresh UDP/TCP queries through alternate and final listeners, A/AAAA/HTTPS, local names and representative DIRECT/VPN domains.
- **TEST-005**: Native Xray stop: DIRECT cold queries succeed, VPN cold queries fail; start restores VPN health. Do not call cache hits outage proof.
- **TEST-006**: Docs-only repository checks; no unrelated browser or full application suite for operator data changes.

## 7. Risks & Assumptions

- **RISK-001**: The independent forwarder remains a DNS service dependency; this removes Xray dependency for DIRECT, not every possible DNS-service failure.
- **RISK-002**: Static geosite export can drift after native geodata updates; compare manifest and refresh explicitly. Never claim automatic synchronization.
- **RISK-003**: Client private DoH bypasses router DNS. Host-specific Keenetic profiles override segment profiles; inspect and disclose scope.
- **RISK-004**: Direct DoH must remain genuinely DIRECT; verify native OUTPUT rules do not intercept the independent service endpoints.
- **ASSUMPTION-001**: No IPv6 default route is currently active on the operator client; do not claim IPv6 LAN qualification.

## 8. Related Specifications / Further Reading

- [Native contract](../docs/NATIVE-XKEEN.md), [v2 plan](architecture-native-shell-v2.md), [Operations](../docs/OPERATIONS.md).
- [Keenetic model CLI reference](https://storage.googleapis.com/docs.help.keenetic.com/cli/5.0/en/cli_manual_kn-1811.pdf).
- [mosdns v5.3.4](https://github.com/IrineSistiana/mosdns/releases/tag/v5.3.4), [domain matchers](https://github.com/IrineSistiana/mosdns/blob/v5.3.4/pkg/matcher/domain/matcher.go), [forward plugin](https://github.com/IrineSistiana/mosdns/blob/v5.3.4/plugin/executable/forward/forward.go).

Operator acceptance and limits: [qualification result](../docs/qualification/independent-split-dns-2026-10-05.md).
