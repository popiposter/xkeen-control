# Native XKeen contract — graphical shell

Active Issue #121 contract, revised by the operator on 2026-10-03.
The [v2 implementation plan](../plan/architecture-native-shell-v2.md) and
[source audit](../plan/audit-native-shell-2026-10-03.md) replace the earlier
native admission/update architecture. Old qualification records remain historical.

## Ownership

- **XKeen code is never modified by this project.** No local dispatcher, init,
  netfilter hook or module patches; no decorated archive/profile or replacement
  updater. A reproducible upstream defect is reported upstream, not fixed by
  patching the router's XKeen. Native commands may perform their own normal changes.
  Embedded setting assignments inside scripts (including `S05xkeen`) are code,
  not editable config: use a supported native command or show them read-only.
- Native XKeen owns installation, dependencies, component downloads/updates,
  service/interception and its cron jobs. The panel invokes supported commands.
- The panel is a lightweight graphical shell: fixed typed native actions and
  convenient editors for the native configuration those actions/data plane use.
- Native configuration is the executable policy. Editors preserve unknown fields
  and untouched regions. No mandatory appliance twin, ProductDefault equivalence,
  layout adoption or native-profile integrity gate for normal GUI use.
- The private node registry owns only panel-managed profiles/subscriptions.
  Unmanaged native outbounds remain intact. Viewing a page does not add routing,
  balancers/API or alter the selected native operating mode.
- Native Xray leastPing/leastLoad/SB/manual selection remains the selection owner.
  The panel can run bounded quality comparisons and propose native leastLoad
  weights through the same config editor. No quality supervisor or automatic
  runtime override is installed; periodic measurements do not restart Xray.
  An explicit operator pin uses Xray's volatile native balancer override and
  validates/readbacks an enabled pool member. It bypasses automatic selection
  and failover until cleared or the native service restarts; no selector loop,
  persistent selection record or automatic override worker is enabled.
- Auth/private management, subscriptions, bounded observations/probes, portable
  config export and the signed updater **for this panel only** remain panel functions.

## Native commands and configuration

The adapter uses a fixed executable and validated argv. Interactive actions expose
the selected XKeen process in a command-bound terminal: the operator answers native
prompts directly. No arbitrary shell/executable, generic PTY or expect/file manager.
Noninteractive actions use buttons/forms with expandable read-only native output.
Conditional interactive jobs start with PTY support; opening the console never
restarts a command. The [complete command matrix](../plan/xkeen-command-inventory-v2.md)
defines relevance, parameters, input mode and workflows from pinned upstream source.
The console is an explicit authenticated private surface, separate from sanitized
status APIs: bounded RAM output/input, job/session ownership and origin/CSRF checks;
no automatic public logging, GitHub/Telegram export or terminal clipboard controls.
Closing the page detaches the viewer; it does not cancel or repeat the job.
Geodata schedule creation/change/removal uses native `-ugc`/`-dgc`; no duplicate
panel geodata scheduler. Script/core/geodata updates use native `-uk`/`-ux`/`-ug`.
Native local backups use its commands. Portable transfer contains configuration
and private managed data, not executables or blind restoration of router state.

Read-only dashboard discovery reads bounded files/process observations; it does
not secretly execute a command that may self-heal packages or change the system.
Capabilities are feature-specific. Unfamiliar arguments/dialogue disable only that
action. No requirement to patch XKeen or freeze its full source tree to use the GUI.

Config editors operate on fixed config paths, not arbitrary files. Supported
paths are data-only JSON/JSONC and native list files; no sourced shell files.
The [editor workflows](CONFIG-EDITOR-WORKFLOWS.md) define shared Form/Text drafts,
undo/redo, private draft saving, validated native saves, pending config sets,
explicit native Restart and optional restoration of the previous generation.
Raw config text is an explicit authenticated private editor surface for those
fixed IDs, not a generic file API. Native/validation diagnostics stay private.
fields/rules/lists have typed validation; full Xray candidate validation precedes
activation. Save uses the existing small transaction/backup for its own changes,
not a journal covering native install/update internals. Changed baseline prompts
reload/diff. Never overwrite external drift while attempting rollback.

## Jobs, concurrency and outcomes

One ordinary panel operation owner serializes panel commands/config writes/refresh.
**It does not serialize external native CLI or cron.** Shared native admission,
ancestry/exec receipts, byte-exact profile compilers and NDM event workers are not
requirements and must be removed from the undelivered implementation.

Normal use performs changes sequentially through the panel. Do not simultaneously
edit the same files via external commands while saving in the GUI. Baseline and
result checks detect visible drift; they do not prove global race exclusion.
This is an explicit operating limitation, not a new Setup or repeated approval
ritual. Guaranteed simultaneous external writers would need a supported upstream
mechanism and a separate decision, never invasive panel-owned script patches.

Jobs have bounded output/time and survive closing the browser. One small last-job
receipt may identify an interrupted mutation; no per-native-stage journal. Exit
zero or HTTP202 alone is not a verified service/tunnel result. Independent state
readback determines observed/failed/unknown. Unknown results are inspected, not
automatically replayed or repaired. Read-only UI remains available.

## DNS, routing and availability

The GUI edits native/Xray DNS/routing/balancing settings; XKeen applies its native
interception policy. The panel never writes firewall/kernel rules itself.
Client policy, DNS interception, direct/proxy resolvers, PBR and killswitch are
visible explicit settings. An access policy specifically named XKeen is not a
universal panel installation prerequisite; use the configured native mode.

Domain/geosite split DNS is supported by Xray configuration. IP-only routing
cannot automatically specify DNS routing before resolving a name. Preserve the
ordinary direct Internet/DNS path as a design goal, and test stopped XKeen,
failed proxy nodes and recovery separately. Do not promise failover from source
inspection or from a PC whose traffic uses Karing. A separate LAN client is
required for those live acceptance claims.

## Delivery boundary

No old-panel migration/backwards compatibility is required. Remove old component
writers/Setup/readers as the native command/editor replacement becomes usable.
Preserve useful auth, nodes, Xray validation, current-config rollback, signed panel
updater, encrypted export and standard shadcn UI/build/test improvements.

Use one normal checkout and the existing Draft PR; no worktrees/self-merge.
Iterate with focused tests, one final exact-HEAD FULL per finished code milestone
and independent review. Docs-only contract/audit changes need content/link/diff
checks, not application/browser runs. Publish usable milestones without waiting
for optional adaptive/fleet features.

This contract revision does not execute deletion, reinstall or router mutation.
Earlier installer/attachment/import/recovery/unknown operations are never replayed.
Future live work needs bounded relevant snapshots/readback; service restarts are
authorized under the active implementation scope. No router reboot, blanket opkg
upgrade, credential rotation or sustained benchmark is planned. Credentials and
raw infrastructure/subscription material stay outside Git/containers/public logs.


Current native delivery has command jobs, Form/Text config editors, geodata
membership/routing examples, enabled subscription refresh, encrypted transfer
and [optional Telegram control](TELEGRAM-CONTROL.md). Production entry points for
the old component/Setup/appliance restore owners and adoption/migration CLI are
retired in the cleanup source with their tests. Signed panel update and current node/config rollback
remain. Shared parsers/validation needed by those paths are retained.

Hardware acceptance (unproxied LAN DNS/DIRECT/VPN/failures, second-router transfer)
and real bot acceptance with configured private credentials are independent
remaining checks; their absence does not create a Setup gate or justify native
script patches. See the v2 plan for exact recorded development/live boundaries.

The cleanup source also removes repository template deployment and legacy secret migration scripts. Native XKeen installation/update commands and the current encrypted transfer UI replace those obsolete entrypoints. This cleanup is not installed until its own exact-source delivery gate passes.
