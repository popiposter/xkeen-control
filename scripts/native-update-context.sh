#!/bin/sh
# Source-only update body binding and fixed staged-child proof. No executor here.
# The future updater must load the protected gate and entry libraries first.
# This does not grant native body/finish authority or settle an operation.
_nu_read_record() {
    _na_small_file "$1" || return 77
    IFS= read -r _nu_record < "$1" || return 77
    [ "$_ng_meta_size" = "$(( ${#_nu_record} + 1 ))" ] || return 77
}
_nu_load_call() {
    [ "$(id -u)" = 0 ] || return 76
    [ "${XKEEN_GATE_ROOT-}" = /tmp/.xkeen-admission ] || return 77
    [ "${XKEEN_ADMISSION_ROLE-}:${XKEEN_ADMISSION_ACTION-}" = update:update-xkeen ] || return 77
    native_gate_join /tmp/.xkeen-admission "${XKEEN_GATE_TOKEN-}" || return 77
    [ "$_ng_action" = update-xkeen ] || return 77
    _nu_gate_record=$_ng_record
    _nu_call=/tmp/.xkeen-admission/operation.lock.d/call.update
    _native_gate_directory "$_nu_call" 0700 || return 77
    _nu_read_record "$_nu_call/context" || return 77
    _nu_context=$_nu_record
    set -- $_nu_context
    [ "$#" = 8 ] && [ "$1" = v1 ] || return 77
    _native_gate_decimal "$2" && _native_gate_decimal "$3" || return 77
    _nu_wrapper_pid=$2; _nu_wrapper_start=$3
    _nu_nonce=${XKEEN_ADMISSION_CALL-}
    [ "${#_nu_nonce}" = 32 ] || return 77
    case "$_nu_nonce" in *[!0-9a-f]*) return 77;; esac
    [ "$_nu_context" = "v1 $_nu_wrapper_pid $_nu_wrapper_start $_nu_nonce update update-xkeen forced $XKEEN_GATE_TOKEN" ] || return 77
}
_nu_recheck_call() {
    native_gate_join /tmp/.xkeen-admission "$XKEEN_GATE_TOKEN" || return 77
    [ "$_ng_record" = "$_nu_gate_record" ] || return 77
    _nu_read_record "$_nu_call/context" || return 77
    [ "$_nu_record" = "$_nu_context" ] || return 77
}
native_update_verifier_context() {
    # Read-only verifier invocation, not body/stage/finish authority. Preflight
    # precedes body binding; postflight runs after that body has exited, so its
    # live parent is the wrapper. Never authenticate by requiring a live body.
    [ "$#" = 1 ] || return 76
    case "$1" in pre|post) ;; *) return 76;; esac
    _nu_verify_phase=$1
    _nu_load_call || return $?
    _nu_verify_gate=$_nu_gate_record; _nu_verify_context=$_nu_context
    _native_gate_self || return 77
    _nu_verify_pid=$_ng_self_pid; _nu_verify_start=$_ng_self_start
    _native_gate_proc "$_nu_verify_pid" || return 77
    [ "$_ng_parent" = "$_nu_wrapper_pid" ] || return 77
    _native_gate_proc "$_nu_wrapper_pid" || return 77
    [ "$_ng_proc_start" = "$_nu_wrapper_start" ] || return 77
    if [ "$_nu_verify_phase" = pre ]; then
        for _nu_verify_name in body staged exec.used exec.argv completed; do
            [ ! -e "$_nu_call/$_nu_verify_name" ] && [ ! -L "$_nu_call/$_nu_verify_name" ] || return 77
        done
    else
        _nu_read_record "$_nu_call/body" || return 77
        _nu_verify_body=$_nu_record
        set -- $_nu_verify_body
        [ "$#" = 4 ] && [ "$1" = v1 ] && [ "$4" = "$_nu_nonce" ] || return 77
        _native_gate_decimal "$2" && _native_gate_decimal "$3" || return 77
        _nu_verify_body_pid=$2; _nu_verify_body_start=$3
        [ "$_nu_verify_body" = "v1 $_nu_verify_body_pid $_nu_verify_body_start $_nu_nonce" ] || return 77
        _nu_read_record "$_nu_call/staged" || return 77
        _nu_verify_staged=$_nu_record
        _nu_dispatcher_hash || return $?
        _nu_verify_dispatcher=$_nu_hash
        [ "$_nu_verify_staged" = "$_nu_verify_body $_nu_verify_dispatcher" ] || return 77
        _native_gate_directory "$_nu_call/exec.used" 0700 || return 77
        _na_small_file "$_nu_call/exec.argv" || return 77
        _nu_verify_argv=$(sha256sum "$_nu_call/exec.argv" 2>/dev/null) || return 77
        [ "${_nu_verify_argv%% *}" = f7549ee949d9d02fa5ba2e6586f286394c31c2243d32d7ed1b520aa37d5439fe ] || return 77
        # The future fixed native body completion producer must publish this
        # only after its reviewed successful terminal, before returning. Stage
        # and exec consumption alone never establish updater completion.
        _nu_read_record "$_nu_call/completed" || return 77
        _nu_verify_completed=$_nu_record
        [ "$_nu_verify_completed" = "$_nu_verify_body updated $_nu_verify_dispatcher" ] || return 77
    fi
    _nu_recheck_call || return 77
    [ "$_nu_gate_record" = "$_nu_verify_gate" ] && [ "$_nu_context" = "$_nu_verify_context" ] || return 77
    if [ "$_nu_verify_phase" = post ]; then
        _nu_read_record "$_nu_call/body" || return 77
        [ "$_nu_record" = "$_nu_verify_body" ] || return 77
        _nu_read_record "$_nu_call/staged" || return 77
        [ "$_nu_record" = "$_nu_verify_staged" ] || return 77
        _nu_read_record "$_nu_call/completed" || return 77
        [ "$_nu_record" = "$_nu_verify_completed" ] || return 77
        _native_gate_directory "$_nu_call/exec.used" 0700 || return 77
        _na_small_file "$_nu_call/exec.argv" || return 77
        _nu_verify_argv_final=$(sha256sum "$_nu_call/exec.argv" 2>/dev/null) || return 77
        [ "$_nu_verify_argv_final" = "$_nu_verify_argv" ] || return 77
        _nu_dispatcher_hash || return 77
        [ "$_nu_hash" = "$_nu_verify_dispatcher" ] || return 77
    fi
    _native_gate_proc "$_nu_verify_pid" || return 77
    [ "$_ng_proc_start:$_ng_parent" = "$_nu_verify_start:$_nu_wrapper_pid" ] || return 77
    _native_gate_proc "$_nu_wrapper_pid" || return 77
    [ "$_ng_proc_start" = "$_nu_wrapper_start" ] || return 77
    if [ "$_nu_verify_phase" = pre ]; then
        for _nu_verify_name in body staged exec.used exec.argv completed; do
            [ ! -e "$_nu_call/$_nu_verify_name" ] && [ ! -L "$_nu_call/$_nu_verify_name" ] || return 77
        done
    fi
    return 0
}
native_update_bind_body() {
    # One exclusive RAM binding in the existing call scope, before body effects.
    # Obtain the actual process identity through /proc, never inherited shell $$.
    [ "$#" = 0 ] || return 76
    _nu_load_call || return $?
    _native_gate_self || return 77
    _nu_binding="v1 $_ng_self_pid $_ng_self_start $_nu_nonce"
    _native_gate_proc "$_ng_self_pid" || return 77
    [ "$_ng_parent" = "$_nu_wrapper_pid" ] || return 77
    _native_gate_proc "$_nu_wrapper_pid" || return 77
    [ "$_ng_proc_start" = "$_nu_wrapper_start" ] || return 77
    [ ! -e "$_nu_call/body" ] && [ ! -L "$_nu_call/body" ] || return 77
    (umask 077; set -C; printf '%s\n' "$_nu_binding" > "$_nu_call/body") 2>/dev/null || return 77
    _nu_recheck_call || return 77
    _nu_read_record "$_nu_call/body" || return 77
    [ "$_nu_record" = "$_nu_binding" ] || return 77
}
native_update_stage_context() {
    [ "$#" = 1 ] || return 76
    _nu_stage_request=$1
    _nu_load_call || return $?
    _nu_read_record "$_nu_call/body" || return 77
    _nu_body=$_nu_record
    set -- $_nu_body
    [ "$#" = 4 ] && [ "$1" = v1 ] && [ "$4" = "$_nu_nonce" ] || return 77
    _native_gate_decimal "$2" && _native_gate_decimal "$3" || return 77
    [ "$_nu_body" = "v1 $2 $3 $4" ] || return 77
    _nu_body_pid=$2; _nu_body_start=$3
    _native_gate_self || return 77
    _native_gate_proc "$_ng_self_pid" || return 77
    [ "$_ng_parent" = "$_nu_body_pid" ] || return 77
    _native_gate_proc "$_nu_body_pid" || return 77
    [ "$_ng_proc_start:$_ng_parent" = "$_nu_body_start:$_nu_wrapper_pid" ] || return 77
    _native_gate_proc "$_nu_wrapper_pid" || return 77
    [ "$_ng_proc_start" = "$_nu_wrapper_start" ] || return 77
    # Save the request before parsing record words above; never accept arbitrary
    # stage paths through environment hints or read-only descendant privilege.
    [ "$_nu_stage_request" = "/opt/sbin/.xkeen.stage.$_nu_body_pid" ] || return 76
    _nu_recheck_call || return 77
    _nu_read_record "$_nu_call/body" || return 77
    [ "$_nu_record" = "$_nu_body" ] || return 77
}
_nu_file_hash() {
    _na_file_ok "$1" || return 76
    _native_gate_metadata "$1" || return 76
    [ "$_ng_meta_size" -gt 0 ] && [ "$_ng_meta_size" -le 524288 ] || return 76
    _nu_hash_line=$(sha256sum "$1" 2>/dev/null) || return 76
    _nu_hash=${_nu_hash_line%% *}
    [ "${#_nu_hash}" = 64 ] || return 76
    case "$_nu_hash" in *[!0-9a-f]*) return 76;; esac
}
_nu_dispatcher_hash() { _nu_file_hash /opt/sbin/xkeen; }
native_update_bind_staged() {
    # Publish only the actual fixed stage child's bounded prepared dispatcher.
    # The caller must first validate/decorate the complete supported profile;
    # this receipt alone does not prove module preservation or grant live moves.
    [ "$#" = 1 ] || return 76
    _nu_publish_stage=$1
    native_update_stage_context "$_nu_publish_stage" || return $?
    _nu_publish_gate=$_nu_gate_record
    _nu_publish_context=$_nu_context
    _nu_publish_body=$_nu_body
    _nu_file_hash "$_nu_publish_stage/xkeen" || return $?
    _nu_stage_hash=$_nu_hash
    _nu_stage_receipt="v1 $_nu_body_pid $_nu_body_start $_nu_nonce $_nu_stage_hash"
    [ ! -e "$_nu_call/staged" ] && [ ! -L "$_nu_call/staged" ] || return 77
    (umask 077; set -C; printf '%s\n' "$_nu_stage_receipt" > "$_nu_call/staged") 2>/dev/null || return 77
    # Keep the receipt and admission on publication/readback failure. Never
    # replace a prior receipt or repair a failed generation in place.
    native_update_stage_context "$_nu_publish_stage" || return 77
    [ "$_nu_gate_record" = "$_nu_publish_gate" ] &&
        [ "$_nu_context" = "$_nu_publish_context" ] &&
        [ "$_nu_body" = "$_nu_publish_body" ] || return 77
    _nu_read_record "$_nu_call/staged" || return 77
    [ "$_nu_record" = "$_nu_stage_receipt" ] || return 77
    _nu_file_hash "$_nu_publish_stage/xkeen" || return 77
    [ "$_nu_hash" = "$_nu_stage_hash" ] || return 77
}
native_update_exec_context() {
    # Only the same native body may enter the installed post-update generation.
    # The future authenticated stage writer publishes staged before live moves.
    [ "$#" = 0 ] || return 76
    _nu_load_call || return $?
    _nu_read_record "$_nu_call/body" || return 77
    _nu_body=$_nu_record
    _native_gate_self || return 77
    _nu_exec_pid=$_ng_self_pid; _nu_exec_start=$_ng_self_start
    [ "$_nu_body" = "v1 $_nu_exec_pid $_nu_exec_start $_nu_nonce" ] || return 77
    _native_gate_proc "$_nu_exec_pid" || return 77
    [ "$_ng_parent" = "$_nu_wrapper_pid" ] || return 77
    _native_gate_proc "$_nu_wrapper_pid" || return 77
    [ "$_ng_proc_start" = "$_nu_wrapper_start" ] || return 77
    _nu_read_record "$_nu_call/staged" || return 77
    _nu_staged=$_nu_record
    _nu_dispatcher_hash || return $?
    _nu_expected_hash=$_nu_hash
    [ "$_nu_staged" = "v1 $_nu_exec_pid $_nu_exec_start $_nu_nonce $_nu_expected_hash" ] || return 77
    # PID preservation alone is not exec proof: the old shell could source this
    # helper. Require the exact supported argv vector, bounded before hashing.
    [ ! -e "$_nu_call/exec.argv" ] && [ ! -L "$_nu_call/exec.argv" ] || return 77
    (umask 077; set -C; dd if="/proc/$_nu_exec_pid/cmdline" bs=128 count=1 > "$_nu_call/exec.argv" 2>/dev/null) || return 77
    _na_small_file "$_nu_call/exec.argv" || return 77
    _nu_argv_line=$(sha256sum "$_nu_call/exec.argv" 2>/dev/null) || return 77
    # /opt/bin/sh NUL /opt/sbin/xkeen NUL -uk_post_update NUL
    [ "${_nu_argv_line%% *}" = f7549ee949d9d02fa5ba2e6586f286394c31c2243d32d7ed1b520aa37d5439fe ] || return 77
    # Consume this phase exclusively, retaining evidence on any later failure.
    (umask 077; mkdir "$_nu_call/exec.used") 2>/dev/null || return 77
    _native_gate_directory "$_nu_call/exec.used" 0700 || return 77
    _nu_recheck_call || return 77
    _nu_read_record "$_nu_call/body" || return 77
    [ "$_nu_record" = "$_nu_body" ] || return 77
    _nu_read_record "$_nu_call/staged" || return 77
    [ "$_nu_record" = "$_nu_staged" ] || return 77
    _nu_dispatcher_hash || return $?
    [ "$_nu_hash" = "$_nu_expected_hash" ] || return 77
}
