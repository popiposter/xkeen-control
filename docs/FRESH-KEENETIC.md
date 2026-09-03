# Fresh Keenetic / restore

This document describes the current qualified panel bootstrap and D.1 portable restore boundaries, and separates them from destructive Entware/KeeneticOS provisioning and the planned D.2 component lifecycle work.

Target currently qualified in production: Keenetic `linux/arm64`, KeeneticOS 5+, Entware/Open Package, XKeen + Xray. Signed stable `v0.2.0` is the current production-qualified panel release for that target.

> **Secret boundary:** Git/source/releases are secretless. Portable restore uses D.1 safe or explicitly encrypted backup bundles. Any bundle containing the VPN/subscription registry remains secret operator material and must never be published to GitHub evidence.

## Current prerequisites

Before panel bootstrap, the router needs:

- working `/opt` / Entware/Open Package;
- root administration;
- supported `linux/arm64` architecture;
- network access to the fixed public GitHub release hosts.

XKeen/Xray may already be installed for a running appliance, but the current panel bootstrap does not install or repair them. Missing XKeen/Xray/configuration is reported as Setup Mode. Typed XKeen/Xray/geodata installation and lifecycle management remains planned under Issue #4 and must not be assumed available in `v0.2.0`.

Do not run blanket `opkg upgrade` as a routine product bootstrap step. Install/update only prerequisites intentionally required by the active procedure. The historical repository `bootstrap.sh` is deliberately not part of the public product path because it predates the bounded installer architecture and used blanket package upgrade behavior.

## Qualified panel bootstrap

The currently production-qualified release-specific installer is:

```sh
sh -c "$(curl -fsSL https://github.com/popiposter/xkeen-control/releases/download/v0.2.0/install.sh)"
```

`v0.2.0` was built from exact source `f170cdb0a9531cb8f4e08c95c0ba9bc8fe3dfd86`, passed the protected signed-release flow and completed the bounded D.1 live Keenetic qualification gate.

The installer requires root, `/opt`, Entware `opkg`, supported architecture and bounded free space; installs only explicitly missing prerequisites; verifies the release's expected manifest/hash/size set; installs the panel binary/init/fixed updater; preserves existing router-local state; and verifies generic health plus exact local build identity.

For a genuinely new panel auth state, bootstrap credential generation is Go-owned, uses `crypto/rand`, stores only a bcrypt hash and prints the plaintext once to the invoking terminal. Reruns do not reset an existing credential.

The panel remains loopback by default or may use one exact private management address. It never opens a wildcard/public listener or WAN firewall rule.

## Existing managed installs

If `/opt/sbin/xkeen-control` is already a valid managed #2 install, rerunning the installer preserves auth/listener/state/secrets and delegates to the installed binary's source-pinned Ed25519 update path rather than downgrading trust to bootstrap-only HTTPS checks.

Panel self-update/rollback remains the #2-owned component boundary. D.1 appliance state and backup/restore do not replace the signed panel update trust model.

## Historical C.1 adoption

The known pre-#2 manual C.1 panel is supported through a narrow release-owned adoption bridge. Adoption is allowed only when the fixed historical binary/init SHA-256 fingerprints match and the managed helper/marker state matches the legacy layout; unknown, partial or corrupt layouts fail closed.

The bridge snapshots only the bounded node authority/generated-outbounds pair required to survive an old-process Apply interruption. After stopping the exact legacy process it distinguishes unchanged state, incoherent mid-write state and coherent post-write/pre-activation state. Coherent changed state must pass typed full Xray runtime reconciliation; failure restores the validated snapshot and converges runtime before panel replacement.

Production qualification for historical stable `v0.1.1` completed the full sequence:

```text
legacy exact
  -> v0.1.1 adoption
  -> rollback to exact legacy binary/init with helper absent
  -> v0.1.1 re-adoption
```

All transitions passed generic health, exact version/source/channel and single PID-file-backed process checks while bounded non-secret fingerprints for auth/listener/node/Xray/XKeen/selection/benchmark state remained unchanged. This remains historical qualification evidence; `v0.2.0` is the current production-qualified release.

## Current D.1 appliance authority and portable restore

Authoritative node/subscription registry:

```text
/opt/etc/xkeen-control/secrets/nodes.json
```

Required permissions:

```text
parent directory 0700
nodes.json        0600
```

After successful typed D.1 adoption, the supported non-secret appliance authority is:

```text
/opt/etc/xkeen-control/config/appliance.json
```

Managed `02_dns.json`, `05_routing.json` and `07_observatory.json` derive deterministically from `appliance.json`; active `04_outbounds.json` derives from `nodes.json` and is not an independent backup authority.

For an existing router that has not adopted the appliance authority, adoption is deliberate and compatibility-gated:

```sh
/opt/sbin/xkeen-control appliance adopt
/opt/sbin/xkeen-control appliance validate
/opt/sbin/xkeen-control appliance verify
```

`adopt` proves the current supported policy and fixed companion generation before creating `appliance.json`; it is designed to perform no active runtime-policy mutation. Before successful adoption, the router retains its explicit repository-derived/legacy policy boundary. Unknown/manual drift fails closed rather than being silently normalized.

D.1 portable backup/restore is current production behavior in `v0.2.0`:

- safe export is the default and excludes VPN/subscription secrets;
- secret-bearing export requires explicit re-authentication plus a passphrase and produces a bounded encrypted bundle;
- restore is authenticated, same-origin/CSRF protected, bounded, session-bound and preview-first;
- settings-only restore is the default and can be a true no-op when equivalent;
- secret-bearing restore may explicitly replace or merge the typed node registry;
- Apply uses the reviewed authority lease, transaction journal, deterministic re-render and rollback/recovery path;
- raw Xray/XKeen JSON, filesystem/archive browsing and generic command execution are not restore surfaces.

Any secret-bearing D.1 backup remains operator secret material. Do not print it, attach it to issues/PRs or upload it as a CI artifact.

For migration of an older working configuration, `scripts/migrate-secrets.sh` remains a one-time compatibility path where applicable. It is not the preferred portable backup mechanism for a D.1-managed appliance. Do not print or copy secret contents into GitHub evidence.

Never restore only a generated `04_outbounds.json` over a different registry and claim the logical state restored. Restore the logical authority through the supported typed path so generated outbounds and runtime readiness converge together.

## Advanced repository deployment

Repository deployment remains available for deliberate development/operator work but is not the normal public distribution path:

1. obtain the intended `popiposter/xkeen-control` revision on a workstation;
2. build/test `xkeen-control` off-router;
3. copy only the intended repository/artifacts to the router;
4. restore/adopt local appliance and secret state through the supported typed/compatibility paths as required;
5. run the bounded repository deploy + verify path.

```sh
cd /opt/etc/xkeen/repo
chmod +x scripts/*.sh
./scripts/deploy.sh
./scripts/verify.sh
```

The deploy builds a complete candidate under `/tmp`, renders outbounds from `nodes.json`, validates Xray, snapshots the current generation, activates/restarts through the bounded foreground lifecycle, waits for RoutingService and rolls back on failure.

It also preserves C.1 ownership: legacy XKeen Speed Balancer/watchdog/full-benchmark writers stay disabled and the control plane owns stable selection/liveness/daily benchmark scheduling.

Repository deployment is an advanced development/operator path, not the D.2 component lifecycle implementation. In particular, current repository geodata scripts must not be treated as equivalent to the planned typed #4 update/rollback manager.

## Verification

For repository deployment use:

```sh
./scripts/verify.sh
```

For installed panel readiness use generic `/healthz`, local `xkeen-control version --json` and init/PID status. For an adopted D.1 appliance also use the typed appliance validation/verification path when the active procedure requires authority coherence evidence.

Verify intended traffic behavior from a trusted LAN client when the active issue requires it. Never use a heavy benchmark merely as a restore smoke test.

## Panel access

Default management endpoint is loopback:

```text
127.0.0.1:8787
```

Use an SSH tunnel or one explicitly configured private LAN bind. Do not expose the panel to WAN or wildcard address.

## Current/next component lifecycle — Issue #4

D.2 / Issue #4 is the current product slice and remains **planned, not deployed** in `v0.2.0`.

It will add typed capability-aware inventory and bounded lifecycle operations for XKeen, Xray and required geodata while preserving the D.1 appliance/node authorities and the shared lifecycle coordinator. The product path must not expose a shell, generic package manager, arbitrary URL downloader or blanket `opkg upgrade` behavior.

Until the relevant #4 phases are implemented, signed-released and live-qualified, Setup Mode reports missing XKeen/Xray prerequisites but does not install/repair them, and operators must not assume component update/rollback controls exist in the panel.
