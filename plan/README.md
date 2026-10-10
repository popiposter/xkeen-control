# Plans and implementation records

Current sequencing is [ROADMAP](../docs/ROADMAP.md), not the latest paragraph of an old implementation log.

## Delivered / reference

| Document | Status |
| --- | --- |
| [Native shell v2](architecture-native-shell-v2.md) | Source implementation delivered in stable0.3.x; chronological checkpoints historical, remaining hardware acceptance #133–135 |
| [Native command matrix](xkeen-command-inventory-v2.md) | Upstream-pinned reference; installed feature discovery still applies |
| [Native quality](native-quality-selection.md) | Delivered expanded-sample/recommendation specification; Xray owns selection |
| [Routing workspace](feature-routing-workspace-1.md) | Completed |
| [LAN DNS integration](feature-native-split-dns-1.md) | Completed; optional installed resolver prerequisite |
| [Independent DNS infrastructure](infrastructure-independent-dns-1.md) | Historical operator installation/acceptance; static exports superseded by integration |
| [Guided fresh installation](feature-guided-install-1.md) | Source delivered in stable0.4.0, Issue145/PR147; restricted initial matrix, hardware acceptance #148 NOTRUN |

## Proposed

| Document | Status |
| --- | --- |
| [Node quality target](architecture-node-quality-v1.md) | Planned shared ARM64/MIPS lifecycle, #199; folds in #194/#198 |
| [Simplification audit 2026-10-10](audit-project-2026-10-10.md) | Dead code removed (#202, #204) and FULL shortened (#203); process, docs and splitdns items open |

## Historical / superseded

Foundation v1, native admission v1, command inventory v1 and dated audits retain decision history. They do not authorize native code patches, takeover, generic repair or resuming disabled workers. UI/availability/DNS planning snapshots are superseded by current product docs where their checkpoints differ. The old evidence subtree contains synthetic/source comparison records, not current production readiness.

New work gets a focused issue and updates this index only when sequencing or delivered behavior changes. Avoid accumulating a new plan for every corrective commit.
