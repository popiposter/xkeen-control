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
