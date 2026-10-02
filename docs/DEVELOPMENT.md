# Development and qualification

This is the local build/test and protected-release authority. Workflow/roles live in `DEVELOPMENT-PROCESS.md`; production mutation rules live in `OPERATIONS.md` and the active issue.

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
lockfile-pinned Playwright Chromium and OS dependencies
```

Exact versions may change deliberately in `Dockerfile.dev`; release evidence must use the repository-pinned environment rather than an arbitrary host toolchain.

Use one normal repository checkout and dedicated branches. Do not create or use Git worktrees for implementation, review fixes or qualification in this repository.

## Local qualification tiers

During iteration, run the focused fixture for the changed subsystem and the fast proportional check:

```powershell
pwsh -NoProfile -File scripts/dev-check.ps1
```

Fast mode prints a JSON check plan before execution. By default it checks uncommitted
changes (tracked, staged and untracked); in a clean checkout it checks the latest
commit. This is iteration evidence, not qualification of the whole branch.

```powershell
# Inspect without building or running tests:
pwsh -NoProfile -File scripts/dev-check.ps1 -Plan
# Explicit scopes (commit/branch also include local changes):
pwsh -NoProfile -File scripts/dev-check.ps1 -Scope working
pwsh -NoProfile -File scripts/dev-check.ps1 -Scope commit
pwsh -NoProfile -File scripts/dev-check.ps1 -Scope branch -Base origin/main
```

`XKEEN_CHECK_BASE` retains its historical meaning: it selects branch comparison
against that base. An invalid requested base fails instead of silently skipping.
Renames include both paths. Unknown paths broaden lane selection.

Go iteration selects changed packages and their transitive reverse dependencies,
including test imports. Missing/deleted packages, graph failure and shared build
inputs broaden to all packages. Test-only edits do not rebuild ARM64 artifacts.
Frontend iteration runs Node unit tests and selected browser specs; shared shell,
style, fixture, dependency or HTTP/session changes select the whole browser suite.
Known page modules also select cross-workspace integration and responsive tests.
Test-only changes skip the production web build and embedded comparison; the plan
reports `webBuild: 0`. The browser tests use the Vite development server.
A deleted spec broadens browser selection instead of producing an empty pass.
Race tests and dependency audit remain final/release checks.

Native admission fixtures are listed in `scripts/test-native-admission.sh`.
Adding a fixture there runs the helper lane without selecting Chromium. Selector,
Git-scope and Go-graph helper changes use their dispatch fixtures; changes to the
actual `dev-check.ps1`/`dev-check.sh` orchestration retain the browser fallback.

Native source-preservation fixtures have a separate explicit catalogue,
`scripts/test-native-upstream.sh`. Supply the four hash-pinned **public** files and
the complete public profile directory;
it downloads nothing and never executes a complete native installer/dispatcher:

```powershell
docker compose -f docker-compose.dev.yml run --rm -T dev env XKEEN_ADMISSION_INIT=/workspace/dist/native-public.sh XKEEN_ADMISSION_DISPATCHER=/workspace/dist/native-dispatcher-public.sh XKEEN_ADMISSION_REGISTER=/workspace/dist/native-update-contract/02_register_xkeen.sh XKEEN_ADMISSION_INSTALLER=/workspace/dist/native-update-contract/03_install_xkeen.sh XKEEN_ADMISSION_PROFILE_ROOT=/workspace/dist/native-update-contract/public-profile-v1 bash scripts/test-native-upstream.sh
```

The builders/fixtures verify exact source hashes. Keep these inputs in ignored
`dist` storage; never mount operator-local credentials or router artifacts.
The profile directory contains all regular files from the pinned public beta
archive; `scripts/native-update-profile-v1.json` records its archive identity and
complete path/size/hash inventory. Inspect bounded archive entries before
extraction; reject links, special files, duplicate/absolute/traversing names.
This opt-in result is separate from the self-contained helper lane and FULL gate;
their scope does not depend on cache presence. To reproduce an upstream
counterexample, invoke its individual fixture with `XKEEN_ADMISSION_UPSTREAM=1`;
the aggregate catalogue rejects that mode. Passing these isolated source fixtures
does not qualify a complete updater, native execution on the router or LAN traffic.

Documentation-only iteration runs host diff hygiene without Docker; inspect links
and content as part of review. Code lanes run public hygiene before toolchains.
Fast npm reuse requires matching package/lockfile, Node/npm/platform identity and
an intact top-level dependency tree. Full/release always use a clean npm install.

After the candidate is final, run one exact-HEAD full gate:

```powershell
pwsh -NoProfile -File scripts/dev-check.ps1 -Full
```

Full mode must run from the normal branch checkout. It fails before Docker work unless the worktree and index are clean, including non-ignored untracked files; it prints the exact HEAD before qualification and requires the same clean HEAD afterward.

Full mode covers each qualification class once:

```text
uncached Go tests where configured
go vet ./...
go test -race ./...
unique shell/runtime fixtures without repeating package tests
npm ci
one frontend production build + Node unit tests + complete Playwright suite
npm audit at repository threshold
tracked embedded-asset consistency
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 build
artifact SHA-256
host git diff --check
```

Do not run the full gate after every edit and do not reuse its evidence after HEAD changes. A focused helper may intentionally repeat its package subset when used alone; the aggregate full gate suppresses those duplicate package invocations and retains only each helper's unique shell/integration assertions. Do not claim a check that was skipped.

For docs-only changes, use proportional checks (references/links/content consistency + diff hygiene) instead of automatically running expensive code builds.

## Interactive container

```powershell
docker compose -f docker-compose.dev.yml run --rm dev bash
```

The repository is bind-mounted; Go/npm caches and `web/node_modules` use Docker volumes so the Windows worktree is not polluted by Linux dependencies. Playwright Chromium is baked into the pinned image, so ordinary runs do not download the browser or install OS packages again.

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
2. explicitly update tracked embedded output with `bash scripts/update-webassets.sh` inside the development container;
3. review the generated diff;
4. run the focused UI spec while iterating and the final full gate once;
5. verify generated assets contain no secrets/local paths.

`scripts/dev-check.sh` and `scripts/verify-webassets.sh` only compare generated output; qualification never rewrites tracked assets.

## Local CI and protected GitHub Release

Ordinary PR/main GitHub Actions CI is intentionally absent. Local Docker/Linux qualification is the development gate, and PR evidence must bind the exact tested HEAD to the actual focused/full commands and results. Never present local evidence as hosted CI.

Do not add `pull_request_target` or another automatic workflow to execute PR code with privileged secrets. Local qualification receives no production release signing key and no router credentials/configuration.

The protected manual Release workflow:

- takes explicit `version`, `channel` and full `source_ref` inputs;
- checks that `source_ref` equals the exact checkout and current remote `main`;
- admits UID 0 immediately after checkout and adds only `$GITHUB_WORKSPACE` to the build job's global Git `safe.directory` before reading Git identity; checkout's own trust configuration is temporary and does not cover subsequent steps;
- runs the read-only-permission build job in a `node:24-bookworm` container with explicit UID 0 and Bash, admits UID 0 and installs `build-essential`, Git and jq before qualification; Linux auth/notification fixtures require root-owned synthetic authorities and ownership-negative cases;
- runs `scripts/dev-check.sh --full`, including the lockfile-pinned, two-worker Chromium `test:ui` suite, before unsigned assembly; a failure blocks `publish` through `needs: build`;
- assembles unsigned deterministic assets from those verified embedded bytes in the unprivileged build job; assembly does not repeat npm installation/build or rewrite tracked assets;
- transfers only secretless release inputs to the protected `release` environment;
- verifies the protected public key matches the compiled/source-pinned trust anchor;
- signs the exact manifest with the protected private key;
- creates a non-public draft, re-downloads and independently verifies the exact signed seven-asset set;
- re-checks current `main` before publishing the verified draft.

Actions artifacts are build handoff only, not release authority.

`scripts/test-release-git-trust.sh` executes that workflow trust step against
synthetic foreign-owned repositories. It proves the next shell can read the
exact checkout while unrelated repositories and a wrong expected SHA remain
rejected. It runs through the release fixture lane and uses no router state.

UID 0 is confined to the build job's container; it grants no protected signing
environment or repository-write permission. Qualification and unsigned assembly
share that container, so fixture/output ownership remains consistent. The publish
job keeps its separate protected `release` environment and source-pinned signing
checks. Do not weaken production ownership checks or skip fixtures to accommodate
the hosted runner's ordinary user. A workflow repair changes release source and
requires a new reviewed exact-main freeze before a fresh publication decision.

## Issue #99 B focused hardening qualification

During iteration, use synthetic protected authorities and local sockets:

```sh
go test -count=1 ./internal/auth ./internal/httpapi ./cmd/xkeen-control
npm --prefix web run test:system-panel-ui
npm --prefix web run test:feature-complete-ui
bash scripts/test-release.sh --fixtures-only
```

Auth fixtures cover 32-session eviction/pruning and 256-attempt admission without
active-lockout eviction, including concurrent pressure. Linux bcrypt fixtures
cover ownership, parent/file permissions, symlinks, type/size and replacement
races without read-side repair. HTTP fixtures inject LocalAddrContextKey only
for direct handler tests and verify private/loopback/ULA numeric health
authorities, loopback localhost, strict password objects and security headers.
Browser fixtures verify read-only guidance from loaded listener facts without
new polling/mutation/storage, and retain notification and #5 integration checks.
Finish with the clean exact-HEAD full gate above. This is local source evidence;
no live credential/delivery, router access or release qualification is implied.

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

Phase E2 qualification keeps local and release qualification deterministic/offline. The reviewed
`2.0.1/Beta` catalog entry pins the exact archive SHA-256, GNU-tar member
manifest and canonical generation digest; any one-time immutable upstream
retrieval/content-equivalence check remains separate review evidence and must
not turn `test-components.sh` or normal qualification into a moving-network test. No
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

Historical Phase F2's panel-owned component updater and Setup browser UI were
removed under Issue121 together with their browser suites and standalone session
fixture. Current native installation/status and component ownership coverage is
`web/tests/native-xkeen.spec.js`; cross-workspace lifecycle gating remains in
`feature-complete.spec.js`. Server-side historical component fixtures remain until
their callers are retired; do not infer removal of all old backend code.


## Qualification inventory

Issue #99 A focused outbound notification qualification uses synthetic local
authorities, injected transport/DNS fixtures and locally signed release metadata:

```sh
go test -count=1 ./internal/notifications ./internal/components ./internal/update ./internal/httpapi ./cmd/xkeen-control
npm --prefix web run test:system-panel-ui
```

It verifies protected strict authority reads/atomic mutations, secretless API
and log projections, fixed Telegram host/TLS/no-proxy/no-redirect behavior,
unsafe DNS rejection, bounded sanitized delivery errors, disabled explicit test,
component success dedupe and next-cadence retry, and read-only panel discovery
that cannot arm or alter explicit checked Apply. Deterministic concurrency
fixtures pause both schedulers at final delivery admission, complete policy
mutation, and verify no old-epoch send starts; already admitted sends finish
without blocking policy mutation. System browser fixtures cover
credential clearing/storage absence, the notification controls, stable notify
and unsupported beta/auto-stable projections. No real Telegram credential,
provider passthrough, router access or live delivery is part of these tests.
The final clean exact-HEAD full gate includes these Go/browser regressions and
the existing component/release/integration suites. Section B remains a separate
review/implementation gate; A does not close Issue #99.

Issue #115's scheduler notification-budget regression uses standard
`testing/synctest` virtual time with the actual production check/delivery/cycle
allowances. It verifies every later check's remaining budget, all bounded
delivery attempts, failed-delivery dedupe and exact virtual elapsed time; it
does not scale a wall-clock deadline or change production limits.

| Entry point | Unique purpose | Aggregate full behavior |
| --- | --- | --- |
| `go test -count=1 ./...` | Complete normal Go package suite | Once |
| `go test -race ./...` | Cross-package race detection | Full only, once |
| `test-c1.sh`, `test-backup.sh`, `test-restore.sh`, `test-setup.sh` | Convenient focused package subsets | Not repeated after the complete Go suite |
| `test-components.sh` | Focused component packages/race plus prohibited-surface assertions | Aggregate uses `--fixtures-only` |
| `test-release.sh` | Focused release packages plus bootstrap/updater/legacy integration | Aggregate uses `--fixtures-only` |
| `test-appliance.sh` | Binary-level appliance/deploy candidate integration | Retained when helpers/build paths change |
| `test-keenetic-env.ps1`, `test-keenetic-env.sh` | Synthetic operator-local environment parser and secret-output boundaries | Helper lane runs both host and container fixtures |
| `test-benchmark-policy.sh`, `test-xkeen-foreground.sh` | Legacy-writer retirement and foreground runtime shell contracts | Retained when helpers/build paths change |
| Playwright per-area scripts | Focused behavioral UI iteration | `test:ui` once in full mode |
| `npm-audit.sh` | Bounded high-severity dependency audit with transient endpoint retries | Full only |

Legacy bridge fixtures remain required while the published `v0.1.1` adoption path is supported. Age or a `legacy` name alone is not a deletion criterion; remove a test only when its supported invariant is retired or equivalent unique behavior is demonstrably covered elsewhere.

## Fresh-checkout expectation

A fresh clone/checkout of PR HEAD must be sufficient for documented qualification. Do not depend on untracked generated files, local secrets, an already-built binary, host npm/go packages or production registry/config files.

## Router SSH boundary

Router qualification is host-side and issue-authorized. Never mount router credentials/private keys, production registry files or subscription credentials into the development container or release workflow.

The operator-local `.env.keenetic` loaders have independent synthetic fixtures:

```powershell
pwsh -NoProfile -File scripts/test-keenetic-env.ps1
```

```sh
bash scripts/test-keenetic-env.sh
```

They exercise the closed bounded host/port/user/authentication grammar,
external-path enforcement, metadata-only private-identity validation,
regular-file handling, stale-variable cleanup and secret-free output. The
fixtures create only temporary placeholder values and never open an SSH
connection. Both run from the helper lane of `dev-check`.

The real credential environment and private identity must live in an
operator-owned host path outside every repository checkout. PowerShell receives
that absolute path through `-EnvFile`; Bash receives it through the host-only
`KEENETIC_ENV_FILE` selector. Repo-local `.env.keenetic*` ignores are only
defense-in-depth. Because real credentials are external, ordinary Docker build
contexts and the `.:/workspace` qualification bind mount cannot read, send or
mount them.

Build/test first, then copy/use only the exact release/artifact/scripts required for the bounded smoke. Snapshot affected state, use repository/typed transactions, sanitize evidence and remove temporary uploads/tunnels afterward.

Go/Node/build tooling is never installed on Keenetic.
