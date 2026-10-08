# Telegram control

The panel runs one optional Telegram long-poll receiver alongside outbound
notifications. Bot control is off by default. Configure the bot token and chat
in System / Panel, then explicitly enable control for one numeric Telegram user
ID. Notifications and control have separate switches. Replacing credentials
disables control. Credentials and the user/chat allowlist stay in the root-only
notification authority and are excluded from portable backups.

| Command | Behavior |
| --- | --- |
| `/help` | Show the fixed command list. |
| `/status` | Read local engine observations; no native command or tunnel probe. |
| `/start`, `/stop`, `/restart` | Call the corresponding installed XKeen command. |
| `/update_xkeen`, `/update_xray`, `/update_geodata` | Call native `-uk`, `-ux auto`, `-ug`. |
| `/refresh` | Queue enabled subscriptions on the existing bounded refresher. |

Commands take no arguments. Only fresh ordinary messages from the configured
user in the configured chat are accepted (120-second TTL). Forwarded messages,
anonymous senders, bots, edited messages and other command text are ignored.
Startup/credential changes discard queued commands using a nonblocking backlog
read before normal long polling. The [Telegram Bot API](https://core.telegram.org/bots/api#getupdates)
offset then advances in RAM and resets after an idle freshness window, so a new
random update-ID sequence cannot remain hidden by an old offset. One bounded
persistent update ID/message-date watermark is written
**before** dispatch: after a crash a command can be lost, but is not replayed.
A lower sequence is accepted only after every command in the old watermark
window has expired; this handles Telegram's random IDs after a week of silence.
Changing/clearing the allowlist invalidates an in-flight poll.

Native commands use the same panel operation lease and job executor. Saved
pending config changes refuse remote native actions; use the config editor to
apply or discard them first. The subscription refresher retains its existing
pending-config guard. External native CLI/cron remain independent writers.

Replies are closed safe status/acceptance messages. They contain no native output,
config, node credentials, URLs or infrastructure details. Acceptance does not
prove completion or VPN reachability. Inspect the job in the authenticated panel:
local operators may view its private console, answer conditional native update
prompts, interrupt it or inspect an unknown outcome. Telegram cannot answer
prompts or submit arbitrary shell/config input. An unknown job is never replayed.

XKeen 2.1 adds a confirmation to ordinary `-uk`. Update acceptance replies
explicitly direct the operator to the panel console. No `-uk auto` or automatic
answer is injected; an unanswered prompt times out with an unknown outcome.
Release identity and Xray process observations are available in the panel,
separate from process completion and client traffic health.

Qualification uses synthetic provider fixtures, real local native-process fixtures
and mocked browser APIs. Live bot acceptance requires an operator-configured
private token/user/chat; no message is sent merely by visiting settings.
