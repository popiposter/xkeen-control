# Existing Ultra KN1810 — panel-only installation

Operator baseline for [Issue150](https://github.com/popiposter/xkeen-control/issues/150):
KeeneticOS5.1.7, `uname -m: mips`, Entware `mipsel-3.4`/`mipsel-3.4_kn`,
stock XKeen2.0.1 Beta/Xray26.7.28. Reported memory values indicate about249MiB
RAM/105MiB available and1GiB swap; these are not panel hardware qualification.

Signed stable [0.4.1](https://github.com/popiposter/xkeen-control/releases/tag/v0.4.1)
includes the MIPS soft-float panel. Source review, hosted FULL, exact ten-file
public verification, both signatures and actual published MIPS emulated startup
passed; see [release evidence](RELEASES.md). KN1810 hardware installation,
RSS/auth/PTY/native preservation and LAN behavior remain NOTRUN. Stable0.4.0
remains ARM64-only. No unsigned binary copy, architecture override, signature
bypass or modification of0.4.0 assets. Fresh
`--setup` remains unsupported for KN1810 and existing installations.

## Before installing a signed MIPS release

1. Connect locally to Entware root shell. Confirm model/firmware, little-endian
   Entware target, free `/opt` storage, curl/jq/sha256sum/stat and current service
   state. Do not expose SSH or panel in WAN; do not print native secrets.
2. Inspect privately whether a panel/init/updater/marker or interrupted setup
   already exists. An unexpected/partial layout is a blocker, not a reinstall.
3. Identify the actual `speed-failover-watchdog` script, its cron/init launch and
   state. One missing conventional path does not prove absence. Record privately
   whether it edits native config, uses balancer overrides or restarts Xray.
4. Take a bounded protected snapshot of affected existing config, native init/
   cron/watchdog and optional managed node registry; retain it off-router in a
   private location. Record file hashes and pre-install native runtime state.
   Do not export `auth/sessions.json` or upload raw config/cron/backups to GitHub.
5. Run that exact signed release's ordinary panel-only installer, without setup.
   It installs the MIPS panel/init/updater and missing panel prerequisites only;
   it does not upgrade XKeen/Xray or rewrite routing, DNS, HOME policy/watchdog.
6. Keep loopback8787 and use an SSH tunnel for first login. Retain the private
   bootstrap password locally. Independently verify version/source/channel,
   PID/executable/health/auth, RSS and unchanged native files/runtime. A successful
   installer exit alone does not establish hardware/network acceptance.

Only after the inventory and protected snapshot above, run the published
panel-only installer:

```sh
xkeen_installer=$(curl -fsSL https://github.com/popiposter/xkeen-control/releases/download/v0.4.1/install.sh) && sh -c "$xkeen_installer"
```

The initial installer trusts GitHub HTTPS and checks manifest/size/hash consistency;
installed self-updates verify the pinned Ed25519 signature. Do not confuse these
trust steps. After installation, `/opt/sbin/xkeen-control version --json` must
report0.4.1/stable/source `68d7ed356980897fc921001375b24cf2af10b90c`.
The MIPS binary SHA256 is
`3d16a7975ba0d52b9d6f7081189af8f3915282efb60ed0be247dddc5892bf9b5`.
Keep private credentials and backup contents out of chat/public evidence.

## Validation and prerequisite repair in source (#154)

The source repair under #154 gives linux/mipsle full Xray validation 120 seconds
and configured Apply preparation 125 seconds; other targets retain 45/50 seconds.
Node transactions allow 375 seconds on MIPS (300 elsewhere), with activation and
rollback each retaining 120 seconds. Validation writes and configured start/restart
wait up to 150 seconds in the UI; lost responses require inspection, never replay.
These changes do not amend published0.4.1 bytes or establish hardware acceptance.

Bootstrap checks selected stat -c metadata and jq buildinfo/regex functionality
before downloads or placement. Missing stat maps to coreutils-stat; an existing
incompatible stat requires explicit prerequisite repair. Existing jq without regex
is refused with a jq-full instruction; automatic removal/force-overwrite is not
allowed. A functional existing jq is accepted regardless of package name. Guided
setup still requires preinstalled tools and remains ARM64-only.

## Before configuring nodes or routing

Panel-managed profiles use the private node registry; unmanaged native outbounds
remain preserved. Do not restore only generated outbounds over a different
registry. Decide explicitly whether to import sources or transfer an encrypted
panel export; preserve existing routing/DNS unless the operator requests a change.

Inspect the watchdog before native configuration Apply. If it is a competing
writer, back up its exact launch entry and quiesce that entry at the controlled
transition. Do not delete unrelated cron jobs or run the historical blanket
`install-watchdog.sh`. The panel does not install a replacement automatic
supervisor: native Xray owns selection/health/failover, and panel throughput
measurements propose costs without automatically pinning/restarting.

Existing stock2.0.1 command capability is discovered independently; an unfamiliar
command stays unavailable. Updating native components is a separate explicit
operation using supported stock commands with its own snapshot/readback.

## Recovery and evidence

Inspect receipts/current state after an unknown outcome; never automatically
repeat install/Apply. Managed panel update retains one same-platform previous
generation; ARM64 previous bytes cannot be restored on MIPS. First-install
recovery inspects and removes only confirmed newly created panel-owned paths or
restores bounded affected files; no full-router restore/native downgrade/reboot.

Record source tests, emulation, signed publication, hardware installation and
independent LAN/native failure checks separately. This preparation does not
complete fresh hardware#148, LAN#133 or encrypted A-to-B transfer#135.

[Operations](OPERATIONS.md) · [Node lifecycle](NODE-LIFECYCLE.md) ·
[Release trust](RELEASES.md) · [Security](../SECURITY.md)
