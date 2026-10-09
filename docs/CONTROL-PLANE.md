# Control plane

Current entry points: `cmd/xkeen-control/main.go`, `internal/httpapi`, embedded React UI. Native behavior is governed by [NATIVE-XKEEN.md](NATIVE-XKEEN.md); security by [SECURITY.md](../SECURITY.md).

## Supported surfaces

- Safe Overview/node/service projections, authenticated node/subscription workflows and bounded diagnostics.
- Allowlisted native commands through `internal/xkeen` jobs. Interactive commands use a private job-bound PTY; no shell or post-exit command execution.
- Fixed native config IDs, private JSON/JSONC editors, validation, pending Save/Apply/Discard and optional previous restore.
- Installed geodata discovery/category/member search and routing examples.
- Native quality measurement/recommendation using `internal/nativequality`; Xray remains selection owner.
- Private encrypted native transfer; optional restricted Telegram control.
- Signed panel update/rollback and exact private management-listener rebind.
- Optional LAN DNS status/sync through `internal/splitdns` and authenticated `/api/v1/dns/split` plus CSRF-protected `/sync`.

## Mutations and results

One panel lease serializes native jobs, configuration changes and related panel operations. External CLI/cron are not participants; source drift is checked and must not be called cross-process exclusion. HTTP202 means accepted handoff, not successful Apply/update. Existing job terminal state and independent configuration/process/health readback determine success. Unknown outcomes are inspected without replay.

Ordinary node transactions retain branch-specific generation/process proof and
sanitized failure stages in the same bounded receipt used by
[offline recovery](NODE-RECOVERY.md). Live process reads do not apply offline
cron/daemon exclusion; offline settlement still requires quiescence.

Native config text, secrets, PTY output and encrypted backups are private surfaces distinct from sanitized status. Session-bound previews, bounded input/output and origin/CSRF checks remain mandatory. Frontend owner/AbortController handling prevents stale session results from reappearing.

DNS validation happens before native config commit; synchronization follows verified native commands/Apply. Pending sets defer DNS changes. Same effective rules do not restart DNS; ambiguous activation has an inspect-only receipt. Native commands are never replayed by synchronization.

## Navigation and tests

[Router resource limits](ROUTER-RESOURCES.md) govern hardware-derived speed-test
budgets, pressure cancellation and native periodic-test conflicts. Measurements
retain the existing coordinator/probe cleanup owner; they do not own recovery.

[Editors](CONFIG-EDITOR-WORKFLOWS.md), [nodes](NODE-LIFECYCLE.md), [transfer](NATIVE-TRANSFER.md), [Telegram](TELEGRAM-CONTROL.md), [UI design](UI-DESIGN.md), [development gate](DEVELOPMENT.md). Retired appliance/Setup/component APIs are not current product surfaces. Their original contract is [historical](archive/CONTROL-PLANE-before-0.3.1-cleanup.md).
