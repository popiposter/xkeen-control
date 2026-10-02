#!/bin/sh
# Source-only elected NDM caller. Requires the protected admission/notification
# libraries already loaded. Missing fixed reconciliation capability retains dirty
# election without native effects.
# One existing invocation, 15 seconds total, at most eight successful passes.
_nec_clock() {
    IFS=' ' read -r _nec_uptime _nec_unused < /proc/uptime || return 77
    case "$_nec_uptime" in *.*) _nec_now=${_nec_uptime%%.*};; *) return 77;; esac
    _native_gate_decimal "$_nec_now" || return 77
}
_nec_remaining() {
    _nec_expired=0
    _nec_clock || return 77
    [ "$_nec_now" -ge "$_nec_begin" ] || return 77
    if [ "$_nec_now" -ge "$_nec_deadline" ]; then _nec_expired=1; return 77; fi
    _nec_left=$((_nec_deadline - _nec_now))
}
_nec_unresolved() {
    # Re-publish even if the event was consumed just before a failed native pass.
    # Do not retire the election or release uncertain operation admission.
    (umask 077; mkdir "$_ne_root/dirty") 2>/dev/null || :
    return 77
}
native_event_converge() {
    [ "$#" = 0 ] || return 76
    # A lifecycle's foreground hook must borrow directly, never wait for this
    # elected caller while holding operation admission.
    [ -z "${XKEEN_GATE_ROOT-}${XKEEN_GATE_TOKEN-}${XKEEN_ADMISSION_ROLE-}${XKEEN_ADMISSION_ACTION-}${XKEEN_ADMISSION_CALL-}" ] || return 76
    native_event_notify || return $?
    _nec_clock || { _nec_unresolved; return 77; }
    _nec_begin=$_nec_now; _nec_deadline=$((_nec_begin + 15)); _nec_passes=0; _nec_acquired=0; _nec_expired=0; _nec_busy_seen=0
    _na_file_ok /opt/lib/xkeen/native-event-reconcile.sh &&
        _na_file_ok /opt/libexec/timeout-coreutils &&
        [ -x /opt/libexec/timeout-coreutils ] || { _nec_unresolved; return 76; }
    while :; do
        _ne_owns || { _nec_unresolved; return 77; }
        if ! _nec_remaining; then
            if [ "$_nec_expired:$_nec_acquired:$_nec_busy_seen" = 1:0:1 ]; then
                # Known no-effect contention is queued work, not an unknown
                # native operation. Do not leave our soon-dead election behind.
                native_event_defer
                return $?
            fi
            _nec_unresolved; return 77
        fi
        [ "$_nec_passes" -lt 8 ] || { _nec_unresolved; return 77; }
        native_gate_acquire /tmp/.xkeen-admission reconcile
        _nec_rc=$?
        if [ "$_nec_rc" = 75 ]; then
            _nec_busy_seen=1
            # Bounded target-side wait only; no detached process or future tick.
            sleep 1 || { _nec_unresolved; return 77; }
            continue
        fi
        [ "$_nec_rc" = 0 ] || { _nec_unresolved; return 77; }
        _nec_acquired=1
        # Only after admission: Stop/config-change may have won during the wait.
        native_event_consume
        _nec_rc=$?
        case "$_nec_rc" in
            0)
                _nec_remaining && _na_file_ok /opt/lib/xkeen/native-event-reconcile.sh || { _nec_unresolved; return 77; }
                # The fixed worker must join this gate, load CURRENT ready/hook
                # state, verify native postconditions and leave no call records.
                # Timeout/nonzero is unknown: retain gate/election, never replay.
                /opt/libexec/timeout-coreutils -s KILL "$_nec_left" /opt/bin/sh /opt/lib/xkeen/native-event-reconcile.sh
                _nec_rc=$?
                [ "$_nec_rc" = 0 ] || { _nec_unresolved; return 77; }
                _nec_passes=$((_nec_passes + 1))
                ;;
            1) ;;
            *) _nec_unresolved; return 77;;
        esac
        _ne_owns && _nec_remaining && native_gate_release || { _nec_unresolved; return 77; }
        native_event_retire
        _nec_rc=$?
        [ "$_nec_rc" != 0 ] || return 0
        [ "$_nec_rc" = 75 ] || { _nec_unresolved; return 77; }
        if [ -z "${_ne_owned-}" ]; then
            # Retirement race: notify may elect us or queue for a new live owner.
            native_event_notify || return $?
        fi
    done
}
