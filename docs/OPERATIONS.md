# Operations

Use [native contract](NATIVE-XKEEN.md) and [quick start](QUICKSTART-RU.md). Stock XKeen owns service/interception/components and cron; the panel stays alongside it. No native admission/profile workers, panel component updater or legacy Setup takeover.

## Configuration and native commands

Inspect the current native status and pending config set before mutations. Save validates the complete Xray candidate; Apply restarts through the native job. Require terminal completed/configuration applied plus fresh process/config/health readback. HTTP202 is acceptance only. Use Discard before Apply or explicit previous restore afterward; no automatic repeated action after an unknown result.

Components and schedules use allowlisted native commands. Interactive prompts remain visible in the command-bound console. External CLI/cron do not share the panel lease: quiesce competing operations and check drift instead of assuming a global lock. Private console output must not be uploaded to public evidence.

## Independent LAN DNS

For existing installations, optional standard mosdns and a Keenetic DNS profile are installed/configured separately. The explicit [fresh `--setup` route](FRESH-KEENETIC.md) prepares them only within its restricted capability matrix. Keenetic retains LAN port53/local names; VPN DNS uses the existing loopback Xray SOCKS/native pool, DIRECT DoH is independent of Xray. VPN names have no DIRECT fallback. Do not replace all firmware DNS with an Xray-only listener.

The panel derives domain decisions from native configs and installed geosite files, preserving first-match order. IP/protocol/port/other conditional traffic rules do not become generic DNS rules. Pending sets postpone sync. Native success triggers reconciliation; a minute observer detects visible external source changes while panel runs. Last successful DNS generation continues if the panel stops; startup reconciles afterward.

Check and synchronize inspects exact current state. Unchanged effective policy avoids service restart; unsupported/drifting inputs keep an explicit failure. Interrupted activation is independently inspected without replay. Retain the bounded previous owned generation/config for recovery. Never use a successful later probe to relabel an earlier unknown operation.

Historical [operator DNS qualification](qualification/independent-split-dns-2026-10-05.md) proved DIRECT/local availability with Xray stopped and protected-name failure without direct fallback. Its original static export procedure is superseded by [Issue125 integration](https://github.com/popiposter/xkeen-control/issues/125). Reboot, IPv6/all-provider outage and broader LAN failover remain separate [acceptance](ROADMAP.md).

## Panel release/update

Use only signed public GitHub Releases. Check the exact stable candidate in System/Releases; one approved Apply is a handoff, followed by independent version/source/channel, PID/executable/health and native-preservation verification. If ambiguous, inspect receipts; do not replay. Keep a bounded previous panel and config snapshot. [Release contract](RELEASES.md) describes signatures and public verification.

For publication: exact reviewed remote main, available version/tag, protected Release workflow exactly once, both build/publish success, fresh independent seven-asset pinned-key/signature/manifest/size/hash/SHA256SUMS verification. A failed workflow is not repaired with a manual tag/Release or retried without a fresh corrected-source decision.

## Operational evidence

[Ledger4](https://github.com/popiposter/xkeen-control/issues/4) retains exact evidence and unknown/no-replay outcomes. Public versions/counts/hashes are sanitized; credentials, raw endpoints/config, cookies, private logs and backups remain local. No opkg upgrade, reboot, sustained benchmark or generic shell repair by default. Historical Gate2/appliance procedures are [archived](archive/OPERATIONS-before-0.3.1-cleanup.md), not current native installation authority.
