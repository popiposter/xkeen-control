# Security

> **Active implementation:** [Native XKeen contract](docs/NATIVE-XKEEN.md) governs Issue #121 on clean Entware. Older panel migration/recovery compatibility is not required. Historical behavior below is not the new installation authority.

`xkeen-control` is designed so the source repository, issues/PRs, qualification logs and release artifacts can be public **without containing router credentials**.

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

`popiposter/xkeen-control` is the public source/release authority. Development qualification is local; the only GitHub Actions workflow is the protected, manually dispatched release workflow.

It was initialized on 2026-08-21 from the validated secretless source tree of the former private repository at:

```text
a1b8c3ce4e7f1914312b23b52c3b96269865e90e
```

Only current source content was migrated into fresh Git history. Old commits, branches, pull-request refs, cached PR views, releases, Actions artifacts, issues, credentials, router backups and other historical Git objects are not imported.

The former `popiposter/xkeen-keenetic` repository remains private historical/quarantine storage and is not a software or release authority. Do not merge, mirror or import its Git history into this repository.

Old clones, archives, reflogs or external copies of the historical repository can still contain former credentials and must be discarded or protected. Rotate provider credentials if their confidentiality is uncertain.

Before accepting migration or release changes, verify that current public history, current tree, issues, local qualification tooling, release workflow configuration and release inputs remain secretless.

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

Sessions, throttling and high-churn runtime state stay in RAM. Issue #99 B caps
sessions at 32 and remote attempt entries at 256. Expired state is pruned before
admission; sessions evict by oldest expiry with a stable token tie-break. Attempt
pressure may displace only non-locked entries. A table of active lockouts rejects
new remotes without adding state or evicting any lockout. Real TCP RemoteAddr,
never proxy headers, identifies login and reauthentication attempts.

Login, Reauthenticate and CredentialState share one bounded bcrypt reader.
Linux requires a real root-owned 0700 parent and a root-owned regular non-symlink
hash with no group/world permissions, at most 256 bytes. No-follow open and
pre/open/post identity checks reject unsafe or changed authority; reads never
repair permissions or disclose native errors. CLI/bootstrap retain their existing
protected atomic writer.

Each Manager snapshots the RAM credential generation with the protected hash,
compares bcrypt concurrently, then checks the generation under a credential
read lock before admitting a session or completing reauthentication. Password
replacement holds the credential write lock through hash mutation, generation
retirement and session invalidation. A valid-length write attempt retires the
generation even on error because the hash may already have committed; invalid
password lengths leave the current authority and sessions unchanged. Ordinary
session reads and CSRF work do not acquire this credential lock.

Every HTTP request, including assets and health, validates Host against the
accepted socket's `http.LocalAddrContextKey`. Numeric private/loopback IP and
effective port must match; IPv6 is bracketed with no zone. Only `localhost` is
allowed as a hostname and only on loopback. Missing ports mean 80 only for an
actual port-80 listener. Missing/unparseable socket context fails closed; config
and Forwarded/X-Forwarded headers confer no Host trust.

Only the three password-bearing routes (login, password replacement and secret
backup export) require one exact application/json Content-Type, no query, bounded
bodies and exact required case-sensitive string fields, rejecting duplicate,
unknown, missing, null and trailing data. Existing browser security headers are
preserved with COOP/CORP same-origin on shell/assets/API/health/rejections.

The separate panel-local notification authority is
`/opt/etc/xkeen-control/secrets/notifications.json`, root-owned 0600 under a 0700
secrets directory. Telegram token/chat ID never appear in safe responses or
logs; this authority is excluded from safe export and encrypted node backup.
It is separately reconfigurable after reinstall; older binaries ignore it.
No inbound command, generic webhook or VPN/firewall/DDNS automation is added.

## Backup / restore

D.1 / Issue #3 is production-qualified in signed stable `v0.2.0`. Safe export excludes VPN/subscription secrets by default. Secret-bearing export requires explicit re-authentication and a passphrase, uses a bounded Argon2id/XChaCha20-Poly1305 envelope, and is not persisted by the panel. Any backup containing `nodes.json` remains secret material.

Restore is authenticated same-origin/CSRF, preview-first, session-bound, bounded and typed. Apply uses the authority lease, transaction journal and recovery path, validates a complete candidate, and does not expose raw config/filesystem/archive/command surfaces. An equivalent settings-only restore is a no-op: it preserves `nodes.json`, generated runtime policy and the running Xray/XKeen state without an unnecessary restart.

Successful typed `appliance adopt` establishes the local non-secret appliance authority and deterministic managed policy. Before adoption, routers retain the explicit repository-derived/legacy compatibility boundary; there is no implicit adoption, and unknown/manual drift fails closed. Later #4/#5 functionality exists in source but remains outside the production-qualified deployed generation.

## Qualification and diagnostics

- Tests use synthetic fixtures only.
- Workflows must never dump complete environments or production configuration.
- Router SSH credentials/private keys must never be mounted into local qualification or release jobs.
- Secret-bearing backups must never be uploaded as artifacts.
- Prefer sanitized public production evidence: versions, bounded counts, state transitions and non-secret hashes rather than live endpoint details.
- Enable and keep secret scanning/push protection where repository features permit it; prevention is preferred to history cleanup.
