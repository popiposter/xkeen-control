# Roadmap

This is the current sequencing/status authority. Issues contain detailed task contracts; historical milestone logs are [archived](archive/README.md).

## Planned node-selection architecture

[#199](https://github.com/popiposter/xkeen-control/issues/199) defines one
subscription, targeted-probe, speed-review, pool-Apply and Xray failover algorithm
for ARM64 and MIPS. Only active-pool Observatory concurrency and bounded speed
test breadth vary with hardware. The [target plan](../plan/architecture-node-quality-v1.md)
is a proposal, not delivered behavior or router acceptance. The installed
five-member KN1810 pool has not yet been automatically replaced. The
[2026-10-10 audit](../plan/audit-project-2026-10-10.md) folded #194
(subscription churn) and #198 (imported-pool initialization) into this target;
both are closed. Dead code was removed in #202 and FULL was shortened to
about three minutes in #203. Phase 0 removed the never-started `c1`
supervisor/benchmark generation in #204. Phase 1 is in source on `main`: targeted RTT
probing before every review (#205), orphaned selectors as unhealthy members (#206), one
pipeline for manual and automatic reviews (#207) and one automatic policy for both
profiles (#208), with joint 05+07 narrowing of native Observatory to the pool completing
Phase 1 in source.

**Release gate:** since #208, `main` makes the ARM64 automatic review apply verified
pools and restart XKeen without operator action. Do not cut a release from `main` until
TASK-008 step 1 (a read-only baseline and a bounded unsigned synthetic candidate on both
router types, with no change to live routing) has passed and been reviewed. The natural
automatic review and Apply on hardware can only be observed after that signed build.

## Delivered

**Stable0.4.12 published and independently verified for ARM64 and MIPS soft-float.** Native-shell implementation #121/PR122, guided fresh setup #145/PR147, MIPS panel delivery #150/PR151 and compact fresh routing #181/PR182 are delivered. #183/PR184 fixes MIPS speed telemetry, #185/PR186 pins the protected release image, #188/PR189 adds bounded MIPS automatic pool review, #191/PR192 rotates an 18-node subset, and #195/PR196 defers futile reviews before traffic. [Release evidence](RELEASES.md) binds exact source and public bytes. Signed0.4.12 installation passed on KN1810 with source/hash and native preservation readback. A live 18-node comparison under0.4.11 completed but retained the five-node pool; automatic Apply and fresh setup remain untested.

- Stock XKeen owns installation, components/interception, service and cron; its code stays unmodified.
- Native jobs/private console, Form/Text config editors and shared pending/Apply/Discard/previous restore.
- Subscription/node lifecycle, bounded balanced speed recommendations, native Xray selection/failover.
- Installed geodata category/member search and routing examples.
- Guided fresh setup uses ordinary Keenetic DNS and compact selective routing;
  legacy independent DNS recovery remains an inspected historical operation.
- Encrypted native transfer and restricted optional Telegram control.
- Signed panel self-update, private listener and session persistence.

[#154 MIPS deadlines/prerequisite capabilities](https://github.com/popiposter/xkeen-control/issues/154) is delivered in merged [PR155](https://github.com/popiposter/xkeen-control/pull/155) and signed0.4.2. Independent source/local FULL, frozen-source review, protected hosted FULL and ten public assets/both signatures passed; the release report records exact identities. Signed0.4.2 router adoption/native validation/client acceptance is pending and must not be inferred from publication.

## Guided fresh setup delivered

[#145 guided fresh installation](https://github.com/popiposter/xkeen-control/issues/145)
extends [#143 research](https://github.com/popiposter/xkeen-control/issues/143): one
entrypoint privately requests a subscription/node, prepares compact selective
routing with ordinary Keenetic DNS, then activates the entire discovered HOME
network after readiness.
The [implementation contract](../plan/feature-guided-install-1.md) governs the
source implementation of initial stock bootstrap, private source import, a full
reference generation, typed firmware assignment and interrupted-setup fencing.
The first adapter is restricted to the observed KN1811/5.01.C.6.0-1 CLI family;
unsupported topology/schema stops before provisioning. Stock commands remain
installation/update owners. [PR147](https://github.com/popiposter/xkeen-control/pull/147)
records independent exact-source review and exact FULL evidence separately.
Stable0.4.12 retains the installer/CLI; protected hosted FULL and independent
ten-public-asset/both-signature verification passed. Fresh hardware acceptance remains
[NOTRUN](https://github.com/popiposter/xkeen-control/issues/148). Source delivery
does not certify fresh installation or client/outage behavior.

## DNS simplification delivered for fresh setup

[#181](https://github.com/popiposter/xkeen-control/issues/181) replaces the heavy reference with compact selective routing and ordinary Keenetic DNS. Published0.4.9 changes fresh setup and removes split-DNS creation/synchronization from the UI. Existing installations require separate DNS retirement; manual operator changes on two routers do not establish automatic migration or fresh-install acceptance. [Transition boundaries](SIMPLE-DNS.md).

PR180 remains held: the compact hardware constructor completed in 16.243 s, but that is not serving or retirement acceptance and does not justify releasing a timeout change first.

## MIPS automatic pool review delivered; Apply acceptance pending

[#188](https://github.com/popiposter/xkeen-control/issues/188) closes the source
gap between the three-node constrained manual sample and automatic pool updates
after subscription refresh. Signed0.4.10 contains sequential bounded batches,
complete fresh eligible-node coverage, a rolling traffic quota and at most one
verified native Apply per complete review. Independent review, exact-HEAD FULL,
MIPS synthetic hardware execution, public signature verification and signed
KN1810 installation passed. The synthetic test did not access live nodes. At
0.4.10 publication, a live sweep had not completed; the later 0.4.11 review is
recorded below. Automatic Apply and controlled resource acceptance remain unproven.

The first signed0.4.10 scheduled attempt on KN1810 at 02:24 MSK on
2026-10-10 refused before transfer or Apply because 46 fresh eligible nodes
exceeded its 18-node admission limit. It transferred 0 MiB and did not change
the active configuration. [#191](https://github.com/popiposter/xkeen-control/issues/191)
in [PR192](https://github.com/popiposter/xkeen-control/pull/192) implements a
bounded, rotating 18-of-46 review and material-change gate. Independent source
review, local/hosted FULL, ten public assets and both signatures passed for
signed0.4.11. A pure synthetic MIPS planner fixture ran on KN1810 before release;
it did not read live nodes or apply configuration. Signed0.4.11 installation
passed exact source/hash, panel health and native-preservation checks. The first
live subset review attempted 18/18 selected nodes, obtained 16 valid samples
and transferred 37.8 MiB, then correctly retained the five-node pool without
Apply because incumbents lacked comparable evidence. [#195](https://github.com/popiposter/xkeen-control/issues/195)
in [PR196](https://github.com/popiposter/xkeen-control/pull/196) now defers such
reviews before quota reservation and traffic while preserving the final Apply
gate. Independent review, local/hosted FULL, a disposable synthetic MIPS
fixture, signed0.4.12 publication and KN1810 installation passed. A natural
review under0.4.12 and automatic Apply still need separate readback.

## Remaining acceptance

[#162 durable update outcomes](https://github.com/popiposter/xkeen-control/issues/162)
is delivered in [PR163](https://github.com/popiposter/xkeen-control/pull/163) and
signed 0.4.4: bounded readiness, verified rollback receipts, mutation fencing and
verified RAM delivery for older helpers. Independent source/local FULL,
frozen-source review, protected hosted FULL and exact ten-asset public verification
passed. [Release evidence](RELEASES.md) binds source and both binary hashes;
One 0.4.4 hardware attempt refused before update reservation or payload placement;
the installed 0.4.2 generation remained unchanged and prior services were restored.
The failure is retained and must not be replayed.

[#165 direct capability probes](https://github.com/popiposter/xkeen-control/issues/165)
is delivered in [PR166](https://github.com/popiposter/xkeen-control/pull/166) and
signed 0.4.5. Direct inherited-descriptor checks remove dependence on firmware
shell positional arguments; the fixed read-only `self-update inspect-capabilities`
checks the same capabilities on RAM files while the panel runs. Independent review,
local FULL, protected hosted FULL and public verification passed. Signed0.4.5
hardware capability inspection and installed-tuple/runtime verification passed.

[#168 verify-existing settlement](https://github.com/popiposter/xkeen-control/issues/168)
is delivered in [PR169](https://github.com/popiposter/xkeen-control/pull/169) and
signed0.4.6. It proves retention of the validated generation and a distinct stable
runtime before settling an existing attempt, with zero lifecycle calls and a
durable completion fence. Independent review, local/hosted FULL and public
signature verification passed. Signed0.4.6 installation and the original
settlement passed [hardware readback](https://github.com/popiposter/xkeen-control/issues/168#issuecomment-6080313195).
A later automatic node transaction has a different unresolved marker.
[#171 ordinary transaction proof](https://github.com/popiposter/xkeen-control/issues/171)
is delivered in PR172 and signed0.4.7, with test-only qualification repairs in PR174/#173. Independent source review, local/hosted FULL and public signature verification passed. Signed0.4.7 installation and the separately approved recovery settlement passed [hardware readback](https://github.com/popiposter/xkeen-control/issues/171#issuecomment-6082322961).
A later automatic subscription transaction retained a new previous-branch
readiness failure. [#176](https://github.com/popiposter/xkeen-control/issues/176)
is delivered in [PR177](https://github.com/popiposter/xkeen-control/pull/177)
and signed0.4.8: bounded in-process readiness and truthful failure classification.
Independent review, local/hosted FULL and public verification passed; signed0.4.8
installation and a new subscription transaction remain pending. The Observatory experiment applied,
but its first resource sample overlapped that automatic transaction. Later quiet
measurements are observational, not controlled causal proof; see the
[resource follow-up](https://github.com/popiposter/xkeen-control/issues/157#issuecomment-6082714508).

[#157 router resources](https://github.com/popiposter/xkeen-control/issues/157)
is delivered in [PR159](https://github.com/popiposter/xkeen-control/pull/159): constrained diagnostic budgets, pressure admission,
native schedule conflict reporting and preservation of routing criteria.
[#158 typed node recovery](https://github.com/popiposter/xkeen-control/issues/158)
is delivered in [PR160](https://github.com/popiposter/xkeen-control/pull/160).
Its [offline maintenance path](NODE-RECOVERY.md) reuses the existing process lock
and preserves unknown-operation fencing. Both passed independent source review
and exact local FULL; signed 0.4.3 passed frozen-source review, protected hosted
FULL and independent verification of all ten public assets and both signatures.

Issues #157, #158, #162, #165 and #168 retain their separate hardware evidence. [Resource evidence and limits](ROUTER-RESOURCES.md)
separate short observations from controlled hardware proof. The KN1810 configuration
experiment has completed its prerequisite recovery and one typed config Apply;
publication alone did not establish either result. Historical 0.4.3 and 0.4.5
attempts retain their original failed outcomes. The newer automatic transaction
remains unresolved despite working VPN/API observations. Controlled load
comparison and independent client acceptance remain incomplete. Expanded diagnostics #138 remain
deferred until the resource and hardware gates are satisfied.

[#150 MIPS panel delivery](https://github.com/popiposter/xkeen-control/issues/150)
delivered panel-only source support for existing Ultra KN1810/KeeneticOS5.1.7/
Entware mipsel-3.4 in merged [PR151](https://github.com/popiposter/xkeen-control/pull/151).
Independent source review and clean local FULL passed at `94cd622f852df4621135d47b93efc5fa03976cac`, including static soft-float ELF checks, emulated startup/platform fixtures and real unsigned assembly. The PR151 merge tree is identical. Separately reviewed source `68d7ed356980897fc921001375b24cf2af10b90c` passed protected hosted FULL and signed0.4.1 publication; exact ten files, both signatures and published MIPS emulated version verified. Operator hardware installation/RSS/auth/PTY/native preservation remain NOTRUN. Stable0.4.0 remains ARM64-only; guided fresh setup is not expanded. Existing native config/watchdog are preserved
by panel bootstrap; competing external writers need inspection before Apply.

| Task | Current boundary | Prerequisite |
| --- | --- | --- |
| [#133 LAN routing/DNS/failover](https://github.com/popiposter/xkeen-control/issues/133) | Development/native DNS and a separate stopped-Xray DNS test passed; wider client/failure behavior remains NOTRUN. Signed0.4.0 installation is not claimed. | Independent client path and bounded authorized baseline/rollback |
| [#134 real Telegram](https://github.com/popiposter/xkeen-control/issues/134) | Source/disabled-bot acceptance passed; real bot exchange NOTRUN | Operator token/user/chat and message authorization |
| [#135 second-router transfer](https://github.com/popiposter/xkeen-control/issues/135) | Same-router encrypted export/validated Preview/Cancel passed; A→B Stage/Apply NOTRUN | Second supported router |
| [#148 guided fresh setup hardware](https://github.com/popiposter/xkeen-control/issues/148) | Source/release/signature verification PASS; actual fresh provisioning, independent HOME/LAN/IPv6 and failure/interruption acceptance NOTRUN | Explicitly authorized supported fresh destination; do not reset the current router |

No new native admission protocol, panel restart supervisor, component updater or legacy takeover is planned. #80/#81 are superseded rather than retrospectively completed. [#4](https://github.com/popiposter/xkeen-control/issues/4) is the live evidence ledger, not an unfinished component-manager feature. [#1](https://github.com/popiposter/xkeen-control/issues/1) tracks current product direction.

## How to use plans

[Plan index](../plan/README.md) distinguishes delivered specifications, current command/quality references and historical audits. The native-shell v2 plan is a completed source implementation record; unrun hardware acceptance lives in the tasks above. Do not resume its superseded protocol or old chronological checkpoints.

Future changes start from current main with an issue, a normal checkout/Draft PR, proportional iteration and one final required exact-source gate. Docs-only updates use link/content/diff checks. Merge needs explicit operator authorization; publication/install qualification remain distinct.
