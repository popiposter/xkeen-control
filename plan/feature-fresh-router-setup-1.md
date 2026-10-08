---
goal: Reliable installation on a fresh Keenetic and a portable reference traffic profile
version: 1
date_created: 2026-10-08
last_updated: 2026-10-08
owner: xkeen-control
status: Planned
tags: [installation, native, routing, dns]
---

# Introduction

![Status: Planned](https://img.shields.io/badge/status-Planned-blue)

[Issue143](https://github.com/popiposter/xkeen-control/issues/143) defines a single guided installation flow whose final normal operation is adding subscriptions and applying configuration. This document specifies the future setup slices; it does not claim an implemented wizard or fresh-router acceptance. [Issue142](https://github.com/popiposter/xkeen-control/issues/142) separately corrects the observed Entware tool lookup failure. Native XKeen remains the installation/component/service owner.

## 1. Requirements & Constraints

- **REQ-001**: Supported target is Linux ARM64 Keenetic/Netcraze, working Entware `/opt`, private administrator access, enough bounded installation storage and usable direct Internet/DNS before any proxy setup.
- **REQ-002**: Primary order is router/Entware prerequisites → official stock XKeen Stable with Xray → firmware client policy → signed panel alongside → independent DNS infrastructure/profile → reference profile Preview → subscriptions → Save/Apply → client verification/export. A DNS profile must not point to an unavailable resolver. Do not activate selected VPN clients before the configuration and DNS path are ready.
- **REQ-003**: Initial firmware setup is explicit once. Normal users then add subscriptions, validate and Apply; successful download/exit0 alone never means ready to use. Empty enabled pool is displayed as not-ready; protected traffic uses BLOCK fallback, ordinary DIRECT remains usable.
- **REQ-004**: Existing routers have a separate inspect/attach route. Never repeat fresh installation, replace existing policies/DNS profiles, overwrite unmanaged config or apply a reference profile while merely viewing the panel.
- **REQ-005**: GNU tar must both be installed and actually selected. Prefer `/opt/bin:/opt/sbin`; check `command -v tar` and GNU version under the same environment that launches native commands. The 2.1 archive failed BusyBox selected-directory extraction on the operator router.
- **REQ-006**: The policy's **description**, case-insensitively `xkeen`, is what stock 2.1 matches in `rci/show/ip/policy`; its internal id can be `PolicyN`. Verify uniqueness, mark, usable WAN and explicit intended client/segment assignments. Policy existence is not client-path acceptance. No global policy change by default. `xkeen_full`/DSCP61 is an optional separate scope.
- **REQ-007**: Fixed read-only firmware preflight distinguishes present/missing/ambiguous/unknown/unavailable/unauthorized, including malformed and partial responses. Missing token on firmware5.2 is not a missing policy. Do not print tokens/RCI bodies into public logs.
- **REQ-008**: Standard mosdns must preserve firmware LAN53/local names, provide independent DIRECT resolution and VPN DoH through the existing loopback SOCKS/native pool, with no DIRECT fallback for VPN names. Include a loopback readiness listener (`127.0.0.1:15354`, current service contract). For firmware forwarding use only an exact trusted LAN address or a separately proven loopback path; never wildcard/WAN. Firmware-profile host overrides are inspected. Exclude native interception of53 using supported commands after inspecting current state; the add action already restarts native service and must not be followed by a redundant restart.
- **REQ-009**: Public `config/presets/ru-selective-v1.json` preserves the current 12 traffic rules, with explicit force-proxy before BitTorrent DIRECT and selective VPN before catch-all DIRECT. Managed service-integration rules stay earlier. Full graph validation checks outbounds/balancer/inbounds/API/DNS/ext categories; no device-specific weights/selectors/interfaces/marks enter the profile.
- **SEC-001**: No native source patch, arbitrary shell/RCI/URL/file API, second component updater, global PATH rewrite, public listener, router toolchain, blanket package upgrade, reboot, automatic credential rotation or secret-bearing public artifact. Auth sessions never enter backups.
- **SEC-002**: The panel's pinned Ed25519 release verification and upstream XKeen asset verification are separate trust boundaries. The fresh native launcher must use the official owner and verify actual installed identity, not manufacture signed-panel trust for XKeen.
- **CON-001**: Current stable0.3.1 panel installer requires stock XKeen/Xray already present. Optional mosdns synchronization does not install mosdns or configure firmware. Until the future slices are delivered, use the executable manual sequence in [FRESH](../docs/FRESH-KEENETIC.md).
- **CON-002**: Official tagged2.1 `install.sh --stable` chooses native Stable but ends with `exec /opt/sbin/xkeen -i`; it does **not** forward `auto` or `cores=xray`. Documented `xkeen -i auto cores=xray` assumes dispatcher availability. Default native auto installs both engines/latest versions (including prereleases) and all geodata. Never claim `install.sh --stable cores=xray` or `xkeen -i auto` on empty Entware works.

## 2. Implementation Steps

### Implementation Phase 1

- GOAL-001: Deliver the environment correction and document a supported manual installation. Depends on Issue142 qualification; no fresh firmware mutation.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | In `internal/xkeen/environment.go` add `withEntwarePath`; use it in `Jobs.start`/native command construction in `jobs.go` and `Lifecycle.runForeground`; export Entware-first PATH in `packaging/S99xkeen-control`. Preserve existing executable/argv/foreground/PTY and unrelated environment. | Implemented; qualification pending | 2026-10-08 |
| TASK-002 | In `docs/FRESH-KEENETIC.md` document standard dependencies, actual GNU selection, tagged upstream launcher, firmware5.2 private token, official interactive Xray selection and signed panel installation. In `config/presets/README.md` specify profile provenance, exact dependencies and merge/activation contract. | Prepared; review pending | 2026-10-08 |

### Implementation Phase 2

- GOAL-002: Add bounded read-only installation prerequisites. Depends on Phase1; no firmware writer or native installer endpoint.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-003 | Add `internal/keenetic/preflight.go` and tests: fixed loopback RCI GET paths `show/version`, `show/ip/policy`, `show/ip/hotspot`; bounded responses/timeouts; bounded private token read from fixed native xkeen.json; typed outcomes. Parse policy description/id/mark and destination WAN/client assignment capabilities. Unsupported firmware schema returns unknown. No mutation call or caller-supplied path/body. | No | — |
| TASK-004 | Add a sanitized installation-status projection in `internal/httpapi` using existing authentication/Host/CSRF rules and a guided status component in `web/src/native-xkeen.jsx`. Show actual selected tools, native/core/geodata availability, intended policy/client selection and DNS readiness. Read-only views do not self-heal or stage config. Link the firmware GUI for creation; provide model/firmware CLI guidance only when its capability/schema is verified. | No | — |

### Implementation Phase 3

- GOAL-003: Implement explicit reference-profile preview through existing native config transactions. Depends on Phase2; separate issue and review before code.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-005 | Add a fixed profile id `ru-selective-v1` in `internal/xkeen/reference_profile.go`, consuming the public JSON. Prepare data edits only through `ConfigWorkspace`: preserve unrelated fields/API/DNS rules, use `BuildAttachment` prerequisites when appropriate, initialize `bal-proxy` using destination enabled managed nodes/BLOCK fallback/leastPing, preserve existing deliberate balancer overrides unless explicitly replaced. Reject missing/duplicate/conflicting tags and unsupported ext references. No generic uploaded template executor. | No | — |
| TASK-006 | Expose authenticated baseline/session-bound Preview then Save using existing pending-set/digest/lease workflow in `internal/httpapi` and Configurations UI. Use destination node registry as authority; never copy current-router selectors/costs. Before subscriptions show not-ready. Apply is one existing foreground native job; independently inspect configuration/process/API/probe/DNS. Unknown outcome uses inspect, not automatic repeat. | No | — |

### Implementation Phase 4

- GOAL-004: Provision optional standard DNS and firmware handoff. Depends on Phase3; separate infrastructure issue with exact release/artifact/schema qualification.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-007 | Define a bounded opt-in terminal setup for official mosdns ARM64, pinned release/digest, `/opt/sbin/mosdns`, `S06mosdns` and `/opt/etc/mosdns`. Reuse `internal/splitdns` compilation/sync for owned generations. Inspect existing installations and never replace an unrelated resolver. Keep logs/cache in RAM; exact file backup/permissions/readiness before firmware handoff. Standard package availability/digest must be verified for the chosen release; historical5.3.4 is evidence, not mutable latest authority. | No | — |
| TASK-008 | Document/create a destination-specific exclusive firmware DNS profile with no alternate DIRECT upstream for protected names. GUI is initial supported owner; CLI uses exact model/firmware command grammar. Confirm listener reachability, local names, host-vs-segment scope and native53 exemption before selecting it. Optional later typed firmware provisioning requires a new reviewed contract, explicit scoped preview, original settings snapshot and independent readback. | No | — |

### Implementation Phase 5

- GOAL-005: Compose a single guided entry flow and qualify it on actually fresh hardware. Depends on Phases2–4 and supported upstream bootstrap parameter forwarding for a fully automatic route.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-009 | Add a release-owned guided setup entrypoint alongside `scripts/install.sh` only after its new issue is reviewed. Compose standard prerequisites, the official supported native launcher, verified signed panel and optional DNS/profile handoff. Do not copy upstream staging/replacement logic or create a panel component updater. On current2.1 use interactive official native install; a zero-question launcher waits for a documented upstream forwarding mechanism, not a local dispatcher patch or hidden double `-i` installation. | No | — |
| TASK-010 | On a fresh supported router run setup → subscription → full validation → one Apply → independent LAN DIRECT/VPN/local DNS and cold-cache stop/failure/recovery tests → encrypted export. Record exact native/panel/DNS/preset identities and manual steps. Without fresh hardware mark NOTRUN; never reformat/reinstall the active router to emulate it. | No | — |

## 3. Alternatives

- **ALT-001**: Install panel first and let it install all components. Not the current signed installer contract; adds install owner and recovery scope. A future guided outer entrypoint may offer this appearance while retaining stock ownership, after a separate contract.
- **ALT-002**: Official XKeen first, panel on top. Selected now: supported, independently inspectable and compatible with existing native jobs/config editors.
- **ALT-003**: Copy live router config/firmware policy or use encrypted A→B transfer as the default template. Rejected for reference distribution: secrets, private identifiers and destination interface/mark differences. Encrypted transfer remains an optional distinct workflow.
- **ALT-004**: Xray owns LAN53/all firmware DNS, global UDP/IPv6 disable or VPN-to-DIRECT DNS fallback. Rejected as defaults; violates availability/scope. Optional IPv6/DSCP/kill-switch settings require explicit selection and separate acceptance.

## 4. Dependencies

- **DEP-001**: #142 selected-tool environment correction; #140 stock2.1 identity and source limitations; current exact main/native contract.
- **DEP-002**: Official XKeen2.1 installer/command interface and full candidate Xray validator. Prefer qualified explicit stable core version rather than silently accepting auto prereleases.
- **DEP-003**: Existing `Jobs`, `ConfigWorkspace`, `BuildAttachment`, native node authority and `internal/splitdns`; one panel lease, external CLI/cron remain outside it.
- **DEP-004**: Model/firmware RCI/CLI policy and DNS schema; private operator token on5.2; actual fresh router and unproxied LAN client for hardware claims.

## 5. Files

- **FILE-001**: `internal/xkeen/environment.go`, `jobs.go`, `lifecycle.go`, corresponding tests; `packaging/S99xkeen-control`, `scripts/test-panel-init-path.sh`, `scripts/test-release.sh` — implemented #142 correction.
- **FILE-002**: `config/presets/ru-selective-v1.json`, `config/presets/README.md` — public proposed policy, not installed automatically.
- **FILE-003**: `docs/FRESH-KEENETIC.md`, `docs/QUICKSTART-RU.md`, `docs/ROADMAP.md`, `plan/README.md`, this plan — current vs planned boundaries.
- **FILE-004**: `internal/keenetic/preflight.go`, `internal/xkeen/reference_profile.go`, scoped `internal/httpapi` and `web/src/native-xkeen.jsx` — proposed future paths, not present features.
- **FILE-005**: `/opt/etc/xkeen/xkeen.json`, native Xray data configs, `/opt/etc/mosdns`, `S06mosdns`, native firmware policy/DNS data — explicit future destination changes only; native scripts remain stock.

## 6. Testing

- **TEST-001**: PATH adversarial inputs and real native child/init startup fixtures; preserve Telegram SOCKS/foreground/session semantics. One clean exact-HEAD Docker/Linux FULL and independent review for #142 code milestone.
- **TEST-002**: Firmware synthetic present/missing/duplicate description/invalid mark/partial/malformed response/timeout/token errors/unsupported firmware; no hidden writes, no private output in status or diagnostics.
- **TEST-003**: Profile first-match tests for local DIRECT, force-proxy, BLOCK vs VPN, BitTorrent scope, selected QUIC, RU categories, targeted IPs and catch-all. Geodata from synthetic fixture; missing real categories produces validation error. DNS derivation counts conditional rules without expanding them to generic names. Test empty pool/BLOCK fallback before accepting ready state.
- **TEST-004**: Preview digest drift, existing unrelated routing/DNS/outbounds, preserved registry/previous, full Xray graph, one Apply, pending/unknown receipts and no automatic restart/update repetition.
- **TEST-005**: Fresh low-space/missing tools/preexisting resolver/native partial installation/firmware profile conflict and interrupted setup; inspect original owner, never automatic reinstall/downgrade. Native installation rollback is separate from existing config Discard/previous restore; snapshot alone is not functional restore proof.
- **TEST-006**: Real fresh hardware/independent LAN cold DNS and stop/failure/recovery including IPv6 only if selected. Current router PATH correction/healthy checks do not qualify a fresh install or full rollback.

## 7. Risks & Assumptions

- **RISK-001**: Firmware profile/WAN/client assignments differ by version/model. Read-only preflight needs real grammar/response evidence before declaring readiness; do not guess Policy0 or modify default policy.
- **RISK-002**: A broad reference category routes more than individually blocked sites; publish its concrete contents and scope. Geodata evolves independently under stock XKeen; future profile revisions require revalidation.
- **RISK-003**: Independent mosdns removes Xray dependency for DIRECT, not resolver/service failure. Client DoH may bypass router DNS; wider client behavior is #133, transfer is #135.
- **RISK-004**: Native update/install can exit0 with Xray stopped, as observed in #140. Fresh readiness requires actual process/executable/config/API/probe and DNS verification, not a second automatic restart.
- **ASSUMPTION-001**: No spare router is currently available. Fresh hardware and disposable native rollback remain NOTRUN; preserve the working router while developing fixtures/source.

## 8. Related Specifications / Further Reading

- [Native ownership](../docs/NATIVE-XKEEN.md), [FRESH](../docs/FRESH-KEENETIC.md), [security](../SECURITY.md), [operations](../docs/OPERATIONS.md), [development](../docs/DEVELOPMENT.md), [reference profile](../config/presets/README.md).
- [XKeen installation](https://github.com/jameszeroX/XKeen/wiki/Порядок-установки), [native auto semantics](https://github.com/jameszeroX/XKeen/wiki/autoinstall), [tagged2.1 install.sh](https://github.com/jameszeroX/XKeen/blob/2.1/install.sh), [description/mark lookup in pinned init generator](https://github.com/jameszeroX/XKeen/blob/c4b0fb5b956b4f70bf1b8e4a2e0eec452d61ebd1/scripts/_xkeen/02_install/07_install_register/04_register_init.sh).
- [Keenetic CLI policy creation/status](https://storage.googleapis.com/docs.help.keenetic.com/cli/5.0/en/cli_manual_kn-1811.pdf), [current official CLI policy example](https://support.keenetic.com/titan/kn-1811/en/20795.html).
- [Existing DNS infrastructure evidence](infrastructure-independent-dns-1.md), [current DNS integration](feature-native-split-dns-1.md), [#133](https://github.com/popiposter/xkeen-control/issues/133), [#135](https://github.com/popiposter/xkeen-control/issues/135).
