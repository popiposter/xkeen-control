# Development process

This document defines the repository workflow. Agents should **not** read it for every task: root `AGENTS.md` tells them when process details are actually needed.

Architecture belongs in the active GitHub issue; implementation details belong in code/tests/PR evidence.

## Roles

### Operator / owner

Chooses priorities, authorizes risky production actions and explicitly authorizes merge.

### Architecture / review agent

Owns framing, architecture, sequencing and review quality:

1. restore factual state from current `main` and the relevant code;
2. create/refresh the detailed issue for non-trivial work;
3. give implementation agents short issue-referencing prompts;
4. review exact current PR HEAD/diff/code;
5. put concrete `P1`/`P2` findings in the PR;
6. keep blocking PRs Draft;
7. record `APPROVED / READY TO MERGE` only when clean;
8. merge only after explicit operator authorization;
9. refresh roadmap/master planning after merge.

### Implementation agent

Owns code, tests, build/release artifacts and bounded qualification. It does not own the architecture contract and does not merge its own work.

## Slice lifecycle

### 1. Architecture issue

Before a non-trivial slice starts, investigate current code/runtime rather than designing from stale documentation. The issue should contain goal/operator-visible outcome, current-state facts/dependencies, decisions/invariants, scope/non-goals, security/resource/flash constraints, API/runtime/data changes where relevant, focused tests, production Keenetic qualification, rollback/failure semantics and acceptance criteria.

If implementation evidence disproves an assumption, update the issue before broadening architecture.

### 2. Short implementation prompt

Preferred shape:

```text
Work in popiposter/xkeen-control. Implement Issue #N according to AGENTS.md and the issue. Start from current main, dedicated branch + Draft PR, do not merge. Run the documented qualification and only the bounded production checks authorized by the issue. Record sanitized exact-HEAD evidence.
```

Do not paste the issue into chat again.

### 3. Branch + Draft PR

- branch from current remote `main`;
- one focused branch/PR per slice unless architecture explicitly says otherwise;
- use Draft while implementation/review is incomplete;
- PR body references/closes the issue where appropriate;
- record base/current HEAD, actual checks, artifact identity and explicit skips;
- do not describe local Docker qualification as hosted CI.

This repository is public. PR bodies/comments/logs are public surfaces and production evidence must be sanitized.

### 4. Review

On `Проверяй #N`, fetch exact PR HEAD, changed files/diff, previous reviews/comments and the active issue. Read only subsystem authorities relevant to the change.

Review priorities:

1. secret/security/public-exposure problems;
2. unsafe update/supply-chain behavior;
3. transaction/rollback/failure semantics;
4. issue-architecture violations;
5. incorrect XKeen/Xray/Keenetic assumptions;
6. concurrency/resource/flash regressions;
7. tests that fail to prove real failure paths;
8. operator/UI/docs correctness.

A finding needs severity, concrete failure mode and required result. Small obvious safe fixes may be made directly on the PR branch. Architectural changes go back through the issue.

### 5. Qualification

For code/build/frontend/packaging/operational-script changes, use `docs/DEVELOPMENT.md` plus focused fixtures.

Production smoke is additive evidence, not a replacement for tests. It must use the active issue's bounded safety contract and report sanitized facts only.

Docs-only changes use proportional checks: links/references/Markdown consistency and diff hygiene; do not run expensive builds by ritual unless the docs affect generated/build-controlled artifacts.

### 6. Ready / merge

When no blocking finding remains, record `APPROVED / READY TO MERGE` and mark Ready.

Merge only after explicit operator command and only if the approved HEAD has not moved. Prefer squash for feature/review series.

### 7. Post-merge

Immediately:

- confirm issue closure/completion;
- update `docs/ROADMAP.md` only if sequencing/status changed;
- refresh master issue #1 if stale;
- remove/refresh contradictory planning evidence;
- refresh the next active issue against the new `main`.

## Public repository / release discipline

`popiposter/xkeen-control` is already the public source/CI/release authority. The historical `popiposter/xkeen-keenetic` repository is private quarantine/history only and its Git history must never be imported here.

- no production secrets in commits, issues, PRs, Actions or artifacts;
- no raw router configuration/registry dumps;
- no production credentials in CI/release jobs;
- release inputs are source + synthetic fixtures only;
- signed immutable release artifacts are introduced by Issue #2;
- router-specific settings remain local and are never auto-synchronized from public Git.

## Production mutation boundary

Implementation agents may perform only mutations authorized by the active issue. No generic shell/browser command plane is ever introduced.

Without explicit issue/operator authority, do not:

```text
merge
rewrite shared history
rotate production credentials
opkg upgrade
reboot
run a sustained/full benchmark
perform a generic package/OS update
```

## Documentation ownership

Avoid volatile duplication:

- `SECURITY.md` — security/public-exposure invariants;
- `ARCHITECTURE.md` — current production architecture;
- `ROADMAP.md` — sequencing/status;
- this file — workflow;
- `DEVELOPMENT.md` — build/test/CI contract;
- `OPERATIONS.md` / `FRESH-KEENETIC.md` — production/runbook;
- `CONTROL-PLANE.md` — current control-plane runtime/API;
- active issue — detailed in-progress architecture.
