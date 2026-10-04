# Native panel usability refresh — Issue #121

Scope: the operator's eight-screen visual audit on 2026-10-04. XKeen scripts,
modules, init and cron remain native and unmodified. No second component updater.

## Observed problems and implementation

The live Overview, Nodes, Routing, DNS, Components, Backup and System screens
were captured and inspected before edits. Private router screenshots stay local.
Observed: monochrome status badges; arbitrary first enabled nodes on Overview;
hard-disabled manual override; fixed node columns; missing Romania flag;
long expanded routing forms; shared all-file selector on DNS/Routing;
monochrome JSON tokens; flat command buttons; stretched backup/password inputs;
System anchor links reveal an overlapping stack of disclosure groups.

1. Semantic success/warning/error/info colours with icons and text in both themes.
2. Shared quality readback, measured ranking using the native throughput/health
   cost, download/upload columns and a remembered nonsensitive column chooser.
   Source is hidden initially. Measurements are distinguished from applied
   weights; an empty/restarted measurement history is displayed honestly.
3. Explicit native volatile manual pin through Xray RoutingService, serialized
   only with panel operations. Validate an enabled native pool member and read
   back the exact override. Clearing restores automatic selection. Pinning
   bypasses automatic selection/failover; no old selector loop is activated.
4. Compact ordered routing summaries with destination icons; edit rules,
   protocol/network conditions and category search in dialogs. Search category
   names and contents across all installed files with snapshot-pinned pagination.
   Selecting host and IP categories creates separate alternative rules, never
   an accidental domain AND IP match.
5. DNS and Routing show only their own file. Configurations is the full editor.
   Forms and colour-highlighted JSONC share drafts, undo and typed validation.
6. Native command groups; System tabbed subpages; bounded backup/input widths.
7. Protected remembered browser sessions; cookies stay HttpOnly/SameSite and
   CSRF remains mandatory. No password or raw session cookie in browser storage.

## DNS boundary and live acceptance

Read-only baseline: native DNS file is empty, router port-53 resolver exists,
no common XKeen port-53 interception found. The preset prepares native Xray
DNS plus two resolver-tag routing rules together. Google and Cloudflare DoH
use literal endpoints through the existing balancer; default Xray lookups use
the system resolver. Use existing native settings, no custom DNS service or
firewall worker. Save-set validates the complete files before one new Apply.

LAN client DNS through Keenetic is independent; Xray configuration does not
prove the client's initial DNS query used VPN. IP-only routing conditions do
not identify names before resolution. DNS category matching is not identical
to ordered routing when selectors overlap. Show these limits in the editor.
Do not claim LAN outage acceptance while the observation machine uses Karing.

## Qualification and rollback

Focused Go/reader/auth fixtures, browser tests for changed workflows and build
while iterating. One final exact-clean-HEAD full Linux gate and ARM64 artifact
before one NEW bounded development panel delivery; independently verify binary,
native process/init and configs/registry preservation. Remembered sessions are
private and excluded from portable bundles. Inspect live screens in light/dark
and narrow layouts. Any new DNS Apply must have a fresh baseline, private
snapshot, recorded intent and independent native health/config readback.
Never replay an unknown command/Apply or any previous delivery. Retain the
previous panel and native config generation for explicit rollback.

## 2026-10-04 operator UX / speed-test revision

Each speed-test Start reads the native API now, takes fresh (<=2min), alive enabled
pool members with RTT<=750ms, ordered RTT/tag. Take first6 regardless of saved
weights/current selection; freeze next6 as replacements. Aim for6valid down/up
results, max12attempts, retaining failed rows. Share original144MiB/180s+cleanup
ceiling, count failed bytes; stop replacements on budget, disclose partial results.
No auto-stage, restart or selection write. Button becomes Run speed test.

Nodes: numeric rank/down/up sorting with missing samples last in both directions;
disabled nodes visually muted. Config actions get icons while retaining labels.
Native commands: rich keyboard/disabled-aware explanations, meaningful groups and
icons; auto-open command-bound console even for read-only commands. Existing ANSI
colour rendering is preserved, terminal follows theme. Read actual recognized native
cron jobs (geodata/speed balancer) into safe projections, never expose arbitrary
crontab text or add a cron writer. Native commands remain unmodified.

System: left-aligned labels, bounded controls, distinct notification/channel/install
groups. Development builds must not be labelled signed release commits.
Metadata-only node/subscription Apply retains existing snapshot/intent/drift checks,
but compares parsed runtime output with numeric precision preserved; when identical,
save only registry, leave runtime bytes and PID untouched. All actual native runtime
changes retain full validation and normal activation. Covers labels, subscription
metadata and edits to disabled profiles; no new transaction authority.

Verification: focused Go fixtures for shortlist/replacement/budget/no-restart/safe
cron; grouped changed UI workflows; one final exact FULL before new panel-only
installation and visual readback. Preserve current native DNS/routing/auth/registry,
no prior router action replay. No new release/merge. Stock XKeen stays unmodified.