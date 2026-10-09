# Operations

Use [native contract](NATIVE-XKEEN.md) and [quick start](QUICKSTART-RU.md). Stock XKeen owns service/interception/components and cron; the panel stays alongside it. No native admission/profile workers, panel component updater or legacy Setup takeover.

## Configuration and native commands

Inspect the current native status and pending config set before mutations. Save validates the complete Xray candidate; Apply restarts through the native job. Require terminal completed/configuration applied plus fresh process/config/health readback. HTTP202 is acceptance only. Use Discard before Apply or explicit previous restore afterward; no automatic repeated action after an unknown result.

Components and schedules use allowlisted native commands. Interactive prompts remain visible in the command-bound console. External CLI/cron do not share the panel lease: quiesce competing operations and check drift instead of assuming a global lock. Private console output must not be uploaded to public evidence.

## Independent LAN DNS

An interrupted node/subscription transaction has a separate
[offline typed recovery procedure](NODE-RECOVERY.md). Native-job inspection does
not own its pending marker. Stop/quiesce, inspect and explicitly activate one
coherent current generation only when activation is admitted. A retained attempted
activation instead needs explicit `verify-existing` in signed0.4.6 with fresh
proof; never delete the marker or replay an unknown action.

For existing installations, optional standard mosdns and a Keenetic DNS profile are installed/configured separately. The explicit [fresh `--setup` route](FRESH-KEENETIC.md) prepares them only within its restricted capability matrix. Keenetic retains LAN port53/local names; VPN DNS uses the existing loopback Xray SOCKS/native pool, DIRECT DoH is independent of Xray. VPN names have no DIRECT fallback. Do not replace all firmware DNS with an Xray-only listener.

The panel derives domain decisions from native configs and installed geosite files, preserving first-match order. IP/protocol/port/other conditional traffic rules do not become generic DNS rules. Pending sets postpone sync. Native success triggers reconciliation; a minute observer detects visible external source changes while panel runs. Last successful DNS generation continues if the panel stops; startup reconciles afterward.

Check and synchronize inspects exact current state. Unchanged effective policy avoids service restart; unsupported/drifting inputs keep an explicit failure. Interrupted activation is independently inspected without replay. Retain the bounded previous owned generation/config for recovery. Never use a successful later probe to relabel an earlier unknown operation.

Historical [operator DNS qualification](qualification/independent-split-dns-2026-10-05.md) proved DIRECT/local availability with Xray stopped and protected-name failure without direct fallback. Its original static export procedure is superseded by [Issue125 integration](https://github.com/popiposter/xkeen-control/issues/125). Reboot, IPv6/all-provider outage and broader LAN failover remain separate [acceptance](ROADMAP.md).

## Panel release/update

Use only signed public GitHub Releases. Check the exact stable candidate in System/Releases; one approved Apply is a handoff, followed by independent version/source/channel, PID/executable/health and native-preservation verification. If ambiguous, inspect receipts; do not replay. Keep a bounded previous panel and config snapshot. [Release contract](RELEASES.md) describes signatures and public verification.

For publication: exact reviewed remote main, available version/tag, protected Release workflow exactly once, both build/publish success, fresh independent pinned-key/signature/manifest/size/hash/SHA256SUMS verification. The dual-platform release has exactly ten public files and two signed platform manifests; ARM64 releases through0.4.0 retain their exact seven-file contract. Verify the published set and both platforms as described in [Releases](RELEASES.md). A failed workflow is not repaired with a manual tag/Release or retried without a fresh corrected-source decision.

### Durable update outcomes (#162)

Signed [0.4.8](RELEASES.md) retains the following commands and the #162 update
contract introduced in 0.4.4. Published 0.4.3 does not provide them. Signed0.4.5
capability checks and installation passed on KN1810. Signed0.4.6 installation
also passed [independent readback](https://github.com/popiposter/xkeen-control/issues/168#issuecomment-6080313195).
Publication alone does not establish an installed generation.

```sh
xkeen-control self-update inspect
xkeen-control self-update inspect-installed
```

These are read-only. The first reports the durable update receipt; an unresolved
operation returns an error while retaining its evidence. The second binds the
fixed installed binary, init, helper and marker in an opaque digest. Neither
command resumes an operation or proves a transient candidate is installed.

The updater reserves an active intent before staging. It records phases before
mutations and permits only one transaction. `installed-verified` proves the
candidate tuple and readiness; `rolled-back-verified` proves restoration but
means the requested update failed. In generations containing #162,
`inspection-required`, a retained active intent, or an interrupted phase blocks
further mutations. Do not remove these files or retry Apply to clear the block.

Candidate startup has a total readiness budget of 30 seconds on ARM64 and
60 seconds on MIPS, with a separate bounded rollback startup phase. Polling does
not repeat service start. Acceptance checks the process identity, build, complete
installed tuple, marker and management health together. These bounds are tested
limits, not a promise of readiness on every appliance.

Durability requires a verified `sync -f` implementation supporting both files
and directories. The helper checks this capability before stopping the panel;
BusyBox `sync` alone may be insufficient. A missing prerequisite is a refusal,
not permission to skip synchronization or install packages automatically. Bounded
process checks also require `timeout -k` support. `flock` must support descriptor
locking, exclusive exclusion and downgrade to a shared lock; command presence
alone is insufficient. These capabilities are checked before reserving an update
and again before service shutdown; the bootstrap installer checks its
prerequisites before placing panel files.

Issue [#165](https://github.com/popiposter/xkeen-control/issues/165) adds
`xkeen-control self-update inspect-capabilities` in signed 0.4.5;
published 0.4.4 does not contain this diagnostic. It runs the same sync, timeout
and descriptor-lock checks on disposable RAM files and reports structured
`ready`/per-capability status. It takes shared admission on the existing setup
lock, without creating missing state, reserving an update, downloading files or
stopping services. Missing or incomplete setup state is a refusal. Run it with
the verified signed RAM CLI before planning another maintenance operation.

One 0.4.4 KN1810 maintenance attempt refused before reservation because the
firmware `/bin/sh -c` dropped positional arguments used by the Go flock probe.
Direct flock operations and script-file argument forwarding passed independent
diagnostics. The #165 fix invokes flock directly and preserves the same lock
semantics; it does not change firmware, XKeen or helper/init shebangs. That failed
attempt remains failed and must not be replayed.

For an affected older helper, use the explicitly approved maintenance delivery
described in [#162](https://github.com/popiposter/xkeen-control/issues/162).
Independently verify the signed release tuple before executing its candidate CLI
from protected RAM storage. Quiesce the old panel and competing update/native
work, retain the required rollback snapshot, inspect the installed digest using
that verified CLI, then invoke it once with
`self-update --maintenance DIGEST --apply VERSION`.
This entrypoint verifies itself and the helper against the same signed release,
rechecks the old installed tuple and hands ownership to the existing updater.
It does not install a helper in advance. Existing setup-lock exclusion and an
active intent protect cooperating panel processes; external native CLI/cron still
require operator quiescence. A rollback to an older generation also restores a
daemon, API and startup background workers that do not understand this intent.
Account for those writers explicitly during migration qualification; restored
health is not proof that their mutations are fenced. Inspect an older generation's
update receipt with the retained verified RAM CLI, not its unsupported old CLI.
After any outcome, inspect the receipt and actual state before restoring prior
services. Node recovery is a separate operation and remains blocked until the
installed generation is established.

## Operational evidence

[Ledger4](https://github.com/popiposter/xkeen-control/issues/4) retains exact evidence and unknown/no-replay outcomes. Public versions/counts/hashes are sanitized; credentials, raw endpoints/config, cookies, private logs and backups remain local. No opkg upgrade, reboot, sustained benchmark or generic shell repair by default. Historical Gate2/appliance procedures are [archived](archive/OPERATIONS-before-0.3.1-cleanup.md), not current native installation authority.
