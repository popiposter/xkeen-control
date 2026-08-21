# Operations

This runbook describes the **current production-qualified C.1 generation**. Planned one-command bootstrap, panel self-update, portable backup and component update management are owned by Issues #2–#5 and are not current operator commands yet.

Production is a live router. Prefer typed/repository transactions over ad-hoc edits.

## Health / status

```sh
xkeen -status
xray api bi -s 127.0.0.1:10085 bal-proxy
/opt/etc/init.d/S99xkeen-control status
```

Control-plane generic health:

```text
GET /healthz
```

The panel is loopback or one exact trusted-LAN address only.

## Current deployment path

Until #2 replaces workstation distribution with signed Releases, repository deployment remains an advanced/operator path:

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

Current C.1 behavior:

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

## Panel service

Current manual install helper expects an already-built artifact:

```sh
./scripts/install-control-plane.sh
/opt/etc/init.d/S99xkeen-control start
```

Authentication initialization/change is local/interactive through the product command; never script or log plaintext credentials.

Default listener:

```text
127.0.0.1:8787
```

For local browser access, an SSH tunnel remains safe:

```sh
ssh -L 8787:127.0.0.1:8787 <router>
```

An exact private LAN bind may be configured; wildcard/public/hostname binds fail closed. No WAN firewall opening is required or authorized.

## Planned distribution / updates

### #2 panel releases and self-update

After #2 ships, normal installation/update moves to public signed GitHub Releases. The installer will be one-command and idempotent, will avoid blanket `opkg upgrade`, generate a one-time setup credential, start the panel and print safe access instructions.

Panel update will use signed manifest/hash/architecture verification, `/tmp` staging, one bounded previous panel generation, health verification and automatic rollback. Do not advertise the final installer command until a real qualified release exists.

### #3 backup / restore

Portable typed export/import is planned. Safe export will exclude VPN/subscription secrets by default; secret-bearing backup will be explicit and encrypted. Until then, `nodes.json` backups are secret operator material.

### #4 component lifecycle

XKeen/Xray/geodata inventory/update/rollback will be managed through typed capability-aware operations. Until #4 ships, do not treat planned UI update controls as available and do not add generic shell/package-manager endpoints.

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
