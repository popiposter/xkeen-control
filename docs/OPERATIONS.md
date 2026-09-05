# Operations

This runbook describes the **currently production-qualified** control-plane generation. Slice D / Issue #2 remains complete, and D.1 / Issue #3 is production-qualified in signed stable `v0.2.0` from exact source `f170cdb0a9531cb8f4e08c95c0ba9bc8fe3dfd86` for `linux/arm64`. #4 is the current/next product slice and #5 remains planned; neither component lifecycle nor visual configuration behavior is deployed.

Production is a live router. Prefer typed/repository transactions over ad-hoc edits.

## Health / status

```sh
xkeen -status
xray api bi -s 127.0.0.1:10085 bal-proxy
/opt/etc/init.d/S99xkeen-control status
/opt/sbin/xkeen-control version --json
```

Control-plane generic health:

```text
GET /healthz
```

The panel is loopback or one exact trusted-LAN address only.

## Supported panel installation

For an Entware/Open Package-ready `linux/arm64` Keenetic, the currently qualified release-specific installer is:

```sh
sh -c "$(curl -fsSL https://github.com/popiposter/xkeen-control/releases/download/v0.2.0/install.sh)"
```

The installer requires root, `/opt`, `opkg`, supported architecture and bounded free space. It may install explicitly missing prerequisites but never performs blanket `opkg upgrade`. It does not install/repair XKeen or Xray and does not rewrite node/Xray/XKeen/routing/DNS/Observatory policy.

A fresh panel can generate a one-time setup credential. Existing auth/listener/state is preserved on rerun. Missing XKeen/Xray/configuration is a supported Setup Mode state.

Existing managed installs use the installed binary's pinned-signature update path. The known historical C.1 manual panel has a release-owned fingerprint-gated adoption bridge; unknown/partial legacy layouts fail closed. After successful typed D.1 `appliance adopt`, local `appliance.json` is the authority for supported non-secret policy. Before adoption, an existing router retains its explicit repository-derived/legacy policy boundary; adoption is not implicit and unknown/manual drift fails closed.

## Panel self-update / rollback

Normal update is product-owned and uses fixed GitHub release policy rather than arbitrary URLs. The installed binary verifies the signed manifest using the source-pinned Ed25519 trust anchor, checks architecture/compatibility/names/sizes/hashes, stages candidates under `/tmp`, then hands lifecycle control to the fixed external updater.

Persistent panel lifecycle paths are bounded:

```text
/tmp/xkeen-control/panel-update/                     candidate work
/opt/etc/xkeen-control/previous/panel/              one previous panel generation
/opt/etc/xkeen-control/state/installed-release.json compact success marker
/opt/etc/xkeen-control/state/update-policy.json     narrow deliberate policy
/opt/libexec/xkeen-control-updater                  fixed lifecycle helper
```

The updater supports only its fixed panel lifecycle operations. It is not a generic command runner/file manager. Readiness is generic health plus exact local version/source/channel and PID/path verification.

The `v0.1.1` production qualification proved:

```text
legacy exact
  -> signed adoption v0.1.1
  -> exact legacy rollback including helper absence
  -> signed re-adoption v0.1.1
```

During that sequence bounded non-secret fingerprints for auth/listener/node/Xray/XKeen/selection/benchmark state remained unchanged.

## Historical repository deployment

Repository deployment remains an advanced developer/operator path for deliberate development qualification, not the normal production distribution path:

```sh
cd /opt/etc/xkeen/repo
./scripts/deploy.sh
./scripts/verify.sh
```

Build `xkeen-control` off-router. Do not install Go/Node on Keenetic.

`deploy.sh` builds a complete `/tmp` candidate, renders outbounds from the local authoritative `nodes.json`, validates Xray, snapshots the active generation, swaps/restarts through the bounded foreground lifecycle and rolls back on failure.

Do not copy only a generated `04_outbounds.json` as a restore mechanism.

## Xray restart

Validate before deliberate restart:

```sh
xkeen -xtest
```

Repository lifecycle wrapper:

```sh
XKEEN_RESTART_TIMEOUT=60 ./scripts/run-xkeen-foreground.sh -restart
```

After Xray restart, runtime override/dynamic probe rules disappear. The supervisor waits for API readiness, validates the persisted stable target with a fresh targeted probe and reapplies only if safe. Otherwise native `leastPing` remains in control.

Do **not** run a full benchmark merely because Xray restarted.

## Stable selection / benchmark

Current behavior:

- active stable target probe: every 60 s, 1 KiB;
- two consecutive active failures clear stale override before replacement validation;
- Observatory quality: bounded 15-minute unique-sample window with materiality/persistence/dwell;
- sustained benchmark: once/day at 04:17 router-local, no catch-up;
- eligible set: all enabled canonical nodes up to 256;
- whole-run traffic budget: 2 GiB;
- per-node timeout: 10 s;
- per-node transient routing uses typed append-only loopback probe rules;
- high-churn state stays in RAM/`/tmp`.

Legacy XKeen Speed Balancer/watchdog/full-benchmark writers are disabled. Do not invoke `xkeen -sbt` as a routine operation.

## Node / subscription operations

Use the authenticated panel's typed preview/apply flow for node import/replace, named subscription refresh, enable/disable/remove and related operations.

The authoritative registry is:

```text
/opt/etc/xkeen-control/secrets/nodes.json
```

It is secret material. Never print/copy its contents into public logs/issues/PRs.

Node mutations build and validate a complete Xray candidate, snapshot the logical generation, activate, wait for readiness/inventory and roll back coherently on failure. Node-only operations must not regenerate unrelated routing/DNS/Observatory policy.

## Panel service

Current installed service files include:

```text
/opt/sbin/xkeen-control
/opt/etc/init.d/S99xkeen-control
/opt/libexec/xkeen-control-updater
```

Default listener:

```text
127.0.0.1:8787
```

For local browser access, an SSH tunnel remains safe:

```sh
ssh -L 8787:127.0.0.1:8787 <router>
```

An exact private LAN bind may be configured; wildcard/public/hostname binds fail closed. No WAN firewall opening is required or authorized.

## Current distribution / update authority

GitHub Releases from `popiposter/xkeen-control` are software distribution authority. Stable/beta publication must continue to use the protected release workflow from an exact current reviewed `main` SHA; raw `main`, branch archives and Actions artifacts are not install/update authorities.

The router requires no GitHub write token. Normal managed updates use the compiled source-pinned public key. First-install trust begins with GitHub HTTPS plus exact release-internal manifest/hash/size checks.

## D.1 appliance state and backup / restore

D.1 is production-qualified in signed stable `v0.2.0`. Successful typed `appliance adopt` establishes local schema-versioned `appliance.json` as the authority for supported non-secret policy. Managed DNS/routing/Observatory policy derives deterministically from it; active `04_outbounds.json` remains generated from the authoritative `nodes.json`. Adoption proves compatibility with fixed companion policy and fails closed on unknown/manual drift.

For an existing router that has not yet adopted the appliance authority, use the typed sequence deliberately:

```sh
/opt/sbin/xkeen-control appliance adopt
/opt/sbin/xkeen-control appliance validate
/opt/sbin/xkeen-control appliance verify
```

`adopt` must complete first and is the only step that creates `appliance.json`; it is designed to prove compatibility before that authority write and must not mutate active runtime policy. `validate` checks the stored typed authority, and `verify` proves the current generated/fixed/runtime generation still corresponds to the adopted authority. If adoption or verification reports incompatibility or unknown/manual drift, stop and investigate through typed/repository paths; do not normalize raw config ad hoc or treat the one-time production qualification normalization as a generic repair procedure.

Safe export is authenticated and excludes VPN/subscription secrets by default. Secret-bearing export requires explicit re-authentication and a passphrase, uses a bounded Argon2id/XChaCha20-Poly1305 envelope and is not persisted by the panel. Any backup containing `nodes.json` remains secret operator material.

Restore is authenticated, same-origin/CSRF protected, session-bound, bounded and preview-first. Apply uses typed validation, authority coordination, a transaction journal and interrupted-import recovery. An equivalent settings-only restore is a no-op that preserves `nodes.json`, generated policy and runtime state without an Xray/XKeen restart. The adapter does not expose raw JSON, filesystem, archive or command surfaces.

## D.2 component lifecycle (#4) — F1/F2 source complete, not deployed

PR #39 completes the source-main Components / Updates UI over the existing F1
Preview/Apply/Rollback/Cancel broker. Source now includes session-safe one-shot
operations, truthful unknown outcomes, stable component error codes and
Coordinator lifecycle hints. There is no component scheduler, automatic
install or Setup Mode repair. The production-qualified baseline remains D.1
`v0.2.0`; source merge does not authorize exposing or invoking new routes on it.

Q0 in [Issue #4](https://github.com/popiposter/xkeen-control/issues/4) adds the
existing browser suite to release qualification and the Q1 protocol below.
This is **source-only / pending authorization**: Q0 has no router access or
release publication. All Q1 gates start as `NOT RUN`; this text grants no live
permission. A missing prerequisite is `BLOCKED`, never a synthetic PASS.

Q1 separates read-only preflight, exact signed trial publication, panel
installation and per-component update/rollback authorization. Use existing
#2 explicit-version signed release paths and F1/F2 typed component paths only.
Before a trial panel exists, the stable panel has no F1/F2 component endpoints;
initial preflight must use the existing safe observation/typed D.1 boundary.
Panel HTTP 202 is handoff acceptance, not verified completion. A lost component
response is an unknown outcome, not evidence of success or restoration.

Qualify one exact component update/rollback pair at a time, prove authority and
unrelated-state preservation, and settle component journals before rolling the
panel back. Panel rollback does not restore component binaries or geodata.
Bound resource collection and account for staging, retained copies and RAM-backed
`/tmp`; do not add a monitoring agent or infer flash writes from hashes alone.
No forced kill, power-loss, reboot, sustained benchmark or generic repair belongs
to the live trial. #5 and automated component lifecycle remain later work.

### Q1 record and finite observation boundary

Keep an operator-local record for each gate: authorization, UTC/router-local
window, reviewed source SHA/tree, signed version/channel, manifest SHA-256,
baseline/candidate/rollback identities, operation count, resource decision,
actual result and evidence limitations. Publish only sanitized versions,
counts, state transitions and non-secret hashes. Hostnames, raw status/node
responses, cookies, tokens, cron contents and request material stay local.
Disable terminal/session recording and shell history for authenticated work;
use the logged-in private browser or a root-only temporary request file in
host RAM. Never put credentials/tokens in URLs or command arguments/history.
Remove temporary request material after settlement, not journals or generations.

Use a host timeout of 30 seconds for each read-only shell observation, a
64 KiB output cap per observation, and at most 1 MiB per operation record.
Truncation, timeout, unsupported OS counters or incomplete traversal must be
recorded; do not treat partial evidence as proof. Hash manifests are collected
only at settled boundaries, with at most 8192 entries / 320 MiB read per
manifest and 120 seconds elapsed. First inventory the fixed roots' metadata;
stop before hashing if the budget cannot cover them. No recursive scan of
all `/opt`, no raw config dumps and no unbounded `tail -f`.

### Gate 1 — separately authorized read-only baseline

Schedule outside 04:17 router-local, with enough time for the entire authorized
pair and panel return. Confirm the configured exact private listener locally;
do not assume loopback if the service uses an exact LAN address. Use the
existing management connection `<operator-local-router>`; create no WAN rule.
On the router, run each command once within the observation limits:

```sh
date
uname -m
/opt/sbin/xkeen-control version --json
/opt/etc/init.d/S99xkeen-control status
/opt/sbin/xkeen-control appliance validate
/opt/sbin/xkeen-control appliance verify
df -Pk /tmp /opt /opt/sbin /opt/etc/xkeen-control
awk '/^(MemTotal|MemAvailable|MemFree|Buffers|Cached|SwapFree):/' /proc/meminfo
```

Expected: ARM64, verified installed D.1 identity (compare to the actual signed
baseline, not only this document), private health reachable, both typed checks
successful. Do not call `adopt`, `render`, upstream info/install/repair or
bootstrap as diagnostics. Before the trial, no `/api/v1/components*` route is
assumed to exist. Missing adoption or unknown/manual layout blocks the trial.

Use `stat -c '%F %a %s %d' <fixed-path>` and `sha256sum <fixed-file>` locally
to create an existence/type/mode/size/device/hash baseline. Reject symlinks or
unexpected objects before hashing. Include `config/appliance.json`,
`secrets/nodes.json`, `auth/password.bcrypt`, `listen-address`,
`state/update-policy.json`, `state/installed-release.json` under
`/opt/etc/xkeen-control`; `/opt/etc/xray/configs` (all fixed policy files,
including generated outbounds); `/opt/sbin/xray`, `/opt/sbin/xkeen`,
`/opt/sbin/.xkeen`, `/opt/etc/init.d/S05xkeen`, the panel binary/helper/service,
`/opt/etc/xray/dat`, `/opt/etc/xkeen/xkeen.json`, the existing
`/opt/etc/init.d/S24xray` (if present), and relevant opkg/cron metadata within
the same manifest budget. Secret-file fingerprints remain local; publish only
their unchanged/not-proven result. Record absence explicitly, including the
XKeen generation marker; do not create it.

Check existence/type/size only, without deleting or dumping, for
`/opt/etc/xkeen-control/state/component-transaction.json`,
`state/appliance-import-transaction.json`, component previous directories
`/opt/etc/xkeen-control/previous/components/{xray,geodata,xkeen}` and their
`.staging`/`.old` siblings, `/tmp/xkeen-control/components/{xray,geodata,xkeen}`,
`/opt/etc/xray/dat/.xkeen-geodata-transaction`, and
`/opt/sbin/.xkeen-control-activation`, and
`/opt/etc/xkeen-control/state/xkeen-generation.json.staging`. Also inspect node/panel pending state
using the installed generation's fixed paths. A retained previous generation
alone is normal; journal/staging/activation residue or unclassified state is
NO-GO until its owner and settlement are proven.

Inspect at most 32 relevant cron/init/deployment configuration files, each
at most 64 KiB, in a private bounded session: `/opt/etc/crontab`,
`/opt/etc/cron.d`, `/opt/var/spool/cron/crontabs`, `/opt/etc/init.d` and the
operator's active deployment/package job list. Review only relevant entries
for `update-geodata`, `deploy.sh`, upstream geodata updates, legacy balancer,
watchdog and benchmark writers, including scripts those entries call. Do not
publish raw crontabs, environments or process command lines. Report owned
entry counts, schedules and active/inactive/unknown conclusions. Unknown
writers or an overlapping deploy/package job block the trial. An active writer
requires a separate proposal naming its exact entry, local rollback snapshot
and reversible disable/restore operation; never stop all cron or grep-delete
entries. In-process locks cannot exclude external writers.

### Gate 2 — freeze, publish, then separately authorize panel installation

After Q0 review/merge, on the host run `git fetch origin main`,
`git rev-parse origin/main` and `gh run list --workflow ci.yml --commit <SHA>`;
inspect the successful run's actual checkout/tree. Freeze that reviewed current
SHA, not the earlier F2 merge. Check proposed tag availability with
`git ls-remote --tags origin refs/tags/v<version>` and
`gh release view v<version> --repo popiposter/xkeen-control` (distinguish not
found from access/network failure). `0.3.0-beta.1` is an example only, not an
allocated or approved version. Record compatibility and exact original panel
rollback identity before requesting publication authorization.

Only after that authorization, use GitHub Actions → Release → Run workflow
with `source_ref=<frozen-SHA>`, `channel=beta`, `version=<approved-version>`.
Do not dispatch it to test Q0. If main moves, stop and re-review. On the host,
download that exact public release into a new empty directory; allow only
seven assets: `xkeen-control-linux-arm64`, `S99xkeen-control`,
`xkeen-control-updater`, `install.sh`, `release-manifest.json`,
`release-manifest.sig`, `SHA256SUMS`. Bound retrieval to 10 minutes and
128 MiB total; exceeded limits are BLOCKED. Using the verifier compiled
off-router from the reviewed source and a local file containing its pinned
public key, run:

```sh
xkeen-release verify-pinned-key --public-key-file <public-key-file>
xkeen-release verify --manifest <dir>/release-manifest.json --signature <dir>/release-manifest.sig --public-key-file <public-key-file>
xkeen-release verify-assets --manifest <dir>/release-manifest.json --asset-dir <dir>
```

Also check `sha256sum -c SHA256SUMS` in that directory and compare manifest
`sourceCommit`, `version`, `channel`, `os=linux`, `architecture=arm64` and
compatibility to the authorization record. Checksum success alone is not
signature verification. No Actions binary, raw-main installer, fixture key,
key rotation or verification bypass is an installation alternative.

Publication grants no install permission. After separate exact-artifact
installation and original-panel rollback authorization, use authenticated
same-origin API requests with JSON content type and `X-CSRF-Token` from the
current session (session cookies remain in the browser):

| Action | Existing route / exact JSON body | Expected result |
| --- | --- | --- |
| Record saved policy/current generation | `GET /api/v1/update` | Safe local projection; baseline policy fingerprint recorded |
| Explicit trial check | `POST /api/v1/update/check`, `{"channel":"beta","version":"<approved-version>"}` | Exact verified signed candidate |
| Approved install, once | `POST /api/v1/update/apply`, same body | 202 is handoff acceptance only |
| Approved final panel return, once | `POST /api/v1/update/rollback`, no body | Accepted handoff; independently verify completion |

Do not call `/update/policy`. The explicit operation channel does not change
the saved policy. Allow at most 60 health observations, ten seconds apart,
each with a five-second request deadline, after panel handoff. Then verify
version/source/channel, service status and actual executable:
read the numeric `/opt/var/run/xkeen-control.pid`, validate digits only, and
`readlink /proc/<pid>/exe`; require the expected `/opt/sbin/xkeen-control`
path and compare `/proc/<pid>/exe` hash to the signed artifact. A healthy old
process, 202, missing response or expired observation window is not a completed
swap. Stop without a second install/restart if identity remains unresolved.

### Gate 3 — trial-panel read-only smoke and candidate approval

Log in again after restart. Open Components / Updates once for lazy inventory,
then explicitly Check one class. Confirm missing/unknown/capability and
maintenance presentation; do not poll inventory or Check. For smoke, select
Preview update, inspect identity and Cancel; repeat at most once per class.
No Apply is authorized by smoke or cached eligibility.

| Intent | Exact F1/F2 action / API contract |
| --- | --- |
| Check | `POST /api/v1/components/check`, `{"component":"xray","channel":"stable"}` |
| Update preview | `POST /api/v1/components/preview`, `{"component":"xray","operation":"update","channel":"stable"}` |
| Cancel | `POST /api/v1/components/cancel`, `{"previewToken":"<RAM-only-token>"}` |
| Approved update | `POST /api/v1/components/apply`, token-only body as above |
| Fresh rollback preview | `POST /api/v1/components/preview`, `{"component":"xray","operation":"rollback"}` |
| Approved rollback | `POST /api/v1/components/rollback`, fresh token-only body |

Substitute `geodata/stable` or `xkeen/dev` only as authorized. F2 handles these
bodies and token/session lifecycle; prefer its Preview → Confirm and Cancel
controls. Preview expires in five minutes. A new token must still match the
approved exact identity; changed metadata requires renewed identity approval.

Record Preview candidate/previous identity without its token. Independently
qualify trusted bytes off-router before authorizing a real transition:
Xray's fixed official stable ARM64 asset and digest, strict ZIP layout and
extracted binary digest; all six geodata files and their exact digest/size/set
identity (`geosite_refilter.dat`, `geosite_v2fly.dat`, `geosite_zkeen.dat`,
`geoip_refilter.dat`, `geoip_v2fly.dat`, `geoip_zkeenip.dat`); XKeen's catalog
build `e461c4e9964fb8ac78e5fe01aa2e27ab980af712`, source parent
`bb4060d6a87364eff8314fa723a168454df372bd`, blob
`e6218668692c41565d288bf3a0bc6a420650edbd`, archive SHA-256
`efbcd977321c35191cb8d31ee5209e5911b81225352c071bad99894b3d0ccc66`
and catalog member/generation identity. Use the fixed source mapping in
`internal/components/inventory.go` and the reviewed core/catalog rules;
no arbitrary download URL or candidate execution. Limit this host qualification
to one candidate per class, ten minutes, and the byte/member ceilings below.
Missing independent proof is BLOCKED; moving-dev rejection cannot be bypassed
by manually installing the pinned build. Same-generation/no-op is not a
successful update/rollback qualification.

### Gate 4 — one exact pair, settlement, then the next class

Default order is Xray → complete rollback → geodata → complete rollback →
XKeen → complete rollback. Obtain a separate approval per exact pair, permitting
one Apply and one explicit Rollback plus ordinary automatic recovery on failure.
Never leave one class changed while diagnosing another. Use only F2/F1 paths
above: no private engine calls, path environment overrides, `-ux/-uk/-i/-fixed`,
`deploy.sh`, `update-geodata.sh` or manual active-file copies.

For component requests allow 540 seconds client-side: the server's synchronous
window is 8m45s including independent recovery. Never abort a transaction to
end sampling. At each settled boundary require the typed result, one explicit
inventory refresh, cleared journal/staging state, exact active bytes/generation
against the independent candidate or baseline manifest, successful `appliance
validate` and `appliance verify`, and unchanged protected fingerprints from
Gate 1. The core preserves one bounded previous generation; verify its identity
in a fresh rollback Preview before confirming. A settings backup is not a
component rollback snapshot. Do not duplicate whole `/opt` or large generations
without explicit space accounting. Optional secret backup uses only the
separately authorized encrypted D.1 contract.

Once per settled boundary inspect authenticated `/api/v1/status`,
`/api/v1/nodes`, `/api/v1/performance` locally and run
`xray api bi -s 127.0.0.1:10085 bal-proxy` within the read-only limits. Compare
RoutingService/bal-proxy inventory and probe availability to baseline; allow
at most one additional observation after 60 seconds for supervisor convergence.
Require safe validated selection or native leastPing fallback, not identical
health counters or a naturally reselected target. Unexpected policy changes,
missing inventory/probes or inconclusive convergence stop the next operation.

Default final state: all component baseline bytes restored and journals settled,
then the already-authorized panel rollback and Gate 2 identity verification.
Panel rollback cannot undo component updates and must not run over an unresolved
component journal. Keeping/reapplying the trial panel, stable promotion or
additional attempts require a new decision. Mark each class separately
PASS/BLOCKED/NOT RUN; keep #4 open, #5 inactive, and F3/G deferred.

### Gate 5 — resource admission and bounded measurements for every pair

Before mutation, record baseline sizes and a per-filesystem worksheet. Let A
be compressed archive bytes, C extracted candidate bytes, B active baseline
bytes. Record already-retained generations separately (already-used space is
not free). Candidate/config snapshots, validator processes, activation copies,
journals/markers and preserved-state reads must all fit alongside the running
panel/Xray. Check both update and rollback demand, not compressed size alone.

| Core | Existing minimum free-space check (not actual memory use) |
| --- | --- |
| Xray | A + 2 × 64 MiB + 8 MiB at staging, active and previous parents |
| Geodata | 2C + B + 8 MiB at staging, dat and previous parents |
| XKeen | Group by device: staging A+C, previous B, activation C, marker parent; add 8 MiB per distinct filesystem |

Xray rejects ZIP expansion over 128 MiB / binary over 64 MiB; geodata rejects
candidate aggregate over 128 MiB; XKeen archive is capped at 8 MiB and extracted
generation at 16 MiB (the current pinned artifact is much smaller). Inventory's
256 MiB read budget is not 256 MiB RSS. Check actual simultaneous copies from
`checkFreeSpace` and staging/activation code at the frozen source. Xray/geodata
checks are conservative per-path checks; do not mistake them for a complete
shared-filesystem/RAM admission calculation. Account for shared mounts by
device and `/tmp` tmpfs consumption against available RAM, plus archive buffers,
validator/Xray overlap and operational margin. Record an explicit operator
headroom GO/NO-GO; unavailable measurements or insufficient reserve block
mutation, without raising limits.

Use only host-stored samples, at most 60 idle and 600 operation samples at
one-second intervals, with a 1 MiB output cap per operation. Sample numeric
`/proc/<pid>/status` fields `VmRSS`, `VmHWM`, `VmSize` and
`/proc/<pid>/io` fields `write_bytes`, `cancelled_write_bytes` for the panel and
at most eight `pidof xray` processes (including transient validators when
observed); refresh PID identities without reading command lines. Sample
`MemAvailable` (or report unavailable), `df -Pk /tmp /opt` and bounded device
write counters. No per-sample inventory, hash, tree scan or flash output file.
Before/peak/after report panel/Xray/validator memory, minimum available RAM and
free space, process churn, missed short-lived validators and write deltas.
Counter absence is an explicit measurement limitation, never zero writes.
Ending the observation window only stops collection, not the operation.
Content equality cannot prove zero writes; OS write counters do not measure
physical flash wear. Separate expected journal/generation/marker writes and
normal runtime telemetry from unauthorized persistent policy changes.

### Failure / lost-response stop point

`preview-stale`, unsupported source/layout, authority drift, unsafe listener,
pending journal, unknown writer or insufficient headroom stops before a new
mutation. EOF, timeout or lost response means **outcome unknown**. Do not replay
the consumed token or start a competing rollback; inventory/health alone cannot
prove settlement. After the original request window, perform one bounded
read-only Gate 1/4 inspection and preserve journal/previous/activation state.

Recovery is the existing core's independent failure recovery and local-only
startup recovery in `cmd/xkeen-control/main.go`; there is no recovery HTTP API
or component recovery CLI. Startup can stop before HTTP exists. If the process
has exited, a separately authorized single `S99xkeen-control start` may invoke
that typed startup path after read-only inspection proves no live operation;
it is not permission for a restart loop. If journal outcome remains unproven,
stop, retain evidence and return the prerequisite to issue #4. Never delete
journals, downgrade the panel opportunistically, reinstall broadly or provoke
power-loss/kill/flash-full failure on the router. Off-router fault-injection
fixtures remain the deliberate failure proof; naturally encountered live
recovery must be verified, not manufactured.

## Geodata

The source D.2 core manages the complete product geodata set transactionally.
Historical `scripts/update-geodata.sh` still replaces active files individually
with fallback mirrors, and `scripts/deploy.sh` invokes it. Neither script is a
D.2 qualification shortcut or part of the typed component transaction boundary.

Before any authorized live component trial, inspect relevant external writers
without publishing raw cron/process details. Source script presence is not
proof that a job is active. An active competing geodata/legacy writer blocks the
trial until its exact entry and reversible change are separately authorized;
do not stop all cron or delete unrelated jobs. Q0/F2 add no scheduler or automatic
mutation, and the signed stable baseline is not relabeled as product geodata
ownership merely because its replacement code merged.

## Logs / diagnostics

Keep diagnostics bounded and sanitized. Useful local logs may include:

```text
/opt/var/log/xray/error.log
/opt/var/log/xray/access.log
/opt/var/log/xkeen-control.log
```

Never paste unbounded logs or raw upstream errors into public GitHub; they can contain infrastructure/secret context.

## Backups

Repository deploy backups under `/opt/backups/...` can contain secret-bearing generated runtime state and must be treated as secrets.

D.1 portable backups have a separate typed/encrypted contract; do not confuse them with raw deploy snapshots. Safe export is the default for portable transfer, while secret-bearing export remains explicit operator material.

## Prohibited routine actions

Unless the active issue/operator explicitly authorizes the exact operation, do not run:

```text
opkg upgrade
reboot
credential rotation
full/sustained benchmark
unbounded log/download collection
KeeneticOS update
```
