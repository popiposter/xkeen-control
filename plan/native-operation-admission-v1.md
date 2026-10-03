# Native operation admission v1 — isolated protocol

Issue #121, TASK-008/012/013. This source-only phase adds an opt-in Linux Go
package and a sourced POSIX-shell library. No production caller, installer,
updater, cron entry or default path uses it. It does not qualify CLI/cron/panel
concurrency on a router.

## Shared format and trust boundary

Both peers take an explicit pre-created root below `/tmp`. `/` must be root-owned
0755 and `/tmp` root-owned 1777; every component beneath `/tmp` must be a real,
root-owned 0700 directory. No symlink, dot component or noncanonical path is
accepted. The operator must separately establish that `/tmp` is RAM on the target.
Phase 1 neither creates the root nor configures its storage. Trusted root-owned
code must not replace or modify its ancestors while admission is in use; this
protocol is serialization between cooperating writers, not isolation from root.

Admission creates `ROOT/operation.lock.d` with mode 0700 using atomic mkdir.
The creator writes `owner.tmp` and atomically renames it to `owner` (0600,
root-owned regular file, one link, at most 200 bytes). Its immutable ASCII record
is exactly one line:

```text
v1 BOOT_UUID PID PROC_STARTTIME TOKEN ACTION
```

`BOOT_UUID` is the kernel boot ID; PID and starttime are canonical positive decimal
integers. Starttime is field 22 of `/proc/PID/stat`; parsing accounts for spaces
and closing parentheses in the comm field. TOKEN is 16 random bytes encoded as
32 lowercase hex digits. ACTION currently permits only `start`, `stop`, `restart`
and `config-change`. The library never executes these actions or accepts argv.
The token is local admission context and must not enter public operation reports.

Any existing gate is busy, including missing, malformed, dead-owner or partly
written metadata. There is no age expiry, stale PID removal or automatic reaping.
Errors after mkdir retain the incomplete gate. Shell exit statuses are 75
(busy/unresolved), 76 (unsafe/unavailable) and 77 (ownership unproven).

## Ownership and foreground borrowing

Go exposes `Acquire`, `Join`, `Release` and explicit foreground environment
projection; shell exposes corresponding `native_gate_*` functions. Join validates
the exact token, current boot, live non-zombie owner PID/starttime, and ancestry
from the actual joining process to that owner. It rereads the immutable tuple
after ancestry traversal. Shell uses builtin reads of `/proc/self/stat`, not `$$`,
because POSIX subshells inherit their parent's `$$`.

A borrower cannot release. Only the original owner can remove its exact unchanged
record and empty gate directory. Unknown directory entries prevent release.
Neither signals nor exit traps automatically release admission. A crash retains
the gate until a separately reviewed recovery path inspects it.

**Caller obligations:** the owner must serialize foreground nested work and wait
for every borrower to terminate before release. This library has no executor and
does not track children. An ancestry/token check is not a child-completion proof;
passing the token to concurrent descendants violates the protocol. Likewise,
`StripEnvironment` / `native_gate_strip` are voluntary helpers, not enforcement.
Before launching Xray, cron, monitors or any other background process, callers
must strip `XKEEN_GATE_ROOT` and `XKEEN_GATE_TOKEN`. Background writers acquire a
fresh gate when they later need to mutate. Native detach/cold-start ownership
transfer is not implemented or qualified by this phase.

## Crash, reboot and future integration

Phase 1 implements no recovery journal and no recovery command. Its RAM lock alone
cannot block a new operation after reboot. Integration must connect admission to
the existing transaction/recovery owner and its durable intent before the first
persistent write. In particular, node `.pending` remains the authority; do not
invent another rollback journal. Unknown outcomes retain the existing intent and
require independent readback, never blind Apply/Start replay.

Integration must hold the gate across baseline recheck, candidate validation,
persistent changes, native lifecycle and independent postconditions. Gating only
the subprocess is insufficient. The native dispatcher currently mutates before
its action switch, and `-uk` replaces/re-execs the dispatcher and generates init;
all those paths need one reviewed seam before any concurrency capability is
enabled. This package does not install or patch that seam.

## Focused qualification

```sh
docker compose -f docker-compose.dev.yml run --rm -T dev go test -count=1 ./internal/authority/nativegate
```

Fixtures use real Linux process identities, Go/shell owners in both directions,
actual killed shell ownership, 16 simultaneous Go contenders, incomplete records,
wrong starttime, symlinks, unknown directory entries, and foreground/background
environment projection. The shell metadata reader uses the shared leading fields
of `stat -t`; it does not require `stat -c`, `flock` or `timeout`.

The container uses `/bin/sh` (dash). A compatibility fixture rejects stat flags
other than `-t` while retaining real metadata reads. This matches the recorded
appliance stat interface but is **not an execution on BusyBox or Keenetic**.
BusyBox is unavailable in the qualification image. Hardware, native integration,
reboot recovery, automatic detach and native-update persistence remain unqualified.

## Phase 2 investigation: complete native writer boundary

Read-only source investigation on 2026-10-02 found that a dispatcher/init-entry
patch alone is insufficient. **No native integration patch was produced or
enabled by this investigation.** The Go protocol remains opt-in; do not enable a
panel default until the native counterpart covers the writers below.

Pinned public upstream: `jameszeroX/XKeen@5aaece27a70d5bd002c615248614914ebbc4569d`.
The source identities inspected locally were:

| Source | SHA-256 |
| --- | --- |
| `scripts/xkeen` dispatcher | `feda355231551c2776da78453b950bfe10b59225b9a5c32c18c333649cc2e542` |
| `_xkeen/02_install/07_install_register/04_register_init.sh` | `fbdba1f1cca6e1923e0c43113b4b6fafc51f92248cad70818f7ac92c937e32e3` |
| Already-qualified planned-Stop correction derived from that template | `312bcb89ad188d14818d5feea547e97d729da506bfc46304f090d63725497b19` |

Line anchors below refer to the original pinned dispatcher/template, not an
installed script modified by configuration registration. The original template
is standalone code copied into `S05xkeen`; it is not sourced as a module.

### Entrypoints and hidden writers

| Pinned anchor | Actual path / consequence | Required native seam |
| --- | --- | --- |
| Dispatcher `install_xkeen_rename` call, line 21 | Renames/removes installation paths before command classification | Validate fixed operation and acquire/join before this call, imports or package self-heal; no side effects before admission |
| Dispatcher self-detach, lines 50-72 | Re-executes a lifecycle command in the background and returns success to its caller | Borrowed foreground operations must never detach; standalone CLI must run synchronously in this slice |
| Dispatcher package self-heal, lines 90-99 | Most commands, including lifecycle, can install/repair packages before dispatch | Keep under outer admission; partial package failure is not a clean no-op |
| Template `_xkeen_secure_rundir`, lines 138-170 | Can remove/recreate `/tmp/.xkeen`, including fallback behavior for Stop | The shared gate root must be separate from this mutable runtime directory; validate it before executing this helper |
| Template `proxy_start`, line 3695; `proxy_stop`, line 4002 | Existing proxy mutex covers only native lifecycle internals | Outer operation gate first, then proxy mutex, then native netfilter lock; nested functions do not independently release outer admission |
| Template core spawn branches, lines 3843-3865 | Xray/Mihomo inherit exported context by default | Strip the admission token in the spawn subshell, preserving required native config/asset environment |
| Template initial crash probe, lines 3906-3923 | Unconditional background subshell sleeps three seconds then calls `emergency_clear` | Make this bounded initial verification foreground inside the current operation; retain native cleanup/killswitch policy |
| Template `monitor_fd`, lines 3394-3420; spawn at 3938 | A later background tick calls `proxy_stop` and `proxy_start` directly | Strip inherited admission state at launch; on a trigger obtain a fresh operation gate, recheck the condition, then run a foreground restart under it |
| Template `emergency_clear`, line 3677 | Cleanup and killswitch mutation under only the proxy mutex | Call under the existing foreground operation for initial failure; any later independent entry must obtain fresh outer admission before the proxy mutex |
| Template `configure_firewall`, line 2139; first hook heredoc at 2151 | Generates executable `/opt/etc/ndm/netfilter.d/proxy.sh` with its own early runtime repair | Generate admission before any generated-hook mutation, not just before its iptables commands |
| Generated hook lock code, lines 2282-2347 | Native hook traps and release paths manage a separate netfilter lock | Keep subordinate lock semantics; they must not overwrite or release the outer operation gate |
| Generated hook fast paths, approximately 3055-3070 | Can refill ipsets, update routes/cache, then sync deny-MAC after releasing the netfilter lock | Hold outer admission over the entire hook, including fast paths and final sync |
| Generated hook absent-core branch, approximately 3210-3258 | Independently starts Xray/Mihomo and invokes `restart_script` | Same gate across core spawn, bounded readback and hook continuation; strip token from core; no detached unowned restart |
| Schedule hook heredoc, lines 3288-3293 | `schedule.d/00-xkeen-hotspot-sync.sh` calls the generated netfilter hook | Delegate to the gated native hook; avoid a second scheduler or firewall writer |
| Template `start` without argument, lines 4072-4080 | Spawns `cold_start`, transfers only a native PID guard, then exits zero | Replace detach with a synchronous bounded cold-start worker sharing the gate, or reject this path before ipset/runtime mutation |
| Template `restart`, line 4107 | `proxy_start` runs even when `proxy_stop` fails | Start only after proven successful Stop; unknown/nonzero Stop retains admission and cannot trigger Start |
| Template `cold_start`, lines 4108-4121 | Rewrites cold-start PID, waits for environment, then invokes startup | Support only as a joined foreground worker in this slice, or keep capability disabled; never inherit a dead parent's token |

Native shell traps currently clear/reset `INT`, `TERM`, `HUP` and hook `EXIT`
handlers. Therefore the outer gate cannot be released through an unreviewed
blanket exit trap: explicit success settlement must follow all nested work and
readback. On error/signal/unknown, keep admission/intent. A borrower cannot settle
or release its parent's operation.

### Minimal coherent source slice

Implement the native changes as one fingerprinted public-source patch set, not a
second interception framework. Continue using native `proxy_start`, `proxy_stop`,
`emergency_clear`, firewall generation, native mutexes and native cron. A local
builder may emit exclusive candidate files plus source/candidate hashes; it must
not execute, install, fetch unknown versions or overwrite an existing candidate.
Anchor count/source drift must reject the whole patch set.

1. Add early typed admission to the dispatcher and standalone init. Explicit
   lifecycle operations support `start`, `stop`, `restart`; mixed/multiple commands
   and unsupported writers reject before import/self-heal. Read-only status must
   not be assumed side-effect-free merely because it prints status.
2. Make startup/cold-start and the initial three-second crash check foreground.
   Native core processes alone remain detached, with tokens stripped. A direct
   boot/NDM start is not qualified until its synchronous timing budget has been
   tested; if rejected instead, advertise boot/autostart as unsupported and do not
   deploy this candidate as a functional replacement.
3. Give later monitor/hook invocations a fresh gate; re-read state after acquiring.
   A successful foreground nested hook borrows its caller's context. Preserve
   native failure cleanup; no panel-synthesized rule or killswitch implementation.
4. Gate the generated netfilter hook through every exit and self-restart path.
   The schedule hook remains a thin native caller. Busy NDM events cannot silently
   count as successful application: the current owner must reconcile current
   native state before settlement, or leave an explicit native pending event for
   the next qualified reconciliation. No additional background retry service is
   introduced. Until lost-event convergence is tested, NDM concurrency is disabled.
5. Fix restart sequencing and verify final native state before standalone-owner
   release. Panel borrowers leave final settlement to the existing Go owner.
   Persistent intent/reboot recovery must be supplied by the existing operation
   owner; this patch set must not create another rollback journal. A lifecycle
   command exit code alone does not authorize releasing unknown state.

The admission root is fixed operator configuration, never request input. Its
creation and first enablement belong to a separately qualified installation
step. Shared helper loading must validate the helper's identity/protected path;
an environment-selected executable or generic command-runner interface is not
part of this design.

### Update and cron boundaries

`_xkeen/02_install/07_install_register/02_register_xkeen.sh::register_xkeen_initd`
copies the template to a temporary init, applies preserved native values, then
publishes it. Patching only the installed init is therefore insufficient.
Dispatcher `-uk` (line 588) calls `install_xkeen`, replaces its own source, then
`exec sh "$0" -uk_post_update` (line 624). That continuation reimports modules and
calls `register_xkeen_initd` (line 635). The existing narrow Stop correction does
not survive this native update by itself.

Until the native update candidate itself contains and validates the complete
seam before activation, reject `-uk`, its post-update entry and other native
installer/update operations before any side effect. Do not add a panel component
updater to repair the seam afterward. A later native-supported update integration
must preserve the same operation through replacement/exec and regenerated hooks,
with an explicit unknown outcome if that proof fails.

`_xkeen/02_install/06_install_cron.sh::install_cron` writes `xkeen -ug`;
`_xkeen/04_tools/08_tools_balancer/02_balancer_control.sh::sb_install_cron` writes
`xkeen -sbt`. Both enter the dispatcher but have distinct update/selection effects.
These commands remain disabled in the first lifecycle-only seam. Preserve their
existing entries; do not report them as qualified merely because a busy/rejection
is safe. Their eventual admission must keep cron native and selection ownership
exclusive.

### Integration acceptance before enablement

Use extracted actual pinned functions/generated hook bodies in isolated Linux
fixtures with synthetic paths and intercepted external commands. Never execute
upstream top-level router mutations in the development container.

- Prove contention blocks **before** rename, self-heal, runtime repair, ipset and
  config reads/writes for every enabled entrypoint; include panel owner ->
  dispatcher -> init -> generated hook nesting and separate direct-init owner.
- Kill owner/borrower at admission, after a write and during verification. The
  next writer cannot auto-reap/replay; foreground success does not hide a live
  mutating child. Background core/monitor environments contain no inherited token.
- Exercise startup success, immediate crash, Stop failure during Restart, repeated
  Stop, and direct boot/cold-start. A failed Stop must produce zero Start calls.
- Run a monitor trigger concurrently with node Apply and a netfilter/schedule
  event. Prove one writer and eventual current-state reconciliation; merely
  returning busy or dropping a one-shot NDM event is insufficient.
- Exercise hook fast-path ipset refill, deny-MAC sync after native lock release,
  absent-core spawn and `restart_script`. Every mutation remains under the outer
  owner and all spawned cores lack the token.
- Reject unsupported/mixed dispatcher actions before side effects, plus changed
  input fingerprints and partially prepared native updates. Prove the init
  generated from the patched template contains the same seam.
- Qualify real BusyBox shell/stat behavior, boot/NDM timing and bounded resource
  use before hardware enablement. Linux dash fixtures are not that qualification.

These source findings define the smallest coherent implementation boundary.
Standalone recovery settlement, one-shot NDM event convergence, and native update
persistence are explicit enablement gates, not completed capabilities.

## Contextual panel integration (source only, next checkpoint)

The existing authority lease now has an explicit opt-in native backend and
context-bearing acquire/try/recovery methods. Ordinary `NewLease` remains the
configured backend. The native backend rejects legacy acquisition methods so a
caller cannot silently discard ownership. Node Apply, automatic refresh,
snapshot and explicit reconciliation now use contextual acquisition before
baseline reads, hold it through settlement, and propagate release failures.
Rollback preserves operation values with `context.WithoutCancel` while keeping
its existing bounded deadline. This does not enable the native backend in main.

The fixed lifecycle adapter strips inherited gate variables and projects only a
verified active context while joining foreground completion. Unknown lifecycle
results, failed rollback, or admission loss after committing a node candidate
retain recovery state. Admission loss must not fall through to Start fallback
or restore previous files without ownership. A poisoned recovery context cannot
launch another mutation; explicit subsequent recovery needs a fresh context and
proof of the same retained owner. There is no foreign-lock adoption or reaping.

Focused regressions reproduced lost rollback context, shared admission reopening
after unknown outcome, and unowned rollback after tampering with an owner tuple.
The last case changed back to the old registry and attempted two lifecycle calls
before the repair; afterward it preserves the committed candidate and `.pending`
without executing a native command. These are synthetic Linux fixtures.

This is still an incomplete integration checkpoint. Other historical component,
restore and CLI mutation paths are not enabled with the native backend; legacy
acquisition fails closed if it is supplied. No production default, command
capability or concurrency claim is enabled until all retained callers and the
complete native hook seam above participate. Real BusyBox and hardware evidence,
update persistence, cross-process recovery after panel exit and reboot recovery
remain outstanding. No new journal or installation authority is introduced.

The lifecycle adapter also treats the shell protocol's reserved refusal statuses
75/76/77 as admission failures, retaining the current claim and prohibiting the
ordinary failed-restart fallback. Other native exit failures keep their existing
behavior. A peer's refusal must not be reduced to a generic process error.

## Disabled native source preparation (initial 2026-10-02 checkpoint)

`scripts/native-admission-patch.mjs` now accepts only the two exact public source
identities in the table above. It emits a new directory containing
`dispatcher.disabled.sh`, `init.disabled.sh` and a final `manifest.json` with
input/output hashes and `enabled: false`. Both scripts unconditionally exit 76
immediately after the shebang, before any native code. No environment variable
or argument changes this fence. **These files must not be installed.** The
builder does not fetch, execute or install code; an existing output directory
is rejected. A partial output without the final manifest is incomplete.

The unreachable native body contains preparatory corrections, plus the
previously qualified source Stop correction:

- Restart invokes Start only after successful native Stop.
- Exhausted native Stop and Start attempts explicitly return failure after their
  existing cleanup. Previously their final mutex release/trap reset returned
  success. Tests execute both actual extracted functions, including owned and
  reentrant mutex paths; failed Stop causes zero Start calls, while successful
  Stop, already-absent Stop and disabled autostart retain their native behavior.
- Automatic Start joins the existing cold-start procedure in the same shell;
  nonzero procedure results propagate. Native delay/readiness logic remains
  native. Contended cold-start guard returns 75 instead of successful detachment.
- Initial crash verification runs as a foreground brace group in the existing
  startup shell. Native `emergency_clear` therefore sees native mutex reentrancy.
  If the core is absent after the settling interval, native cleanup/killswitch
  runs and startup returns failure before its success announcement/log/monitor.

This is preparation for admission, **not an implemented shared native seam**.
The dispatcher body is otherwise unchanged; all actions, including status,
updates and cron commands, are disabled by the fence. Generated hooks and core
spawn paths remain unchanged and cannot be generated by these disabled scripts.
Do not remove the fence to obtain a usable candidate.

Remaining work recorded at that checkpoint (entry/finish source integration and
the corrected recovery boundary are described below):

1. Fixed protected helper identity/root, early dispatcher/init Acquire/Join and
   exact final owner settlement; unknown/error must retain admission. The panel
   borrower's settlement and independent native owner's settlement need one
   consistent contract without another rollback journal.
2. Strip context from every detached core/monitor launch; monitor must acquire
   fresh admission before deleting its PID file, recheck its trigger and preserve
   native `fd_out` behavior without cleanup killing the current worker.
3. Gate every generated-hook branch, including early runtime repair, fast paths,
   post-netfilter-lock deny-MAC sync and absent-core restart. Its exec continuation
   cannot lose owner state or borrow a dead parent's identity.
4. Prove busy NDM convergence. A possible bounded native event wait must acquire
   the outer gate before subordinate native locks, limit/coalesce concurrent
   waiters, and re-read current state after admission. Expiry/retained unknown
   stays explicitly unresolved; returning busy is not event convergence. No
   additional retry daemon, silent event drop or permanent event journal exists
   in this slice. This solution is not yet implemented or qualified.
5. Native update persistence and cron admission remain unsupported. Direct
   boot/cold-start timing, real BusyBox behavior, crash/reboot recovery and live
   network behavior remain unqualified. The retained native readiness loop has
   not acquired a new bounded-operation guarantee through this source rewrite.

Focused Docker/Linux evidence: four behavioral assertions failed against the
original pinned template (failed Stop followed by Start, detached autostart
losing startup failure, readiness failure ignored, background crash check
returning success). Review then reproduced two further failures using actual
native Stop/Start functions: exhausted attempts returned success after cleanup.
The final eleven tests pass against the generated disabled
body. Fixtures execute extracted native command dispatch and crash/emergency
functions with synthetic external commands; they never execute full upstream
top-level code. Additional checks cover unconditional pre-effect rejection,
source drift/repatch rejection, shell syntax, exclusive output and manifest
hash/content identity. These are dash fixtures, not BusyBox or hardware proof.
The synthetic readiness-failure fixture proves propagation only: pinned native
`wait_for_ready` returns zero even at its deadline. That native policy, including
`start_delay=0`, is unchanged; no real readiness-refusal guarantee is claimed.
No full qualification, router action, production default change or installation
was performed for this source preparation.

## Native entry/finish helper (source only)

`scripts/native-admission-entry.sh` adds source-only orchestration around the
existing RAM gate. The fenced builder now emits early calls to this helper;
neither candidate nor helper is installed or enabled.
Its only roles are `dispatcher` and `init`; actions are `start`, `stop`, and
`restart`. Paths are fixed native paths, never supplied commands or callbacks:
`/opt/sbin/xkeen`, `/opt/etc/init.d/S05xkeen`, `/opt/bin/sh`, and the native-owned
`/opt/lib/xkeen/native-admission-verify.sh`. The latter is deliberately not
implemented here. Missing verification retains admission; there is no dependency
on the panel process, binary, CLI or API. The precreated gate root is
`/tmp/.xkeen-admission`. Library/native/verifier files must have protected,
root-owned nonsymlink ancestry; deployment must validate the entry library too.

`native_admission_enter ROLE ACTION MODE` either runs a fixed foreground child as an
owner/borrower wrapper, or sets private `_na_body=1` after validating a child.
Callers must exit after a completed wrapper (`_na_body=0`), and execute native
body only when `_na_body=1`. `native_admission_finish STATUS` belongs at the final
reviewed body endpoint, after all mutating work; success does not authorize any
subsequent mutation. The wrapper joins child termination and checks fixed native
postconditions before settling. No EXIT/signal trap releases the shared gate.
An inherited internal role flag alone grants no body authority: token, complete
owner tuple, live owner ancestry, protected call nonce and live immediate wrapper
PID/start-time must agree. Dispatcher-to-init is the only additional nesting.
Standalone lifecycle actions cannot change inside a differently typed lifecycle
owner; a panel `config-change` owner may invoke these lifecycle actions.

Mode is the finite value `forced` or `automatic`. Dispatcher permits forced only;
init permits forced lifecycle and automatic Start/Restart. Forced init children
receive native `on`; automatic init children receive no second argument.
Dispatcher children receive the fixed native `-start`/`-stop`/`-restart` flag.
Mode is bound in the protected eight-field invocation record, completion and
fixed verifier's third argument. No additional environment hint is introduced.
Changing an automatic child's arguments to `on` cannot reuse its parent's proof.

At most two invocation directories (`call.dispatcher`, `call.init`) live inside
the existing gate directory. Each holds a bounded 0600 context and exclusive
completion record, binding wrapper PID/start-time, random nonce, role/action/mode and
gate token. Verified success removes its own invocation metadata. Failed finish,
failed verification, abnormal child termination or missing completion retains
the gate and attempts an `unresolved` directory marker only while the exact owner
tuple still agrees. Native admission then rejects further joins. No stale owner
is adopted/reaped; there is no second persistent intent or rollback journal.

**Cross-peer source integration:** Go `readOwner` now uses `Lstat` to reject any
`unresolved` entry; shell `_native_gate_read_owner` does the same with existence
and symlink checks. Thus Go Verify/Join and shell Join refuse the poisoned tuple,
including a dangling marker symlink. Both environment projections also strip the
three admission hints. Active call directories still permit borrowing but block
primitive Release until verified cleanup; a failed caller cannot release merely
because its own child exited. Focused parent evidence covers three marker kinds,
active-call borrow/release behavior and all four affected Go packages, alongside
17 entry fixtures. This remains source-only. Recovery/settlement integration and
native error propagation to the typed Go admission path must precede enablement.
A killed borrower can leave call metadata without a poison marker; that too is
unresolved, not an automatically recoverable receipt, and prevents Release.

`native_admission_strip` removes exported context and copied private finish/owner
state in a launch subshell. Background launch sites must explicitly call it;
the helper cannot enforce stripping at unmodified native sites. Generated hooks,
monitor, NDM convergence, native verifier, reboot admission/configuration validation
and update persistence remain blockers. No functional or hardware enablement is claimed.

Focused Linux fixtures use only synthetic protected native files and substitute
the fixed paths in a test copy. They exercise real shared-gate acquire/join,
foreground nested completion, borrower non-release, forged flags, premature
exit/EXIT traps, typed failure, failed verification, native child signals,
live/dead/incomplete owner contention, native poisoning and explicit context
stripping. These are Docker `/bin/sh` fixtures, not the router's NDM Shell Wrapper
or Entware BusyBox qualification. No full upstream script runs in these tests.

### Fenced builder entry integration and automatic mode

The generated dispatcher/init still unconditionally exit 76 before their early
admission prelude. Below this unchanged fence, the prelude accepts only one
dispatcher lifecycle flag or typed init lifecycle arguments. It rejects updates,
cron commands, status, mixed commands, unsupported values and direct legacy
`cold_start` before loading native code. Bare init Start/Restart map to automatic;
`on` maps to forced. Stop accepts bare or `on` as forced. No bare boot action is
silently upgraded to forced.

The prelude checks the fixed entry library's root-owned nonsymlink protected
ancestry before sourcing it, then enters admission before template runtime repair
or native dispatcher imports. It preserves original positional arguments and
sets `XKEEN_FOREGROUND=1` for the admitted native body. The three classified
dispatcher lifecycle actions skip `install_xkeen_rename` and package self-heal
entirely: admitting a lifecycle operation does not authorize installation/package
mutation. Native install/update remains separate and unsupported here. The eventual
panel adapter should call native init directly, without dispatcher self-heal.

Successful wrapper callers exit without replaying the body. Terminal dispatcher
and init command-manager results pass through typed finish. Automatic-disabled
Start reaches successful finish without calling startup; a contended cold-start
guard reaches failed finish. The existing native cold-start procedure remains a
joined function, so no detached direct `cold_start` entry is needed by this
candidate. Unexpected early native exit (even zero) lacks completion and retains
admission. Actual native preflight functions and hook code remain unqualified;
their unknown outcomes cannot be advertised as completed work.

Targeted RED cases reproduced automatic-mode refusal, two command-manager paths
bypassing finish, and installer rename/package self-heal during lifecycle. Adapted
entry fixtures, patched native-function/manager fixtures and connected extracted
entry/dispatcher/manager fixtures cover the repairs. Connected tests preserve
bare/on arguments, automatic no-op, cold-start failure, premature zero exit,
nested dispatcher Restart, rejected mode mutation, contention and unsupported
inputs. Native external commands and verifier remain synthetic; this is not
execution of the entire upstream template or a real native postcondition check.
The public-input fixtures remain opt-in; the standalone entry fixtures require
no external public input. The fence and missing hook/monitor/NDM/verifier/update
integration remain activation blockers.

### Corrected native recovery boundary

There is no generalized durable lifecycle journal. Ordinary native Start/Stop
owns its generated runtime files. The existing node `.pending` transaction is
the only authoritative configuration-transaction fence; this helper adds only
RAM admission metadata. Same-boot unknown outcomes retain their RAM gate. After
reboot, native Start is a new operation that must validate configuration and
check `.pending`, not replay an old unknown lifecycle attempt. That validation
and pending-state refusal are not implemented by this slice. Earlier statements
in this plan requiring standalone persistent lifecycle intent are superseded by
this native-contract boundary, not deferred into a second journal implementation.

### Long-lived launch context isolation (still fenced)

The pinned template has eight core spawn variants: six in `proxy_start` (Xray
and Mihomo, each with `fd_out`/nohup, verbose, and quiet paths) plus two in the
generated hook's absent-core branch. The builder now emits each as a launch
subshell that calls `native_admission_strip`, then `exec`s the original native
command with unchanged arguments, nohup choice and redirections. This keeps
native `$!` PID semantics while removing all five admission environment hints
and copied private finish/owner state from the child only. Xray config/asset
paths, Mihomo home/memory policy and the CA path remain native environment.

The generated hook is a standalone shell and cannot inherit init functions.
Its quoted heredoc embeds the exact `native_admission_strip` function extracted
from the repository entry helper at build time. Thus the local entry helper is
also a source dependency of the candidate builder; the emitted candidate hash
binds the resulting embedded definition. No command executor or hook ownership
mechanism is added by this extraction.

The existing monitor launch similarly strips in its own subshell. Its initial
source checkpoint refused threshold-triggered restart with status 76; the fresh
admission slice below replaces that refusal. Polling remains read-only. The earlier
cold-start detach and startup crash-check background worker have already been
replaced by joined foreground paths; no further long-lived launch sites were
found in the pinned lifecycle init/hook source.

Nine extracted-source Linux fixtures reproduced inherited admission authority
before the patch, then passed for all eight core launch variants and the monitor.
Synthetic core executables prove child hints/private state absent, parent state
unchanged, exact native argv/environment retained and core PID equal to `$!`.
The monitor fixture uses the actual extracted native function and proves its
restart trigger requests fresh admission without inherited authority. These
public-input fixtures remain opt-in and execute no complete router script.
No generated-hook gate, runtime installation or hardware claim is introduced
by this launch-isolation slice.

### Fresh monitor restart admission (source-only, still fenced)

The stripped existing monitor captures the core PID and proc start time when
the native file-descriptor threshold is exceeded. It acquires the existing
`restart` RAM gate, then rechecks process identity and threshold. Busy admission
uses the existing polling cadence. A stale sample releases its unused gate and
returns to polling; it does not restart a replacement process.

Before mutation, the monitor checks its PID marker is a bounded root-owned
regular nonsymlink file, with its own canonical PID and live proc start time.
It rechecks the marker device/inode and content immediately before removing it
under the gate. An unowned marker is preserved and the unused gate released.
After removal, the monitor synchronously calls the fixed native init
`restart on`, with command-scoped `fd_out=true` preserving the native nohup
branch. The init wrapper borrows admission and performs its explicit pre/post
verification. The monitor releases only after that verified success. It never
removes the marker again, since the new native monitor may have published one.
Child failure, failed postcondition or cancellation retains admission; there is
no trap release, lifecycle retry, additional daemon or second journal.

Nine dedicated extracted-source fixtures exercise fresh ownership, busy polling,
changed core, reused PID, cleared threshold, replacement marker, child failure,
post-verification failure and cancellation of the owner during its foreground
child. The initial eight fixtures failed against the old refusal and passed
after implementation; cancellation coverage was then added. These fixtures use
the real gate and entry helper with synthetic core observations, native body and
verifier, so they establish source protocol behavior rather than router lifecycle
postconditions. The complete candidate remains unconditionally fenced pending
generated-hook convergence and the remaining native integration qualification.

### Generated-hook propagation and convergence boundary

The pinned `_xkeen_apply_table` already returns failure after three failed
restore attempts. Its aggregator masked every result with `|| true`; both
cached and full rebuild paths could then publish cache/state and finish
successfully. The source builder now preserves each enabled IPv4/IPv6 nat/mangle
failure and exits before that success tail. Native rule rendering is unchanged.
The unchanged-WAN/intact-rules fast path now calls the native deny-MAC sync
function too: schedule.d is event-driven, so skipping it cannot be repaired by
assuming a future periodic tick. Nine extracted-source fixtures cover these
paths, including the real bounded restore retry. Six behavioral assertions were
RED before the patch; the final focused hook/monitor/launch set has 27 passes.

The source candidate additionally checks generated-hook route reads/writes,
enabled-family ipset creation, bounded geo refill producers and deny-MAC API/JSON
producers before reporting success. API data is parsed as one supported host
collection; an error-shaped or null reply cannot replace the live deny set with
an empty set. Slow deny-MAC synchronization remains after native netfilter-lock
release and before successful cache/WAN-state publication. Failed membership
queries refuse rather than treating an unreadable populated set as empty.
Temporary read files have explicit RAM/file-size bounds and are removed at exit.

This is error propagation, not complete native proof. The separate init-level
deny-MAC loader and other rendering/load helpers are not qualified by these
generated-hook changes; `_xkeen_rules_intact` still checks weak presence.
The separate bounded proof below never invokes that generated hook or its
runtime-directory, lock, ipset and route writers. Neither successful shell
status nor weak chain presence is sufficient.

The planned event admission path uses a bounded transient elected invocation
and coalesced RAM notification, not a permanent daemon or persistent journal.
It must load the current fixed generated hook after admission, drain current
state including schedule deny-MAC, and close the final-event/leader-retirement
race. A deadline or dead/incomplete leader leaves an explicit unresolved event;
there is no claimed future tick or stale-owner reaping. A lifecycle's direct
foreground hook borrows its existing admission and never waits for the event
leader while holding the gate. This convergence protocol is still design-only;
the unconditional whole-candidate fence remains mandatory.

`scripts/native-event-notification.sh` supplies source-only notification primitives
for that design, without an executor or retry loop. One protected RAM directory
coalesces dirty events; an elected existing invocation publishes a boot/PID/start
record and consumes dirty immediately before its later current-state reconcile.
Only that live process may consume or retire. A new event during retirement
requires re-election; an already elected replacement is never deleted. Dead or
incomplete elections remain unresolved, with no stale-owner reaping. These
primitives do not yet acquire operation admission, enforce a wait deadline or
invoke/read back the current hook. The caller must implement and qualify all of
those before reporting event convergence or enabling the native candidate.

`scripts/native-event-convergence.sh` now provides a source-only elected caller:
one existing invocation waits outside operation admission, with a fifteen-second
monotonic budget and at most eight successful passes. `reconcile` is a distinct
shared shell/Go admission action, not authorization to replay Start/Restart.
Dirty is consumed only after admission. The caller invokes only the protected
fixed `native-event-reconcile.sh` under the remaining timeout; failure/timeout
re-publishes dirty and retains the election and uncertain operation. Successful
readback must leave no borrower records before release and retirement. A queued
follower returns 75, which does not mean convergence has completed.

The fixed source worker `native-event-reconcile.sh` now borrows that `reconcile`
owner. It reads current protected native `ready` after joining, and uses a fixed
event role whose start/stop labels mean running/stopped expectations, not commands.
Running invokes only the current generated hook; stopped invokes neither init nor
hook. Both have native pre/post configuration/core/kernel proof. `start_auto=off`
does not suppress reconciliation of an already-ready service. Ready is frozen
through proof; a changed/unsafe marker or retained config pending stays unresolved.
Foreground init's hook still borrows directly without event waiting. A direct
generated hook (including the existing native schedule caller) elects this bounded
caller before any native effects. Its internal event hook borrows the same owner.

Caller fixtures use a synthetic worker to isolate deadline/race orchestration.
Separate integration fixtures execute actual entry/caller/worker/core-verifier
code with synthetic /proc/Xray/kernel readback, including direct NDM running and
stopped events and ready drift. They are not real generated-hook, BusyBox or target
kernel acceptance. Native candidate fences remain mandatory; native update/boot
preservation and hardware timing/readback still need qualification before install.

The source entry also prepares its fixed RAM admission root after RAM loss before
a fresh native operation. Creation uses protected parents and an exclusive mkdir;
concurrent init/NDM entrants accept the same protected winner. Existing unsafe
paths or retained unknown operation contents are never chmodded, deleted, adopted
or settled. Existing pending refusal/config-validation still precedes native body.
This supplies RAM bootstrap only, not a real boot/reboot qualification or update
preservation. Root creation and a new operation do not replay a former operation.

### Native registration/update preservation boundary

The shared gate has a distinct finite `update-xkeen` action; this adds no native
executor and does not enable the backend. The source-only staged-child context
proof uses the existing protected gate and `call.update` RAM scope, with the
live wrapper under that owner and a separate nonce-bound body PID/start record.
The wrapper may own admission or borrow an existing joined foreground owner;
it never creates a second operation gate.
Only the body's immediate child with the exact PID-derived native staging path
authenticates. Deeper descendants, lifecycle tokens, changed/noncanonical/unsafe
records and arbitrary stage paths refuse. The helper rereads owner/context/body
and never releases admission or publishes native completion. The body publisher
exclusively creates one bounded root-only RAM record from its actual `/proc`
identity after strict wrapper-child proof; repeated publication, deeper inherited
children and existing foreign paths refuse without repair. It cannot publish
from a stage worker or settle its parent. Native update entry integration,
connection of the exec proof and the actual
staging writer/verifier are still prerequisites; the protocol helper alone cannot
run an update or qualify file preservation. No new durable journal is introduced.

The source-only post-exec proof requires the actual same body PID/start and
immediate wrapper parent, a canonical protected staged-generation receipt and
the fixed protected installed dispatcher's expected hash. PID identity alone is
insufficient: a checked bounded RAM snapshot of `/proc` command arguments must
match exactly `/opt/bin/sh /opt/sbin/xkeen -uk_post_update`, including NUL argument
boundaries. Only then is one exclusive RAM `exec.used` phase consumed. Owner,
context, body, staged receipt and live hash are rechecked after consumption;
failures retain all evidence and admission. Fixtures perform real same-PID exec
and refuse an old-body helper call without exec, unexpected arguments, repeated
phase entry, unsafe receipts and post-consumption drift. The argv snapshot and
phase marker stay in the existing call scope, not a new journal. Actual dispatcher
builder now pins the native handoff to that exact interpreter/path/argument vector
in its still-disabled dispatcher. An isolated actual-handoff fixture demonstrates
the original PATH/argv0 dependency and the fixed handoff with a shadow interpreter.
The stage child's receipt publisher now hashes the actual protected, nonempty,
bounded (512 KiB) dispatcher at the fixed PID-derived stage path. It creates
`staged` exclusively inside the existing call scope, then rechecks ancestry,
context, body, receipt and dispatcher hash. A repeated publisher, deeper inherited
child, unsafe/oversized file or existing foreign receipt path refuses. Drift after
publication retains receipt and admission. Real post-exec fixtures now use this
publisher instead of constructing the receipt themselves. This proves generation
handoff only: the future fixed stage worker must validate/decorate the complete
supported module profile before publishing. The publisher neither replaces live
files nor proves module preservation or grants completion. The shared installed
dispatcher hash check uses the same bounded protected-file reader.
Update entry integration, the complete staged-profile writer, module preservation
and native update postconditions remain pending and fenced.

`native-update-profile.mjs` is a build-time compiler for one complete public
archive profile, not a runtime installer or patch interpreter. The pinned archive
from upstream commit `5aaece27a70d5bd002c615248614914ebbc4569d` is 125,747 bytes,
SHA-256 `1d246871d8fc9df2e68e80cef18562e6222661e40eaea1b6853cf8e0e6348b5f`.
The checked-in inventory contains all 72 regular files (559,777 content bytes),
including `import.sh` and every module. Every path, size and hash must match;
extra/missing/changed files, unknown directories, symlinks and hard links refuse.
The compiler invokes the existing builders for exactly three overlays:
dispatcher, registration template and native installer module. The complete
prepared inventory preserves all other hashes. All overlays remain fenced;
an exclusive new output directory and manifest-last publication do not install
anything.

The compiler now emits one disabled fixed stage worker with literal inventory
checks; it does not source a staging manifest or accept a profile selector.
Its isolated executable bootstrap protects the fixed libraries before sourcing.
The authenticated immediate stage child verifies every source path/type/size/hash
and all three protected overlay payloads before any decoration. Exclusive bounded
temporary copies are verified before replacing only the three named staging
files. Complete prepared-profile checks and the original owner/context/body proof
surround receipt publication. Failures retain admission; the native installer
owns staging cleanup and live replacement. Both the template and compiled worker
retain unconditional fences.

Fixtures execute the actual extracted native installer function with this real
authenticated worker on disposable paths: missing worker/unknown module preserves
both old live files, while success promotes all 72 prepared files. Successful
archive cleanup belongs to the later native post-update phase; the installer
function leaves that archive in place. These are source-function integration
results, not execution of the complete updater, update postconditions or router
acceptance. Updater entry/finish, exec integration, registration/error propagation,
all writer/cron coverage and target qualification still block activation.

The disabled dispatcher now propagates reported failure from mandatory init,
configuration creation, package registration/normalization and permission steps
in the native post-update branch. Failed native restart exits before later
cleanup/output can hide it. Missing/failed post-update discovery refuses rather
than falling through after replacement; discovery and exec both use the fixed
installed dispatcher path. Isolated actual-branch fixtures reproduce three
upstream false-success cases and preserve successful running/stopped ordering.
This guards reported return codes only: module-internal masked errors,
new-feature/port/cron effects and independent update-specific postconditions
still need qualification before update admission can be enabled.

The disabled `native-admission-update-patch.mjs` experiment pins the actual
native installer module by SHA-256 and inserts a fixed protected staging callback
after native extraction/shape checks and before its first live rename. Native
archive extraction and the existing `.old` replacement remain upstream-owned.
Missing or rejected preservation leaves both live dispatcher and module tree
unchanged in the actual extracted-function fixture. A successful synthetic
callback proves only replacement ordering, not real admission preservation.
The module output is unconditionally fenced. The authenticated staging worker
and complete supported profile now have source fixtures described above;
updater executor/exec wiring, full postconditions and long-update event settlement
remain unimplemented. Nothing from this experiment may be installed.

The opt-in fixture requires the exact **public** installer input, outside the
default helper catalogue:

```powershell
docker compose -f docker-compose.dev.yml run --rm -T dev env XKEEN_ADMISSION_INSTALLER=/workspace/dist/native-update-contract/03_install_xkeen.sh node --test scripts/native-admission-update-patch.test.mjs
```

Pinned upstream `register_xkeen_initd` copies
`07_install_register/04_register_init.sh`, reapplies declared native settings, then
atomically renames the result onto the live init. It preserves setting values,
not arbitrary added code. A local fixture executes that exact hash-pinned function
and demonstrates that decorating the live init alone loses the admission seam.
The source builder now emits a separately named fenced registration template
identical to its init candidate; native registration from that template retains
the seam, generated-hook integration and declared settings. This uses the native
registration function; no second registration writer or installer is introduced.

This is only the regeneration half of persistence. Native `-uk` replaces its
dispatcher and module tree, then execs the new `-uk_post_update`, which registers
init again. The archive staging path must preserve a supported integration profile
before native installation replaces live files. Unknown profiles must refuse at
staging, not discover lost admission after a successful update. No update command
is enabled here; the template remains fenced, uninstalled, and native ownership
of downloading/installing components and cron remains unchanged. Long update vs
NDM deadline/notification settlement also needs explicit qualification before the
shared native backend is enabled.

The source-only `native-update-init.awk` primitive separates exactly the pinned
native registration's declared assignments plus `start_auto` and `start_delay`
from init code. It preserves their exact assignment bytes, requires each key
once and refuses expansions, escapes, compound assignments and unsupported
numeric literals without emitting partial output. Other lines remain visible
in the normalized code projection; comparing that projection to the prepared
template detects manual code drift. Actual native regeneration fixtures prove
preservation of policy labels, descriptor limits, init delay and autostart.
This pure data parser executes no native code. Its future fixed verifier caller
must first authenticate admission, protect/bound the input and require a final
LF, then compare both code and settings digests. That caller, update baseline,
package/kernel postconditions and executor integration are not implemented by
the primitive and still block activation. It is not installed on the router.

`native_update_verifier_context pre|post` now supplies read-only invocation
authentication for a future update-specific verifier: immediate live wrapper
parent with the canonical current call generation. Preflight refuses existing
body/exec/completion evidence. Postflight permits the native body to have exited,
but requires its stored identity, prepared dispatcher/stage/consumed exec proof
and a canonical typed completion record; all are rechecked before return.
It never grants body/finish authority or releases admission. The fixture's
exclusive RAM completion publication is synthetic protocol evidence, not the
native completion producer. Actual terminal publication, baseline/postcondition
verification, already-current no-op handling and executor integration remain
unimplemented and block enabling this candidate.

The existing native verifier now contains a source-only init identity reader
primitive. It authenticates the direct wrapper pre/post context itself, checks
protected bounded init/template/parser files and their final LF, and performs
the data projections under a file-size limit in exclusive RAM query storage.
It checks prepared code equality, native Xray/autostart literals and unchanged
input/parser hashes and call generation after projection. Only digests become
result fields; raw settings never enter a diagnostic response. Unsafe input,
existing query state, producer failure, code/setting or generation drift retains
admission and any query evidence. The executable verifier still has no update
branch: complete profile, config/core/kernel/package/cron baselines, native
completion producer and final settlement remain required before wiring it.

Update kernel readback now reuses the existing native Hybrid query/proof helper.
A bounded read-only observer may descend below the live update wrapper (for the
existing timeout/query processes); its full PID/start ancestry is frozen and
rechecked. Direct verifier/body/stage privileges remain restricted separately.
Preflight requires intact running interception, or genuinely stopped state for
an unchanged stopped baseline. Postflight requires typed completion evidence
and the same running profile or unchanged stopped snapshot. Autostartoff does
not suppress explicit running-update proof. Unsupported state, kernel drift or
missing completion retains the baseline. Fixtures use actual admission/proof
code with synthetic kernel views and synthetic phase receipts: no updater,
timeout utility on hardware, packet traffic or live completion is qualified.

The profile compiler also emits an unconditionally fenced installed-profile
checker. It checks the 72 prepared file hashes/sizes at fixed native locations
and rejects unknown files/directories or unsafe links in the owned module tree.
It leaves unrelated `/opt/sbin` programs outside its inventory. Protected library
bootstrap precedes authenticated read-only observer context; the original owner
generation is rechecked after the inventory. No native code is executed, no
body/stage/completion receipt is published, and admission is never released.
This checker is not yet wired into updater acceptance and must not be installed.

The source-only update environment preflight now excludes legacy init/port/list
migration and first-install/new-feature effects. It requires protected existing
native configuration and ipset/crontab directories, valid native JSON and a
successful size/time-bounded native package query proving either one installed
cron package or its absence with the exact pinned native version0.6 cron init.
Both make native cron-init registration a no-op. It refuses every crontab line matched by the pinned native removal
expression and freezes safe remaining content or absence. Fixed inputs and
capabilities are checked again after the query; ambiguous output, migration
state, drift or existing query evidence retains admission. Pinned actual native
feature/cron/migration/configuration functions on disposable files demonstrate
that supported inputs skip feature installation/legacy migration/cron init
replacement and preserve config/crontab content through their native operations.
The native script's1711 bytes are derived from its pinned public echo-e literal;
read-only installed-script hash comparison matches that generation on the target.
No cron package installation or live native registration is performed or claimed.
Main update wiring, complete prepared profile/package/kernel/config baselines
and final native completion remain pending; all candidates stay fenced.

Normal long-writer contention has a separate known-no-effect result. If an
elected NDM invocation reaches its valid monotonic deadline after busy admission,
and has **never acquired** operation admission, it may retire only its own live
election while leaving the canonical empty protected dirty marker published.
It returns queued/deferred `75`, not convergence success. Unknown files,
ownership/clock failures, missing capabilities and any post-acquisition failure
retain the existing unresolved boundary; no dead leader is reaped. Notifications
during cleanup remain dirty and successor elections are untouched. Native updater
completion still needs an explicit fresh current-state drain after verified
completion and release; there is no background retry or future-tick guarantee.

### Bounded native Hybrid kernel proof (source-only)

The source-only foreground seam now gives the init's generated hook one bounded
`call.hook` record under the same RAM admission owner. The hook derives its action
and forced/automatic mode from the authenticated live init record; it executes
only the fixed generated path. Native exec-self preserves its immediate-child
body identity. A new child merely inheriting hints cannot enter or finish it.
The wrapper supplies native pre/post verification and success completion only
after the reviewed WAN/intact/cache/full terminals finish their writers. Early
ready/lock exits, failed synchronization or failed readback retain the operation.
Admission precedes even native runtime-directory repair. Direct NDM/schedule
hooks now enter the separate bounded elected caller described above; complete
target event convergence still needs qualification before installation. No new gate owner,
persistent journal, renderer or panel dependency is introduced.

`native-admission-hook-verify.sh pre|post ROLE ACTION MODE EXPECTATION` is a
fixed read-only helper called by the native core verifier. It authenticates the
live gate and canonical foreground call record through `_na_descendant_ok`;
the completion writer retains the stricter direct-child check. Its only writes
are one bounded query file and one context-bound baseline in that existing RAM
call directory. A failed/unknown postcondition retains the baseline; successful
post removes it. There is no persistent journal or native writer command.

The supported profile is native Xray Hybrid with both IP families, the active
TCP/UDP DSCP force branch, native chain/tag/mark/table identities, router
proxying off, proxy DNS off, file DNS false, killswitch off, aghfix off and no
full-policy mark. Native init/config checks enforce DNS and killswitch capability
even before a generated hook exists. Protected init/generated-hook
scalar literals are parsed as data, never sourced. Four unambiguous transparent
inbounds are selected from strict JSON using the native mode/tag rules; generated
ports must match them after Start. Conditional profiles outside this scope
refuse explicitly. Preflight permits empty/missing generated hooks and stopped
kernel state, validates readback capability and prospective inbound shape, and
does not require running-state postconditions before Start.

Running proof checks exact required ports, protocols, proxy IPs and mark values;
normal/force capture chains and jumps; deny-MAC RETURN before capture jumps;
CONNMARK full masks, restore state, negated-zero save condition, and essential
restore/socket-mark/save/TPROXY order. Selected anchor predicates must have the
native polarity: inverted DSCP, deny-set or restore-state matches refuse, while
the native negated-zero mark-save condition is required. The policy rule must
be the unconditional native `from all fwmark ... lookup ...` form; conflicting
partial masks or other rules targeting the owned table refuse. Native Hybrid
policy-all jumps without an explicit protocol remain valid. Only the fixed
capture/restore/socket/save/deny/forced-DSCP anchors use a constrained native
option grammar, rejecting extra source/interface restrictions and binding the
deny set direction to its own argument. Variable normal policy jumps are not
re-rendered. It checks required ipset types/families
and native policy-routing invariants, including local default and copied source
routes. Stop requires no owned capture chains/tagged rules, policy rule/routes,
cleanup-owned ipsets or schedule hook, and an empty/absent netfilter hook.
Automatic no-op compares only the bounded owned structural snapshot, excluding
packet counters, unrelated rules/sets and dynamic ipset statistics.

Queries use a finite command switch. Producer output goes first to the protected
RAM query file under a file-size limit, with actual producer exit status retained;
only bounded successful output enters shell memory. `ipset list -terse` supplies
metadata without enumerating geo memberships. Its syntax and the kernel rule
normalizations were checked against a sanitized read-only target projection.
This is structural lifecycle proof, not complete firewall semantic equality,
deny-MAC membership freshness, or TCP/UDP/LAN/DNS functional acceptance. Native
sync errors still require propagation, and generated-hook admission/convergence
and update persistence remain fenced prerequisites before installation.

## Actual Entware shell protocol probe (2026-10-02)

The first isolated RAM probe of the old shell library stopped before creating a
gate: the appliance BusyBox `od` supports neither GNU `-A`, `-N` nor `-t`. Its
empty test directory was removed after independent before/after PID and config
hash comparison; the failed intent/receipt remain local and were not replayed.
`/bin/sh` is the Keenetic NDM wrapper, not a BusyBox symlink. Script execution is
delegated to Entware; the probe therefore identifies and uses `/opt/bin/sh`.

The corrected library reads a bounded sixteen bytes, uses `od -v -b`, validates
exactly sixteen octal bytes and converts them to lowercase hex. Word output was
rejected during review because fifteen input bytes can be padded to sixteen.
Focused fixtures cover empty/short/oversized/malformed input, a byte-exact vector,
limited BusyBox options and a Go child borrowing a shell owner's gate.

Fresh reviewed probe of library SHA-256
`29c9c9b38920fbd59a5b911d60b56bccf30797c569f3c6c2047e7ee908981ec2`
passed on the actual Entware BusyBox shell. It checked owner acquire/release,
competing-owner rejection, a foreground subshell joining but unable to release,
wrong-token rejection, foreign-entry retention, and a second clean ownership.
It used a distinct exclusive RAM directory and durable local intent. Final
readback matched service PIDs and panel/init/config/registry hashes; no service
commands or installed-file changes occurred and the RAM directory was removed.
Raw outputs and secrets remain operator-local. This qualifies these shell
protocol paths with the actual PATH utilities, not all utilities as BusyBox.
It does not qualify native hooks, Go-owner hardware execution, reboot recovery,
update persistence or a working panel/native concurrency integration.

Reproduce the disabled native-function fixtures after placing only the two
hash-verified **public** inputs at the indicated ignored `dist` paths (never
mount the operator credential/artifact directory):

```powershell
docker compose -f docker-compose.dev.yml run --rm -T dev env XKEEN_ADMISSION_INIT=/workspace/dist/native-public.sh XKEEN_ADMISSION_DISPATCHER=/workspace/dist/native-dispatcher-public.sh node --test scripts/native-admission-patch.test.mjs
```

Set `XKEEN_ADMISSION_UPSTREAM=1` on that same isolated command to reproduce the
upstream behavioral failures. This opt-in public-input fixture is not implicitly
counted as part of `dev-check.ps1` when its external pinned inputs are absent.
