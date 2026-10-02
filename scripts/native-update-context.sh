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
