# Operations

This runbook describes the **currently production-qualified** control-plane generation. Slice D / Issue #2 is complete: signed public releases, bounded bootstrap and transactional panel update/rollback are current behavior for qualified `linux/arm64`. D.1 / Issue #3 is the next product slice; portable appliance backup/import/export is not available yet.

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
sh -c "$(curl -fsSL https://github.com/popiposter/xkeen-control/releases/download/v0.1.1/install.sh)"
```

The installer requires root, `/opt`, `opkg`, supported architecture and bounded free space. It may install explicitly missing prerequisites but never performs blanket `opkg upgrade`. It does not install/repair XKeen or Xray and does not rewrite node/Xray/XKeen/routing/DNS/Observatory policy.

A fresh panel can generate a one-time setup credential. Existing auth/listener/state is preserved on rerun. Missing XKeen/Xray/configuration is a supported Setup Mode state.

Existing managed installs use the installed binary's pinned-signature update path. The known historical C.1 manual panel has a release-owned fingerprint-gated adoption bridge; unknown/partial legacy layouts fail closed.

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

## Planned D.1 backup / restore

Portable typed export/import is the current product slice (#3), not current behavior yet. Safe export will exclude VPN/subscription secrets by default; secret-bearing backup will be explicit and encrypted. Until #3 is merged and qualified, `nodes.json` backups are secret operator material.

## Planned D.2 component lifecycle

XKeen/Xray/geodata inventory/update/rollback will be managed through typed capability-aware operations in #4. Until then, do not add generic shell/package-manager endpoints and do not treat component lifecycle controls as available.

## Geodata

Current repository deploy/update tooling owns the exact geodata files referenced by policy. Any update must keep candidate validation/rollback semantics and must not introduce a competing updater schedule.

#4 will consolidate component/geodata schedules under one maintenance manager.

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

Future D.1 portable backups have a separate typed/encrypted contract; do not confuse them with raw deploy snapshots.

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
