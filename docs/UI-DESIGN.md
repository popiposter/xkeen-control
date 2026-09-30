# Compact operator workspace

Issue #102 implements the operator-selected graphite sidebar direction with the
operator's subsequent density correction: no slogans, large titles or spacious
node rows. This is a source-only UI change; production qualification remains a
separate release boundary.

The working set is task-oriented: inspect runtime in Overview; find, filter,
select and edit nodes in Nodes; edit policy, review a server-issued Preview and
confirm in Routing, DNS and Performance. Components retains explicit Check,
Preview, confirmation and rollback. Backup retains safe/encrypted export and
preview-first restore. System starts with Access and reveals Password, Releases,
Notifications and runtime details on demand. Setup retains its fixed plan and
explicit confirmation. Presentation does not own an operation or create a new
polling loop, API, scheduler, browser store or credential authority.

Desktop type is 13px with 12px secondary labels and 20px page titles. The sidebar
is 184px; forms/toolbars use 32px controls. Node rows are 30px, paginated at 25;
only selected nodes expose the bulk toolbar. A bounded pagination window keeps
large registries from producing an unbounded control strip. Compact row actions
open the existing selection/edit workflow. Touch layouts use 44px controls and
stacked node rows; the table remains a semantic table with its column headings.
Keyboard focus, a skip link, native disclosure and explicit accessible names are
part of the design.

## Image references and coverage

Generated with the built-in ImageGen tool, saved locally under
`dist/ui-design/mocks/` alongside `prompts.json`. These synthetic raster references
are design evidence; they are not shipped as app UI. The mock set covers:

| Reference | Surface |
| --- | --- |
| `01-overview-dense.png` | Overview, runtime, selection and node summary |
| `02-nodes-dense.png` | Dense 25-row table, filters, selection and pagination |
| `03-routing.png` | Ordered rule edit, review sequence and protected context |
| `04-dns.png` | Resolvers, caching, sampling and protected context |
| `05-performance.png` | Six policy fields, adaptive status and fixed limits |
| `06-components.png` | Inventory, component actions and discovery policy |
| `07-backup.png` | Safe/encrypted export and restore |
| `08-system.png` | Access, optional forms and private-management guidance |
| `09-login-setup.png` | Login; setup composition superseded by reference 12 |
| `10-review-workflows.png` | Import and node confirmation; handoff superseded by 12 |
| `11-responsive-diagnostics.png` | Mobile node composition and diagnostic states |
| `12-setup-handoff.png` | Fixed setup plan, listener handoff and component confirmation |

Generated captions cannot change the product contract. Corrections deliberately
follow the existing source: source/role labels come from safe DTOs; DNS does not
expose resolver endpoints or invent sequential/fallback guarantees; Observatory
cadence retains its minute catalog. Performance retains exactly its six existing
fields and bounds, without a speculative local preview. Components cannot update
from Nodes or offer upgrades for informational components. Setup has no editable
host or notification settings. Listener Preview never starts a service; its
read-only before/after facts describe the panel bind, not a proxy node. Preview
expiry comes from its token, never an invented profile lifetime. Diagnostics
retain fixed source-owned limits, never editable transport/throughput ceilings.
The implementation keeps the existing typed requests and authoritative feedback
while matching the approved compact visual system.

## Verification boundary

Use synthetic fixtures for browser comparisons and core workflow regression
tests. Large-list acceptance checks 1,000 nodes with only 25 rows rendered and at
least 20 fully visible at 1440×900. Check 320px, 375px and 768px layouts without
page overflow. Record actual visual comparison and exact-HEAD full qualification
separately; a build or screenshot alone does not prove either gate.
