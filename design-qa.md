# Compact workspace visual qualification

final result: passed

## Findings and comparison history

No actionable P0/P1/P2 visual findings remain in the reviewed synthetic states.
The first pass was blocked by the following issues; the later pass includes
post-fix browser captures, rather than inferring the result from CSS.

| Severity / earlier finding | Fix | Post-fix evidence |
| --- | --- | --- |
| P2: Overview grid forced page overflow at 320px | `min-width: 0` on section children and wrapping active-node content | All-workspace 320/375/768px tests; `rendered/01-overview-375.png` |
| P2: mobile node Role occupied the checkbox column | Explicit mobile column placement and inline labels | `rendered/02-nodes-375.png`, `comparisons/11-mobile.png` |
| P2: initial Notifications read expanded an optional form | Attention excludes the initial status read; action/error feedback still reveals the form | System initial-closed/read-count test; `rendered/08-system-375.png` |
| P2: desktop component actions wrapped while space was available | Nonshrinking, nowrap action buttons and explicit desktop grid tracks | Revised `rendered/06-components-1586.png`, `comparisons/06.png`; measured 30px action height |
| P2: specific DNS/component rules overrode mobile target height | Scoped mobile selectors enforce 44px buttons | Revised DNS/components 375px captures, DOM measurements and all-page touch-height tests |

A browser capture immediately after viewport resizing once retained the previous
paint layout. Those captures were replaced after a separate state observation;
this was an evidence-capture issue, not an application layout finding.

The operator subsequently requested a permanent icon toolbar, row-click
selection, a separate subscription column/filter, shorter fields and less copy.
These requests supersede the mock's Actions column and earlier selective toolbar.
The updated eight desktop/mobile captures were reviewed again. At 1440x900 the
table starts at 199.84375 CSS pixels before and after row selection. The new
subscription fixture distinguishes Work and Travel; changing the filter removes
out-of-filter selections. Names are the existing safe DTO projection, so equal
subscription display names are grouped by the filter. Numeric fields use 12ch;
password/release forms and Save actions no longer span the workspace.

## Targets, state and normalization

Source visual truth: the twelve generated references listed in
[docs/UI-DESIGN.md](docs/UI-DESIGN.md), locally in `dist/ui-design/mocks/`.
Implementation: the actual React application rendered through the in-app browser
at host-loopback `http://127.0.0.1:4173/`, using synthetic fixture DTOs only.
Screenshot and comparison paths below are relative to `dist/ui-design/`; this
ignored evidence directory is retained locally and is not a production artifact.

Eight full-view comparisons (`comparisons/01.png` through `08.png`) place source
and rendered frames together in one input. Source canvases are 1586x992 pixels.
Desktop CSS viewport was 1586x992, dark theme, authenticated synthetic session;
the corresponding `rendered/*-1586.png` frames are 1586x992 except Nodes, which
is 1571x983 after browser scrollbar/capture normalization. Full pairs preserve
aspect ratio and use equal-width 720px presentation. Exact-pixel identity is not
claimed. Focused table controls in `comparisons/nodes-detail.png` use original
pixel-density crops. Actual DOM measurements establish CSS sizes independently
of raster capture normalization.

At CSS viewport 1440x900, `rendered/02-nodes-final-1440.png` shows 25 rendered
rows, 30px per row, 22 fully visible and no page overflow. The 1,000-node fixture
test checks bounded rendering, at least 20 visible rows, bounded pagination and
selection across pages. Native desktop controls are compact; mobile buttons are
44px. Mobile captures use a 375x812 CSS viewport; the combined
`comparisons/mobile-all.png` contains all eight workspaces. Automated responsive
checks additionally cover 320px and 768px.

## Focused regions and product constraints

Focused combined inputs were opened and reviewed for login (`09-login.png`),
profile import (`10-import.png`), node confirmation (`10-node-review.png`), mobile
nodes (`11-mobile.png`), setup (`12-setup.png`), listener (`12-listener.png`) and
component confirmation (`12-components.png`), all under `comparisons/`.
The matching rendered workflow frames include `09-login.png`,
`10-add-profiles.png`, `10-node-review.png`, `12-setup-confirm.png`,
`12-listener-preview.png` and `12-component-confirm.png`. Component integrity
facts are also captured in `rendered/12-component-integrity.png`.

These compare composition and affordances with the following intentional
content/state differences, not fictitious pixel-perfect operation matches:

- Fixture node names, sorting, status and versions differ from invented mock data.
- The node confirmation capture reviews one Disable operation; the source board
  illustrates Add. Their shared confirmation structure is assessed visually;
  Add and the exact operation-specific request contracts are verified by tests.
- The real fixed Setup plan, listener facts, resolver labels, six Performance
  fields, minute cadence and component capabilities take precedence over
  generator-invented fields. Informational components offer no update actions.
- Profile import remains a compact inline composer, and Setup stays in Overview;
  the generated large modal and incorrect workspace ownership were rejected.
- Fixed diagnostic limits and failure/cleanup states are covered by the existing
  browser fixtures; the diagnostic board is a style reference, not proof of a
  measured live benchmark. No router throughput or production state is claimed.

## Required fidelity surfaces

- **Fonts/typography:** rendered system sans-serif stack, 13px body, 12px helpers,
  20px page titles. The raster mock does not identify a licensed font; matching
  optical density and hierarchy is the target. Focused regions show readable
  labels without oversized display copy, headings wrapping normally and safe
  endpoint truncation/scrolling within tables.
- **Spacing/layout:** 184px desktop sidebar, aligned editors before optional
  facts, 30px node rows, flat separators and restrained grouped surfaces. The
  density correction explicitly supersedes spacious early mocks. Mobile rows
  stack their semantic cells and the sidebar becomes a toggle.
- **Colors/tokens:** near-black graphite surfaces, muted secondary text, restrained
  blue primary actions and consistent green/amber/red semantic feedback. Selected
  navigation and focus outlines remain visible; no decorative gradients were
  added. This is visual inspection, not a formal accessibility certification.
- **Image quality/assets:** the product contains no mock photography or decorative
  raster art. Existing country flags remain intact; stock Tabler vector icons
  provide consistent stroke weight and named controls. Generated boards are
  references only and are not rasterized into the interface.
- **Copy/content:** concise task titles, no slogans; existing server-owned facts,
  bounds and uncertainty warnings remain truthful. Preview/Apply/Cancel and
  credential/secret warnings stay explicit. System guidance is read-only and
  does not automate VPN, firewall or DDNS.

## Interaction and test evidence

Browser inspection covered all eight desktop/mobile workspaces, login, node
selection/search/edit/import, Routing edit/review/result, fixed Setup review and
confirmation/cancel, listener review/cancel, component check/review/cancel and
integrity disclosure. All mutations were synthetic fixture operations. Fresh
browser console inspection returned no errors or warnings. Native disclosures
and mobile navigation were exercised; no real password was entered.

The original candidate passed 162 browser tests and the Full gate at historical
HEAD `4cda1a3ac6cdec1597b396755f377eac6e98a632`. That gate is not reused for the
subsequent operator refinements. The revised full browser suite passed 163/163
(exit 0, 1.3 minutes); the final Nodes/task-workspace focused pass passed 18/18
(exit 0, 14.3 seconds) after the last form-width and composer-copy adjustment.
Tests cover stable table position, always-visible toolbar availability, row and
keyboard selection, named subscription filtering, bounded 1,000-node rendering,
all-page responsive layouts and the existing typed workflows.
Exact-HEAD Full qualification is recorded separately in the Draft PR after the
candidate commit; this visual report does not stand in for that gate.

## Implementation checklist and limits

- [x] All-page image references, full-view pairs and focused regions inspected.
- [x] Earlier substantive findings repaired and checked with revised captures.
- [x] Desktop density, mobile targets, native keyboard disclosure and no page overflow.
- [x] Existing typed contracts, controller ownership and operation warnings retained.
- [ ] Independent Draft PR review and separately authorized production qualification.

Residual scope: no full screen-reader, browser-matrix, zoom or accessibility audit;
no deployed/live acceptance. No actionable P3 refinement is required for this
handoff. Public evidence contains safe synthetic counts/states only.