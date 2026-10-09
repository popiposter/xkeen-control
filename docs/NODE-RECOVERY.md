# Offline recovery of an interrupted node operation

Signed 0.4.5 retains [#158](https://github.com/popiposter/xkeen-control/issues/158),
first delivered in 0.4.3, and adds [durable panel update outcomes](OPERATIONS.md#durable-update-outcomes-162).
Source review, local/hosted FULL and signed publication passed. One hardware
activation attempt retained `activation-intent` after a failed result; its exact
failure stage is unknown. Working VPN traffic afterward does not settle it.
It does not turn an
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

## Verify an existing attempt (#168; not in signed 0.4.5)

The proposed delivery adds a separate offline action for a retained attempt:

```sh
/opt/sbin/xkeen-control nodes recovery inspect
/opt/sbin/xkeen-control nodes recovery verify-existing --digest DIGEST_FROM_INSPECT
```

Use only a subsequently verified signed release containing this command. The
same maintenance exclusion and external-writer quiescence apply. `canVerify`
is separate from `canActivate`; an unresolved activation never permits replay.
Verification invokes no Restart, Start, Stop, Apply, rendering or restoration.

For the initial legacy attempt, the original digest can be reconstructed from
the exact protected marker, registry, sorted config files and previous artifacts
(including inode/mode/mtime and absent files), followed by the stored old runtime
identity. The new receipt is deliberately excluded from that reconstruction.
Exact equality proves retention of the originally validated generation. If a
prior receipt participated in the original digest and cannot be reconstructed,
verification refuses; coherent JSON or a working tunnel alone is insufficient.

The operation also requires a distinct stable runtime, bounded API readiness,
the existing outbound/balancer verification, and unchanged protected input and
receipt identities on re-read. This is not a claim to read endpoint credentials
back from Xray memory. Native routing policy and restricted quality pools retain
their existing verifier semantics.

After proof, a durable `verified` receipt binds the generation without the pending
marker and the new runtime. A protected completion fence is synced before
identity-checked marker settlement. A fresh read after marker removal must still
match the generation, runtime and the identity of the receipt just written.
The `completed` receipt and its parent directory are synced while the fence
remains. A write or sync failure leaves fresh processes blocked even if the
receipt rename already happened. Only the identity-checked fence unlink commits
completion; failure of its subsequent cleanup sync is a durability warning, since
a crash can only restore the conservative fence over an already durable receipt.
An explicitly requested verification can finish a crash
between these steps without any lifecycle command, including after marker unlink
when this durable generation proof exists. Older missing-marker receipts without
the proof remain blocked. A completed receipt with a retained fence needs the
same verification; malformed or mismatched fences refuse settlement. A completed
receipt without a fence is returned without writes.

New attempts retain sanitized stage/reason fields; failed pre-activation validation
uses `validation-failed` and cannot authorize verification. Later failures retain
their activation fence. Native error text is never persisted. This does not
retroactively identify the unknown failure stage of the 0.4.5 hardware attempt.
Existing lifecycle/readiness timeouts are unchanged.
