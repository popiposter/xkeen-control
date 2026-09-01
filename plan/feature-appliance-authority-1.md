---
goal: Phase A typed appliance authority and zero-mutation adoption
version: 1.0
date_created: 2026-09-01
last_updated: 2026-09-01
owner: Codex
status: 'In progress'
tags: [feature, appliance, migration, xray, security]
---

# Introduction

![Status: In progress](https://img.shields.io/badge/status-In%20progress-yellow)

Implement Phase A of Issue #3: a typed schema-versioned `appliance.json` authority, strict adoption/verify/render operations, compatibility gates for fixed policy inputs, generated-outbound consistency proof, complete candidate validation, and deployment integration. Later backup/export/import/UI capabilities are excluded.

## 1. Requirements & Constraints

- **REQ-001**: Start from the current remote `main` after confirming PR #15 is merged, use a dedicated branch and one Draft PR, and include `Refs #3`.
- **REQ-002**: Represent appliance v1 with explicit Go structs and deterministic canonical JSON; reject unknown fields, unsupported enums, invalid network/port grammar, unbounded documents and unsupported tags.
- **REQ-003**: Preserve Xray routing rule order and require exactly one typed outbound or balancer action per rule.
- **REQ-004**: Adoption must parse bounded regular active files, prove fixed-template compatibility and `nodes.json`/generated-outbounds consistency, validate a complete temporary candidate, and atomically write only `appliance.json`.
- **REQ-005**: Verify and render must not restart services or rewrite active runtime/configuration state; render writes only an explicit non-runtime candidate directory.
- **REQ-006**: After adoption, deployment must render appliance-managed policy from local authority; before adoption, repository defaults remain available; generated outbounds remain derived from `nodes.json`.
- **SEC-001**: Never expose or commit production credentials, node registries, generated full outbounds, router credentials or secret-bearing backups.
- **SEC-002**: Keep the #2 release/update signature, trust-anchor and rollback boundary unchanged; do not mutate production Keenetic.
- **CON-001**: Do not implement HTTP API/UI, backup/export, encryption or import/restore in Phase A.
- **CON-002**: Reuse the existing bounded `nodes.CommandActivator.ValidateCandidate` capability for Xray candidate validation.

## 2. Implementation Steps

### Implementation Phase 1

- GOAL-001: Implement and verify the typed appliance v1 model, runtime renderers and compatibility templates.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Add `internal/appliance/model.go` with explicit DNS, routing, typed port/network, action, balancer and Observatory structures plus strict decoding, bounds and canonical serialization. | ✅ | 2026-09-01 |
| TASK-002 | Add `internal/appliance/render.go` with active-policy parsing, deterministic runtime rendering, semantic JSON comparison and embedded fixed templates. | ✅ | 2026-09-01 |
| TASK-003 | Add `internal/appliance/service.go` with bounded regular-file reads, node/outbound coherence proof, zero-mutation adoption/verify, complete temporary candidate validation and root-only writes. | ✅ | 2026-09-01 |
| TASK-004 | Add focused Go fixtures for current-policy round trips, strict rejection, fixed/outbound drift blocking, candidate completeness and active-file immutability. | ✅ | 2026-09-01 |

### Implementation Phase 2

- GOAL-002: Expose Phase A operations through the local CLI and deploy path.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-005 | Add `xkeen-control appliance validate`, `adopt`, `verify` and `render --output DIR` using the existing bounded Xray validator and configured local paths. | ✅ | 2026-09-01 |
| TASK-006 | Update `scripts/deploy.sh` to select valid local appliance rendering after adoption while preserving repository-derived pre-adoption deployment. | ✅ | 2026-09-01 |
| TASK-007 | Add `scripts/test-appliance.sh` and include it in `scripts/dev-check.sh` for secretless CLI/adoption/verify/render coverage. | ✅ | 2026-09-01 |

### Implementation Phase 3

- GOAL-003: Qualify the exact final branch state and hand off an unmerged Draft PR.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-008 | Run focused Phase A tests, the pinned Docker qualification command, diff hygiene and public-secret hygiene; record exact HEAD and sanitized evidence. | | |
| TASK-009 | Create or update one Draft PR with `Refs #3`, preserve Draft/open state, and do not merge or mutate production Keenetic. | | |

## 3. Alternatives

- **ALT-001**: Keep appliance state as arbitrary JSON or raw Xray fragments; rejected because the issue requires typed bounded state and fixed compatibility failures instead of silent loss.
- **ALT-002**: Reuse repository files as the post-adoption authority; rejected because per-router policy must become local appliance authority after equivalence proof.
- **ALT-003**: Restart or rewrite runtime files during adoption; rejected because Phase A adoption is an authority commit with zero runtime mutation.

## 4. Dependencies

- **DEP-001**: Existing `internal/nodes` registry validation and deterministic outbound renderer.
- **DEP-002**: Existing `nodes.CommandActivator.ValidateCandidate` bounded `xray run -test` path.
- **DEP-003**: Embedded fixed compatibility templates matching the current repository policy.
- **DEP-004**: Docker Desktop and `scripts/dev-check.ps1` for pinned Linux/Go/Node qualification.

## 5. Files

- **FILE-001**: `internal/appliance/model.go` — typed v1 policy authority and validation.
- **FILE-002**: `internal/appliance/render.go` — active parsing, canonical rendering and fixed templates.
- **FILE-003**: `internal/appliance/service.go` — adoption, verification, candidate rendering and safe file operations.
- **FILE-004**: `internal/appliance/appliance_test.go` — Phase A behavior fixtures.
- **FILE-005**: `internal/appliance/templates/*.json` — embedded fixed Xray/XKeen compatibility inputs.
- **FILE-006**: `cmd/xkeen-control/main.go` — root-only appliance CLI dispatch.
- **FILE-007**: `scripts/deploy.sh` — post-adoption appliance authority candidate construction.
- **FILE-008**: `scripts/test-appliance.sh` and `scripts/dev-check.sh` — focused qualification fixture and registration.

## 6. Testing

- **TEST-001**: Strict schema, unknown-field, enum, typed network/port and canonical round-trip tests.
- **TEST-002**: Current repository DNS/routing/Observatory parse-render-parse and first-match rule-order tests.
- **TEST-003**: Fixed compatibility drift and nodes/generated-outbounds mismatch adoption blockers.
- **TEST-004**: Complete candidate validation, no active-file mutation, root-only appliance write and verify drift tests.
- **TEST-005**: Secretless shell CLI fixture for adopt, validate, verify, render and drift rejection.
- **TEST-006**: `pwsh -NoProfile -File scripts/dev-check.ps1` on the exact final HEAD.

## 7. Risks & Assumptions

- **RISK-001**: Existing active policy may contain unsupported/custom fields; adoption must fail closed and leave active state unchanged.
- **RISK-002**: Docker qualification from a linked Windows worktree may not resolve Git metadata; if encountered, qualify from an ordinary clean clone at the exact HEAD.
- **RISK-003**: A generated outbound artifact can diverge from its registry; adoption and verify must block rather than treat generated output as authority.
- **ASSUMPTION-001**: Current repository fixed templates and qualified production policy remain semantically aligned at the requested implementation base.
- **ASSUMPTION-002**: The existing Xray validator accepts the complete candidate assembled from the current fixed templates and typed policy.

## 8. Related Specifications / Further Reading

- [Issue #3](https://github.com/popiposter/xkeen-control/issues/3)
- [`docs/ARCHITECTURE.md`](../docs/ARCHITECTURE.md)
- [`docs/CONTROL-PLANE.md`](../docs/CONTROL-PLANE.md)
- [`docs/DEVELOPMENT.md`](../docs/DEVELOPMENT.md)
- [`SECURITY.md`](../SECURITY.md)
