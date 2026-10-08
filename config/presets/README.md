# Reference traffic profiles

`ru-selective-v1.json` is the proposed public reference for Issue [143](https://github.com/popiposter/xkeen-control/issues/143), derived from the operator's running stock 2.1 routing on 2026-10-08. It contains **12 traffic rules**, in their observed first-match order. It is not a complete Xray configuration, an installer input or an automatically deployed default.

Order: explicit force-proxy; private networks and names DIRECT; vulnerable UDP BLOCK; ads/telemetry BLOCK; QUIC for selected VPN services BLOCK; BitTorrent DIRECT; selected domain categories VPN; Russian domains DIRECT; selected service IPs VPN; ReFilter IPs VPN; everything else DIRECT. Explicit force-proxy precedes the torrent exception. Protocol sniffing cannot guarantee detection of encrypted torrents. QUIC fallback is scoped to the VPN service domains and does not disable all UDP.

The profile preserves the current public service/category selections, including the operator's broad AI/development/Google/Microsoft and media categories. These are a selectable **RU selective** policy, not universal preferences for every user. Review them before choosing this profile. Missing geodata/categories must fail validation; do not silently drop a rule or turn its VPN route into DIRECT.

Required installed files/categories are referenced by `ext:`: `geosite_refilter.dat`, `geosite_v2fly.dat`, `geosite_zkeen.dat`, `geoip_refilter.dat`, `geoip_v2fly.dat`, `geoip_zkeenip.dat`. Their contents are not bundled here. Stock XKeen owns their installation/update.

## Integration prerequisites

- `direct` must be a freedom outbound and `block` a blackhole outbound.
- `bal-proxy` is supplied by the existing panel/native integration. On a fresh router use enabled managed `proxy-node-` members and a BLOCK fallback, with native leastPing initially; node-specific selectors and measured leastLoad costs come from that router's registry/recommendation, not this profile.
- Keep the panel API and DNS transport rules **before** these traffic rules, preserving their fixed integration tags. Keep the native inbound tags/settings and loopback SOCKS/API/probe transports consistent. The service-integration rules and balancer are deliberately absent from this policy file.
- VPN names in Xray DNS and the optional independent resolver derive from the same unconditional domain policy. IP/protocol/port rules remain conditional traffic rules and do not classify DNS questions.
- Do not replace an existing `05_routing.json` with this fragment. A future explicit profile Preview merges it with destination integration, retains unrelated native fields and checks a baseline digest. Full candidate validation, pending Save, explicit Apply and existing Discard/previous restore remain the only activation path.

No node identifiers, measured weights, endpoints, subscription credentials, auth/sessions, RCI tokens, device interfaces or firmware policy marks are copied. The private source remains outside Git. This profile has not been applied to the live router and is not yet a selectable UI feature. Fresh-router hardware acceptance remains NOTRUN.
