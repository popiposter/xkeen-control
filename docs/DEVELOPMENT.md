# Development and qualification

This is the build/test/CI authority. Workflow/roles live in `DEVELOPMENT-PROCESS.md`; production mutation rules live in `OPERATIONS.md` and the active issue.

## Supported developer environment

Primary local development path: Docker Desktop with Linux containers. Go, cgo/build tools, Node/npm and dependency caches stay off the router.

Pinned development image currently provides:

```text
Go 1.27
Node 24
Debian build-essential / cgo tools
Git
jq
OpenSSH client
```

Exact versions may change deliberately in `Dockerfile.dev`; release evidence must use the repository-pinned environment rather than an arbitrary host toolchain.

## Full local qualification

From PowerShell in repository root:

```powershell
pwsh -NoProfile -File scripts/dev-check.ps1
```

This is the default floor for code, frontend, build, packaging and operational-script changes. It covers the repository's current equivalent of:

```text
uncached Go tests where configured
go vet ./...
go test -race ./...
focused shell/runtime fixtures
npm ci
frontend checks + production build
npm audit at repository threshold
tracked embedded-asset consistency
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 build
artifact SHA-256
host git diff --check
```

Run additional focused fixtures for the subsystem changed. Do not claim a check that was skipped.

For docs-only changes, use proportional checks (references/links/content consistency + diff hygiene) instead of automatically running expensive code builds.

## Interactive container

```powershell
docker compose -f docker-compose.dev.yml run --rm dev bash
```

The repository is bind-mounted; Go/npm caches and `web/node_modules` use Docker volumes so the Windows worktree is not polluted by Linux dependencies.

## Release artifact

Developer/local build helper:

```sh
./scripts/build-control-plane.sh
```

Current qualified target:

```text
CGO_ENABLED=0
GOOS=linux
GOARCH=arm64
output: dist/xkeen-control-linux-arm64
```

The binary is built off-router. Record exact source HEAD and artifact SHA-256 for production evidence.

For production distribution, Slice D / Issue #2 is complete: signed GitHub Releases are the software authority. `scripts/release-build.sh` assembles deterministic release inputs, while `.github/workflows/release.yml` performs protected signing/publication from one exact current reviewed `main` SHA. Production signing material is confined to the protected `release` environment; fixture keys are never valid production material.

Stable release `v0.1.1` from source `8f15246099538426ef08163b832c3aa6f73e8265` completed protected publication and bounded live legacy adoption/rollback/re-adoption qualification.

## Frontend embedding

`web/` is source. Built assets consumed by Go `embed` live under `internal/webassets/dist/` and are tracked so a clean checkout can compile/test without an implicit frontend build.

When frontend source changes:

1. run the documented frontend build;
2. update tracked embedded output;
3. run full qualification;
4. verify generated assets contain no secrets/local paths.

## Public GitHub Actions

PR/main CI is current repository behavior. It runs on standard GitHub-hosted Linux runners with read-only repository contents permission and covers Go/vet/race, focused shell/runtime fixtures, frontend install/check/build/audit, embedded-asset consistency, static `linux/arm64` build and repository/public-hygiene checks.

Do not use `pull_request_target` to execute PR code with privileged secrets. PR/main CI receives no production release signing key and no router credentials/configuration.

The protected manual Release workflow:

- takes explicit `version`, `channel` and full `source_ref` inputs;
- checks that `source_ref` equals the exact checkout and current remote `main`;
- runs full release qualification;
- assembles unsigned deterministic assets in the unprivileged build job;
- transfers only secretless release inputs to the protected `release` environment;
- verifies the protected public key matches the compiled/source-pinned trust anchor;
- signs the exact manifest with the protected private key;
- creates a non-public draft, re-downloads and independently verifies the exact signed seven-asset set;
- re-checks current `main` before publishing the verified draft.

Actions artifacts are build handoff only, not release authority.

## Issue #2 focused fixtures

The release/bootstrap/updater qualification fixture is:

```sh
bash scripts/test-release.sh
```

It covers manifest/signature tamper rejection, candidate hash/size validation, policy bounds, bootstrap idempotence, managed rerun trust, historical C.1 adoption, updater lifecycle/rollback, legacy node-Apply recovery, and absence of blanket package upgrades or upstream interactive installer invocation.

These fixtures remain regression coverage after #2 completion; later slices must not weaken them.

## Issue #4 component lifecycle focused fixture

The component inventory, trusted metadata-check and internal transactional
Xray qualification fixture is:

```sh
bash scripts/test-components.sh
```

It uses temporary synthetic component paths and covers the fixed panel, XKeen,
Xray, geodata, KeeneticOS and Entware projection, bounded version probing,
strict parsing, unknown/manual geodata expressions, filesystem safety, no
script/opkg execution, no writes, the authenticated read-only HTTP route and
the Phase B trusted metadata-check contract. Phase B fixtures use offline
synthetic upstream responses, verify fixed sources/digests/cache/security
bounds, and never download artifact bodies or read/mutate a production
Keenetic. Phase C fixtures additionally cover fresh exact-identity
re-resolution, fixed HTTPS artifact transport, ZIP traversal/duplicate/
non-regular rejection, complete candidate rendering and validation, shared
Coordinator-to-authority lock order, stale authority rejection, one previous
generation, journal fault injection, verified rollback, local-only startup
recovery and restore-journal conflict. The Phase C primitive has no HTTP/UI
mutation route and all source qualification remains synthetic/offline.

## Issue #3 Phase B focused fixtures

The typed backup/export qualification fixture is:

```sh
bash scripts/test-backup.sh
```

It covers deterministic secretless export, typed section metadata, encrypted round trips, strict envelope tamper rejection, coherent node snapshots, single-flight crypto, HTTP session/origin/CSRF/re-authentication and session invalidation. It uses synthetic credentials and never reads router state.

## Issue #3 Phase C1 focused fixtures

The internal restore/transaction/recovery qualification fixture is:

```sh
bash scripts/test-restore.sh
```

It covers strict safe/encrypted mode handling, stable-ID replace/merge semantics, bounded session-bound previews, exact authority stale checks, fixed-template/current-generation blockers, complete candidate validation before mutation, deterministic generated-file/runtime convergence, logical previous-generation rollback, secret-free journal metadata and interrupted-import startup recovery. It uses synthetic authorities and never mutates a production Keenetic.

## Issue #3 Phase C2 focused fixtures

The bounded import HTTP/UI adapter qualification is covered by the HTTP package tests together with the existing backup and restore fixtures:

```sh
go test -count=1 ./internal/httpapi
bash scripts/test-backup.sh
bash scripts/test-restore.sh
bash scripts/test-restore-ui.sh
```

The HTTP regressions cover authenticated same-origin/CSRF routes, strict query/multipart/body limits, hostile filenames without temporary uploads, preview single-flight admission, logout/password/in-flight session invalidation, token-only Apply/Cancel, fixed safe error mappings and response secret scanning. The frontend check covers the first-class Backup & Restore flows and tracked embedded assets. These tests use synthetic data and do not mutate a production Keenetic.

## Fresh-checkout expectation

A fresh clone/checkout of PR HEAD must be sufficient for documented qualification. Do not depend on untracked generated files, local secrets, an already-built binary, host npm/go packages or production registry/config files.

## Router SSH boundary

Router qualification is host-side and issue-authorized. Never mount router credentials/private keys, production registry files or subscription credentials into the development container or CI.

Build/test first, then copy/use only the exact release/artifact/scripts required for the bounded smoke. Snapshot affected state, use repository/typed transactions, sanitize evidence and remove temporary uploads/tunnels afterward.

Go/Node/build tooling is never installed on Keenetic.
