# Existing Ultra KN1810 — panel-only installation

Operator baseline for [Issue150](https://github.com/popiposter/xkeen-control/issues/150):
KeeneticOS5.1.7, `uname -m: mips`, Entware `mipsel-3.4`/`mipsel-3.4_kn`,
stock XKeen2.0.1 Beta/Xray26.7.28. Reported memory values indicate about249MiB
RAM/105MiB available and1GiB swap; these are not panel hardware qualification.

Signed stable [0.4.9](https://github.com/popiposter/xkeen-control/releases/tag/v0.4.9)
includes the MIPS soft-float panel. Source review, hosted FULL, exact ten-file
public verification, both signatures and actual published MIPS emulated startup
passed; see [release evidence](RELEASES.md). Signed0.4.5 installation and native
preservation passed on KN1810; its node activation failed with retained intent.
Signed0.4.6 and0.4.7 installation and their separately verified recovery settlements passed. A later automatic subscription transaction retained a previous-branch readiness failure;0.4.8 delivered that repair. Signed0.4.9 installation and one bounded manual speed test passed on KN1810. A new subscription transaction under0.4.9 and broader RSS/auth/PTY/LAN acceptance remain incomplete. Stable0.4.0
remains ARM64-only. No unsigned binary copy, architecture override, signature
bypass or modification of0.4.0 assets. Fresh
`--setup` remains unsupported for KN1810 and existing installations.

For pre-release hardware qualification of [Issue188](https://github.com/popiposter/xkeen-control/issues/188), an exact-HEAD MIPS **test binary** may be copied to a private disposable path under `/opt` and run against synthetic data. Record its hash, bound execution and remove only its owned files after checking that no child remains. This does not install or replace the panel, invoke live XKeen/Xray, read production credentials or configuration, or authorize native Apply. The prohibition on unsigned **panel installation** above still applies; a successful synthetic run proves MIPS algorithm execution, not live automatic selection or application. Live acceptance follows a verified signed installation with a fresh state snapshot.

## Before installing a signed MIPS release

1. Connect locally to Entware root shell. Confirm model/firmware, little-endian
   Entware target, free `/opt` storage, curl/jq/sha256sum/stat, compatible flock,
   file/directory `sync -f`, bounded `timeout -k` and current service
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
xkeen_installer=$(curl -fsSL https://github.com/popiposter/xkeen-control/releases/download/v0.4.9/install.sh) && sh -c "$xkeen_installer"
```

The initial installer trusts GitHub HTTPS and checks manifest/size/hash consistency;
installed self-updates verify the pinned Ed25519 signature. Do not confuse these
trust steps. After installation, `/opt/sbin/xkeen-control version --json` must
report0.4.9/stable/source `771df6c35e303fa8842fb5c76503db62e1eec238`.
The MIPS binary SHA256 is
`f30de1e938541927f31903355b78080f550b1de518745f2a969d2ce0840365ae`.
Keep private credentials and backup contents out of chat/public evidence.

For an existing older panel helper, 0.4.9 provides the separate
[verified maintenance delivery](OPERATIONS.md#durable-update-outcomes-162) path.
The observed 0.4.3 attempt rolled back to 0.4.2; do not replay that old updater
or treat a transient candidate version as successful installation. Actual 0.4.9
installation and one bounded manual speed test passed; the next controlled
subscription transaction under0.4.9 remains untested.

## Validation and prerequisite repair delivered in0.4.2 (#154)

The source repair under #154 gives linux/mipsle full Xray validation 120 seconds
and configured Apply preparation 125 seconds; other targets retain 45/50 seconds.
Node transactions allow 375 seconds on MIPS (300 elsewhere), with activation and
rollback each retaining 120 seconds. Validation writes and configured start/restart
wait up to 150 seconds in the UI; lost responses require inspection, never replay.
Published0.4.1 bytes remain immutable;0.4.2 hardware acceptance is separate.

Bootstrap checks selected stat -c metadata and jq buildinfo/regex functionality
before downloads or placement. Missing stat maps to coreutils-stat; an existing
incompatible stat requires explicit prerequisite repair. Existing jq without regex
is refused with a jq-full instruction; automatic removal/force-overwrite is not
allowed. A functional existing jq is accepted regardless of package name. Guided
setup still requires preinstalled tools and remains ARM64-only.

## Before configuring nodes or routing

Stable 0.4.3 adds [constrained resource limits](ROUTER-RESOURCES.md): automatic
speed comparisons are disabled on MIPS; explicit single-node tests are bounded
to 4 MiB / 20 seconds, comparisons to 24 MiB / 90 seconds. Pressure admission
and cancellation preserve the existing cleanup owner. These conservative limits
are not a measured line-rate guarantee; controlled hardware acceptance is pending.
Interrupted node transactions use the separate [offline recovery](NODE-RECOVERY.md)
procedure, never marker deletion or automatic replay.

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
