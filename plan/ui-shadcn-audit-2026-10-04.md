# Unified panel UI and native configuration forms

Issue121 / existing Draft PR122. Operator requested an audit of every screen,
standard shadcn styling, removal of obsolete views, dark mode and purposeful
forms for each native config. XKeen commands/code/config validation remain owned
by their existing adapters; no native script mutation or repeated live actions.

## Current-run visual audit

1. Login: standard neutral Card, but no appearance choice.
2. Overview: light navigation beside a legacy dark workspace; duplicate profile
   actions and obsolete starting/adaptive status despite an active native runtime.
3. Nodes: standard controls inside the old dark frame; unreachable-node sentinel
   displayed as 99999999ms and obsolete adaptive-supervisor details.
4. Routing: outlined actions and labels inherited white text on white cards;
   lengthy rule forms lacked readable summaries.
5. DNS: same unreadable controls and no unified form grouping.
6. Performance: table text and footer inherited legacy colors; consumed-result
   message implied a restart even after the configuration was already applied.
7. Components: invisible native command labels and duplicate config editor
   displaying the last DNS file after visiting that workspace.
8. Transfer: white-on-white form labels and inconsistent surrounding frame.
9. System: legacy controls/cards; inherited form and notification styling.

Screenshots were captured from the actual installed panel in this run, saved
privately and inspected. They may include private infrastructure and must not
enter Git/public issues. Accessibility conclusions are visual risks, not a claim
of WCAG certification. Authentication/mutation/job semantics are preserved.

## Implementation cohort

- Remove obsolete scoped dark CSS and retain responsive layout only.
- Standard Button/Input/NativeSelect/Table/Card/Badge/Field across all screens;
  dialogs and disclosures retain keyboard/focus and operation owners.
- Light/dark/system appearance, non-secret versioned preference only, system
  changes and cross-tab updates; text editor colors follow semantic tokens.
- Overview uses actual native health/selection, no inactive supervisor state.
- Components shows native commands; config editor remains Routing/DNS-only.
- Native forms: logging detail/destinations; DNS resolver/match lists; listener
  protocol/bind/port/sniffing; ordered rule conditions/destinations and balancers;
  numeric timeout/statistics policy; health probes; local API/listeners.
- Keep JSONC/comments/unknown values, one Form/Text document and existing
  save-validation/group-Apply/discard/previous controls. Unknown typed values stay
  in Text instead of being silently coerced. No secret browser persistence.

## Acceptance and rollback

Review all eight workspaces, private dialogs, loaded/empty/error and responsive
states in both themes. Verify theme persistence/system preference and no page
horizontal overflow at320/375/768px; regression tests must preserve session,
preview, draft, validation and unknown-operation boundaries. Retire tests for
removed inactive supervisor UI instead of retaining its false product claims.
One final clean exact-HEAD FULL plus ARM64/embed before NEW panel-only delivery.
Rollback retains one preceding qualified panel binary; native configs/registry/
credentials/service processes must be unchanged by UI delivery. Do not replay
any previous panel replacement/config Apply or measurement.

Focused verification: 39/40 browser scenarios passed together; the remaining
legacy CSS selector was corrected and its exact scenario passed separately.
Coverage includes native Form/Text, drafts, grouped Apply/restore, unknown
response boundaries, System operations, light/dark/system and all eight
workspaces at desktop and320/375/768px. Seven native command scenarios passed
in the preceding focused run. Production frontend/embed and ARM64 build passed.
Private current-run before screenshots and synthetic after screenshots inspected.

Status: source cohort complete; exact-HEAD FULL and NEW live panel delivery
remain pending. No native configuration or XKeen code changed live.

Final-gate correction: b3fba27 reached93/98 browser PASS; five failures
shared a missing native workspace read in the integration fixture. The fixture
now checks lazy fixed-file inspection with CSRF and rejects mutations. Loaded
mobile editors exposed short Form/Text targets; those are now44px. The17
focused integration/responsive cases passed after that correction. The password
reset scenario explicitly awaits Preview before entering its next form.

## Live audit correction and document CSP

Exact d185 FULL passed Go/race/helpers98browser/frontend/embed/audit0 and ARM64.
One NEW panel-only replacement plus independent readback passed: exact running
executable hash, unchanged native PID/start/executable, configs/registry/auth/
stock init and60nodes53enabled2subscriptionsWL0, no pending generation.
All eight live workspaces were captured and visually inspected in both themes;
all seven fixed configuration forms and Form/Text were opened without writes.
The live Text screenshot revealed CodeMirror style injection rejected by the
production CSP, which Vite-only tests had not exercised. This remains a real
blocker for complete editor acceptance despite d185's broader UI success.

The correction gives each non-cached HTML document a fresh32-byte random style
nonce, preserves the existing script and other CSP directives, and provides it
to CodeMirror. No unsafe-inline or script nonce is enabled. A Go fixture checks
nonce uniqueness/header/meta matching and retained restrictions. A new browser
case serves the actual production embedded HTML/JS/CSS under CSP and checks
editor geometry, gutters and syntax colors. Both focused checks passed.
A NEW exact FULL99browser and another NEW panel-only delivery remain pending;
never replay the completed d185 replacement.
