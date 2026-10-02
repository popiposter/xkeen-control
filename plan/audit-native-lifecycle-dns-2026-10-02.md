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
restart then start and no rollback replay. Focused native/lifecycle/unknown/
reconciliation fixtures pass. Final full qualification and deployment are pending.

## Availability boundaries

| Event | Evidence | Remaining acceptance |
| --- | --- | --- |
| Planned Stop with running core | Live rules removed; router DNS/HTTPS work | Client inside/outside policy |
| Late core crash | Source emergency check is one shot, not continuous | Native cleanup/fallback |
| Stop with already absent core | Source skips clean_firewall | Native idempotent cleanup correction |
| All VPN nodes unavailable | VPN balancer fallback is block | Direct routes/DNS remain available |
| Panel stopped | Source retains native ownership | Independent client traffic |

Do not create a second panel firewall implementation. Late-crash and already-stopped
cleanup require a native-owner correction. Direct Internet should remain independent
of VPN failure. VPN-only traffic must not accidentally leak directly during recovery.

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

Next: deploy lifecycle correction; settle the current generation without Apply
replay; finish subscriptions; qualify native failure cleanup; implement consistent
domain routing/DNS. Final LAN TCP/UDP/DNS claims require clients inside and outside
the policy and remain pending.
