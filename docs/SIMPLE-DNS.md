# Simple routing and ordinary Keenetic DNS

[Issue #181](https://github.com/popiposter/xkeen-control/issues/181) is the reviewed transition contract. Published0.4.9 contains the fresh-setup changes. Existing routers were simplified by separate manual operator actions; a panel update alone does not retire their DNS service.

Fresh setup keeps Keenetic DNS and local-name resolution. The native DNS reference is the empty official XKeen 2.1 template. It adds no mosdns process, DNS-over-VPN lists, custom DNS profile, or replacement resolver. Stock XKeen still owns interception and native configuration; its code is unmodified.

The compact policy sends selected Refilter domains and IPs through the native VPN balancer. Local/private and recognized BitTorrent traffic are DIRECT; everything else is DIRECT. Explicit force-proxy and API/measurement inputs remain separate from ordinary selective LAN traffic. No RU/provider/ads/QUIC category union is installed. Extra service exceptions belong in explicit native configuration. Dataset coverage changes over time, IP ranges can include shared hosts, and protocol sniffing does not identify every encrypted torrent.

The panel retains subscriptions, native node selection, bounded measurements, and native config editing. It no longer offers split-DNS generation or a synchronization action. Existing resolver status remains visible without a periodic UI polling loop. The legacy resolver owner is temporarily retained for existing installations; deleting a button does not detach its clients.

## Existing installations

Retire one router at a time using its own baseline and supported typed operation. Preserve the private node registry/outbound pair, native balancer, authentication, management access, and HOME XKeen policy. Do not use the whole fresh-setup rollback: it also removes that policy.

Required order is dependency-driven: inspect pending operations; validate the complete native candidate; prove ordinary DNS availability; detach only the owned firmware/native DNS consumers with runtime and persisted readback; then stop the identified mosdns service and disable its autostart. Keep the bounded previous state until successful client verification. Unknown outcomes require inspection, never repeated Apply or service commands.

Existing mosdns must stay available until its consumers are detached. Manual retirement follows the inspected dependency order above; no blanket package uninstall, firewall rewrite, reboot, or WAN exposure is part of it. Fresh-setup changes do not provide an automatic migration of existing installations.

## Hardware evidence

On MIPS, stock configuration plus one node constructed in 2.380 s; a routing control with all 52 nodes took 2.674 s; a compact two-geodata-reference candidate took 16.243 s. The old routing without duplicated DNS predicates took 48.435 s; the full old configuration exceeded 75 s. These bounded `xray run -test` measurements do not prove serving startup, full client coverage, or DNS retirement. The compact diagnostic still retained some old DNS infrastructure and is not a production configuration.

Normal configured upstream DNS servers answered on both routers. Local-name behavior, hostname-node bootstrap, post-detachment LAN DNS, subscription refresh, and before/after resource use still require separate serving acceptance. DNS encryption and privacy depend on each router’s normal configuration after retirement; no new public resolver is silently selected.
