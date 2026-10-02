# Test, build and release audit

Scope: Issue121, scripts at b6cfef5. Independent review by a separate reviewer;
router work remains in progress. Measured receipts: full394a609 took about
11 minutes including181browser tests; fast97548f8 took63seconds and failed only
at final hygiene after Go/helpers/ARM64 passed. These are past run durations,
not predicted savings.

## Findings and immediate corrections

| Finding | Impact | Action |
|---|---|---|
| packaging paths absent from fast selection | init/updater changes escape relevant checks | Include packaging in Go/helper/artifact lanes |
| bash -n invoked with a glob as multiple arguments | Only first file is parsed | Check every shell file individually, including native panel init |
| Direct fast invocation with no selection | Hygiene-only run looks like code qualification | Require explicit lane selection |
| Hygiene runs after expensive work | Invalid tracked path wastes the complete run | Run hygiene first |
| Public empty native fixture has production filename | Correct safeguard rejects harmless fixture | Rename fixture without exempting testdata or weakening the guard |

## Next improvements

1. Introduce a machine-readable check plan with changed paths, selected checks,
   skipped checks and reasons. Distinguish working edits, last commit and full
   branch comparison; cumulative branch scope grows too broad for iteration.
2. Select changed Go packages plus transitive reverse dependencies, including
   TestImports/XTestImports, from go list. Unknown changes or failed graph
   construction broaden checks. Targeted test-name filters are iteration evidence
   only and must be reported as such.
3. Map helper scripts and frontend files to relevant integration fixtures/UI
   specs. Run an ARM64 link when runtime/build inputs change, not for test-only
   edits. Add selector regressions for packaging, deleted files and new packages.
4. Key iterative npm installation reuse to lockfile, runtime and platform.
   Keep clean installation for final/release. Documentation-only checks should
   avoid starting Docker.
5. Split web asset generation from release assembly. The protected workflow
   should assemble/version-link using already-qualified embedded bytes and
   require unchanged tracked source before and after assembly. Currently
   release-build calls build-control-plane, which installs/builds web again and
   rewrites the embedded tree after verification.
6. Keep one full exact-final-candidate Linux gate: all packages, race tests,
   browser acceptance, embedded consistency and ARM64 build. Keep hosted release
   qualification, protected signing and independent public asset verification.
   Do not replace those with an unverifiable local receipt.
7. Retire legacy Setup/adoption assertions with their implementation, replacing
   them with native ownership/clean installation contracts. Do not retain dead
   product code solely to satisfy historical tests.

Live audit found the module volume was mounted at `/go/pkg/mod`, but the Node
base image defaults Go to `/root/go/pkg/mod`. Modules were downloaded on every
disposable container run. Compose now explicitly sets `GOPATH=/go`, matching
the existing module volume. The build and npm cache paths were already correct.
Bundled Chromium remains useful.
Aggregate helpers already use fixtures-only to avoid repeating Go sweeps;
do not claim or remove a duplication that is already eliminated. The repeated
race stress fixture is intentional until its covered implementation is retired.
Measure per-step durations after changes before claiming a speedup.

## Implemented cleanup after the 198-test run

The failed exact7ce0582 run spent about4m24s in Chromium:195PASS/3FAIL.
All three failures referenced removed navigation CSS; no application defect was
established by those failures. The following changes are pending final reviewed
qualification (do not reuse the previous failed full result):

- Removed obsolete Setup/component-updater UI and its three suites/session
  fixture together. Production supplies native discovery; mocks now reflect it.
  Preserve missing/unknown lifecycle blocking independently of the old controller.
- Consolidated four duplicate navigation/lazy/no-poll scenarios into one
  cross-workspace scenario covering each retained endpoint, including notifications.
  Kept distinct mutation/session/secret/unknown-outcome and mobile behaviors.
- Migrated three pure dashboard-reader concurrency tests to Node's test runner.
  These did not launch a browser individually even before migration; the benefit
  is removing coupling to the Playwright runner/dev server.
- Removed redundant five-second waits and used virtual browser time with observed
  request completion in consolidated polling tests. Two bounded browser workers
  use independent contexts/mocked state. Measure final suite before claiming speedup.
- Added inspectable working/commit/branch plans, conservative browser area selection,
  reverse Go dependency selection including test imports, and no ARM64 link for
  test-only edits. Missing graph/packages and unknown changes broaden selection.
- Matching local npm dependencies can be reused during iteration; clean install is
  retained for full/release. Documentation iteration does not start Docker.
- Release assembly consumes the already-built/verified embedded assets without
  repeating npm install/build. Embedded-only fixture proves no npm invocation or
  embedded rewrite. Protected signing/public verification are unchanged.

Remaining scope: helper fixtures stay conservatively grouped for runtime/script
changes; further splitting needs a real dependency inventory. Old server component
and appliance fixtures still cover existing code and are not deleted just to lower
counts. Full exact-final Linux qualification is retained once before deployment;
never run it after every intermediate edit or label a proportional pass as full.

Inventory after cleanup:149 browser cases in11 specs, plus3 dashboard-reader
Node cases. Focused native/cross-workspace runs passed19 and10 cases respectively;
these overlap and are not claimed as29 unique cases. Production JS decreased
from524.35KB to485.22KB (raw Vite output) after removing unused legacy components.
Final149-case duration/pass and exact Linux artifact remain to be measured.
