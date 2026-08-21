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

Current build helper:

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

Slice D / #2 promotes this into signed GitHub Releases with full build revision metadata and a compatibility manifest. Until #2 merges and the release gate is qualified, `dist/` remains a local build output rather than a public release authority. The deterministic signed asset assembly helper is `scripts/release-build.sh`; it requires a protected key file and must not be run with a fixture key for a production release.

## Frontend embedding

`web/` is source. Built assets consumed by Go `embed` live under `internal/webassets/dist/` and are tracked so a clean checkout can compile/test without an implicit frontend build.

When frontend source changes:

1. run the documented frontend build;
2. update tracked embedded output;
3. run full qualification;
4. verify generated assets contain no secrets/local paths.

## Public GitHub Actions direction

Issue #2 owns the CI/release implementation.

This repository is public, but hosted CI/release behavior is still planned until #2 implements and qualifies it:

- PR/main CI should run the Linux-equivalent full qualification on standard GitHub-hosted runners;
- release workflow runs only from reviewed exact tags/commits and re-verifies uploaded release assets;
- no production secrets/router SSH credentials are available to CI;
- tests use synthetic fixtures;
- release signing material is scoped to the protected release environment and never exposed to PR code.

Local Docker qualification remains useful after hosted CI exists; record which evidence came from which environment.

Issue #2 focused fixtures are:

```sh
bash scripts/test-release.sh
```

They cover manifest/signature tamper rejection, candidate hash/size validation, policy bounds, bootstrap idempotence, update lifecycle wiring and the absence of blanket package upgrades or upstream interactive installer invocation.

## Fresh-checkout expectation

A fresh clone/checkout of PR HEAD must be sufficient for documented qualification. Do not depend on untracked generated files, local secrets, an already-built binary, host npm/go packages or production registry/config files.

## Router SSH boundary

Router qualification is host-side and issue-authorized. Never mount router credentials/private keys, production registry files or subscription credentials into the development container or CI.

Build/test first, then copy only the exact artifact/scripts required for the bounded smoke. Snapshot affected state, use repository/typed transactions, sanitize evidence and remove temporary uploads/tunnels afterward.

Go/Node/build tooling is never installed on Keenetic.
