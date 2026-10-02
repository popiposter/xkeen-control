# Native XKeen compatibility

Issue #121 targets native XKeen on clean ARM64 Entware. The panel is being
refactored; the installation below does not establish integrated VPN acceptance.

## Tested baseline

| Item | Evidence |
| --- | --- |
| Upstream source | `jameszeroX/XKeen@5aaece27a70d5bd002c615248614914ebbc4569d` |
| Installer | Unmodified upstream `install.sh --beta`; SHA256 `734af136cbde1ede2775bae86e158c19ca588378347f57f336be790503462811` |
| Beta archive | 125,747 bytes; SHA256 `1d246871d8fc9df2e68e80cef18562e6222661e40eaea1b6853cf8e0e6348b5f` |
| Installed modules | 72 of 72 regular files independently matched the public archive |
| Native version | XKeen 2.0.1 Beta |
| Selected core | Native installer selected Xray 26.9.30, ARM64 |
| Native output | S05xkeen, six Xray config files, six dat files, one geodata cron job |
| Duration | 92.6 seconds for the native installer process, including missing curl/CA prerequisites |
| Initial runtime | Xray stopped; no VPN profiles, API or bal-proxy yet; panel absent |
| Snapshot | Nine native config/init/cron files retained privately before panel onboarding |

The beta download URL is moving. This run checked its SHA256 against the pinned
archive before invoking the installer and checked all installed script/module
bytes afterward. The native installer and future native updaters have upstream's
own verification semantics; they are not covered by the panel's Ed25519 signature.

Stable 2.0 was inspected as a separate public artifact in the audit (71 files,
no native speed-balancer modules). It has not passed this clean-device contract
and is not advertised as a supported alternative yet. Beta was chosen for the
tested native feature set; the panel's quality algorithm is not evidence that
the remaining stable/beta differences are irrelevant.

## Native defaults that the panel must understand

- Fresh `xkeen.json` is `{}`. Missing optional keys mean native defaults, not a
  broken installation. Never execute the shell metadata file to read its version.
- Native Xray templates contain `//` comments. Readers accept comments outside
  strings, preserve unknown JSON values and reject ambiguous duplicate keys.
- Geodata lives in `/opt/etc/xray/dat`, not beside the configs or in the root
  Xray directory. Dashboard discovery only counts files; content parsing is a
  separate bounded feature.
- Fresh outbounds is an empty object with instructions. Native routing refers to
  the placeholder `vless-reality`. Before starting Xray, explicit onboarding must
  supply usable outbounds and consistent routing; native installation alone is
  not a functioning VPN.
- Native install creates no panel API, `bal-proxy` or panel observatory. Onboarding
  must add those scoped regions before using the existing node activation API.
- The native netfilter hook may be absent while the core is stopped. This fact
  is shown separately and must not become a global `layout-mixed` rejection.
- Native commands can install missing dependencies on entry. Discovery reads
  files; it does not run `xkeen -status` as an assumed side-effect-free probe.
- Native lifecycle must use `XKEEN_FOREGROUND=1` to observe the actual operation.
  A launcher exit or HTTP202 does not prove traffic health.

## Installer dialogue observed on the clean target

The operator-local driver selected only recognized menus from the pinned source.
It is not an API accepting arbitrary commands or automatic `yes` input.

| Menu | Selected native option |
| --- | --- |
| Proxy core | Xray |
| Xray release list | First release offered by native installer |
| GeoSite / GeoIP | Install all available native datasets |
| GeoIPSET | Native RU exclusion list |
| Geodata update | Native daily cron at 04:23 router time |
| Autostart | Enable native XKeen autostart |

All ten prompts completed, native exit0 was followed by independent file/version
readback. Unexpected/repeated prompts, output beyond 2 MiB or a 20-minute deadline
stop the driver for inspection. A failed/unknown attempt is not automatically
replayed. Native `-ux`, `-ugc` and channel-change dialogues still require their
own contract tests before exposing those actions in the panel.

## Discovery interface

`xkeen-control native inspect --json` is a read-only operator command. Authenticated
`GET /api/v1/xkeen` returns the same safe facts, with the usual private-management
and same-origin checks. Neither accepts a root path, command, raw config or URL.

States are `available`, `missing`, `unsupported` or `unknown`, per capability.
File presence, syntactically readable config, loaded modules and running Xray are
separate observations. None is a declaration of working LAN interception/tunnel.
`apiConfigured` describes the recognized loopback declaration/routing, not a live
gRPC request; `needsOnboarding` is a UI hint, not mutation admission.

The development ARM64 inspection command was executed on the native installation.
All nine snapshot files retained their private SHA256 values; six dat files and
native default-disabled speed balancing were recognized. No native command or
service restart was invoked by discovery.

## Attachment candidate

`xkeen-control native attachment-check --json` prepares an owner-scoped candidate
in a temporary directory and invokes the installed Xray's full configuration
test. It reports only source/candidate hashes, sizes and filenames. It does not
commit configuration or start/restart native XKeen. The source is rechecked after
validation; a concurrent change invalidates the check.

On the clean native installation, the candidate passed the installed Xray test
and all nine native snapshot files remained unchanged. Four candidate files were
produced: managed outbounds, routing integration, observatory and loopback API.
Native DNS, inbounds, policy, init and cron were preserved. Existing catch-all
rules that could shadow appended per-node probes require explicit integration
review; they are not silently rewritten. This is candidate validation only.

The native lifecycle adapter waits for foreground command completion. API/PID
appearance does not finish the command early. A timeout is an unknown outcome
and must not trigger an automatic second native start.

## Remaining hardware contracts

The operator-only `native attach-stopped --source-sha256 <checked-digest>
--exclusive-maintenance` command implements the initial connection while Xray
is positively observed stopped. Unreadable/missing process observation rejects
the operation. It validates the complete candidate, snapshots the source with
file and directory synchronization before mutation, changes only the four
integration files, verifies the entire resulting config set and records a
`committed-stopped` receipt. It never starts a service. An existing receipt blocks
replay; interrupted operations retain their snapshot for inspection. Rollback
only restores still-owned bytes while the service is verifiably stopped. This
CLI flag declares operator-exclusive maintenance; it does not provide native
CLI/cron exclusion. Focused Linux fixtures pass; live attachment remains pending.

Scoped config onboarding, native start/restart/readback, subscription import,
LAN TCP/UDP/DNS policy, panel-stop autonomy, native update/cron collision and
failure recovery remain pending. A common admission seam is required for claims
of concurrent CLI/cron/panel safety. Exclusive operator maintenance during this
initial installation is not a substitute for that concurrency qualification.
