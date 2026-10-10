# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**Read [AGENTS.md](AGENTS.md) first.** It is the mandatory entry point and a task router: it says which single authority document to open for each kind of task. Do not preload the `docs/` directory. Read [SECURITY.md](SECURITY.md) (secret boundary) before any change, and treat `docs/ROADMAP.md` as the only source of sequencing and status. This file adds only orientation that AGENTS.md leaves out.

## What this is

XKeen Control is an authenticated web panel that runs **alongside unmodified stock XKeen/Xray** on Keenetic routers (`linux/arm64` and `linux/mipsle` soft-float). It ships as one static Go binary with the React UI embedded. XKeen owns installation, component updates, interception, init and cron. Xray owns routing and balancer selection. The panel calls allowlisted native XKeen commands, edits native Xray config files through fixed config IDs, and owns the private node/subscription registry (`/opt/etc/xkeen-control/secrets/nodes.json`), from which managed `04_outbounds.json` entries are generated. The governing contract is `docs/NATIVE-XKEEN.md`, with the v2 plan at `plan/architecture-native-shell-v2.md`. Documents under `docs/archive/` describe the historical "appliance" design and are not authority.

## Layout (big picture)

- `cmd/xkeen-control/` contains the panel binary: `main.go`, the native wiring (`native.go`), the panel lifecycle and the `setup` subcommand used by the guided fresh install (`setup.go`; `scripts/install.sh --setup` calls it). `cmd/xkeen-release/` is the release tooling.
- `internal/httpapi/` holds the HTTP API server and routes: native config, native jobs/console, quality, transfer, geodata, notifications and split DNS. Auth, CSRF, Origin and Host checks plus private-management listener rules live here, in `internal/auth`, and in `internal/panellistener`.
- `internal/xkeen`, `internal/nodes`, `internal/nativequality`, `internal/configjson`/`configview`, `internal/nativebackup`, `internal/update`, `internal/setup` and `internal/keenetic` contain the domain logic. Their package names match their subsystems.
- `web/` holds the React 19 + Vite + shadcn/base-ui source (`web/src/native-*.jsx` are the workspace pages). The built output is **tracked** in `internal/webassets/dist/` and embedded with Go `embed`, so a clean checkout compiles without a frontend build.
- `config/` holds the reference Xray/XKeen configs and presets. `packaging/` holds the init script. `scripts/` holds the dev-check orchestration, fixture shell tests (`test-*.sh`), installer and release build.

## Commands

Supported toolchain: Docker Desktop with Linux containers (`Dockerfile.dev`: Go 1.27, Node 24, Playwright Chromium). There is no PR CI. Local qualification is the gate. Never run Go, Node or build tooling on the router.

```powershell
pwsh -NoProfile -File scripts/dev-check.ps1          # fast proportional check of working changes (or last commit if clean)
pwsh -NoProfile -File scripts/dev-check.ps1 -Plan    # print the selected check plan only
pwsh -NoProfile -File scripts/dev-check.ps1 -Scope branch -Base origin/main
pwsh -NoProfile -File scripts/dev-check.ps1 -Full    # final exact-HEAD gate; requires clean tree, run once
docker compose -f docker-compose.dev.yml run --rm dev bash   # interactive dev container
```

Focused iteration inside the container:

```sh
go test -count=1 ./internal/httpapi -run TestName
go test -count=1 ./internal/auth ./internal/httpapi ./cmd/xkeen-control
npm --prefix web run test:unit
npm --prefix web run test:nodes-ui              # one Playwright spec; see web/package.json for the rest
(cd web && npx playwright test tests/native-config.spec.js -g "title")
bash scripts/update-webassets.sh                # regenerate tracked internal/webassets/dist after UI source changes
ARCHITECTURE=mipsle ./scripts/build-control-plane.sh   # build dist/xkeen-control-linux-<arch>; default arm64
```

Playwright specs mock the API and navigate through `openSection` in `web/tests/fixtures/disclosures.js`; the dashboard has six sections (Overview, Nodes, Performance, Configuration with Routing/DNS/All files tabs, XKeen, System with a Backup tab). After changing frontend source, regenerate the embedded assets explicitly and review that diff. The check scripts only compare the embedded output and never rewrite it. For docs-only changes, check links and content and run `git diff --check` instead of the code gates. Do not run `-Full` after every edit, and do not reuse its evidence after HEAD changes.

## Workflow constraints worth remembering

- **Never use Git worktrees** in this repo. Use one normal checkout, a dedicated branch and one Draft PR per slice, starting from current remote `main`.
- Never modify XKeen code, dispatcher, init, hooks or modules, and never add a second component updater. Do not add a generic shell, file manager or raw-config API.
- Production is a live router. Mutate it only when the active issue authorizes it. The standing authority to merge and release is described in AGENTS.md §6.
