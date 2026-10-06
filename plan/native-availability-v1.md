> Historical implementation/audit record. Current behavior and remaining work are in [plan index](README.md) and [ROADMAP](../docs/ROADMAP.md). Chronological checkpoints below are not current runtime state or permission to replay operations.

# Native late-crash availability

Issue #121 follow-up under the [native contract](../docs/NATIVE-XKEEN.md) and
[shared admission plan](native-operation-admission-v1.md). This is a source-grounded
implementation plan, not a new live qualification or a second recovery framework.

## Observed boundary

Pinned upstream: `jameszeroX/XKeen@5aaece27a70d5bd002c615248614914ebbc4569d`.
Line references below identify its original native init template, before local
admission patches. Source inspection used the pinned public copy; it contains no
production configuration.

| Native source | Consequence |
| --- | --- |
| `check_fd="off"`, line 104; `delay_fd=60`, line 107 | FD monitoring is optional and uses a 60-second cadence. |
| `monitor_fd`, lines 3394–3420 | Only a present core above its FD threshold triggers recovery. An absent core is skipped. |
| Startup crash probe, lines 3906–3923 | A single three-second check cannot detect a later crash. The admission candidate makes this initial check foreground, without extending its coverage. |
| Monitor spawn, lines 3936–3940 | No monitor is launched when `check_fd` is off. |
| `clean_firewall`, lines 3305–3391 | Native cleanup removes its interception, policy routes, owned ipsets and generated hooks. Return status alone does not prove all cleanup succeeded. |
| `enable_killswitch`, lines 3640–3674 | Optional DROP rules select whole policy marks, including their direct destinations. Missing marks cannot safely become an all-client DROP. |
| `emergency_clear`, lines 3677–3692; failed Start, lines 3951–3953 | Native failure handling performs cleanup and optional kill-switch installation; independent readback and error propagation are required. |

Fresh read-only operator evidence on 2026-10-02 reported `check_fd=off`,
`delay_fd=60`, and generated-hook `killswitch=off`. These flags imply that the
conditional FD monitor is not launched by the current native startup path; they
are not a process inventory or a late-crash traffic test. Keep this policy unchanged.
Planned Stop and repeated Stop have separate qualification; neither establishes
late-crash behavior. See the [lifecycle/DNS audit](audit-native-lifecycle-dns-2026-10-02.md).

## Required semantics

The user requires direct Internet and direct DNS to remain usable when XKeen
stops or fails. Preserve the native policy while making its limits explicit:

- With kill switch off, removing stale interception permits ordinary WAN routing
  and independently supplied DNS. VPN destinations may also route directly while
  the core is absent; this is not a selective no-leak guarantee.
- A deliberately enabled native kill switch blocks participating policy marks,
  including direct destinations. It does not provide direct availability within
  that policy. Do not enable it covertly or label it selective VPN-only protection.
- Xray-internal split DNS does not establish independent LAN DNS when Xray is
  absent. Keep this availability step separate from DNS interception changes and
  the [domain DNS/policy design](native-dns-policy-v1.md). Direct resolver and
  bootstrap dependencies must remain independently reachable.

## Smallest next implementation

Depends on complete native hook admission and verified lifecycle settlement.
Coordinate with the owner of `scripts/native-admission-patch.mjs`; this document
does not authorize a competing monitor edit or remove the candidate fence.

1. Reuse the existing single native monitor after a proven successful Start,
   including when `check_fd=off`. Keep FD-threshold recovery conditional on that
   existing option; add core-death observation to the same worker and cadence.
2. Bind the worker to its launch generation: observed core PID/start time and its
   protected RAM monitor marker. A replacement process, removed/replaced marker
   or planned Stop must not cause a restart. Stop remains the authority for
   cancelling the worker; do not infer desired-running from absence alone.
3. On confirmed death, acquire the existing fresh `restart` gate, then recheck
   absence, generation and marker before mutation. Busy admission waits for the
   existing cadence. Never bypass retained/unknown ownership or configuration
   `.pending`; dismiss a stale observation without mutation.
4. Remove only the verified worker's marker and invoke the fixed native init
   `restart on` once, using the existing borrower/foreground path. Native code
   owns cleanup, restart attempts and optional kill-switch behavior. Do not add
   an outer retry loop, panel watcher, persistent marker or second daemon.
5. Release only after verified success. Failed/unknown restart or failed cleanup
   retains admission for readback; no automatic replay. Establish the native
   failed-Start cleanup postcondition before claiming restored direct access.
   A dead worker alone does not prove cleanup, and a blocked gate can delay it.

At a 60-second cadence, detection may take one interval; admission contention and
the bounded native operation add delay. Do not promise a fixed recovery SLA from
the polling interval alone. A whole-router/OOM failure that also kills the monitor
is outside this bounded process-crash mechanism.

## Focused qualification

First use extracted pinned functions with the real gate and synthetic process/kernel
observations: crash after the initial three seconds; death with FD checking off;
planned Stop; reused/replacement PID; changed marker; busy/unknown gate; pending
config intent; successful recovery; one failed restart with no outer retry;
cleanup failure retaining ownership; kill-switch-off cleanup and explicitly-on
policy-mark blocking; removal of native DNS interception. Preserve unrelated rules.

Later hardware qualification requires a bounded snapshot and exact candidate
identity, followed by an explicitly scoped late core termination and independent
readback of core identity, hooks, capture rules, routes and DNS. Qualify both a
successful restart and failed-restart cleanup; any kill-switch-on trial requires
an explicit temporary policy change and restoration. No reboot is required.

Router DNS/HTTPS checks and client checks are separate evidence. Test independent
LAN clients inside/outside the selected policy against direct and VPN destinations
and their resolvers. The current PC uses Karing and cannot provide that LAN proof.
No live failure injection or new traffic guarantee is recorded by this document.
