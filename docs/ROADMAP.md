# Roadmap

This is the current sequencing/status authority. Issues contain detailed task contracts; historical milestone logs are [archived](archive/README.md).

## Delivered

**Stable0.3.1 published and independently verified.** Native-shell implementation #121/PR122 and DNS integration #125/PR126 are delivered. PR128/130 corrected release dependency/fixture gates; [release evidence](RELEASES.md) binds the exact source and public bytes.

- Stock XKeen owns installation, components/interception, service and cron; its code stays unmodified.
- Native jobs/private console, Form/Text config editors and shared pending/Apply/Discard/previous restore.
- Subscription/node lifecycle, bounded balanced speed recommendations, native Xray selection/failover.
- Installed geodata category/member search and routing examples.
- Optional independent LAN DNS synchronization/no-op/inspect-only recovery.
- Encrypted native transfer and restricted optional Telegram control.
- Signed panel self-update, private listener and session persistence.

## Setup implementation in review

[#145 guided fresh installation](https://github.com/popiposter/xkeen-control/issues/145)
extends [#143 research](https://github.com/popiposter/xkeen-control/issues/143): one
entrypoint privately requests a subscription/node, prepares the reference and
independent DNS, then activates the entire discovered HOME network after readiness.
The [implementation contract](../plan/feature-guided-install-1.md) governs the
source implementation of initial stock bootstrap, private source import, a full
reference generation, typed firmware assignment and interrupted-setup fencing.
The first adapter is restricted to the observed KN1811/5.01.C.6.0-1 CLI family;
unsupported topology/schema stops before provisioning. Stock commands remain
installation/update owners. [PR147](https://github.com/popiposter/xkeen-control/pull/147)
records independent exact-source review and exact FULL evidence separately.
Published availability and fresh hardware acceptance remain pending.

## Remaining acceptance

| Task | Current boundary | Prerequisite |
| --- | --- | --- |
| [#133 LAN routing/DNS/failover](https://github.com/popiposter/xkeen-control/issues/133) | Development/native DNS and a separate stopped-Xray DNS test passed; wider client/failure behavior remains NOTRUN. Signed0.3.1 installation is not claimed. | Independent client path and bounded authorized baseline/rollback |
| [#134 real Telegram](https://github.com/popiposter/xkeen-control/issues/134) | Source/disabled-bot acceptance passed; real bot exchange NOTRUN | Operator token/user/chat and message authorization |
| [#135 second-router transfer](https://github.com/popiposter/xkeen-control/issues/135) | Same-router encrypted export/validated Preview/Cancel passed; A→B Stage/Apply NOTRUN | Second supported router |

No new native admission protocol, panel restart supervisor, component updater or legacy takeover is planned. #80/#81 are superseded rather than retrospectively completed. [#4](https://github.com/popiposter/xkeen-control/issues/4) is the live evidence ledger, not an unfinished component-manager feature. [#1](https://github.com/popiposter/xkeen-control/issues/1) tracks current product direction.

## How to use plans

[Plan index](../plan/README.md) distinguishes delivered specifications, current command/quality references and historical audits. The native-shell v2 plan is a completed source implementation record; unrun hardware acceptance lives in the tasks above. Do not resume its superseded protocol or old chronological checkpoints.

Future changes start from current main with an issue, a normal checkout/Draft PR, proportional iteration and one final required exact-source gate. Docs-only updates use link/content/diff checks. Merge needs explicit operator authorization; publication/install qualification remain distinct.
