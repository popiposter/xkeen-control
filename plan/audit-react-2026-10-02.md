# React audit and component strategy

Issue #121; source reviewed: `c97852426d0f2b04999545ac7ab90fb7dc0e734e`.
Independent read-only review covered React controllers, shared UI, styles and
browser tests. These are source findings, not live-router acceptance.

## Findings

| Priority | Finding | Required change |
| --- | --- | --- |
| P2 | Node preview declares a modal but lacks initial focus, containment, Escape and restoration. | Shared accessible modal; test keyboard and busy dismissal. |
| P2 | Dashboard/manual refresh and performance polls overlap without response/session epochs. | Single request owner, session invalidation, stale-response rejection. |
| P2 | The main API helper turns invalid JSON success into `{}`. | Reject malformed success and validate endpoint projections; preserve unknown mutation outcomes. |
| Resource | Dashboard polls three endpoints every five seconds even when hidden. | Pause background polling while hidden, refresh on return, retain explicit operation readback. |
| Maintainability | `main.jsx` combines transport, authentication, polling, navigation, nodes, backup and presentation. | Extract by responsibility while preserving feature transaction controllers. |
| Payload | All feature pages and retired Setup/component fallbacks enter one bundle. | Remove obsolete flows with their backend callers; lazy-load infrequent pages. |

Measured built files at reviewed HEAD: JS 413,709 bytes (gzip 117,282), CSS
65,668 (gzip 11,651). Spain flag SVG alone is 80,958 bytes (gzip 14,984).
Source has 4,571 JSX lines and 904 CSS lines; `main.jsx` has 1,283 lines.
File sizes do not establish browser latency or attribute bytes to a library.

## Current library comparison

Official documentation checked on 2026-10-02:

| Option | What it saves | Fit / cost |
| --- | --- | --- |
| [Base UI](https://base-ui.com/react/overview/quick-start) | Accessible interaction primitives, including dialog, popover, select and combobox. Tree-shakable; plain CSS supported. | Best incremental fit: reuse existing CSS tokens, introduce only components needed. Still requires styling. |
| [Radix Primitives](https://www.radix-ui.com/primitives/docs/overview/introduction) | Accessible, unstyled composable primitives. | Also viable. Choose one primitive library; mixing both adds maintenance without a present need. |
| [Mantine](https://mantine.dev/getting-started/) | Ready styled controls, forms, theme and broader application UI. | Stronger reduction in styling work, but brings a wider integration surface. Measure actual imported controls and CSS before proposing a full conversion. |
| [shadcn/ui](https://ui.shadcn.com/docs) | Ready component source, consistent styling and composition. [Base UI variants](https://ui.shadcn.com/docs/components/base/dialog) are available. | Best fit when reducing both custom interaction and styling work. The application owns copied wrappers; Vite integration requires Tailwind and coordinated CSS migration. |

Updated recommendation after the operator's clarification: retain React/Vite and
use **shadcn/ui with Base UI** as the target shared UI layer. Base UI supplies
interaction behavior; shadcn supplies ready styling/composition. Introduce the
official Base UI variants, one theme and only required components. Do not keep
parallel hand-styled and shadcn implementations of the same converted control.
This is a maintainability decision, not a claim of minimum bundle size.

The operator explicitly accepted Base UI. `@base-ui/react` is pinned to 1.8.0
and the node confirmation now uses its Dialog. Measured Vite gzip JS changed
from 117.19 kB for the corrected native-dialog implementation to 136.00 kB for
the Base UI version, approximately +18.81 kB. This is one actual Dialog import,
not an estimate for all controls or the final shadcn conversion. Keyboard
containment/restoration, Escape and busy-state dismissal tests pass. The modal
also passed a runtime CSP check with `script-src 'self'; style-src 'self'`.
`CSPProvider disableStyleElements` keeps external stylesheet ownership; no CSP
relaxation was added. Measure Select/Combobox and final CSS as they are adopted.

The [official Vite setup](https://ui.shadcn.com/docs/installation/vite) adds
Tailwind's Vite plugin and path aliases. Do not blindly replace the current CSS
as the new-project instructions suggest: retire old rules with converted screens
and verify mobile layouts, ports/URLs, focus, and pending-operation presentation.

## Implementation order and acceptance

1. Correct the three behavior defects above with narrow browser regressions.
2. Extract shared transport/session/polling and node UI; do not replace operation
   state machines with presentation-component state.
3. Remove historical Setup/component fallbacks during native-flow retirement.
4. Split routing/geodata, backup and advanced settings; measure production assets
   and first navigation to each split page.
5. Introduce primitives only where they replace meaningful custom interaction.

Keep session-invalidated, uncertain-Apply, stale-preview and keyboard tests.
Add out-of-order replies, logout/login with an outstanding request, malformed
200 responses, hidden-page polling and node-dialog focus/busy tests. A component
library does not prove transaction correctness or replace these checks.

The operator subsequently authorized standard shadcn styling and discarding the
old design. The official Base UI Nova/Neutral preset is initialized with Tailwind
v4, local Geist fonts, and generated Button/Dialog/Card/Input/Field/Alert
components. Login and node confirmation use this standard styling. Existing
screen CSS is temporarily scoped to `.legacy-workspace`; remove it alongside
the remaining screen conversions rather than creating permanent parallel themes.

The complete 192-test browser suite passed for `3d4b2bd` (3.9 minutes, durable
job `f714b9974c3d4ecd91d8ac7e49d22b05`). That evidence precedes the shadcn styling
and subsequent review corrections. New focused browser regressions cover a late
logout after a new login, malformed JSON/object Apply replies, modal focus/CSP/
busy dismissal and 375/1440px standard login. Two tests coupled to old `.diff-row`
styling were changed to assert semantic list contents and passed afterward.

Remaining: convert other screens, retire legacy CSS, split infrequent routes,
finish broader controller extraction and review the exact final candidate.
No complete frontend rewrite or live-router acceptance is claimed.
