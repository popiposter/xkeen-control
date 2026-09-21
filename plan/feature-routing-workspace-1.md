---
goal: Implement the Issue #85 visual Routing workspace over the existing typed policy broker
version: 1.0
date_created: 2026-09-20
last_updated: 2026-09-20
owner: Codex
status: Completed
tags: [feature, frontend, routing, playwright]
---

# Introduction

![Status: Completed](https://img.shields.io/badge/status-Completed-brightgreen)

This plan implements Issue #85 from `origin/main` at `e51927b75c131549a5c17570b12546abb07e4d14`. It adds only the operator-facing Routing workspace over the unchanged `/api/v1/appliance/policy` broker and records focused browser plus full repository qualification.

## 1. Requirements & Constraints

- **REQ-001**: Add `Routing` after `Nodes`; load one fresh typed policy projection lazily and only refresh by explicit or bounded outcome-driven requests.
- **REQ-002**: Keep the controller at Dashboard scope with ordered draft, dirty state, Preview/token/expiry, Apply state, outcomes, and session/late-response guards.
- **REQ-003**: Send only the complete ordered `rules` DTO to Preview and only `{previewToken}` to Apply/Cancel.
- **REQ-004**: Expose only supported rule fields, protected/DNS/Observatory facts, semantic diff facts, and conservative outcome messages.
- **SEC-001**: Never render or submit protected rules, generated configuration, node/subscription material, paths, URLs, commands, or browser-storage state.
- **SEC-002**: Preserve session-bound, one-shot, non-replayed Apply semantics and fail closed for drift, unavailable, lifecycle-blocked, stale, expired, and unproven outcomes.
- **CON-001**: Do not change Go broker/API/transaction semantics, DNS authority, Observatory authority, routing grammars, or lifecycle owners.
- **CON-002**: Do not access the router, perform live mutation, dispatch a release, or claim Keenetic qualification.
- **TEST-001**: Add dedicated intercepted-local-API Playwright coverage for Issue #85 and retain all existing browser suites.
- **TEST-002**: Run `pwsh -NoProfile -File scripts/dev-check.ps1` and bind sanitized results to the pushed exact HEAD.

## 2. Implementation Steps

### Implementation Phase 1

- GOAL-001: Build the Dashboard-owned Routing controller and typed editor.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Add `web/src/routing-policy.jsx` with typed projection validation, draft editing, ordered rule operations, lifecycle/session/navigation guards, Preview/Apply/Cancel, semantic outcomes, and global pending/unknown notices. | ✅ | 2026-09-20 |
| TASK-002 | Integrate the controller and Routing navigation/view into `web/src/main.jsx` without adding policy polling or changing existing broker calls. | ✅ | 2026-09-20 |
| TASK-003 | Add responsive and accessible Routing workspace styles to `web/src/styles.css`. | ✅ | 2026-09-20 |

### Implementation Phase 2

- GOAL-002: Verify the frozen broker integration and prepare the Draft PR.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-004 | Add `web/tests/routing-policy.spec.js` with intercepted local API responses covering the Issue #85 acceptance matrix, mobile/desktop screenshots, storage, and console/page-error hygiene. | ✅ | 2026-09-20 |
| TASK-005 | Run focused Routing Playwright coverage and `pwsh -NoProfile -File scripts/dev-check.ps1`; inspect actual exit status and required artifacts. | ✅ | 2026-09-20 |
| TASK-006 | Commit only the Issue #85 implementation/tests/assets/plan, push `codex/issue-85-routing`, create a Draft PR, and record exact SHA plus sanitized evidence without merging or releasing. | ✅ | 2026-09-20 |

## 3. Alternatives

- **ALT-001**: Add routing state and mutation logic directly to `web/src/main.jsx`; rejected because Issue #85 requires a focused module and Dashboard-owned controller without enlarging the existing entrypoint.
- **ALT-002**: Add browser-side copies of the server routing grammar; rejected because the server remains the acceptance authority and the browser must submit typed expression text unchanged.
- **ALT-003**: Add a new backend route or transaction owner; rejected because the #83 broker and D.1 settings transaction are frozen for this slice.

## 4. Dependencies

- **DEP-001**: Existing authenticated `GET /api/v1/appliance/policy` projection.
- **DEP-002**: Existing CSRF-bound `/preview`, `/apply`, and `/cancel` broker endpoints.
- **DEP-003**: Existing React/Vite/Playwright toolchain and `scripts/dev-check.ps1` qualification path.

## 5. Files

- **FILE-001**: `web/src/routing-policy.jsx` — controller, typed rule editor, facts, preview, apply, and outcome UI.
- **FILE-002**: `web/src/main.jsx` — Dashboard-scoped controller integration, navigation, and global notices.
- **FILE-003**: `web/src/styles.css` — Routing workspace, editor, facts, diff, and responsive styles.
- **FILE-004**: `web/tests/routing-policy.spec.js` — focused synthetic Playwright acceptance coverage.
- **FILE-005**: `plan/feature-routing-workspace-1.md` — this implementation plan.
- **FILE-006**: embedded frontend output, if `dev-check` proves it is repository-controlled and changed by the UI build.

## 6. Testing

- **TEST-001**: Run the focused Routing Playwright suite against intercepted local API responses only.
- **TEST-002**: Cover lazy GET/no dashboard polling, editable and blocked projections, exact DTO serialization, ordering, dirty refresh, semantic Preview/no-op, one-shot Apply, expiry/cancel/session/navigation races, lifecycle gating, conservative outcome codes, storage, and responsive/error hygiene.
- **TEST-003**: Run `pwsh -NoProfile -File scripts/dev-check.ps1` from the final tested HEAD.
- **TEST-004**: Verify `git diff --check`, exact `git rev-parse HEAD`, Draft/open/unmerged PR state, and sanitized evidence boundaries.

## 7. Risks & Assumptions

- **RISK-001**: Late GET/Preview/Apply responses can overwrite a newer session or navigation state; sequence, epoch, and consumed-token guards must reject stale continuations.
- **RISK-002**: A lost Apply response has an unknown outcome; the UI must persist the warning and never replay the consumed token.
- **RISK-003**: A malformed or drifted projection could accidentally become editable; projection validation and explicit editability gates must fail closed.
- **ASSUMPTION-001**: The existing #83 response shapes and safe error codes are the only backend contract available to this PR.
- **ASSUMPTION-002**: `web/dist` or other embedded assets are regenerated only if the repository qualification script requires and verifies them.

## 8. Related Specifications / Further Reading

- [Issue #85](https://github.com/popiposter/xkeen-control/issues/85)
- `SECURITY.md`
- `docs/CONTROL-PLANE.md`
- `web/src/components-updates.jsx`
