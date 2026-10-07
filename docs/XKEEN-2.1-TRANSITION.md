# Transition to stock XKeen 2.1 Stable

Issue [#140](https://github.com/popiposter/xkeen-control/issues/140) separates
panel compatibility from installation and independent LAN acceptance. XKeen
remains unmodified and owns its update, generated init/hook, components and cron.
The panel invokes one supported command and observes its result.

## Pinned upstream evidence

The [2.1 release](https://github.com/jameszeroX/XKeen/releases/tag/2.1) was
published on 2026-10-06 at 07:58:48 UTC. On 2026-10-07 it remained the latest
stable release. Annotated tag `d36fb0832e43b1998ad246199de3c8c92c347bea`
points to source `c4b0fb5b956b4f70bf1b8e4a2e0eec452d61ebd1`; GitHub reports a
verified tag signature. This is separate from the panel's Ed25519 update trust.

`xkeen.tar.gz`: 129691 bytes, SHA-256
`4b9350b11fab7fd3e4973db609db0fb780994f4b5ab0096f0549bc52acac8c73`,
matching the release API digest. Its 72 regular files were compared against
pinned `scripts/`: 71 byte-identical, one difference in `01_info_variable.sh`
is the release build stamp `2026-10-06 10:58:45 MSK` in place of an empty stamp.
Dispatcher SHA-256:
`66a00579938cbb9ff9fa4f595636481f3aa528380af8e79f6861b811998c434c`.
No complete upstream installer was executed for this inspection.

The [command matrix](../plan/xkeen-command-inventory-v2.md) records the delta
from Beta source `68eca60fedf03957952d1f8b44bfdd98f8a282e5`: nine commits,
seven changed script files, unchanged 69 branches/74 flag spellings. Most
release notes describe features already present in that Beta, not missing panel
features. Local synthetic tests qualify adapter semantics; they do not certify
the native installer, firmware or live traffic.

## Panel behavior and result boundaries

The ordinary update uses `-uk` with its private interactive console. In 2.1
the operator confirms the native menu. The panel never adds `auto`, answers
prompts itself, or repeats a command when the viewer reconnects. Telegram
accepts only the fixed update request and tells the operator to use the local
panel console for answers. An unanswered prompt expires as unknown; Telegram
does not forward native output or prompt input.

`completed`/exit 0 means the process exited. Refusal, no available update and an
update whose internal restart failed can all yield exit 0. The panel therefore
shows independent public version/channel/build observations before and after
`update-xkeen`, plus the observed Xray process as running/stopped/unknown.
An unchanged identity does not distinguish refusal from no update and does not
prove identical installed bytes. A changed identity does not certify integrity,
firewall, DNS or tunnel health. A stopped process remains visible even when the
outer update exited successfully. These facts live in RAM; after a panel restart
the durable receipt retains the command state, but release readback is unavailable.
Interrupted or failed-receipt jobs have no verified after-snapshot. Inspection
does not turn an unknown operation into successful completion.

Readback uses fixed bounded file/process reads under the ordinary panel lease.
It never invokes native self-heal or writes native files. External CLI/cron
remain independent: observations do not prove global race exclusion. DNS
reconciliation after completed commands keeps its own outcome and no-op behavior;
neither it nor the release readback certifies the native updater's internal work.

## RCI and changed native behavior

On KeeneticOS 5.2+, provision the RCI token privately through native
`/opt/etc/xkeen/xkeen.json`, protected by a 0700 directory and 0600 file.
The Xray editor does not edit this file and must not feed it to Xray validation.
No new token UI or RCI endpoint is introduced by #140.

The pinned `01_info_common.sh` performs its RCI check during import, before
dispatcher action selection, and exits 1 on HTTP 401/403. Generated `S05xkeen`
skips its own check for stop/status, but the top-level check still precedes
`xkeen -stop/-status`. Thus the panel cannot promise those commands are an
emergency stop with an invalid token. The reproducible source condition is
import returning 401/403 before action dispatch; required upstream behavior is
to preserve safe stop/status despite invalid RCI authority. Hardware reproduction
and upstream correction remain separate; never patch installed native scripts.

The removed `udp_flush` embedded switch means the TProxy/Hybrid hook clears
policy-marked UDP conntrack on renew without that switch. Assess active UDP
sessions on an independent LAN client. Native update may install missing GeoIPSET,
regenerate init/cron, tighten permissions and restart an already running service;
budget for a traffic interruption. Neither archive staging nor exit 0 is a full
transaction guarantee for those side effects.

## Authorized update sequence

Live mutation needs a named router/window and a verified rollback baseline under
#140. Before mutation, inspect exact panel/native/core/firmware identities,
pending configs/jobs, applicable settings, cron and process/DNS state. Preserve
required installed fixes from separate PRs, including Telegram connectivity
#137; a candidate from main alone does not contain that unmerged fix.
Use an independent LAN client without a client VPN for traffic claims.

1. Privately capture and verify one bounded root-only baseline covering stock
   dispatcher/modules, native init/hook, `xkeen.json`, affected data config/lists,
   cron, private node registry and current DNS generation. Verify space and
   retention; never put these secrets into Git, containers or public logs.
2. Exclude concurrent external native/config operations through operator
   coordination. Check permissions and RCI prerequisites without printing tokens.
3. Recheck latest stable and its archive/digest. Native stable download follows
   `releases/latest`; if it is no longer 2.1, stop this qualification and revise
   the contract. Native verification defaults to `warn`, so record the selected
   verification mode and actual result, not merely its presence. Do not silently
   change mode or implement another downloader.
4. For Beta/Dev, run supported `-channel`, choose Stable, and read it back. This
   native command owns its own script change. A plain `-uk` on Beta/Dev otherwise
   downloads the dev archive. If already Stable, omit channel switching.
5. Run ordinary `-uk` once through the job/console; answer native prompts. Inspect
   the resulting release identity, native installation, expected generated
   changes, private permissions, preserved registry/unmanaged outbounds/settings/
   cron, Xray process and independent service health. Do not update cores/geodata
   separately during this transition without an additional task contract.
6. Check applicable LAN DIRECT/VPN/local DNS and protected-name failure without
   direct fallback. Outages/recovery, enabled kill-switch, DSCP61/`xkeen_full`
   and UDP/DHCP renew need bounded relevant authorization and independent client
   tests. Record broader results in #133 rather than claiming them from a smoke test.

## Rollback gate: not yet hardware-qualified

`-kb` backs up dispatcher/modules only; `-xb` backs up Xray config only.
Automatic native backup can be disabled by `backup=off`. `-kbr` selects the latest
native backup and restores code, not the complete regenerated init/hook/cron/data
state. It is not in the panel catalog. Upstream removes staging `.old` trees on
successful install; they are not a durable rollback snapshot.

Before live update, establish and prove a supported full rollback on a disposable
contour: select the exact baseline, restore code through native supported paths,
regenerate init/hook using the prior stock version, restore protected data and
verify independent service/config state. Do not invent script edits, a generic
command endpoint or a second panel restoration owner to fill this gap. This
procedure is **NOTRUN**, so hardware transition remains **BLOCKED** on this gate.
An upstream/operator decision is required if no complete supported path exists.

On invalid token, drift, competing operation, incomplete backup/digest, changed
latest or uncertain update/readback, stop and inspect durable receipts/state.
No automatic replay, downgrade or recovery restart. No router reboot, blanket
package upgrade, credential rotation or sustained `-sbt` benchmark is authorized.

Source/local compatibility, development-router update, signed panel installation
and independent LAN acceptance remain separate evidence classes. Publish only
safe versions/counts/hashes/transitions in [ledger #4](https://github.com/popiposter/xkeen-control/issues/4).
