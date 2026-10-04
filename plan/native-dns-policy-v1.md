# Native domain routing and DNS policy v1

Issue #121, next bounded design for TASK-017/026. Source investigation only;
no router configuration changed. Incident history and existing hardware evidence
remain in [the lifecycle/DNS audit](audit-native-lifecycle-dns-2026-10-02.md).
The development PC uses Karing; it supplies no independent LAN acceptance.

## Recommended first slice

Implement a scoped native-config editor for one ordered list of domain-only
DIRECT/VPN rules and their DNS resolver choices. Default onboarding preserves
native policy, interception, firmware DNS and listener ownership. The first slice
must not execute `xkeen -dns`, enable `opkg dns-override`, install a listener,
change client membership or create firewall rules. It can preview and validate
the DNS/routing candidate; activation remains subject to existing native
admission, transaction and postcondition requirements.

DNS intent and DNS coverage are separate facts. Show whether queries actually
reach the configured Xray DNS path. A correct `dns.servers` object alone is not
evidence that LAN or router applications use it.

## Exact baselines and additional findings

- Native XKeen: `5aaece27a70d5bd002c615248614914ebbc4569d`. Its public six-file
  config fixture contains an empty DNS object and a comment linking to an
  upstream example. Attachment preserves native DNS. The populated legacy
  `config/xray/02_dns.json` is not the new installation authority.
- Installed-core version recorded in compatibility evidence: Xray 26.9.30.
  The public tag resolves to `b26a91de4f3294e26a0ad0a970b81a386a41f789`.
- The native init generator inspected locally matched its pinned GitHub blob
  `642ab941a1ca034955c0f2804e6e27470a6b99a1`; it was not executed.
- Existing `internal/appliance/custom_policy.go:customProxyDomains` unions VPN
  domains without resolving earlier DIRECT exceptions or other routing
  conditions. Reusing that helper would not satisfy ordered native DNS policy.

Xray's pinned `sortClients` sorts domain matches by configured rule index and
collects matching DNS clients. `disableFallbackIfMatch` prevents adding unmatched
fallbacks; it does **not** remove later matching clients. `finalQuery` stops
collection at that client. Therefore overlapping DIRECT/VPN groups need an
explicit final boundary, not merely `skipFallback` or server ordering.
[Pinned DNS selection source](https://github.com/XTLS/Xray-core/blob/b26a91de4f3294e26a0ad0a970b81a386a41f789/app/dns/dns.go#L271).

## Minimal compilation contract

1. Read native JSONC, the complete routing/DNS/outbound graph and installed
   geodata identities. Preserve unknown fields and unrelated arrays. Refuse an
   ambiguous ownership region or a predecessor rule whose effects cannot be
   preserved. Do not require complete legacy ProductDefault equivalence.
2. Retain local-name handling, static hosts and endpoint bootstrap exceptions.
   Show when an existing hosts entry answers before resolver selection. Protect
   exact VPN endpoint/bootstrap domains from a dependency on the VPN being
   bootstrapped. Use source-known numeric DoH endpoints or verified static hosts
   for resolver bootstrap; do not resolve a resolver recursively through itself.
3. For each supported rule, emit the same domain matcher and order into routing
   and one DNS server group. Preserve `full:`, `domain:` and installed
   `geosite:`/`ext:` meaning; do not expand geosite into a stale second database.
   Initial support can require one resolver per group, `skipFallback:true`,
   `finalQuery:true`, `enableParallelQuery:false`. Any later redundancy is confined
   to adjacent servers in that same class, with a final boundary at its end.
4. Use distinct internal DNS tags, e.g. `dns-direct` and `dns-vpn`. Before general
   traffic rules, route the former to the existing direct outbound and the latter
   to `bal-proxy`; preserve its block fallback. These rules cover generated DNS
   transport, not every client connection to a resolver address.
5. End with an explicit DIRECT default resolver group, no domain filter,
   `skipFallback:false`, `finalQuery:true`. Keep `disableFallback:false` and
   `disableFallbackIfMatch:true`: a matched VPN group must fail rather than try
   DIRECT, whereas an unmatched domain uses the DIRECT default. Avoid an empty
   fallback set: this Xray source falls back to its first client when no client
   was otherwise selected.
6. Apply under the shared authority/native gate: recheck config/geodata digests,
   validate the complete installed-Xray candidate, retain one coherent rollback
   snapshot, atomically replace only changed files, native restart, independent
   readback. Unknown outcome retains existing transaction intent. No new journal.

For generated DNS transports use ordinary routed `https://` DoH, with numeric
endpoints valid for the server certificate. `https+local://`, `tcp+local://` and
`localhost` bypass normal tag-based routing; do not silently label them VPN DNS.
Local names may keep an explicitly understood local resolver path, but it must
not forward back into the same Xray listener. Preserve address-family settings
unless the operator separately edits them. [Xray DNS documentation](https://xtls.github.io/en/config/dns.html).

Example of the compiler's policy shape (schematic, not a deployable whole config):

| Ordered domain rule | Data route | DNS group |
| --- | --- | --- |
| Protected local/bootstrap names | Existing verified direct/local route | Existing verified direct/local resolver |
| `full:direct.example.test` | DIRECT | Direct resolver; final |
| `domain:example.test` | VPN pool | VPN resolver; final |
| No domain match | Preserved explicit data default | Explicit direct DNS default |

The concrete resolver addresses and existing outbound identities come from typed
operator choices/native projection. Do not invent a routable VPN DNS resolver
such as the remote server's loopback without verifying that server's contract.

## Supported meaning and boundaries

The initial shared DNS view supports domain-only rules applying to the same
client/inbound scope. A rule conditional on destination port, protocol, source,
user or different client policies cannot in general choose one DNS path for the
same QNAME. Preserve such native rules and mark DNS derivation unsupported for
their affected policy, rather than projecting just their domain field. Existing
BLOCK rules likewise require a separately tested ordered DNS rejection projection;
do not silently convert them into DIRECT defaults. Arbitrary regex or negated
geodata support requires pinned matcher tests before being advertised.

IP/geoip-only routing cannot generally determine DNS egress before resolution:
the IP is the answer being requested. Cached addresses, literal IP connections,
ECH/unavailable sniffing and client DoH/DoT/DoQ can remove or bypass the domain
information used here. `routeOnly` sniffing can select a data route without
rewriting the client's earlier DNS request. Preserve `domainStrategy`; a final
catch-all may prevent `IPIfNonMatch` from ever entering its resolution pass.
[Xray routing documentation](https://xtls.github.io/en/config/routing.html).

The pinned DNS outbound handles plaintext TCP/UDP DNS and imports A/AAAA into
the built-in resolver. Its default for other query types is an empty successful
response; this is not full MX/TXT/SRV/HTTPS/SVCB fidelity. Do not advertise a
general replacement for firmware DNS using only this mechanism. Encrypted client
DNS requires its own explicit coverage decision, not transparent inspection.
[Pinned DNS outbound source](https://github.com/XTLS/Xray-core/blob/b26a91de4f3294e26a0ad0a970b81a386a41f789/proxy/dns/dns.go#L131).

## Keenetic policy and outage behavior

An XKeen policy selects participating clients; it is not required for all-client
operation. Keep the existing policy as the recommended scope boundary, verify
its ordinary WAN access, and refuse policy-scoped activation if discovery is
missing/ambiguous. Native absence of the named base policy can broaden capture to
all clients; custom policies depend on it. PBR for Xray's own WAN selection is a
separate setting. [Upstream policy documentation](https://github.com/jameszeroX/XKeen/wiki/Configuration#пользовательские-политики).

The pinned native `enable_killswitch` drops selected policy marks, including
their DIRECT traffic; it does not retain domain-level VPN/DIRECT classification.
Without policy marks it declines to install a broad drop. The source's crash check
is one delayed check after startup, not a continuing core watchdog. These facts
do not establish late-crash recovery or selective no-leak availability.
[Pinned native init generator](https://github.com/jameszeroX/XKeen/blob/5aaece27a70d5bd002c615248614914ebbc4569d/scripts/_xkeen/02_install/07_install_register/04_register_init.sh#L3634).

| Failure | Bounded supported expectation | Unqualified boundary |
| --- | --- | --- |
| All VPN nodes down, Xray running | VPN DNS/data fail; no direct fallback for their class | Actual client path and observatory behavior |
| One selected resolver unavailable | Fail within that resolver class | Other direct resolver availability needs same-class redundancy tests |
| Panel down, native core running | Native config remains executable without panel | Independent client traffic |
| Core down, stale interception remains | Both intercepted direct and VPN traffic can fail | Native late-crash handling |
| Core down, interception removed, killswitch off | Ordinary WAN may carry formerly intercepted traffic | VPN-only destinations may leak directly |
| Native policy killswitch active | Selected policy traffic dropped | Direct availability inside that policy is not promised |

Routing all LAN DNS into Xray also couples DIRECT DNS availability to the core.
The upstream DNS-over-VLESS example intentionally replaces the firmware resolver;
it is not a suitable automatic default for the required core-independent DIRECT
availability. Keep firmware DNS/listeners untouched in this slice. Achieving both
direct-domain survival and selective VPN no-leak during core failure requires
separate native-owner/forwarder design and client qualification, not a second panel
interception framework. [Upstream DNS example](https://jameszero.net/4773.htm).

## Acceptance for implementation

- Synthetic overlap: earlier DIRECT exact name under later VPN suffix/geosite,
  and reversed order; route winner and DNS group must agree.
- Matched DIRECT/VPN resolver failure must not contact another class, including
  with later overlapping matches. Test actual pinned Xray DNS dispatch, not only
  a clone of its selection algorithm.
- Verify unmatched DIRECT default, bootstrap/local preservation, stale config and
  geodata digests, unknown fields, empty groups, unsupported conditions, A/AAAA
  and explicit non-address-record limitations.
- Full candidate validation and rollback/readback fixtures under one admission
  owner. No native DNS toggle, firmware change or listener appears in the diff.
- Later hardware qualification needs unproxied clients inside/outside the policy,
  TCP/UDP/DNS, IPv4/IPv6 as configured, node/core/resolver failure and recovery,
  plus independent resolver egress evidence. This stage has none of those results.

No implementation or live split-DNS acceptance is claimed by this design.
