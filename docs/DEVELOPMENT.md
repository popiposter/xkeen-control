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
- runs full release qualification, including the lockfile-pinned, single-worker Chromium `test:components-ui` suite after `npm ci` and before unsigned assembly; a failure blocks `publish` through `needs: build`;
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

The release fixture also runs `TestReleaseBrowserBuildBoundary`: a narrow workflow-contract regression that rejects a missing or ignored F2 invocation and a detached publish job. Its negative cases remove the suite in memory, without dispatching a workflow. Browser installation/execution stays in the read-only build job, outside the protected signing environment.

These fixtures remain regression coverage after #2 completion; later slices must not weaken them.

## Issue #4 component lifecycle focused fixture

The component inventory, trusted metadata-check and internal transactional
component lifecycle qualification fixture is:

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
Keenetic. Phase E0 fixtures additionally verify the component-specific
`dev`/`stable` channel matrix, the fixed `jameszeroX/XKeen` source, signed
automated build-commit plus exact tree/blob identity projection, rejection of
the legacy `Skrill0/XKeen` release path, and `S05xkeen` versus explicit legacy
`S24xray` inventory behavior. E0 remains metadata-only: it does not download
the dev archive or add component mutation. Phase C fixtures additionally cover
fresh exact-identity re-resolution, fixed HTTPS artifact transport, ZIP
traversal/duplicate/non-regular rejection, complete candidate rendering and
validation, shared Coordinator-to-authority lock order, stale authority
rejection, one previous generation, journal fault injection, verified rollback,
local-only startup recovery and restore-journal conflict. The Phase C primitive
has no HTTP/UI mutation route and all source qualification remains
offline/synthetic. Phase D fixtures additionally cover the complete six-file
geodata transaction, fresh exact-set resolution, fixed-host artifact transport,
staged Xray config validation, shared Xray/geodata recovery arbitration,
whole-set rollback, unrelated-file preservation and fail-closed
journal/maintenance behavior.

Phase E1 fixtures additionally cover the internal transactional jameszeroX dev
XKeen primitive: fixed exact build/tree/blob identity, bounded Git-blob
downloader semantics, strict GNU-tar file-only/path/type/mode/trailing-data
qualification, canonical `xkeen + .xkeen` generation hashing and marker
coherence, purpose-specific `S05xkeen` runtime convergence without executing the
candidate `xkeen`, preserved Xray/geodata/config/opkg/cron/Entware state,
purpose-specific preserved-state bounds, uncompressed-generation free-space
admission, shared `xray|geodata|xkeen` journal/recovery arbitration, one previous
generation, activation-parent preservation, ordinary rollback and local-only
startup recovery. Phase E2 independently pins the real `2.0.1/Beta` catalog
entry's exact archive SHA-256, GNU-tar member manifest and canonical generation
digest for the installable fixed identity.

Phase E2 qualification keeps repository CI deterministic/offline. The reviewed
`2.0.1/Beta` catalog entry pins the exact archive SHA-256, GNU-tar member
manifest and canonical generation digest; any one-time immutable upstream
retrieval/content-equivalence check remains separate review evidence and must
not turn `test-components.sh` or normal PR CI into a moving-network test. No
Phase E2 production mutation or temporary operator mutation surface is
authorized.

Phase F1 fixtures cover the four authenticated backend routes, strict closed
request/token bodies, exact `application/json` media type, trailing-data and
unknown-field rejection, CSRF/origin enforcement, session/password preview
invalidation, one-shot/TTL/bounded token retention and one in-flight preview.
They prove fresh uncached preview resolution without artifact bodies, exact
six-item geodata candidates, moving `jameszeroX/XKeen` `main` provenance mapped
only to the reviewed installable catalog entry, typed Apply/Rollback dispatch,
rollback target rotation/stale rejection and sanitized transaction errors. F1
has no UI, persisted policy, scheduler, automatic update or production
qualification; all fixtures remain offline/synthetic.

Phase F2 adds the real mounted Components / Updates UI and two bounded
presentation seams: stable allowlisted component-mutation error codes and the
Coordinator-backed `maintenance` / `applying` status projection. The pinned
dev-only Chromium suite uses synthetic intercepted API responses, one browser
and one worker; it performs no external metadata passthrough and stores no
production cookies, HAR files or component tokens. Run it after `npm ci` with:

```sh
cd web
npx playwright install --with-deps chromium
npm run test:components-ui
```

The suite covers lazy inventory, explicit Check/Preview, token-only one-shot
Apply/Rollback, navigation and dashboard refresh during a delayed synchronous
request, cancellation/expiry/session invalidation, conservative unknown
outcomes, lifecycle maintenance, browser-storage absence, keyboard focus and
desktop/mobile screenshots from synthetic fixtures. Full developer
qualification and hosted CI install the pinned Playwright Chromium build and
run this suite off-router.

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
