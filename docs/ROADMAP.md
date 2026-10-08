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

## Remaining acceptance

[#140 stock2.1 transition](https://github.com/popiposter/xkeen-control/issues/140):
development router updated with official payload/config preservation checks and
Running/API/probe/DNS readback. Initial panel attempt failed safely on BusyBox tar;
GNU tar invocation succeeded, then an explicit Start was needed. Disposable full
rollback and wider independent LAN acceptance remain NOTRUN by operator decision;
this does not qualify signed installation of the separate panel candidate.

[#142 tool lookup](https://github.com/popiposter/xkeen-control/issues/142) corrects
Entware precedence in native jobs/lifecycle/panel startup, with bounded installed
init qualification separately from a release. [#143 fresh setup](https://github.com/popiposter/xkeen-control/issues/143)
has a [planned implementation](../plan/feature-fresh-router-setup-1.md) and a proposed
[reference policy](../config/presets/README.md). Current installation still requires
native XKeen first. The wizard, typed firmware preflight and DNS provisioning are
not delivered; no fresh-router hardware acceptance is claimed.

| Task | Current boundary | Prerequisite |
| --- | --- | --- |
| [#133 LAN routing/DNS/failover](https://github.com/popiposter/xkeen-control/issues/133) | Development/native DNS and a separate stopped-Xray DNS test passed; wider client/failure behavior remains NOTRUN. Signed0.3.1 installation is not claimed. | Independent client path and bounded authorized baseline/rollback |
| [#134 real Telegram](https://github.com/popiposter/xkeen-control/issues/134) | Source/disabled-bot acceptance passed; real bot exchange NOTRUN | Operator token/user/chat and message authorization |
| [#135 second-router transfer](https://github.com/popiposter/xkeen-control/issues/135) | Same-router encrypted export/validated Preview/Cancel passed; A→B Stage/Apply NOTRUN | Second supported router |

No new native admission protocol, panel restart supervisor, component updater or legacy takeover is planned. #80/#81 are superseded rather than retrospectively completed. [#4](https://github.com/popiposter/xkeen-control/issues/4) is the live evidence ledger, not an unfinished component-manager feature. [#1](https://github.com/popiposter/xkeen-control/issues/1) tracks current product direction.

## How to use plans

[Plan index](../plan/README.md) distinguishes delivered specifications, current command/quality references and historical audits. The native-shell v2 plan is a completed source implementation record; unrun hardware acceptance lives in the tasks above. Do not resume its superseded protocol or old chronological checkpoints.

Future changes start from current main with an issue, a normal checkout/Draft PR, proportional iteration and one final required exact-source gate. Docs-only updates use link/content/diff checks. Merge needs explicit operator authorization; publication/install qualification remain distinct.
