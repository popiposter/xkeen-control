# Architecture

The current product is a graphical shell alongside **unmodified stock XKeen**.
Stable v0.4.12 is published; [release verification](RELEASES.md) and [remaining acceptance](ROADMAP.md) are separate.

## Ownership

| Owner | Responsibility |
| --- | --- |
| XKeen | Official installation, Xray/geodata/component updates, interception, native service and cron |
| Xray | Traffic routing, observation, native balancer selection and failover |
| Panel | Authenticated UI, native command jobs/console, config editors, node/subscription registry, bounded quality recommendation, encrypted transfer, optional Telegram, its own signed updater |
| Keenetic DNS | Ordinary router/client DNS and local names; fresh setup preserves it |

The panel never patches native dispatcher/init/hooks/modules or installs a second component updater. Its lease serializes panel operations only; it does not lock external native CLI or cron.

Issue #145 authorizes the [guided fresh setup](../plan/feature-guided-install-1.md)
implementation as a separate explicit terminal entry. Initial verified stock
dispatcher delivery is restricted to empty destinations; one supported native
auto install owns components. A fixed firmware writer prepares an unassigned
policy before native activation, then assigns all HOME only after validation.
Setup defers normal panel startup and hands off through a protected durable
receipt and kernel process lock. Config pending/jobs/registry/DNS retain their
existing owners; no native patch, generic command API or second updater exists.
Published installer availability and fresh hardware qualification remain separate.

## State and configuration

`/opt/etc/xkeen-control/secrets/nodes.json` is the private node/subscription authority. Managed `04_outbounds.json` entries derive from it; unrelated native fields/outbounds are preserved. Native config files are edited through fixed config IDs, not an appliance-policy twin or arbitrary filesystem API.

Form/Text share drafts and undo/redo. Validated Save creates a pending set; explicit Apply runs one native restart. Discard restores pre-Apply state; optional previous restore is proposed to the operator after Apply. Errors and native console are private. See [editor workflows](CONFIG-EDITOR-WORKFLOWS.md).

Native Xray owns selection inside the configured pool. In released 0.4.12 only the
MIPS automatic review can apply a verified pool change; the standard-profile review
measures without applying. Source on `main` (unreleased, see the ROADMAP release gate)
applies the same automatic policy on both profiles and narrows native Observatory
to the applied pool. Neither pins the top node. The proposed [shared target architecture](../plan/architecture-node-quality-v1.md)
([#199](https://github.com/popiposter/xkeen-control/issues/199)) unifies those
decisions and limits continuous Observatory to the active pool. [Node lifecycle](NODE-LIFECYCLE.md)
records the older implementation snapshot; [roadmap](ROADMAP.md) distinguishes
delivered source from hardware acceptance.

Issue #181 replaces the fresh reference with compact Refilter domain/IP selection and DIRECT otherwise, including torrents. Native DNS uses the stock empty template and system resolution. Fresh setup no longer installs mosdns or assigns a custom firmware DNS profile. Existing deployments retain legacy DNS ownership until explicit retirement; an upgrade does not stop their resolver. The UI offers legacy inspection only and no split-DNS generator or synchronization action. See [simple DNS transition](SIMPLE-DNS.md).

## Trust and resources

Trusted private management only: authentication, CSRF/origin/Host checks, bounded requests, private storage. No generic shell/file manager. Native PTY is bound to one allowlisted job. High-churn state is in RAM; persistent writes and previous generations are bounded. Router credentials never enter Git, build containers or public evidence. [Security](../SECURITY.md) remains mandatory.

GitHub signed Releases are software authority. Source/Actions artifacts are not substitutes. Native upstream update trust is distinct from the panel's pinned Ed25519 trust anchor.

## Authorities

[Native contract](NATIVE-XKEEN.md), [control plane](CONTROL-PLANE.md), [roadmap](ROADMAP.md), [development](DEVELOPMENT.md), [operations](OPERATIONS.md). Historical appliance/admission architecture is retained in [archive](archive/README.md), not an implementation contract.
