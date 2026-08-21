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

Only current source content is migrated into fresh Git history. Old commits, branches, pull-request refs, cached PR views, releases, Actions artifacts, issues, credentials, router backups and other historical Git objects are not imported.

The former `popiposter/xkeen-keenetic` repository remains private historical/quarantine storage and is not a software or release authority. Do not merge, mirror or import its Git history into this repository.

Old clones, archives, reflogs or external copies of the historical repository can still contain former credentials and must be discarded or protected. Rotate provider credentials if their confidentiality is uncertain.

Before accepting migration or release changes, verify that current public history, current tree, issues, CI configuration and release inputs remain secretless.

## Releases and update supply chain

Slice D / Issue #2 is introducing signed releases, one-command bootstrap and transactional panel self-update. Until it is merged and qualified, those capabilities are planned rather than current production behavior.

The release design requires:

- no production secrets available to build/release jobs;
- exact source revision provenance;
- signed release manifest plus artifact hashes/sizes;
- architecture/compatibility validation before install;
- candidate downloads in `/tmp`;
- one bounded previous panel generation and health-verified rollback;
- no GitHub write credential on the router.

The production Ed25519 public key is a release trust anchor and must be source-controlled only after the operator has provisioned the matching protected `release` environment private key. Synthetic fixture keys must never be copied into release configuration. Until that gate is complete, update checks fail closed and no stable release is published.

Normal public release install/update should require no GitHub credential.

## Control-plane UI

The panel defaults to `127.0.0.1:8787`. An operator may configure one exact private LAN address for trusted management access. Wildcard, public and hostname binds fail closed.

Never expose the panel directly to WAN or add a WAN firewall opening for convenience. Authentication, CSRF/same-origin checks, rate limiting, bounded responses and security headers remain mandatory defense in depth.

Authenticated node projections may include a display name and endpoint host/port for operator identification, but must never return UUIDs, REALITY key material, short IDs, subscription URLs, VLESS strings, raw secret registry/outbound JSON or raw upstream error payloads that can contain secrets.

Sessions, throttling and high-churn runtime state stay in RAM.

## Backup / restore

Until Slice D.1 / Issue #3 is implemented, secret backup handling remains an explicit operator responsibility. Any backup containing `nodes.json` is secret material.

D.1 will add a portable safe export without VPN/subscription secrets by default and an explicitly encrypted secret-bearing export. Do not describe that planned format as already available.

## CI and diagnostics

- Tests use synthetic fixtures only.
- Workflows must never dump complete environments or production configuration.
- Router SSH credentials/private keys must never be mounted into CI.
- Secret-bearing backups must never be uploaded as artifacts.
- Prefer sanitized public production evidence: versions, bounded counts, state transitions and non-secret hashes rather than live endpoint details.
- Enable and keep secret scanning/push protection where repository features permit it; prevention is preferred to history cleanup.
