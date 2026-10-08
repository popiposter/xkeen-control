# XKeen 2.1 transition qualification record

Prepared 2026-10-08 for [Issue #140](https://github.com/popiposter/xkeen-control/issues/140).
Use with the [transition contract](../XKEEN-2.1-TRANSITION.md) and
[configuration workflow](../CONFIG-EDITOR-WORKFLOWS.md). This record starts
**NOTRUN**. No router mutation or full rollback qualification is claimed.

## Source and release prerequisites

Record the panel source, installed panel identity and native baseline separately.
Draft PR #141 contains compatibility work; it is not an installed panel release.
Preserve required installed behavior from the separate Telegram and quality
candidates (#137/#139). Installing #141 alone is not proof that those fixes remain.

| Gate | Required evidence | Initial state |
| --- | --- | --- |
| Native target | Latest stable is 2.1; release tag/source and downloaded archive match the transition contract | Recheck at execution |
| Panel candidate | Exact installed source/artifact, compatibility checks, required fixes and independent review | Review/install NOTRUN |
| Full rollback | Disposable-contour restore of the exact baseline, generated files, private data and independent service readback | NOTRUN / blocks live update |
| Router/window | Named private target, bounded window and no competing CLI/config/native cron operation | NOTRUN |
| RCI | Applicable firmware requirements and token presence/permissions checked privately; no token in evidence | NOTRUN |

The 2026-10-08 public release lookup still resolves `releases/latest` to
[2.1](https://github.com/jameszeroX/XKeen/releases/tag/2.1). The locally retained
archive still hashes to `4b9350b11fab7fd3e4973db609db0fb780994f4b5ab0096f0549bc52acac8c73`.
These checks describe a public asset, not installed bytes or native download
verification. Recheck both at the actual update window; stop if latest changes.

## One private baseline before changing channel

Assign one baseline identifier and retention deadline. Keep its manifest and
contents in protected operator storage outside Git, containers and public logs.
Record space, file counts, sizes, hashes and original owner/mode; verify the copy
against the source. Treat missing required paths and unreadable files as failures,
and explicitly record optional absent paths. Native `-kb` and `-xb` output alone
does not verify the complete baseline.

| Baseline group | Capture / verify | Expected preservation or change |
| --- | --- | --- |
| Native code | Dispatcher, complete installed modules, version/channel/build | Changes only through supported native update/channel commands |
| Generated lifecycle | Active native init and netfilter hook; relevant native settings read privately | May regenerate; compare settings and ownership, not just bytes |
| Native data | Protected `xkeen.json`, applicable port/exclusion/policy lists, existing GeoIPSET configuration | Existing settings preserved; distinguish newly created files |
| Xray | Complete active config set, core identity, config directory and process identity | Core remains unchanged; configs retained and validate together |
| Panel private authority | Registry, notification settings, authentication/session files, pending/previous workspace state | Preserve privately; no export into the public qualification record |
| DNS | Owned resolver generation/config, synchronization receipt and service state | Same effective policy; synchronization outcome recorded separately |
| Cron | Entire private baseline plus native-owned entries | Unrelated entries preserved; native schedule changes explained |
| Geodata | Installed file identities/counts and relevant configured sources | Do not request an independent geodata/core update in this window |

Before mutation, the saved config set must have no unresolved pending changes;
there must be no running/unknown panel job. Working drafts are excluded from
native Apply and must not be mistaken for the active baseline.

For this operator's current routing, include the restored `bittorrent -> direct`
rule in the **fresh** baseline: inbound tags `redirect`, `tproxy`, `socks`, ahead
of selective domain/IP VPN rules. Keep the existing force-proxy behavior and
ordinary-input sniffing settings. Read back the actual current document rather
than assuming the 2026-10-07 change is still present. Updating XKeen must not
silently replace this policy with an upstream routing template.

## Prove rollback on a disposable contour

Pinned 2.1 [backup implementation](https://github.com/jameszeroX/XKeen/blob/c4b0fb5b956b4f70bf1b8e4a2e0eec452d61ebd1/scripts/_xkeen/04_tools/06_tools_backups/01_backups_xkeen.sh)
copies only the dispatcher/modules. Restore iterates `*xkeen*` directories and
uses the last matching directory in glob order; there is no backup-selection
prompt, explicit baseline ID or timestamp/integrity verification. Do not infer
the selected baseline from the word "latest". Inspect the actual prior-version
restore implementation as well; the target source does not prove its behavior.

The pinned dispatcher confirms `-kbr`, then calls that restore function; it does
not invoke the update post-processing path. That separate `-uk_post_update` path
regenerates init/cron, registers native lists/control/status and may restart the
service. A successful code restore is therefore not complete state recovery.
Neither `.old` staging nor a backup command's exit zero is adequate evidence.

The previously inspected Beta source `68eca60fedf03957952d1f8b44bfdd98f8a282e5`
does expose supported [`-ri`](https://github.com/jameszeroX/XKeen/blob/68eca60fedf03957952d1f8b44bfdd98f8a282e5/scripts/xkeen):
it migrates settings from the current init, regenerates init and restarts a running
core. Native init then generates its hook. This establishes a candidate public
regeneration command, not a qualified full rollback. It neither reconstructs
the root crontab nor restores saved pre-update settings; for example, the removed
`udp_flush` assignment cannot be recovered from a regenerated 2.1 init. Verify
the exact installed old source and private baseline before testing this path.

The disposable-contour record must identify:

1. The exact copy native restore will select, and verified dispatcher/module
   hashes matching the pre-channel baseline. Stop on ambiguous selection.
2. The supported prior-version commands that regenerate native lifecycle files
   without fetching latest code or replacing unrelated data. Record the actual
   commands only after investigating that version; do not substitute `-i`, an
   internal `-uk_post_update` invocation or native script edits as a guessed fix.
3. The operator-supported restoration path for protected data, cron and DNS,
   preserving unrelated current state. A panel config previous-generation restore
   covers its own edited configs, not this complete native-install baseline.
4. Independent code/config/permission/process/service/DNS readback after restore,
   including routing order and the selected baseline identity.

If any step lacks a supported path, record the exact gap and keep the live gate
blocked. This worksheet introduces no second panel updater or restore owner.
Do not test destructive restore on the working router to discover the procedure.

## Update and readback record

After all gates pass, use the private native console once for each required
command. For Beta/Dev, `-channel` selects Stable and must be independently read
back before ordinary `-uk`. If already Stable, skip channel switching. Do not
inject `auto`, answer guessed prompts, update Xray separately or replay a lost
request. Record the configured native verification mode and actual verification
result; mode `warn` alone does not prove a verified download.

| Observation | Acceptance | Initial state |
| --- | --- | --- |
| Command receipt | Terminal state and actual exit; private console inspected, unknown is not replayed | NOTRUN |
| Native identity | 2.1 / Stable / expected build; independent installed-tree readback | NOTRUN |
| Data preservation | Registry, unmanaged outbounds, DNS/routing including BitTorrent order, native settings, unrelated cron and private permissions | NOTRUN |
| Runtime | Xray process/executable/config-directory identity plus independent health | NOTRUN |
| DNS integration | Separate reconciliation/service result, no accidental empty/direct policy | NOTRUN |
| Independent LAN | Unproxied client's DIRECT, VPN and local DNS checks; protected names have no direct DNS fallback | NOTRUN |
| Broader behavior | Relevant outages, kill-switch, DSCP61 and UDP renew under their own bounded contract (#133) | NOTRUN |

Record unchanged, unavailable, failed and unknown observations literally. Version
change and exit zero do not certify native integrity, VPN/DNS health or complete
preservation. BitTorrent sniffing is limited; retaining its rule does not prove
that every encrypted torrent flow avoids VPN. A real torrent route claim requires
separate bounded traffic observation.

Keep private console/config/backups local. Publish only sanitized identities,
counts, hashes and outcome boundaries in #140 and the operational ledger #4.
No automatic rollback/retry/restart, router reboot, package upgrade or sustained
benchmark follows a failed or ambiguous check.
