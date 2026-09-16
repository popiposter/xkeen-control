---
goal: Safely classify and project Xray candidate preparation rejection reasons
version: 1.0
date_created: 2026-09-16
last_updated: 2026-09-16
owner: Codex
status: 'In progress'
tags: [feature, xray, http, ui, security]
---

# Introduction

![Status: In progress](https://img.shields.io/badge/status-In%20progress-yellow)

Implement Issue #64 from exact remote `main` by retaining the stable
`candidate-rejected` outcome while carrying only a closed, sanitized Xray
pre-commit reason through the component broker, HTTP API and Components /
Updates UI. Correct the compact D.2 ROADMAP status in the same change.

## 1. Requirements & Constraints

- **REQ-001**: Keep `candidate-rejected` as the stable top-level error code and HTTP status.
- **REQ-002**: Classify Xray preparation failures as `artifact-download`, `artifact-integrity`, `archive-extract`, `binary-probe`, `candidate-render`, `candidate-config-validation`, `staging-io`, or generic `candidate-validation`.
- **SEC-001**: Never expose raw errors, paths, URLs, response bodies, config contents, command output or secrets through the error chain’s text or HTTP/UI projection.
- **SEC-002**: Preserve pre-commit no-mutation, bounded temporary staging and existing transaction/recovery trust boundaries.
- **CON-001**: Do not change unknown, rollback, maintenance, stable-release selection, no-auto-retry or no-replay semantics.
- **CON-002**: Do not access or mutate a production router; use synthetic/offline fixtures only.
- **DOC-001**: Update `docs/CONTROL-PLANE.md` only for the API contract and update the compact D.2 status in `docs/ROADMAP.md`.

## 2. Implementation Steps

### Implementation Phase 1

- GOAL-001: Add closed typed Xray reason classification at every bounded candidate-preparation boundary.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Add the closed Xray reason type and generic-safe rejection error in `internal/components/xray.go`. | ✅ | 2026-09-16 |
| TASK-002 | Map download, integrity, archive, probe, render, validation and staging failures without retaining raw causes. | ✅ | 2026-09-16 |
| TASK-003 | Preserve reason codes through `internal/components/mutation.go` while retaining all existing sentinel classification. | ✅ | 2026-09-16 |

### Implementation Phase 2

- GOAL-002: Project the safe reason to authenticated operators and document the corrected sequencing state.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-004 | Add optional `reasonCode` only to HTTP `candidate-rejected` responses in `internal/httpapi/server.go`. | ✅ | 2026-09-16 |
| TASK-005 | Parse and allowlist candidate reason codes in `web/src/components-updates.jsx`; render a concise explanation without automatic retry. | ✅ | 2026-09-16 |
| TASK-006 | Add synthetic component, HTTP and UI regression coverage and update `docs/CONTROL-PLANE.md` plus compact D.2 `docs/ROADMAP.md` status. | ✅ | 2026-09-16 |

## 3. Alternatives

- **ALT-001**: Forward the underlying error text; rejected because it can disclose paths, URLs, output or candidate details.
- **ALT-002**: Introduce a diagnostic endpoint or persistent rejection history; rejected because Issue #64 requires a narrow typed projection and no new raw diagnostic surface.
- **ALT-003**: Change the top-level error code per failure class; rejected because the existing `candidate-rejected` contract and fail-closed semantics must remain stable.

## 4. Dependencies

- **DEP-001**: Existing Xray transaction preparation, staging, archive validation and candidate validator seams.
- **DEP-002**: Existing F1 typed component mutation broker and authenticated HTTP error writer.
- **DEP-003**: Existing F2 Components / Updates UI and pinned synthetic Playwright fixtures.

## 5. Files

- **FILE-001**: `internal/components/xray.go` — typed reason set and preparation classification.
- **FILE-002**: `internal/components/mutation.go` — F1 reason preservation.
- **FILE-003**: `internal/httpapi/server.go` — optional safe HTTP field.
- **FILE-004**: `web/src/components-updates.jsx` — allowlisted operator explanation.
- **FILE-005**: `internal/components/xray_test.go`, `internal/components/mutation_test.go` — core and broker regressions.
- **FILE-006**: `internal/httpapi/component_mutation_test.go`, `web/tests/components-updates.spec.js` — HTTP/UI regressions.
- **FILE-007**: `docs/CONTROL-PLANE.md`, `docs/ROADMAP.md` — contract and sequencing correction.

## 6. Testing

- **TEST-001**: Run `go test -count=1 ./internal/components ./internal/httpapi`.
- **TEST-002**: Run `bash scripts/test-components.sh` for focused component/HTTP synthetic qualification.
- **TEST-003**: Run the pinned `npm run test:components-ui` suite after frontend build/dependency setup.
- **TEST-004**: Run `pwsh -NoProfile -File scripts/dev-check.ps1` and record sanitized exact-HEAD results.
- **TEST-005**: Run `git diff --check` and verify tracked embedded assets match the built UI.

## 7. Risks & Assumptions

- **RISK-001**: A future failure path could bypass the closed reason mapper; every current preparation return is covered by focused synthetic tests.
- **RISK-002**: UI or HTTP consumers could display an untrusted reason string; both boundaries allowlist the closed set before projection.
- **ASSUMPTION-001**: The existing Xray preparation boundaries are the complete pre-commit surface for this issue.
- **ASSUMPTION-002**: Gate 3 is complete and the Xray live pair remains blocked pending the source correction and revised beta installation; this is documentation context only.

## 8. Related Specifications / Further Reading

- [Issue #64](https://github.com/popiposter/xkeen-control/issues/64)
- `SECURITY.md`
- `docs/CONTROL-PLANE.md`
- `docs/ROADMAP.md`
- `docs/DEVELOPMENT.md`
