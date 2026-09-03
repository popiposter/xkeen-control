# Security

`xkeen-control` is designed so the source repository, issues/PRs, CI logs and release artifacts can be public **without containing router credentials**.

## Secret boundary

Production-only material lives on the router under root-only storage:

```text
/opt/etc/xkeen-control/secrets/nodes.json
/opt/etc/xkeen-control/auth/password.bcrypt
```

`nodes.json` (`schemaVersion: 1`) is the authoritative VPN node/subscription registry. It can contain node credentials and subscription URLs/tokens and must be mode `0600` under a `0700` parent directory.

The active:

```text
/opt/etc/xray/configs/04_outbounds.json
```

is generated from `nodes.json`. It is runtime output, not a second source of truth. Never restore only a generated `04_outbounds.json` over a different registry and claim the logical state restored.

Never commit, publish or paste:

- VLESS URLs or UUIDs;
- REALITY key material or short IDs;
- subscription URLs/tokens;
- admin/bootstrap passwords or password hashes;
- SSH credentials/private keys;
- secret-bearing backups or generated full outbounds;
- raw production registry contents.

Treat GitHub issues, PRs, Actions logs/artifacts and release metadata as public surfaces.

## Repository-history boundary

`popiposter/xkeen-control` is the public source/CI/release authority.

It was initialized on 2026-08-21 from the validated secretless source tree of the former private repository at:

```text
a1b8c3ce4e7f1914312b23b52c3b96269865e90e
```

Only current source content was migrated into fresh Git history. Old commits, branches, pull-request refs, cached PR views, releases, Actions artifacts, issues, credentials, router backups and other historical Git objects are not imported.

The former `popiposter/xkeen-keenetic` repository remains private historical/quarantine storage and is not a software or release authority. Do not merge, mirror or import its Git history into this repository.

Old clones, archives, reflogs or external copies of the historical repository can still contain former credentials and must be discarded or protected. Rotate provider credentials if their confidentiality is uncertain.

Before accepting migration or release changes, verify that current public history, current tree, issues, CI configuration and release inputs remain secretless.

## Releases and update supply chain

Slice D / Issue #2 remains production-qualified. Signed public releases, bounded bootstrap and transactional panel self-update/rollback are current behavior for the qualified `linux/arm64` target. Historical stable release `v0.1.1` was built from exact reviewed source `8f15246099538426ef08163b832c3aa6f73e8265`, passed the protected release workflow, and completed bounded live legacy adoption → rollback → re-adoption qualification. D.1 / Issue #3 is now production-qualified in signed stable `v0.2.0` from exact source `f170cdb0a9531cb8f4e08c95c0ba9bc8fe3dfd86`.

The release/update design requires:

- no production router secrets available to build/release jobs;
- exact source revision provenance;
- a source-pinned Ed25519 public trust anchor with the matching private key confined to the protected GitHub `release` environment;
- signed release manifest plus artifact hashes/sizes;
- architecture/compatibility validation before install;
- candidate downloads in `/tmp`;
- one bounded previous panel generation and health/version/PID-path-verified rollback;
- no GitHub write credential on the router.

Synthetic fixture keys remain test-only and must never be copied into release configuration. Release publication must continue to fail closed if protected signing material or the source-pinned public-key match is unavailable.

Normal public release install/update requires no GitHub credential. Existing managed installs use the installed binary's pinned-signature update path. The historical C.1 bridge is fingerprint-gated and exists only to adopt the known pre-#2 panel layout; unknown or partial layouts fail closed.

## Control-plane UI

The panel defaults to `127.0.0.1:8787`. An operator may configure one exact private LAN address for trusted management access. Wildcard, public and hostname binds fail closed.

Never expose the panel directly to WAN or add a WAN firewall opening for convenience. Authentication, CSRF/same-origin checks, rate limiting, bounded responses and security headers remain mandatory defense in depth.

Authenticated node projections may include a display name and endpoint host/port for operator identification, but must never return UUIDs, REALITY key material, short IDs, subscription URLs, VLESS strings, raw secret registry/outbound JSON or raw upstream error payloads that can contain secrets.

Sessions, throttling and high-churn runtime state stay in RAM.

## Backup / restore

D.1 / Issue #3 is production-qualified in signed stable `v0.2.0`. Safe export excludes VPN/subscription secrets by default. Secret-bearing export requires explicit re-authentication and a passphrase, uses a bounded Argon2id/XChaCha20-Poly1305 envelope, and is not persisted by the panel. Any backup containing `nodes.json` remains secret material.

Restore is authenticated same-origin/CSRF, preview-first, session-bound, bounded and typed. Apply uses the authority lease, transaction journal and recovery path, validates a complete candidate, and does not expose raw config/filesystem/archive/command surfaces. An equivalent settings-only restore is a no-op: it preserves `nodes.json`, generated runtime policy and the running Xray/XKeen state without an unnecessary restart.

Successful typed `appliance adopt` establishes the local non-secret appliance authority and deterministic managed policy. Before adoption, routers retain the explicit repository-derived/legacy compatibility boundary; there is no implicit adoption, and unknown/manual drift fails closed. Component lifecycle in #4 and visual configuration in #5 remain planned rather than deployed.

## CI and diagnostics

- Tests use synthetic fixtures only.
- Workflows must never dump complete environments or production configuration.
- Router SSH credentials/private keys must never be mounted into CI.
- Secret-bearing backups must never be uploaded as artifacts.
- Prefer sanitized public production evidence: versions, bounded counts, state transitions and non-secret hashes rather than live endpoint details.
- Enable and keep secret scanning/push protection where repository features permit it; prevention is preferred to history cleanup.
