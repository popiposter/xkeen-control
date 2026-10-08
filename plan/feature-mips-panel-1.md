---
goal: Signed panel-only delivery for Ultra KN-1810
version: 1
date_created: 2026-10-08
last_updated: 2026-10-08
owner: xkeen-control
status: 'In progress'
tags: [feature, packaging, mips]
---

# Introduction

![Status: In progress](https://img.shields.io/badge/status-In%20progress-yellow)

[Issue150](https://github.com/popiposter/xkeen-control/issues/150) adds linux/mipsle soft-float alongside ARM64. Operator baseline: Ultra KN1810, KeeneticOS5.1.7, Entware mipsel-3.4, stock XKeen2.0.1 Beta/Xray26.7.28. Existing native configuration and external watchdog remain untouched by panel-only bootstrap.

## 1. Requirements & Constraints

- **REQ-001**: Publish ten exact assets: the existing seven ARM64 names plus `xkeen-control-linux-mipsle`, `release-manifest-mipsle.json`, `release-manifest-mipsle.sig`. Each schema1 platform manifest references exactly its own binary and the same init/installer/updater.
- **REQ-002**: Fixed current-platform client selection rejects foreign manifests before staging. Preserve old ARM64 manifest URLs/validation.
- **REQ-003**: MIPS builds use `CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat`; unknown/big-endian Entware targets refuse installation before writes.
- **SEC-001**: Existing pinned signature, exact size/hash/source/channel/version checks and protected signing remain mandatory. Private router material never enters build/Git/public evidence.
- **CON-001**: No native upgrade/patch, watchdog deletion, routing/DNS/HOME mutation or fresh MIPS setup. No merge/release/router mutation in the source implementation slice.
- **PAT-001**: Use existing panel update/rollback owners and one previous generation; do not replay ambiguous operations.

## 2. Implementation Steps

### Implementation Phase 1

- GOAL-001: Define platform identity and signed candidate boundaries; phase complete when both manifest/client regression suites pass.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Add fixed artifact/manifest names in `internal/release/platform.go`; extend `BuildManifest`, `Manifest.Validate`, `VerifyCandidate` in `internal/release/manifest.go` with architecture-specific exact sets. | ✅ | 2026-10-08 |
| TASK-002 | Extend `Client.Check`/`FetchCandidate` in `internal/release/client.go` with fixed runtime selection and synthetic test injection; reject cross-platform manifests. Extend `cmd/xkeen-release/main.go` with explicit manifest/verification architecture. | ✅ | 2026-10-08 |
| TASK-003 | Use the current client's binary name for previous generation admission in `internal/update/manager.go`; test MIPS rollback/candidate selection. | ✅ | 2026-10-08 |

### Implementation Phase 2

- GOAL-002: Deliver both platforms without changing ARM64 installed contracts. Depends on Phase1; complete when shell fixtures and both actual cross-builds pass.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-004 | Select platform in `scripts/install.sh` using uname plus fixed Entware architecture confirmation; choose suffixed MIPS manifests and binary. Reject MIPS setup/legacy adoption before writes. Verify exact shared checksum set while downloading only current-platform assets. | ✅ | 2026-10-08 |
| TASK-005 | Select fixed binary name in `scripts/xkeen-control-updater` for snapshot/install/recovery/rollback; add synthetic MIPS lifecycle coverage in `scripts/test-updater.sh`. | ✅ | 2026-10-08 |
| TASK-006 | Build both binaries in `scripts/build-control-plane.sh`, `scripts/dev-check.sh` and `scripts/release-build.sh`; inspect ELF and softfloat Go metadata off-router. Add bootstrap/build fixtures. | ✅ | 2026-10-08 |
| TASK-007 | Extend `.github/workflows/release.yml` to hand off both unsigned manifests/binaries, sign both, verify equal provenance/exact ten assets, re-download and validate both before publishing. Preserve protected environment separation and final-main check. | ✅ | 2026-10-08 |

### Implementation Phase 3

- GOAL-003: Qualify exact source and document installation limits. Depends on Phase2; complete when clean FULL, exact source review and Draft PR evidence are recorded. Publication/hardware are separate subsequent decisions.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-008 | Run actual emulated MIPS startup/32-bit smoke if emulator available; record unavailable/failure honestly. Update `docs/DEVELOPMENT.md`, `docs/RELEASES.md`, `docs/QUICKSTART-RU.md`, `docs/ROADMAP.md` with source-versus-published/hardware boundaries. | | |
| TASK-009 | Commit final candidate; run `pwsh -NoProfile -File scripts/dev-check.ps1 -Full` once on clean exact HEAD. Record both binary hashes and independent review in Draft PR/Issue150. | | |

## 3. Alternatives

- **ALT-001**: Unsigned manual copy was rejected because it bypasses the software installation authority and update/rollback checks.
- **ALT-002**: Replace ARM64 manifest with a multi-platform schema was rejected because installed ARM64 clients would stop updating. Separate MIPS manifests preserve those URLs/schema/artifact sets.
- **ALT-003**: Fresh setup on KN1810 was excluded because its firmware/RCI/DNS matrix is unqualified and native components already exist.

## 4. Dependencies

- **DEP-001**: Repository Docker/Linux Go1.27 toolchain and embedded web assets.
- **DEP-002**: Existing protected release signing environment, used only after exact-source approval/authorized publication.
- **DEP-003**: Operator shell on the KN1810 for later private snapshots and hardware acceptance; unavailable to this source task.

## 5. Files

- **FILE-001**: `internal/release/{platform,manifest,client}.go` and tests; `cmd/xkeen-release/main.go`.
- **FILE-002**: `internal/update/manager.go` and tests; `scripts/{install.sh,xkeen-control-updater,test-bootstrap.sh,test-updater.sh}`.
- **FILE-003**: `scripts/{build-control-plane.sh,release-build.sh,dev-check.sh,test-build-embedded.sh}`; `.github/workflows/release.yml`.
- **FILE-004**: Development/release/quickstart/roadmap documents and this plan.

## 6. Testing

- **TEST-001**: Both manifest/signature/hash/asset-set variants, foreign platform rejection, old ARM64 validation.
- **TEST-002**: MIPS bootstrap refuses unknown/big-endian/setup before mutation; successful panel-only fixtures preserve native config/watchdog/cron.
- **TEST-003**: MIPS install/update/failed-candidate recovery/rollback; reject ARM64-only previous generation on MIPS.
- **TEST-004**: Two-platform workflow exact set/signature/provenance checks and existing protected browser/root/signing separation.
- **TEST-005**: Actual static little-endian ELF/MIPS softfloat build and emulated startup separate from hardware; full clean exact-HEAD gate.

## 7. Risks & Assumptions

- **RISK-001**: Cross-compilation/emulation do not qualify KN1810 kernel, memory, authentication performance, PTY/native jobs or LAN availability. Hardware stays NOTRUN.
- **RISK-002**: An external watchdog may change native config/runtime while panel operations run. Inspect privately and quiesce only confirmed competing entries before configuration Apply; bootstrap never deletes it.
- **ASSUMPTION-001**: Operator `free` values are KiB despite `-m`; approximately249MiB total/105MiB available is an observation, not RAM suitability proof. Swap does not establish suitability.

## 8. Related Specifications / Further Reading

[Native contract](../docs/NATIVE-XKEEN.md), [Security](../SECURITY.md), [Development](../docs/DEVELOPMENT.md), [Operations](../docs/OPERATIONS.md), [Release](../docs/RELEASES.md), [second-router acceptance](https://github.com/popiposter/xkeen-control/issues/135).
