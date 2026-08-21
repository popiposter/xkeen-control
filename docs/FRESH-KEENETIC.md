# Fresh Keenetic / restore

This document separates the **current C.1 restore path** from the planned one-command product bootstrap in Issue #2.

Target currently qualified in production: Keenetic Ultra KN-1811, KeeneticOS 5+, Entware/Open Package, XKeen + Xray.

> Git/source is secretless. A real restore also needs the router's secret node registry or a future D.1 encrypted backup.

## Current prerequisites

Before repository deployment, the router needs:

- working `/opt` / Entware/Open Package;
- SSH administration;
- XKeen + Xray installed;
- current required geodata/runtime dependencies;
- local VPN secret material when restoring an existing configuration.

Do not run blanket `opkg upgrade` as a routine product bootstrap step. Install/update only prerequisites intentionally required by the active procedure. The historical repository `bootstrap.sh` is deliberately not part of this fresh public baseline because it predates the #2 installer architecture and used blanket package upgrade behavior.

## Current secret restore

Authoritative current registry:

```text
/opt/etc/xkeen-control/secrets/nodes.json
```

Required permissions:

```text
parent directory 0700
nodes.json        0600
```

For migration of an older working installation, `scripts/migrate-secrets.sh` is a one-time compatibility path. Do not print or copy secret contents into GitHub evidence.

Generated active `04_outbounds.json` is not a backup authority. Restore the registry, then use the product transaction/render path so outbounds and runtime readiness are verified together.

## Current repository deployment

Until #2 ships signed public releases:

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

Use:

```sh
./scripts/verify.sh
```

Then verify the intended traffic behavior from a trusted LAN client: ordinary traffic remains DIRECT while explicitly proxied services use the unified `bal-proxy` pool.

Never use a heavy benchmark merely as a restore smoke test.

## Panel access

Default management endpoint is loopback:

```text
127.0.0.1:8787
```

Use an SSH tunnel or one explicitly configured private LAN bind. Do not expose the panel to WAN or wildcard address.

## Planned one-command install — Issue #2

#2 replaces the workstation/manual software distribution step with a signed public GitHub Release and idempotent installer.

Target UX after `/opt`/Entware is available:

```sh
sh -c "$(curl -fsSL https://github.com/popiposter/xkeen-control/releases/latest/download/install.sh)"
```

**This command is not ready for use until the first signed release is published and qualified.**

The #2 installer contract is:

- detect root, `/opt`, CPU/ABI, free space and required dependencies;
- use `opkg update` + explicit missing packages only, never blanket `opkg upgrade`;
- verify signed/checksummed panel release artifact;
- install/start panel transactionally;
- generate a one-time setup credential and print it once without logging;
- choose only loopback/exact private management address;
- detect XKeen/Xray state and leave a usable Setup Mode if components still need typed installation;
- preserve existing local settings/secrets on repeated runs.

Fresh destructive Entware/KeeneticOS provisioning remains outside the normal panel installer and must not be tested against the production router merely to qualify #2.

## Planned portable restore — Issue #3

D.1 adds a schema-versioned appliance backup:

- safe export without VPN/subscription secrets by default;
- optional passphrase-encrypted secret-bearing backup;
- destination admin password preserved/generated locally rather than silently cloned;
- hardware-local settings validated before restore;
- preview-first transactional import.

Until #3 is merged, raw `nodes.json` backups remain secret operator-managed material.

## Component lifecycle — Issue #4

D.2 will make XKeen/Xray/geodata versions and update/rollback visible and manageable in the panel through typed capability-aware operations. Do not assume those controls exist in the current C.1 generation.
