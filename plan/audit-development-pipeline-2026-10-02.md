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
