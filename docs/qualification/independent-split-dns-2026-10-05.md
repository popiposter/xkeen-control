# Independent split DNS: operator qualification, 2026-10-05

**PASS on the operator appliance.** This is an installation/configuration result,
not a new panel binary, automatic installer feature, or change to signed v0.3.0.
Source baseline: `8140c9cda51ca9bf703d3a7965f4595f2c69445a`.
Scope and execution plan: [independent DNS](../../plan/infrastructure-independent-dns-1.md),
under [Issue #121](https://github.com/popiposter/xkeen-control/issues/121).

## Installed path

```text
LAN client -> Keenetic DNS :53 -> native custom DNS profile -> mosdns :15354
  local router names: answered by Keenetic
  ads: NXDOMAIN
  VPN domain set: TLS-verified DoH -> loopback SOCKS :5310 -> Xray native balancer
  other names: TLS-verified public DoH independently of Xray
```

The native profile has one forwarder upstream. mosdns has separate terminal
DIRECT and VPN sequences; the VPN sequence has no DIRECT fallback. Both use
Cloudflare/Google DoH endpoints by IP, without a bootstrap DNS dependency.
The tunnel inbound is loopback-only; the forwarder binds only explicit loopback
and trusted LAN addresses. Keenetic keeps port 53; `opkg dns-override` was not used.
No new firewall rules, native XKeen code patches, cron jobs, updater, watchdog,
second Xray process, panel replacement or node speed test were introduced.

The existing 47 VPN selectors were exported from pinned installed geosite files:
85,586 unique entries, preserving `full`, `domain`, `regexp` and substring-to-
`keyword` semantics. The existing ad policy contributes 853 unique entries.
Node-endpoint bootstrap exception list is empty on this appliance. Precedence is
node bootstrap exceptions, ads, VPN domains, then DIRECT. Keenetic retains its
existing local-name handling. IP-only routing rules cannot classify an unknown
DNS name before its resolution and are not silently converted to domain rules.

Standard mosdns v5.3.4 is installed with a conventional Entware init script,
`S06mosdns`, using Entware's existing `rc.func`. No native XKeen init change.
The Go runtime memory target is 48 MiB; measured RSS after qualification was
35,108 KiB. Config/domain-set writes are explicit. Routine query logs are disabled;
bounded operator console/diagnostic receipts are retained privately. RAM staging
files were removed after installed-service verification.

## Evidence

- Official ARM64 archive: 6,591,859 bytes; SHA256
  `82d80a1a21606fca0bc6b65ac6f90d30cff6bb4a19a6ab6a246cf247dbb78bc0`,
  matching GitHub's published asset digest.
- Extracted/installed/running mosdns executable SHA256:
  `5e651992dbec784df43e0e483428319b0f2892f5fadfd4e39a1462a5d62cb495`.
- Installed mosdns config SHA256:
  `f9375023180097ff06d38aa39ee74162b5dba47dcf764bfb714a66a5be62eb29`.
- Native `03_inbounds.json` SHA256:
  `e4d425b3173f00d0d36d1fcfbf61b1c40190840726075caa5dadfe493ba179cc`.
- Native `05_routing.json` SHA256:
  `6ce8b9ba90a6effbf137e116826c88533bdf769f36a79b75b9f591f90af01476`.

One new typed SaveSet/Apply added the loopback SOCKS inbound and its explicit
balancer routing rule. Full installed-Xray candidate validation passed; terminal
job was completed/applied, exit 0. Independent readback proved a new healthy
native PID/executable and preserved native code/core, panel, auth, node registry,
six-node balancers and all unrelated native configuration. Pending/override absent.

One subsequent new bounded native Stop/Start tested actual Xray absence. During
the verified stop, fresh unique DIRECT positive-control names resolved over UDP
and TCP; local router DNS also worked. Fresh unique VPN names returned DNS refusal,
without DIRECT fallback. Start completed with exit 0; native files were unchanged,
the new PID/API were healthy, and VPN DNS recovered. No accepted operation was replayed.

Final independent acceptance: direct cold lookup, fresh VPN negative lookup,
uncached-forwarder positive VPN lookup, local names, ad NXDOMAIN, VPN AAAA and
HTTPS records all passed. TLS-validated direct HTTPS returned 200 and VPN HTTPS
connectivity returned 204. No cache hit was used as proof of stopped-Xray VPN
recovery. Saved Keenetic startup configuration contains the single new DNS
profile/interface assignment and public DNS engine. The installed executable,
init syntax, manifest, geodata hashes and listener restrictions were verified.

Initial alternate-listener and native-profile first UDP probes timed out during
startup. Those failed observations are retained. Verification after listener/
proxy readiness passed; temporary unsuccessful profile tests were removed with
exact baseline restoration. They were not reported as successful outage tests.

## Maintenance and rollback

The forwarder's exported domain sets are a **static snapshot**. After a native
geodata update or a routing-domain edit, compare the saved source manifest with
the new files, explicitly regenerate both VPN/ad sets preserving match semantics,
validate the standard forwarder config, and restart mosdns. No automatic
synchronization with XKeen updates is delivered or claimed in this result.
Native XKeen continues to own geodata updates; do not add a competing downloader.

Rollback order: first remove only the new exact native interface profile
assignment/profile and restore the previously disabled filter engine, then save
Keenetic configuration. This returns LAN DNS to its prior native resolver.
Restore the private pre-change `03_inbounds.json`/`05_routing.json` snapshot through
the typed native editor, validate the complete config and apply once. Finally
stop the independent service using its conventional init script. These are
documented recovery actions, not actions executed during qualification.
Never erase node registry/auth/geodata or rewrite native XKeen code for rollback.

Private rollback data and operation receipts remain operator-local. No passwords,
subscription URLs, node endpoints, cookies, tokens or raw appliance configuration
are published.

## Boundaries

No router reboot, all-provider tunnel failure, IPv6 LAN routing, guest/other LAN
segment, second client or private client DoH acceptance was performed. A client
using its own DoH bypasses this router resolver. If mosdns itself fails, external
DNS through this profile remains unavailable; this removes the DIRECT dependency
on Xray, not the independent DNS service dependency. Signed-in service eligibility
and external egress capture are not inferred from DNS/HTTPS availability.

References: [Keenetic CLI](https://storage.googleapis.com/docs.help.keenetic.com/cli/5.0/en/cli_manual_kn-1811.pdf),
[official mosdns release](https://github.com/IrineSistiana/mosdns/releases/tag/v5.3.4),
[forward plugin](https://github.com/IrineSistiana/mosdns/blob/v5.3.4/plugin/executable/forward/forward.go),
[domain matching](https://github.com/IrineSistiana/mosdns/blob/v5.3.4/pkg/matcher/domain/matcher.go).
