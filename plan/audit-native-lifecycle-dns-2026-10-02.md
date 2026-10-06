> Historical implementation/audit record. Current behavior and remaining work are in [plan index](README.md) and [ROADMAP](../docs/ROADMAP.md). Chronological checkpoints below are not current runtime state or permission to replay operations.

# Native lifecycle and DNS audit — 2026-10-02

Issue #121. Installed development checkpoint: b330e559e69199f4f3afa5c5e4f90b7e21aba939.
Native archive: jameszeroX/XKeen@5aaece27a70d5bd002c615248614914ebbc4569d.
The operator's computer uses Karing and is not an independent LAN test client.

## Incident

One subscription Preview produced 46 profiles. Exactly one Apply returned a
rejection while retaining the new registry, generated outbounds, previous snapshot
and pending marker. Independent readback found Xray absent but interception rules
present. Full installed-Xray validation passed. Router-local DNS through its own
resolver and a public resolver, plus direct HTTPS, passed during that state.
Client traffic can therefore fail through stale interception without a DNS failure
on the router itself. Client-side causation remains unverified.

After verifying no native operation was running, one separately recorded recovery
Start completed in 2.87 seconds. Independent readback confirmed Xray, native hooks,
API/balancer, 46 profiles, 40 enabled and zero enabled WL profiles. Observatory
initially reported 31 alive. The original Apply was not replayed or declared
successful; pending settlement still requires coherent readback and a receipt.

A planned native Stop then completed in 0.99 seconds. Independent readback found
zero XKeen iptables rules and stopped Xray; router-local DNS and direct HTTPS
passed. A separate Start restored Xray/API/balancer. Raw evidence remains local.

## Confirmed source defects

1. Lifecycle.Run assigned io.Discard to exec.Cmd output. Go creates pipes and
   waits for copier EOF; native background services inherit those descriptors.
   Foreground completion can thus be mistaken for a timeout, followed by killing
   the whole process group including the new core. Leave streams nil to use real
   os.DevNull descriptors. This mechanism matches the incident; no live FD trace
   was collected.
2. CommandActivator.Restart discarded an unknown fallback Start result after an
   explicit restart failure, permitting rollback/restart after an unknown outcome.
   Preserve ErrLifecycleUnknown from fallback too.

Both regressions were reproduced with failing Linux fixtures before correction.
After correction, foreground success/failure returns promptly while the background
child survives; fallback timeout retains candidate/snapshot/pending with exactly
restart then start and no rollback replay. Exact development source
`4114f4af81b9a407ae10f0e345997876391b3216` passed the full local Linux gate,
including 149 browser cases, and its ARM64 artifact was deployed with independent
hash, process and health verification. It is not a signed public release.

The earlier unknown import was not replayed. Registry/render/native-baseline
coherence, full Xray validation, one explicit native recovery restart, its new
process and native `XRAY_LOCATION_CONFDIR` binding were verified. A durable
recovered-current receipt preceded settlement of the pending marker. The original
Apply remains unknown; the current generation is recovered. A fresh second
subscription Preview/Apply then completed with independent readback: two
subscriptions, 59 profiles, 52 enabled, zero enabled WL profiles, no pending marker,
and native Xray, panel and cron running.

One router-originated bounded diagnostic completed in 7.4 seconds using 8 MiB:
approximately 22.6 Mbit/s download, 20.0 Mbit/s upload and 1017 ms HTTP latency.
This measures one node/sample, not the best node or LAN interception. Router-origin
DNS and direct HTTPS also passed with the current running generation. The
development PC's Karing remained enabled throughout.

## Availability boundaries

| Event | Evidence | Remaining acceptance |
| --- | --- | --- |
| Planned Stop with running core | Live rules removed; router DNS/HTTPS work | Client inside/outside policy |
| Late core crash | Source emergency check is one shot, not continuous | Native cleanup/fallback |
| Stop with already absent core | Pinned native init corrected; offline stale-rule delegation and live repeated Stop pass | Real stale-crash state and update persistence |
| All VPN nodes unavailable | VPN balancer fallback is block | Direct routes/DNS remain available |
| Panel stopped | Source retains native ownership | Independent client traffic |

Do not create a second panel firewall implementation. Late-crash and already-stopped
cleanup require a native-owner correction. Direct Internet should remain independent
of VPN failure. VPN-only traffic must not accidentally leak directly during recovery.

These are requirements, not established guarantees: the current native emergency
killswitch drops the entire Keenetic policy mark, including direct destinations.
With killswitch disabled, removing interception can instead allow VPN destinations
to go direct. A cleanup fix alone cannot promise both direct availability and
selective no-leak behavior. The late-crash policy needs explicit design and client
qualification before enabling automatic recovery.

## Keenetic policy and split DNS

Upstream supports operation without an xkeen policy, intercepting all eligible
clients. The named policy selects participating clients; it does not establish VPN
availability. Verify its ordinary WAN/default route too. A policy-only setting must
not silently become all-client interception when policy discovery fails. The live
router reports an XKeen policy; its presence does not prove client connectivity.

Domain/geosite rules can choose DNS servers and route queries directly or through
VPN. Compile routing and DNS from one ordered domain policy, preserving local names
and direct bootstrap resolution for VPN/resolver endpoints. IP/geoip-only rules
cannot always choose a DNS path before resolution: the destination IP is its result.
Client DoH/DoT and cached IPs also need explicit coverage boundaries.

Xray DNS configuration alone does not make LAN clients use it. Sending every query
to an Xray listener would couple direct-domain availability to Xray. Before enabling
split DNS, qualify listener/forwarder ownership and direct availability during core,
node, resolver and bootstrap failures. No proxy-DNS switch or new DNS policy was
enabled during this incident. Do not disable the operator's Karing for testing.

Sources:
- https://github.com/jameszeroX/XKeen/wiki/Configuration
- https://github.com/jameszeroX/XKeen/blob/main/wiki/Knownissues.md
- https://xtls.github.io/en/config/dns.html

Next: qualify native failure cleanup and implement consistent domain routing/DNS.
Final LAN TCP/UDP/DNS claims require clients inside and outside the policy and
remain pending. See PR122 comment 5954037252 and Issue121 comment 5954037993 for
sanitized deployment and recovery evidence.

Follow-up hardware qualification on development panel `38d9fcb`: one planned Stop
and one already-stopped Stop both left no native IPv4/IPv6 rules. Router-origin DNS
and HTTPS passed while stopped, followed by verified native Start, API, panel and
cron restoration. Registry/config hashes and 59/52 node counts were preserved.
Only the native init was patched; native regeneration may overwrite this correction.
Late crashes and unproxied LAN clients remain unqualified.
