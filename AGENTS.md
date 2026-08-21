# AGENTS.md

This is the mandatory entry point for coding/review agents in this repository.

The goal is **high signal with minimal context**. Do not preload every document “just in case”. Read the active issue, inspect the affected code, then open only the authorities required by the task.

## 1. Minimal boot sequence

Always establish:

1. current remote `main` (or exact current PR HEAD for review);
2. the active GitHub issue when implementing/reviewing a slice;
3. `SECURITY.md` — the short non-negotiable secret/safety boundary.

Then inspect the affected code/files before loading more documentation.

### Read only what the task needs

| Task | Additional authority to read |
| --- | --- |
| New architecture / new slice | `docs/ARCHITECTURE.md`, `docs/ROADMAP.md`; use `docs/DEVELOPMENT-PROCESS.md` only for workflow details |
| Go control plane / API / UI | `docs/CONTROL-PLANE.md` plus the active issue |
| Build, CI, release, packaging | `docs/DEVELOPMENT.md` plus the active issue |
| Production deploy / mutation / rollback | `docs/OPERATIONS.md`; add `docs/FRESH-KEENETIC.md` only for bootstrap/fresh-install work |
| Routing / DNS / Xray policy | `docs/ARCHITECTURE.md` plus the exact relevant `config/` files |
| Docs-only change | the documents being changed and any authority they explicitly claim to summarize |
| PR review | active issue + exact HEAD/diff + only docs governing the changed subsystem |

Do **not** read `FRESH-KEENETIC.md` for ordinary application changes. Do **not** read the full development-process document merely to implement an already-specified issue. Do **not** load the whole docs directory for completeness.

If code, issue and an authority document conflict, stop broad implementation, determine the factual current state, and update the stale authority before proceeding.

## 2. Repository invariants

- Git, public issues/PRs, CI and release artifacts are secretless. Never commit or print production VLESS URLs, UUIDs, REALITY keys, short IDs, subscription URLs/tokens, passwords, SSH credentials or secret-bearing backups.
- `/opt/etc/xkeen-control/secrets/nodes.json` is the authoritative production node/subscription registry. Active `04_outbounds.json` is generated runtime output, not a second authority.
- Current production routing/DNS/Observatory policy is repository-derived until the local appliance-state migration is implemented. Node-only mutations must not silently regenerate unrelated policy.
- High-churn/transient state belongs in RAM or `/tmp`; persistent router writes must be explicit and bounded.
- No generic shell, PTY, command runner, file manager or arbitrary raw-config API.
- The panel is loopback / exact trusted-LAN / management-VPN only. Never open a wildcard/public listener or WAN firewall rule for convenience.
- Go/Node/build toolchains stay off Keenetic.
- Production evidence in public GitHub must be sanitized: prefer versions, bounded counts, state transitions and hashes. Do not publish live infrastructure details unless they are intentionally public product information.

## 3. Architecture and implementation flow

For a non-trivial new slice, the architecture/review agent first investigates current `main`, runtime constraints and relevant code, then creates/refreshes a detailed GitHub issue containing goal, current state, decisions, scope/non-goals, security/resource invariants, tests, Keenetic qualification, rollback and acceptance criteria.

Implementation agents:

- start from current remote `main`;
- use a dedicated branch and one Draft PR;
- implement the issue contract rather than inventing a competing architecture;
- keep unrelated refactors out of the PR;
- record exact HEAD and exact qualification evidence;
- never merge their own PR.

The implementation prompt should normally be only repository + issue number + any execution constraint not already in the issue.

## 4. Build / test contract

For code, frontend, build, packaging or operational-script changes, use the supported Docker/Linux qualification described in `docs/DEVELOPMENT.md`.

Primary Windows-host command:

```powershell
pwsh -NoProfile -File scripts/dev-check.ps1
```

Run focused fixtures for the subsystem changed in addition to the full check. Do not claim checks that were skipped.

For docs-only changes, do not run expensive application qualification by ritual. Check links/references, Markdown/content consistency and `git diff --check`; run code/build tests only if the docs change generated/embedded/build-controlled artifacts.

## 5. Production Keenetic boundary

Production is a live router. Mutate it only when the active issue explicitly requires bounded qualification.

Before mutation, inspect current state and take the rollback snapshot required by the issue. Use repository/typed transactional paths rather than ad-hoc edits.

Unless the active issue explicitly authorizes it, do not run:

```text
opkg upgrade
reboot
credential rotation
xkeen -sbt / full sustained benchmark
unbounded logs/downloads
```

Never mount or copy router credentials into development containers or GitHub artifacts.

## 6. Review loop

Review the **exact current PR HEAD**, not the agent report. Read the changed code and supporting call paths, the active issue, prior review findings, and only the subsystem authorities needed for those changes.

Put concrete findings in the PR with severity (`P1`, `P2`, …), failure mode and required result. Keep/return the PR Draft while blocking findings remain. A short correction prompt should point Codex to the review comment rather than duplicate it.

Small obvious safe fixes may be made directly on the PR branch. Architectural changes go back into the issue first.

When clean, record `APPROVED / READY TO MERGE` and mark the PR Ready. Merge only after explicit operator authorization and only if the approved HEAD has not changed.

## 7. After merge

Confirm the issue is closed/completed, update `docs/ROADMAP.md` and master issue #1 when sequencing/current-state claims changed, remove stale planning evidence, and refresh the next issue against the new `main`.

## 8. Current direction

Do not memorize sequencing from this file; `docs/ROADMAP.md` is authoritative. The product direction after C.1 is an installable XKeen/Xray appliance manager: signed releases/bootstrap/self-update, portable local appliance state, managed component lifecycle, then typed visual configuration.
