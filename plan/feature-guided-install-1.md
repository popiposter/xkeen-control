---
goal: One guided fresh-router installation with private subscription input and entire HOME activation
version: 2
date_created: 2026-10-08
last_updated: 2026-10-08
owner: xkeen-control
status: Planned
tags: [installation, bootstrap, native, dns, routing]
---

# Introduction

![Status: Planned](https://img.shields.io/badge/status-Planned-blue)

[Issue145](https://github.com/popiposter/xkeen-control/issues/145) specifies the
actual installer product requested by the operator: one clear terminal entry,
private subscription/node input at the beginning, a reference configuration and
automatic connection of **the entire HOME network** after readiness checks.
This is the implementation contract, not an available installer or hardware
qualification. Stable0.3.1 has no setup CLI. The earlier [Issue143 design](https://github.com/popiposter/xkeen-control/issues/143)
remains installation research; its GUI-first provisioning and selectable-client
default are superseded here for this explicit fresh-install mode.

The user-facing sequence is:

1. `Проверяем роутер и возможность установки`.
2. `Вставьте ссылку на подписку или VLESS-узел` — hidden input, checked before installing.
3. `Устанавливаем XKeen, Xray и панель` — qualified, pinned artifacts.
4. `Настраиваем эталон маршрутизации и независимый DNS`.
5. `Проверяем VPN и DNS`.
6. `Подключаем всю домашнюю сеть` — only after previous checks.
7. `Готово` — panel access, initial password, component status and any remaining acceptance limitations.

No instructions to paste credentials into a shell command, edit JSON, choose
individual nodes or configure firmware interfaces manually in the supported
automatic route. Unsupported firmware or an existing/partial installation gets
a precise inspect-only result before mutation, rather than a success message.

## 1. Requirements & Constraints

- **REQ-001**: A release-owned `install.sh --setup` mode is the proposed single
  entry point. Keep the current panel-only installation route distinct. The
  published installer must select its own qualified panel release, including
  its setup CLI; do not invoke unpublished setup commands in stable0.3.1.
- **REQ-002**: Target a genuinely fresh supported ARM64 Keenetic with Entware,
  direct Internet/DNS and private management. Preflight root, free storage,
  installed/tool selection, model/firmware capability and empty native/panel/DNS
  paths. Symlinks, preexisting files, unknown state and interrupted setup are not
  fresh. A separately prepared, validated root-only native RCI token data file
  alone is the explicit firmware5.2 prerequisite exception; it is preserved and
  does not permit preexisting dispatcher/modules/core/native config or resolver
  replacement. No automatic reinstallation of an existing component.
- **REQ-003**: Read one subscription URL or supported VLESS URI privately from
  terminal/stdin before component mutation. Hide echo and restore terminal mode
  on every exit/signal. Use current subscription fetch/parser/SSRF/size/redirect
  safeguards; reject invalid/empty input and an empty enabled node pool. The
  source stays in RAM or bounded root-only temporary storage until committed to
  the canonical private registry. Never place it in argv, environment, history,
  progress messages, public receipts or shell tracing.
- **REQ-004**: The HOME-wide choice is explicit operator intent. Discover and
  verify the destination HOME interface/address, usable WAN and free policy/DNS
  ids; do not hardcode `Home`, `Policy0`, marks, tables or LAN IP. Account for
  host-level policy/DNS overrides, which take precedence over segment defaults.
  Unknown membership/overrides stop activation. Guest, WAN, other segments and
  management access stay outside the assignment scope.
- **REQ-005**: Stage registry, generated managed outbounds, native integration,
  reference traffic policy and DNS transports in one complete Xray-validated
  pending generation through `ConfigEditor`. Use the existing Save/Apply/
  Discard/previous owner. No sequence of per-node `nodes.Apply` calls or native
  restarts while the full graph is incomplete. Xray owns automatic node choice;
  setup does not measure/pin a winning node or run a sustained benchmark.
- **REQ-006**: Required ext geodata/categories must exist and validate. The
  public RU selective profile contains policy only; destination registry supplies
  node selectors and a native `bal-proxy` with BLOCK fallback. Preserve the
  ordering of managed API/DNS integration before traffic rules, BitTorrent DIRECT
  outside explicit force-proxy, scoped QUIC BLOCK and catch-all DIRECT.
- **REQ-007**: Standard mosdns owns independent DIRECT DNS and VPN DoH through
  the existing loopback SOCKS/native pool, without DIRECT fallback for protected
  names. Use fixed fresh-only binary/init/config paths, loopback readiness15354
  and, when needed, one exact trusted HOME IP listener. Firmware retains LAN53
  and local names. Inspect native53 interception before using supported excluded
  port commands; those commands can already restart Xray and require REQ-010 plus
  the complete validated staged graph. Count that restart as the activation;
  never follow it with a redundant Apply restart. The firmware public DNS
  engine is global: require it already absent/public and check unrelated profiles
  before provisioning. A conflicting engine is an explicit pre-mutation
  unsupported result, not an excuse to change guest/non-HOME DNS.
- **REQ-008**: Before HOME activation independently check full native config,
  executable/confdir/process identity, enabled pool, API/probe and real DIRECT/VPN
  DNS and actual policy-scoped interception. The unassigned dedicated policy
  precedes native provisioning under REQ-010; prepare the dedicated DNS profile
  before HOME assignment.
  Assign HOME last, read back actual segment/host assignments, then save firmware.
  Local process/TCP checks do not become independent LAN packet acceptance.
- **REQ-009**: `autostart=off` is staging only. After validated runtime/DNS and
  before HOME assignment/Ready, enable native startup through supported
  `xkeen -auto on` and independently read back its persisted startup setting.
  Verify fixed mosdns/panel init startup configuration as well. Running now is
  distinct from configured startup; unknown startup readiness prevents Ready.
  No router reboot is needed for source/fixture checks, and no extra restart is
  introduced solely to enable autostart.
- **REQ-010**: Before native provisioning, create the dedicated unassigned
  policy with discovered usable WAN. Independently read back its unique
  case-insensitive `xkeen` description, actual valid policy mark/table and no
  assigned clients; save and verify persisted prerequisites before any possible
  activation. Recheck policy identity immediately before EVERY start/restart-
  capable native command, including installation and excluded-port implicit
  restart. Missing, inaccessible, changed or unknown policy forbids dispatch;
  stock missing-policy fallback must never activate interception for all clients.
  Verify the running interception actually uses that policy before HOME binding.
- **REQ-011**: Initial setup runs while the normal panel daemon is deferred.
  One fixed kernel-backed setup/daemon lock and one bounded durable root-only
  setup receipt enforce the startup handoff described below. Existing owners
  share one ordinary lease inside the setup CLI process. A separate CLI lease
  cannot exclude a daemon; neither mechanism locks external native CLI/cron.
  Initial native cron is explicitly off and remains off in this mode; external
  writers must be quiescent. No native init/hook/dispatcher patch is permitted.
- **SEC-001**: No native script patches, `expect`, generic command/file/RCI API,
  second component updater, router toolchain, blanket `opkg upgrade`, reboot,
  automatic credential rotation, wildcard/WAN listener or secret-bearing public
  artifact. Auth sessions never enter snapshots. Existing signed-panel trust and
  upstream native/DNS artifact trust remain separate.
- **SEC-002**: Initial dispatcher/module delivery is a narrowly reviewed
  **fresh bootstrap exception**, not a replacement/update path. Require strictly
  empty destinations, fixed official source, pinned SHA-256/size, safe archive
  members and actual verification. Then invoke ONE stock supported native auto
  installation with autostart off. Native owns core/geodata/init/cron/interception
  and all subsequent native updates; no copied native replacement/recovery logic.
- **CON-001**: Tagged2.1 upstream launcher ends with interactive `xkeen -i` and
  does not forward `auto`; the automatic route therefore needs initial verified
  dispatcher delivery, not a patched launcher or a second full installation.
  Use `xkeen -i auto cores=xray xray=<qualified-version> ... autostart=off` only
  after that delivery. Select reviewed geodata/cron options explicitly.
- **CON-002**: Firmware5.2 private RCI token must be prepared before native
  initialization, which can terminate on401/403. A hidden token prompt is a
  conditional prerequisite, not a log/environment variable. Initial supported
  model/firmware combinations and CLI/RCI schemas must be declared explicitly;
  an unavailable capability is never treated as absence.

## 2. Implementation Steps

### Initial setup ownership and restart recovery

These are proposed requirements for implementation, not delivered safeguards.
All phases below are sequential. The launcher first checks lock capability and
acquires an exclusive kernel `flock` on the fixed root-owned non-symlink inode
`/opt/var/lock/xkeen-control/initial-setup.lock`; keep the inode stable, validate its parent,
and never unlink it as stale. Concurrent setup/recovery fails busy. The same
exclusive lock covers launcher provisioning and the setup CLI handoff without
an unlocked gap. Normal daemon startup holds a shared lock for its entire
lifetime and rechecks the receipt while holding it. No PID file, directory lock
or RAM lease substitutes for this process exclusion.

Before the first firmware/component mutation, atomically persist and sync the
bounded root-only `/opt/etc/xkeen-control/state/initial-setup.json` receipt in a
validated root-only parent. Include schema, qualified release/source identity,
phase, scoped object identities, safe hashes and references to existing config
pending/job receipts; never subscription URLs, node credentials or raw config.
Private scoped snapshots use separate bounded protected storage. The receipt
records initial progress; it is not a second config transaction journal.

| Persisted state | Permitted owner and next action |
| --- | --- |
| Absent | Ordinary existing installations may start the normal daemon under its shared lock. Fresh setup requires fresh preflight and exclusive lock, then writes its receipt before mutation. |
| Incomplete / unknown / aborted | Normal panel init and Go startup refuse normal operation. Explicit root-only setup inspect/recovery may acquire the exclusive lock; no automatic native replay or fresh reinstall. |
| Malformed / unreadable / unsafe | Fail closed before normal worker/HTTP startup; explicit bounded root-only inspection and verified recovery only. Never treat this as absence or delete the receipt automatically. |
| Completed and structurally valid | Normal daemon may start under the shared lock; recheck receipt and existing config recovery fences. Completed records independently confirmed saved configuration, not daemon health or independent LAN acceptance. |

Extend the panel-owned `S99xkeen-control` startup guard and Go startup before
HTTP routes, subscription refresher, DNS observer, Telegram/native jobs and any
normal registry/config writer. The setup branch of `scripts/install.sh` must
defer its current unconditional init start; no environment bypass or second
lease against a live daemon. Reuse the existing config/jobs/registry/splitdns
owners and their one same-process ordinary lease in root-only setup operations.

The current config Apply path starts its own Restart. A port-exemption job must
not be treated as an already supported config Apply: extend the existing
`ConfigEditor`/jobs integration with a fixed setup-only action binding that calls
the existing `beginApply` before dispatch and `finishApply` after it, recording
the same saved digest, job id and before-process identity. This is a typed
extension of the existing lifecycle owner, never a second activation journal or
arbitrary action argument. Missing/unknown linkage blocks completion. Test this
binding and `InspectApplied` recovery before admitting the restart-capable path.

Policy/WAN and DNS prerequisites must be saved and read back without HOME
assignment before enabling native autostart. Record that prerequisite save
separately from the final HOME save. Unknown persistence stops progression.
Panel guards do not stop stock native init on reboot; native can boot only with
already persisted scoping/DNS prerequisites. Do not claim the panel lock fences
external CLI, native cron or firmware operators.

After full config/runtime/DNS checks, persisted startup checks, scoped HOME
readback and final firmware save, independently verify saved state and require
no pending/unknown config or firmware outcome. Durably promote the receipt to
completed, stop setup mutation owners and release the exclusive lock, then
start the normal daemon which takes its shared lock. Report Ready only after
daemon executable/version/health and worker startup readback. Crash after
completion but before daemon startup permits normal startup, never setup replay.

Crash before completion, partial HOME/save or uncertain Apply requires explicit
root-only typed inspect/recovery under the exclusive lock. Inspect actual state
and existing config pending/job receipts; use `ConfigEditor.InspectApplied` for
uncertain activation, not another native command. Scoped inverses use only
unchanged own effects and existing config recovery owners. Abort retains a
blocked receipt until independently verified recovery establishes completion;
no implicit cleanup/reinstall, full config replay or guessed native downgrade.

### Implementation Phase 1

- GOAL-001: Establish the reviewed fresh bootstrap and firmware contract before
  component delivery or device-wide assignment code.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Review Issue145 and this plan; update native/architecture/security authorities to authorize only the initial empty-path dispatcher delivery and fixed HOME writer. Preserve current delivered behavior statements. Reuse PR144 environment correction and reference profile without losing its evidence boundary. | Contract prepared; implementation authority update pending | 2026-10-08 |
| TASK-002 | Add `internal/keenetic/setup.go` with bounded fixed read-only firmware discovery and typed dedicated-policy/DNS plans. Establish exact model/firmware CLI transport, context grammar, HOME membership/overrides and scoped inverse commands. No caller-supplied command, endpoint or JSON body. | No | — |

### Implementation Phase 2

- GOAL-002: Deliver the release-owned terminal flow, private input and strictly
  fresh stock-component provisioning.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-003 | Add explicit `--setup` dispatch in `scripts/install.sh`, fixed kernel lock, durable initial setup receipt and Russian phase output. Check freshness/capabilities/private source before mutations; write receipt before the first component/firmware change. Create/read back/save the dedicated unassigned xkeen policy with usable WAN and actual mark under REQ-010, before TASK-004. Install only standard required dependencies and verify actual GNU tar selection. Preserve panel-only mode. Download/run usage retains terminal input and checks download success. | No | — |
| TASK-004 | Implement fixed official native2.1 dispatcher/module bootstrap into empty destinations with strict archive/hash checks and unknown-result fencing. Require verified persisted unassigned policy before dispatch. Execute one stock `-i auto` with reviewed stable Xray version, explicit geodata, cron=off and autostart=off. Existing installs/partial native results are inspected, never overwritten/replayed. | No | — |
| TASK-005 | Deliver same-release verified panel through current release installer owner with setup-only deferred daemon start. Extend panel-owned init and `cmd/xkeen-control/main.go` with the fixed shared-lock/receipt startup guards and root-only `setup` phase operations, no credential arguments. Transfer the launcher exclusive lock to CLI without a gap. Add bounded initial pinned mosdns provisioning; refuse existing/unowned resolver paths. No normal panel workers run during setup. | No | — |

### Implementation Phase 3

- GOAL-003: Build and activate one coherent private configuration generation.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-006 | Add `internal/setup` orchestration and tests. Construct existing config/jobs/registry/splitdns owners with one same-process ordinary lease while normal daemon remains deferred. Import private subscription/node with existing parser/fetcher/canonical registry. Compose destination attachment, public reference and DNS transports; Preview/Stage via `ConfigEditor.PreviewTransferUnderLease` and `StageTransferUnderLease`, including registry/outbounds consistency and splitdns derived validation. No competing transaction or early per-node Apply. | No | — |
| TASK-007 | Prepare mosdns through existing splitdns compilation/service owner. Validate the full staged native set and recheck persisted policy/mark before every possible activation. Perform one explicit supported activation using existing jobs/config outcome owners. If necessary port-exemption changes restart native, stage the full candidate first, count that restart as the activation and inspect it without redundant Apply. Verify exact process/config/API/probe, actual policy-scoped interception and DIRECT/VPN DNS. Lost/uncertain results become inspect-only phase outcomes; use existing InspectApplied rather than repeating native commands. | No | — |

### Implementation Phase 4

- GOAL-004: Connect all proven HOME clients only after readiness, with scoped
  firmware changes and independent readback.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-008 | Revalidate the unassigned dedicated xkeen policy created in TASK-003; provision the custom exclusive DNS profile forwarding to tested mosdns address/port. Persist/read back policy/DNS prerequisites without assigning HOME before enabling native autostart. Use the verified typed CLI plan and private scoped snapshots; check drift before every mutation. Reject unsupported transport/schema rather than guessing RCI POST. | No | — |
| TASK-009 | Confirm global DNS engine absent/public and unaffected non-HOME profiles before mutation. After runtime and persisted prerequisite readiness enable/read back supported native autostart and verify mosdns/panel configured startup guards; then assign HOME policy and DNS, including supported host overrides or a precise pre-mutation unsupported result. Read back objects/segment/hosts and actual routing table/resolver path, then `system configuration save` and saved-state readback. Keep guest/WAN/management/default policy outside scope. Scoped rollback restores only unchanged own effects; no full running-config replay. | No | — |
| TASK-010 | Require no pending/unknown outcomes; independently verify saved configuration/startup and durably complete setup receipt. Stop setup owners, release exclusive lock, start normal daemon under shared lock and verify its identity/health/workers before Ready. Report panel access/password, native/DNS/component versions, enabled-node count and LAN verification boundary. Successful router-side provisioning is distinct from fresh independent-client acceptance. Normal subsequent refresh uses existing panel, not setup again. | No | — |

### Implementation Phase 5

- GOAL-005: Qualify exact source, release composition and actual fresh hardware.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-011 | Run focused adverse fixtures, independent exact-source review, final clean-HEAD Docker/Linux FULL and changed release assembly/trust checks. Publish a runnable setup command only after protected signed release contains the implemented mode/CLI; development artifact is not the published installer. | No | — |
| TASK-012 | On actual fresh supported hardware test subscription → full configuration → one activation → all HOME readback → unproxied LAN DIRECT/VPN/local names/cold DNS/failure/recovery → scoped rollback. Current operator router is not a fresh test target; preserve NOTRUN if hardware is unavailable. | No | — |

## 3. Alternatives

- **ALT-001**: Panel first appears to install everything. The outer entrypoint
  may provide that experience, but native `-i` still owns component installation
  and the existing panel release owner owns panel installation. Do not create
  another updater or native installation engine.
- **ALT-002**: Official interactive launcher followed by manual JSON/DNS/policy
  setup. Keep as existing fallback documentation; it does not satisfy the
  requested supported automatic route or subscription-only onboarding.
- **ALT-003**: Pipe canned answers into native menus, modify launcher forwarding,
  or execute native installation twice. Reject; use native supported auto args
  after narrowly scoped initial stock payload delivery.
- **ALT-004**: Change the default policy or replay a copied router configuration
  to connect HOME. Reject; destination-scoped dedicated policy/DNS assignments
  preserve other networks and account for host overrides.
- **ALT-005**: Download a mutable latest core/mosdns/panel and call exit0 ready.
  Reject; one qualified version set, verified artifacts and actual readback.

## 4. Dependencies

- **DEP-001**: PR144 / Issue142 Entware-first environment and reference fragment;
  Issue143 research; Issue140/PR141 native update evidence remains separate.
- **DEP-002**: Official native2.1 archive verified2026-10-08:129691 bytes,
  SHA256 `4b9350b11fab7fd3e4973db609db0fb780994f4b5ab0096f0549bc52acac8c73`.
  Initial dispatcher delivery does not qualify any future changed archive/tag.
- **DEP-003**: Verified current mosdns Stable5.3.4 ARM64 ZIP:6591859 bytes,
  SHA256 `82d80a1a21606fca0bc6b65ac6f90d30cff6bb4a19a6ab6a246cf247dbb78bc0`.
  Extracted executable identity and actual service readiness are additional checks.
- **DEP-004**: Same-release signed panel artifact with setup CLI, one qualified
  stable Xray version and required native geodata. Do not advertise current0.3.1
  as containing this new mode. Recheck mutable upstream information per release.
- **DEP-005**: Official firmware CLI describes `ip policy <id>`, policy-context
  `description xkeen` and `permit global <wan>`, hotspot-context
  `policy <home> <id>`, DNS-proxy-context `filter profile <id>` /
  `filter profile <id> dns53 upstream <ip>:15354` / `filter engine public` /
  `filter assign interface profile <home> <id>`. `/bin/ndmc -c command-string`
  transport was observed read-only on KN1811/5.01; context flattening, mutation
  results and exact discovery schemas still require verification. CLI grammar
  does not authorize guessed RCI POST bodies.

## 5. Files

- **FILE-001**: `scripts/install.sh`, release assembly/fixtures and installer
  documentation and panel-owned init — proposed release-owned `--setup`, deferred
  daemon start and receipt/shared-lock startup guard, not implemented here.
- **FILE-002**: `cmd/xkeen-control/main.go`, new `internal/setup` and
  `internal/keenetic/setup.go` — proposed root-only setup and fixed firmware adapter.
- **FILE-003**: `internal/xkeen` config pending/transfer/jobs, `internal/nodes`
  parser/fetcher/renderer/store, `internal/splitdns` — existing owners to reuse.
- **FILE-004**: Public RU selective policy prepared in PR144; fixed private
  registry/native config/mosdns paths and bounded setup receipts on destination.
- **FILE-005**: This plan, native/security/architecture/installation authorities,
  ROADMAP and plan index — current, proposed and qualified boundaries.

## 6. Testing

- **TEST-001**: Fresh/preexisting/symlink/partial/unsupported model or schema,
  missing token/tool, low space, invalid archive/digest, interrupted phases and
  repeat invocation. No native/panel/resolver overwrite or automatic replay.
- **TEST-002**: Private input/echo restoration/signals, bounded subscription
  fetch/parse/SSRF errors, empty source/pool and secretless progress/receipts.
- **TEST-003**: Representative first-match reference rules, conditional traffic
  vs DNS classification, missing ext categories/tag collisions, full graph,
  canonical registry/render consistency, preserved unknown fields and stale
  pending baseline. One configuration owner and one activation.
- **TEST-004**: Native exit0 with stopped/wrong executable/confdir, API/probe or
  DNS failure, unknown receipt, all HOME assignments withheld before readiness,
  host overrides and non-HOME clients, conflicting global DNS engine, native
  autostart still off/unknown or missing DNS/panel startup, drift before firmware
  writes/save/rollback. Token-only5.2 prerequisite does not admit a partial install.
- **TEST-005**: Same-release bootstrap/CLI availability, strict trust separation,
  release assembly plus exact clean-HEAD supported FULL and independent review.
- **TEST-006**: Fresh hardware and unproxied HOME clients, DIRECT/VPN/local DNS,
  cold-cache stop/failure/recovery and scoped rollback. Fixtures/process reads
  never stand in for independent LAN acceptance; current target remains NOTRUN.
- **TEST-007**: Missing/inaccessible/duplicate-description/changed policy or mark
  forbids native install/start/restart, including excluded-port implicit restart.
  Complete candidate precedes any necessary restart; no redundant Apply. Verify
  actual scoped interception before HOME. Interrupt before/after policy creation,
  prerequisite save and native activation; no fallback for all clients.
- **TEST-008**: Competing setup/daemon startup, init restart/direct Go startup,
  CLI crash/router reboot, unsafe receipt/lock path, partial HOME/save/completion,
  crash after completion before daemon startup and uncertain Apply. Assert no
  normal registry/config/native/DNS observer/refresh writes before completion;
  kernel lock is continuous across launcher/CLI and daemon retains shared lock.
  Explicit inspect/recovery reuses existing pending/job owners with no replay.
  Absent receipt preserves ordinary existing installation startup. Native boot
  after autostart enable uses already saved policy/DNS prerequisites; guards do
  not claim to control native init or external writers.

## 7. Risks & Assumptions

- **RISK-001**: Firmware command support, schemas and HOME/WAN topology vary.
  Declare supported capability combinations and stop unknown before mutation;
  private full config is evidence, not an executable recovery script.
- **RISK-002**: Broad RU selective categories are a chosen reference, not every
  user's preference. Show its scope; torrent sniffing cannot guarantee universal
  bypass, and force-proxy intentionally precedes that exception.
- **RISK-003**: Native component installation has its own failure/recovery scope.
  A file backup does not prove functional native rollback. Never call a partial
  native install ready or repeatedly install to repair it.
- **RISK-004**: All-HOME DNS/policy assignment affects connectivity. Preserve
  private management and original assignments; staged components must be ready
  first. Client DoH/IPv6 and independent client checks remain explicit boundaries.
- **ASSUMPTION-001**: No spare router is currently available. Implement source
  and synthetic fixtures independently; do not reinstall the operator router to
  fake fresh acceptance. No router mutation is authorized by this design PR.

## 8. Related Specifications / Further Reading

- [Issue145](https://github.com/popiposter/xkeen-control/issues/145), [Issue143](https://github.com/popiposter/xkeen-control/issues/143), [PR144](https://github.com/popiposter/xkeen-control/pull/144).
- [Native contract](../docs/NATIVE-XKEEN.md), [architecture](../docs/ARCHITECTURE.md), [security](../SECURITY.md), [operations](../docs/OPERATIONS.md), [current fresh procedure](../docs/FRESH-KEENETIC.md), [development](../docs/DEVELOPMENT.md).
- [Official2.1 launcher](https://github.com/jameszeroX/XKeen/blob/2.1/install.sh), [supported native auto options](https://github.com/jameszeroX/XKeen/wiki/autoinstall), [native config/token](https://github.com/jameszeroX/XKeen/wiki/Порядок-установки).
- [Keenetic KN1811/5.0 CLI manual](https://storage.googleapis.com/docs.help.keenetic.com/cli/5.0/en/cli_manual_kn-1811.pdf), [mosdns5.3.4](https://github.com/IrineSistiana/mosdns/releases/tag/v5.3.4), [current splitdns integration](feature-native-split-dns-1.md).
