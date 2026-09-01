# Fresh Keenetic / restore

This document describes the current qualified panel bootstrap boundary and separates it from destructive Entware/KeeneticOS provisioning and the planned D.1 portable restore model.

Target currently qualified in production: Keenetic `linux/arm64`, KeeneticOS 5+, Entware/Open Package, XKeen + Xray.

> **Secret boundary:** Git/source/releases are secretless. Restoring an existing VPN configuration still requires the router's local secret node registry or the future D.1 encrypted backup format.

## Current prerequisites

Before panel bootstrap, the router needs:

- working `/opt` / Entware/Open Package;
- root administration;
- supported `linux/arm64` architecture;
- network access to the fixed public GitHub release hosts.

XKeen/Xray may already be installed for a running appliance, but the panel bootstrap does not install or repair them. Missing XKeen/Xray/configuration is reported as Setup Mode.

Do not run blanket `opkg upgrade` as a routine product bootstrap step. Install/update only prerequisites intentionally required by the active procedure. The historical repository `bootstrap.sh` is deliberately not part of the public product path because it predates the bounded installer architecture and used blanket package upgrade behavior.

## Qualified panel bootstrap

The currently production-qualified release-specific installer is:

```sh
sh -c "$(curl -fsSL https://github.com/popiposter/xkeen-control/releases/download/v0.1.1/install.sh)"
```

It requires root, `/opt`, Entware `opkg`, supported architecture and bounded free space; installs only explicitly missing prerequisites; verifies the release's expected manifest/hash/size set; installs the panel binary/init/fixed updater; preserves existing router-local state; and verifies generic health plus exact local build identity.

For a genuinely new panel auth state, bootstrap credential generation is Go-owned, uses `crypto/rand`, stores only a bcrypt hash and prints the plaintext once to the invoking terminal. Reruns do not reset an existing credential.

The panel remains loopback by default or may use one exact private management address. It never opens a wildcard/public listener or WAN firewall rule.

## Existing managed installs

If `/opt/sbin/xkeen-control` is already a valid managed #2 install, rerunning the installer preserves auth/listener/state/secrets and delegates to the installed binary's source-pinned Ed25519 update path rather than downgrading trust to bootstrap-only HTTPS checks.

## Historical C.1 adoption

The known pre-#2 manual C.1 panel is supported through a narrow release-owned adoption bridge. Adoption is allowed only when the fixed historical binary/init SHA-256 fingerprints match and the managed helper/marker state matches the legacy layout; unknown, partial or corrupt layouts fail closed.

The bridge snapshots only the bounded node authority/generated-outbounds pair required to survive an old-process Apply interruption. After stopping the exact legacy process it distinguishes unchanged state, incoherent mid-write state and coherent post-write/pre-activation state. Coherent changed state must pass typed full Xray runtime reconciliation; failure restores the validated snapshot and converges runtime before panel replacement.

Production qualification for `v0.1.1` completed the full sequence:

```text
legacy exact
  -> v0.1.1 adoption
  -> rollback to exact legacy binary/init with helper absent
  -> v0.1.1 re-adoption
```

All transitions passed generic health, exact version/source/channel and single PID-file-backed process checks while bounded non-secret fingerprints for auth/listener/node/Xray/XKeen/selection/benchmark state remained unchanged.

## Current secret restore

Authoritative node/subscription registry:

```text
/opt/etc/xkeen-control/secrets/nodes.json
```

Required permissions:

```text
parent directory 0700
nodes.json        0600
```

For migration of an older working configuration, `scripts/migrate-secrets.sh` remains a one-time compatibility path where applicable. Do not print or copy secret contents into GitHub evidence.

Generated active `04_outbounds.json` is not a backup authority. Restore the registry, then use the product transaction/render path so outbounds and runtime readiness are verified together.

## Advanced repository deployment

Repository deployment remains available for deliberate development/operator work but is not the normal public distribution path:

1. obtain the intended `popiposter/xkeen-control` revision on a workstation;
2. build/test `xkeen-control` off-router;
3. copy only the intended repository/artifacts to the router;
4. restore/migrate local secret state separately;
5. run the bounded repository deploy + verify path.

```sh
cd /opt/etc/xkeen/repo
chmod +x scripts/*.sh
./scripts/deploy.sh
./scripts/verify.sh
```

The deploy builds a complete candidate under `/tmp`, renders outbounds from `nodes.json`, validates Xray, snapshots the current generation, activates/restarts through the bounded foreground lifecycle, waits for RoutingService and rolls back on failure.

It also preserves C.1 ownership: legacy XKeen Speed Balancer/watchdog/full-benchmark writers stay disabled and the control plane owns stable selection/liveness/daily benchmark scheduling.

## Verification

For repository deployment use:

```sh
./scripts/verify.sh
```

For installed panel readiness use generic `/healthz`, local `xkeen-control version --json` and init/PID status. Verify intended traffic behavior from a trusted LAN client when the active issue requires it. Never use a heavy benchmark merely as a restore smoke test.

## Panel access

Default management endpoint is loopback:

```text
127.0.0.1:8787
```

Use an SSH tunnel or one explicitly configured private LAN bind. Do not expose the panel to WAN or wildcard address.

## Planned portable restore — Issue #3

D.1 is the current product slice and will add a schema-versioned appliance backup:

- safe export without VPN/subscription secrets by default;
- optional passphrase-encrypted secret-bearing backup;
- destination admin password preserved/generated locally rather than silently cloned;
- hardware-local settings validated before restore;
- preview-first transactional import.

Until #3 is merged and production-qualified, raw `nodes.json` backups remain secret operator-managed material and there is no product portable restore bundle.

## Planned component lifecycle — Issue #4

D.2 will make XKeen/Xray/geodata versions and update/rollback visible and manageable in the panel through typed capability-aware operations. Do not assume those controls exist in the current release.
