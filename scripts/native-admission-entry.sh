#!/bin/sh
# Source-only native entry/finish protocol. Not installed or connected by default.
# Fixed native paths only. This library does not install a verifier or clear stale state.

# The installer must protect this library itself. Dependencies and fixed native
# code paths are checked before use; request/environment paths are never executed.
_na_protected_file() {
    case "$1" in /*) ;; *) return 76;; esac
    _na_walk=$1
    while :; do
        [ ! -L "$_na_walk" ] || return 76
        _na_meta=$(stat -t "$_na_walk" 2>/dev/null) || return 76
        set -- $_na_meta
        [ "$#" -ge 9 ] && [ "$5" = 0 ] || return 76
        case "$4" in ''|*[!0-9a-fA-F]*) return 76;; esac
        [ "$((0x$4 & 0022))" -eq 0 ] || return 76
        if [ "$_na_walk" = "$_na_file" ]; then
            [ -f "$_na_walk" ] && [ "$9" = 1 ] || return 76
        else
            [ -d "$_na_walk" ] || return 76
        fi
        [ "$_na_walk" != / ] || break
        _na_walk=${_na_walk%/*}
        [ -n "$_na_walk" ] || _na_walk=/
    done
}
_na_file_ok() { _na_file=$1; _na_protected_file "$1"; }
_na_small_file() {
    [ ! -L "$1" ] && [ -f "$1" ] || return 77
    _native_gate_metadata "$1" || return 77
    [ "$_ng_meta_mode" = 8180 ] && [ "$_ng_meta_uid" = 0 ] &&
        [ "$_ng_meta_links" = 1 ] && [ "$_ng_meta_size" -le 200 ]
}
_na_gate_ok() {
    [ "${XKEEN_GATE_ROOT-}" = /tmp/.xkeen-admission ] || return 77
    native_gate_join /tmp/.xkeen-admission "${XKEEN_GATE_TOKEN-}" || return $?
    if [ -n "${_na_gate_identity-}" ]; then
        [ "$_na_gate_identity" = "$_ng_record" ] || return 77
    else
        _na_gate_identity=$_ng_record
    fi
    case "$_ng_action:$_na_action" in
        config-change:start|config-change:stop|config-change:restart|start:start|stop:stop|restart:restart) ;;
        *) return 77;;
    esac
    [ ! -e /tmp/.xkeen-admission/operation.lock.d/unresolved ] &&
        [ ! -L /tmp/.xkeen-admission/operation.lock.d/unresolved ] || return 77
}
_na_poison() {
    # Do not write to a replacement owner's gate after loss of identity.
    native_gate_join /tmp/.xkeen-admission "${XKEEN_GATE_TOKEN-}" || return 77
    [ -n "${_na_gate_identity-}" ] && [ "$_na_gate_identity" = "$_ng_record" ] || return 77
    (umask 077; mkdir /tmp/.xkeen-admission/operation.lock.d/unresolved) 2>/dev/null
    return 77
}
_na_read_record() {
    _native_gate_directory "$_na_call_dir" 0700 || return 77
    _na_small_file "$_na_call_dir/context" || return 77
    IFS= read -r _na_record < "$_na_call_dir/context" || return 77
    [ "$_ng_meta_size" -eq "$(( ${#_na_record} + 1 ))" ] || return 77
    set -- $_na_record
    [ "$#" = 8 ] && [ "$1" = v1 ] && [ "$5" = "$_na_role" ] &&
        [ "$6" = "$_na_action" ] && [ "$7" = "$_na_mode" ] && [ "$8" = "$XKEEN_GATE_TOKEN" ] || return 77
    _native_gate_decimal "$2" && _native_gate_decimal "$3" || return 77
    [ "${#4}" = 32 ] || return 77
    case "$4" in *[!0-9a-f]*) return 77;; esac
    _na_wrapper_pid=$2; _na_wrapper_start=$3; _na_nonce=$4
}
_na_child_ok() {
    _na_gate_ok || return $?
    _na_read_record || return 77
    [ "$_na_nonce" = "${XKEEN_ADMISSION_CALL-}" ] || return 77
    _native_gate_self || return 77
    _native_gate_proc "$_ng_self_pid" || return 77
    [ "$_ng_parent" = "$_na_wrapper_pid" ] || return 77
    _native_gate_proc "$_na_wrapper_pid" || return 77
    [ "$_ng_proc_start" = "$_na_wrapper_start" ] || return 77
}

native_admission_enter() {
    # A private marker is only a routing hint. Gate tuple + live immediate
    # wrapper ancestry + nonce in protected RAM authorize the body.
    _na_body=0
    [ "$#" = 3 ] || return 76
    case "$1:$2:$3" in
        dispatcher:start:forced|dispatcher:stop:forced|dispatcher:restart:forced|init:start:forced|init:stop:forced|init:restart:forced|init:start:automatic|init:restart:automatic) ;;
        *) return 76;;
    esac
    PATH=/opt/bin:/opt/sbin:/usr/bin:/bin; export PATH
    [ "$(id -u)" = 0 ] || return 76
    _na_file_ok /opt/lib/xkeen/native-operation-gate.sh || return 76
    . /opt/lib/xkeen/native-operation-gate.sh
    _na_gate_identity=
    _na_role=$1; _na_action=$2; _na_mode=$3
    _na_call_dir=/tmp/.xkeen-admission/operation.lock.d/call.$_na_role
    case "${XKEEN_ADMISSION_ROLE-}" in
        '')
            [ -z "${XKEEN_ADMISSION_ACTION-}${XKEEN_ADMISSION_CALL-}" ] || return 77
            ;;
        "$_na_role")
            [ "${XKEEN_ADMISSION_ACTION-}" = "$_na_action" ] || return 77
            _na_child_ok || return $?
            _na_body=1
            return 0
            ;;
        dispatcher)
            # Only the fixed dispatcher -> init nesting is supported here.
            [ "$_na_role" = init ] && [ "$_na_mode" = forced ] && [ "${XKEEN_ADMISSION_ACTION-}" = "$_na_action" ] || return 77
            _na_gate_ok || return $?
            ;;
        *) return 77;;
    esac
    _na_owns=0
    if [ -n "${XKEEN_GATE_ROOT-}${XKEEN_GATE_TOKEN-}" ]; then
        _na_gate_ok || return $?
    else
        [ -z "${XKEEN_ADMISSION_ROLE-}" ] || return 77
        native_gate_acquire /tmp/.xkeen-admission "$_na_action" || return $?
        _na_gate_identity=$_ng_owned_record
        _na_owns=1
    fi
    # At most two foreground invocation records: dispatcher and init. No
    # unbounded per-event records, detached waiter or persistent operation journal.
    (umask 077; mkdir "$_na_call_dir") 2>/dev/null || { _na_poison; return 77; }
    _native_gate_self || { _na_poison; return 77; }
    _na_nonce=$(dd if=/dev/urandom bs=16 count=1 2>/dev/null | od -v -b 2>/dev/null |
        awk 'NF > 1 { for (i = 2; i <= NF; i++) {
            count++
            if ($i !~ /^[0-3][0-7][0-7]$/) { bad = 1; continue }
            byte = substr($i, 1, 1) * 64 + substr($i, 2, 1) * 8 + substr($i, 3, 1)
            token = token sprintf("%02x", byte)
        }} END { if (count == 16 && !bad) printf "%s", token }')
    [ "${#_na_nonce}" = 32 ] || { _na_poison; return 77; }
    case "$_na_nonce" in *[!0-9a-f]*) _na_poison; return 77;; esac
    _na_record="v1 $_ng_self_pid $_ng_self_start $_na_nonce $_na_role $_na_action $_na_mode $XKEEN_GATE_TOKEN"
    (umask 077; set -C; printf '%s\n' "$_na_record" > "$_na_call_dir/context") || { _na_poison; return 77; }
    XKEEN_ADMISSION_ROLE=$_na_role; XKEEN_ADMISSION_ACTION=$_na_action; XKEEN_ADMISSION_CALL=$_na_nonce
    export XKEEN_ADMISSION_ROLE XKEEN_ADMISSION_ACTION XKEEN_ADMISSION_CALL
    # Configuration validation, retained intent refusal and native verification
    # capability checks precede ALL native body effects.
    _na_file_ok /opt/lib/xkeen/native-admission-verify.sh || { _na_poison; return 76; }
    /opt/bin/sh /opt/lib/xkeen/native-admission-verify.sh pre "$_na_role" "$_na_action" "$_na_mode" || { _na_poison; return 77; }
    # No caller-supplied callback, executable or arbitrary command arguments.
    case "$_na_role" in
        dispatcher)
            _na_file_ok /opt/sbin/xkeen || { _na_poison; return 76; }
            /opt/bin/sh /opt/sbin/xkeen "-$_na_action"
            _na_rc=$?
            ;;
        init)
            _na_file_ok /opt/etc/init.d/S05xkeen || { _na_poison; return 76; }
            if [ "$_na_mode" = automatic ]; then
                /opt/bin/sh /opt/etc/init.d/S05xkeen "$_na_action"
            else
                /opt/bin/sh /opt/etc/init.d/S05xkeen "$_na_action" on
            fi
            _na_rc=$?
            ;;
    esac
    [ "$_na_rc" = 0 ] || { _na_poison; return 77; }
    _na_gate_ok || return 77
    _na_small_file "$_na_call_dir/complete" || { _na_poison; return 77; }
    IFS= read -r _na_complete < "$_na_call_dir/complete" || { _na_poison; return 77; }
    [ "$_na_complete" = "$_na_record" ] &&
        [ "$_ng_meta_size" -eq "$(( ${#_na_complete} + 1 ))" ] || { _na_poison; return 77; }
    # Must be native-owned verification; no panel process/API/CLI dependency.
    # Missing implementation is intentionally an unresolved outcome.
    _na_file_ok /opt/lib/xkeen/native-admission-verify.sh || { _na_poison; return 76; }
    /opt/bin/sh /opt/lib/xkeen/native-admission-verify.sh post "$_na_role" "$_na_action" "$_na_mode" || { _na_poison; return 77; }
    _na_gate_ok || return 77
    rm "$_na_call_dir/complete" "$_na_call_dir/context" && rmdir "$_na_call_dir" || { _na_poison; return 77; }
    unset XKEEN_ADMISSION_ROLE XKEEN_ADMISSION_ACTION XKEEN_ADMISSION_CALL
    [ "$_na_owns" = 0 ] || native_gate_release || return 77
    _na_body=0
    return 0
}

native_admission_finish() {
    [ "$#" = 1 ] && [ "$1" = 0 ] && [ "${_na_body-}" = 1 ] || { _na_poison; return 77; }
    _na_child_ok || return 77
    # Exclusive completion is written only at the reviewed end of the native
    # body. Normal exit, signals and native EXIT traps cannot create this proof.
    (umask 077; set -C; printf '%s\n' "$_na_record" > "$_na_call_dir/complete") || { _na_poison; return 77; }
    _na_body=0
    return 0
}
native_admission_strip() {
    unset XKEEN_GATE_ROOT XKEEN_GATE_TOKEN XKEEN_ADMISSION_ROLE XKEEN_ADMISSION_ACTION XKEEN_ADMISSION_CALL
    unset _na_body _na_role _na_action _na_mode _na_nonce _na_record _na_call_dir _na_owns _na_gate_identity _ng_owned_record _ng_owned_root
}
