# Offline recovery of an interrupted node operation

This source path implements [#158](https://github.com/popiposter/xkeen-control/issues/158).
Signed release and hardware acceptance are separate gates. It does not turn an
unknown historical subscription operation into a successful one.

Only an explicitly approved **activate-current** operation is supported. The
authoritative registry and current generated outbounds must be coherent. Recovery
does not import, replace or restore either file, overwrite the previous pair,
patch XKeen, or guess a rollback.

## Admission and preview

Use a signed panel version containing this feature. Record the installed binary
identity and inspect native/DNS/panel state before maintenance. Retain the affected
private files and durable receipts outside public logs. Stop only the panel using
its normal service control, and quiesce competing native cron work. Xray may remain
running during inspection. Confirm no detached native lifecycle child remains.

The offline CLI takes an exclusive lock on the existing fixed setup process lock.
The daemon and ordinary node CLI in signed 0.4.2 already hold shared locks for
their entire lifetimes. This proves exclusion of participating panel processes;
it does not exclude external native CLI/cron or foreign older panel binaries.
The lock is never replaced or upgraded from shared to exclusive. Incomplete
initial setup still blocks maintenance.

Run through the trusted root console:

```sh
/opt/sbin/xkeen-control nodes recovery inspect
```

This performs protected bounded reads only and returns classification, previous
pair classification, counts, phase and an opaque digest. It invokes no native
action. An existing non-completed recovery receipt prevents another activation.
Unresolved native-job receipt, saved native edits, unreadable state, unsafe paths,
mixed registry/outbounds, live cron or native workers refuse admission.

## One explicit activation

After reviewing `current-coherent` and `canActivate: true`, use that exact digest:

```sh
/opt/sbin/xkeen-control nodes recovery activate-current --digest DIGEST_FROM_INSPECT
```

The command reacquires exclusive admission, checks drift, validates a complete
private copy with the existing Xray validator, and rechecks all bound state.
It persists and syncs `previous/node-recovery.json` with `activation-intent`
before one existing native transaction activation. It has no fallback replay
or automatic rollback. Validation uses the normal full configuration budget,
not the small speed-test budget.

Settlement requires unchanged current/previous configuration, a confirmed new
Xray process identity bound to the configured executable/config directory,
readiness and existing API inventory verification. This combination proves an
explicit activation and independent readback; the API alone does not prove every
loaded endpoint credential. The stock environment-based config-directory launch
is supported by the existing process reader.

Recovery persists `verified`, settles the original pending inode, then persists
`completed`. Any interruption retains inspection/replay fencing. Daemon startup
and ordinary node transactions reject unfinished recovery, including a crash
after marker removal but before completion. Never delete a marker/receipt or
restart a CLI command to make it retryable.

On success, independently inspect the receipt and actual native/panel/DNS state,
then restore the panel and prior cron service state. On an unknown result, inspect
without another activation. The panel can be explicitly resumed with its retained
blocked state for read-only diagnosis; that does not settle recovery. A failed
settlement or unknown activation needs a separate reviewed recovery decision.
No reboot, WAN opening or native code patch is part of this procedure.
