#!/bin/sh
# Source-only RAM notification/election protocol. No executor or retry daemon.
# A leader is one existing NDM invocation, distinct from operation admission.
# 75 = queued/more work; 76 = unsafe; 77 = unresolved. Never reap a leader.

_ne_prepare() {
    _ne_root=/tmp/.xkeen-admission/events
    _native_gate_root /tmp/.xkeen-admission || return 76
    if [ ! -e "$_ne_root" ] && [ ! -L "$_ne_root" ]; then
        (umask 077; mkdir "$_ne_root") 2>/dev/null || :
    fi
    _native_gate_directory "$_ne_root" 0700 || return 76
}
_ne_read_leader() {
    _native_gate_directory "$_ne_root/leader" 0700 || return 77
    [ ! -L "$_ne_root/leader/owner" ] && [ -f "$_ne_root/leader/owner" ] || return 77
    _native_gate_metadata "$_ne_root/leader/owner" || return 77
    [ "$_ng_meta_mode:$_ng_meta_uid:$_ng_meta_links" = 8180:0:1 ] && [ "$_ng_meta_size" -le 160 ] || return 77
    _ne_size=$_ng_meta_size
    IFS= read -r _ne_record < "$_ne_root/leader/owner" || return 77
    set -- $_ne_record
    [ "$#" = 4 ] && [ "$1" = v1 ] || return 77
    _native_gate_uuid "$2" && _native_gate_decimal "$3" && _native_gate_decimal "$4" || return 77
    [ "$_ne_record" = "$1 $2 $3 $4" ] && [ "$_ne_size" = "$(( ${#_ne_record} + 1 ))" ] || return 77
    _ne_pid=$3; _ne_start=$4; _ne_boot=$2
    IFS= read -r _ne_current_boot < /proc/sys/kernel/random/boot_id || return 77
    [ "$_ne_boot" = "$_ne_current_boot" ] || return 77
    _native_gate_proc "$_ne_pid" || return 77
    [ "$_ng_proc_start" = "$_ne_start" ] || return 77
}
_ne_owns() {
    _ne_prepare && _ne_read_leader || return 77
    [ -n "${_ne_owned-}" ] && [ "$_ne_record" = "$_ne_owned" ] || return 77
    _native_gate_self || return 77
    [ "$_ne_pid:$_ne_start" = "$_ng_self_pid:$_ng_self_start" ] || return 77
}
native_event_notify() {
    [ "$#" = 0 ] || return 76
    _ne_prepare || return $?
    # Publish before election: a retiring leader will observe the final event.
    (umask 077; mkdir "$_ne_root/dirty") 2>/dev/null || :
    _native_gate_directory "$_ne_root/dirty" 0700 || return 76
    if ! (umask 077; mkdir "$_ne_root/leader") 2>/dev/null; then
        _ne_read_leader || return 77
        return 75
    fi
    _native_gate_directory "$_ne_root/leader" 0700 || return 77
    _native_gate_self || return 77
    IFS= read -r _ne_current_boot < /proc/sys/kernel/random/boot_id || return 77
    _native_gate_uuid "$_ne_current_boot" || return 77
    _ne_owned="v1 $_ne_current_boot $_ng_self_pid $_ng_self_start"
    (umask 077; set -C; printf '%s\n' "$_ne_owned" > "$_ne_root/leader/owner") || return 77
    _ne_owns
}
native_event_consume() {
    [ "$#" = 0 ] || return 76
    _ne_owns || return 77
    if [ ! -e "$_ne_root/dirty" ] && [ ! -L "$_ne_root/dirty" ]; then return 1; fi
    _native_gate_directory "$_ne_root/dirty" 0700 || return 77
    # The caller consumes immediately before its fixed current-state reconcile.
    # Later notifications recreate the directory and require another pass.
    rmdir "$_ne_root/dirty" || return 77
}
native_event_retire() {
    [ "$#" = 0 ] || return 76
    _ne_owns || return 77
    if [ -e "$_ne_root/dirty" ] || [ -L "$_ne_root/dirty" ]; then
        _native_gate_directory "$_ne_root/dirty" 0700 || return 77
        return 75
    fi
    rm "$_ne_root/leader/owner" && rmdir "$_ne_root/leader" || return 77
    _ne_owned=
    # Close the notify-before-retirement race. A new invocation may already be
    # elected: re-election uses notify, never deletes that invocation's record.
    if [ -e "$_ne_root/dirty" ] || [ -L "$_ne_root/dirty" ]; then
        _native_gate_directory "$_ne_root/dirty" 0700 || return 77
        return 75
    fi
    return 0
}
