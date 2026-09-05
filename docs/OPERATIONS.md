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

## Current/next D.2 component lifecycle (#4) — F1 source-only, not deployed

Issue #4 Phase F1 now provides a source-main backend broker for typed manual
XKeen/Xray/geodata preview, update and rollback routes. It has no UI, persisted
policy, scheduler, automatic update or release surface, and its source fixtures
are offline/synthetic only. The four routes are not production-qualified and
must not be called or exposed on the signed stable router until a separate live
and release qualification explicitly authorizes them. Do not add generic
shell/package-manager endpoints. #5 visual typed configuration remains
planned as well.

## Geodata

Current repository deploy/update tooling owns the exact geodata files referenced by policy. Any update must keep candidate validation/rollback semantics and must not introduce a competing updater schedule.

Later #4 phases may consolidate component/geodata scheduling under one
maintenance manager; F1 adds no scheduler or automatic mutation.

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
