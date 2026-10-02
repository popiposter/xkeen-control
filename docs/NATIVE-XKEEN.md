# Native XKeen implementation contract

Issue #121 implements the [ordered plan](../plan/architecture-xkeen-foundation-v1.md).
On 2026-10-02 the operator authorized implementation and qualification on a clean
Entware router, including installation, configuration, service restarts and local
SSH key setup. Password rotation and router reboot are not part of this work.
Credentials remain outside Git, build containers and public evidence.

This contract supersedes historical D.1/D.2 installation and compatibility rules
for the new clean-install generation. The old release records remain historical
evidence; they do not qualify the new implementation.

## Ownership

- Native XKeen owns installation, dependencies, core/geodata updates, its init
  scripts, netfilter hooks, configuration and cron jobs. Invoke its supported
  commands; do not copy their implementation into the panel.
- The panel owns authentication, subscriptions and managed nodes, typed editors,
  status, optional selection supervision and its own signed updater.
- Native configuration is the source for editing. Preserve unknown/unmodified
  fields. A changed candidate must pass full Xray validation before activation.
- The protected node registry owns panel-managed VPN profiles; preserve unrelated
  native outbounds. Generated runtime files are not a second secret authority.
- Use one native-operation admission path and one selection writer. A Go mutex
  alone does not serialize external XKeen/cron activity.
- Discovery does not execute XKeen commands with hidden self-heal side effects.
  Missing capabilities disable the relevant action, not the entire panel.

## Clean-install scope

No migration from older panel generations, old Setup journal recovery, historical
layout detection or backwards-compatible panel APIs are required. Remove obsolete
writers/readers and their callers as the native replacement becomes usable. Keep
current-operation rollback and signed panel artifact verification. Do not preserve
legacy complexity merely to support installations the operator has discarded.

Native XKeen is installed first using its upstream installer. The panel must not
replace its service, hooks or cron during attachment, and stopping/removing the
panel must leave the native data plane functional. A basic native installation
without imported VPN profiles is not claimed to provide working VPN traffic.

## Qualification and delivery

Implementation uses one normal checkout and a Draft PR. Review exact code and
callers, run focused fixtures, then the full local Linux gate on the final clean
commit. Independent review remains separate from author checks. This instruction
does not authorize self-approval or invent a successful hardware test.

Router qualification may use source-built development artifacts explicitly
identified as development builds; they are not signed public releases. Before a
configuration change retain a bounded coherent local snapshot. Native operation
exit status must be followed by independent readback; interrupted/unknown writes
are inspected rather than replayed. Service restarts are authorized. No blanket
opkg upgrade, sustained benchmark or router reboot is needed for this scope.

Subscriptions, routing, quality selection, geodata browsing, backup and Telegram
follow the ordered plan. A second physical router, bot token and optional fleet
controller may be needed for their respective live acceptance; synthetic tests
must never be represented as those live results.
