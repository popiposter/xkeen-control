#!/bin/sh
# Sourced POSIX-shell protocol library. No executor, default path or cleanup trap.
# 75 = busy/unresolved; 76 = unsafe/unavailable; 77 = ownership unproven.
# Only foreground, serially joined descendants may borrow a token. Call strip
# before starting background services. _ng_* variables are library-private.

_native_gate_action() {
    case "$1" in start|stop|restart|config-change) return 0;; *) return 1;; esac
}
_native_gate_decimal() {
    case "$1" in ''|0*|*[!0-9]*) return 1;; *) return 0;; esac
}
_native_gate_uuid() {
    [ "${#1}" = 36 ] || return 1
    case "$1" in ????????-????-????-????-????????????) ;; *) return 1;; esac
    _ng_uuid_hex=$(printf '%s' "$1" | tr -d '-')
    [ "${#_ng_uuid_hex}" = 32 ] || return 1
    case "$_ng_uuid_hex" in *[!0-9a-f]*) return 1;; esac
}
_native_gate_metadata() {
    # GNU and the qualified appliance BusyBox stat -t share these first nine
    # fields. Gate paths cannot contain whitespace. Do not require stat -c.
    _ng_meta=$(stat -t "$1" 2>/dev/null) || return 1
    _ng_meta_ifs=$IFS
    IFS=' '
    set -- $_ng_meta
    IFS=$_ng_meta_ifs
    [ "$#" -ge 9 ] || return 1
    _ng_meta_size=$2
    _ng_meta_mode=$4
    _ng_meta_uid=$5
    _ng_meta_links=$9
    case "$_ng_meta_size:$_ng_meta_uid:$_ng_meta_links" in *[!0-9:]*) return 1;; esac
    case "$_ng_meta_mode" in ''|*[!0-9a-fA-F]*) return 1;; esac
}
_native_gate_directory() {
    [ ! -L "$1" ] && [ -d "$1" ] || return 1
    _native_gate_metadata "$1" || return 1
    [ "$_ng_meta_uid" = 0 ] && [ "$((0x$_ng_meta_mode))" -eq "$((040000 + $2))" ]
}
_native_gate_root() {
    [ "$(id -u)" = 0 ] || return 76
    case "$1" in /tmp/?*) ;; *) return 76;; esac
    case "$1/" in *[!a-zA-Z0-9_./-]*|*//*|*/./*|*/../*) return 76;; esac
    _native_gate_directory / 0755 && _native_gate_directory /tmp 01777 || return 76
    _ng_walk=/tmp
    _ng_tail=${1#/tmp/}
    while [ -n "$_ng_tail" ]; do
        _ng_part=${_ng_tail%%/*}
        if [ "$_ng_part" = "$_ng_tail" ]; then _ng_tail=; else _ng_tail=${_ng_tail#*/}; fi
        _ng_walk=$_ng_walk/$_ng_part
        _native_gate_directory "$_ng_walk" 0700 || return 76
    done
}
_native_gate_proc() {
    IFS= read -r _ng_stat < "/proc/$1/stat" || return 77
    _ng_rest=${_ng_stat##*) }
    [ "$_ng_rest" != "$_ng_stat" ] || return 77
    # /proc stat has no whitespace in fields after the closing comm delimiter.
    _ng_saved_ifs=$IFS
    IFS=' '
    set -- $_ng_rest
    IFS=$_ng_saved_ifs
    [ "$#" -ge 20 ] || return 77
    case "$1" in Z|X) return 77;; esac
    _ng_parent=$2
    shift 19
    _ng_proc_start=$1
    _native_gate_decimal "$_ng_proc_start" || return 77
    case "$_ng_parent" in ''|*[!0-9]*) return 77;; esac
}
_native_gate_self() {
    # A builtin opens /proc/self, so this is the actual shell PID, even inside
    # a subshell where POSIX $$ still names the original parent shell.
    IFS= read -r _ng_self_stat < /proc/self/stat || return 77
    _ng_self_pid=${_ng_self_stat%% *}
    _native_gate_decimal "$_ng_self_pid" || return 77
    _native_gate_proc "$_ng_self_pid" || return 77
    _ng_self_start=$_ng_proc_start
}
_native_gate_read_owner() {
    _ng_lock=$1/operation.lock.d
    _native_gate_directory "$_ng_lock" 0700 || return 77
    [ ! -L "$_ng_lock/owner" ] && [ -f "$_ng_lock/owner" ] || return 77
    _native_gate_metadata "$_ng_lock/owner" || return 77
    [ "$_ng_meta_uid" = 0 ] && [ "$_ng_meta_links" = 1 ] && [ "$((0x$_ng_meta_mode))" -eq "$((0100600))" ] || return 77
    _ng_size=$_ng_meta_size
    [ "$_ng_size" -le 200 ] || return 77
    IFS= read -r _ng_record < "$_ng_lock/owner" || return 77
    IFS=' ' read -r _ng_version _ng_boot _ng_pid _ng_start _ng_token _ng_action _ng_extra < "$_ng_lock/owner" || return 77
    [ "$_ng_version" = v1 ] && [ -z "$_ng_extra" ] || return 77
    _native_gate_uuid "$_ng_boot" && _native_gate_decimal "$_ng_pid" && _native_gate_decimal "$_ng_start" || return 77
    [ "${#_ng_token}" = 32 ] || return 77
    case "$_ng_token" in *[!0-9a-f]*) return 77;; esac
    _native_gate_action "$_ng_action" || return 77
    [ "$_ng_record" = "v1 $_ng_boot $_ng_pid $_ng_start $_ng_token $_ng_action" ] || return 77
    [ "$_ng_size" -eq "$(( ${#_ng_record} + 1 ))" ] || return 77
}

native_gate_acquire() {
    [ "$#" = 2 ] || return 76
    _native_gate_action "$2" && _native_gate_root "$1" || return 76
    _native_gate_self || return 76
    IFS= read -r _ng_current_boot < /proc/sys/kernel/random/boot_id || return 76
    _native_gate_uuid "$_ng_current_boot" || return 76
    _ng_new_token=$(od -An -N16 -tx1 /dev/urandom 2>/dev/null | tr -d ' \n')
    [ "${#_ng_new_token}" = 32 ] || return 76
    case "$_ng_new_token" in *[!0-9a-f]*) return 76;; esac
    _ng_new_record="v1 $_ng_current_boot $_ng_self_pid $_ng_self_start $_ng_new_token $2"
    # umask affects only this subshell; the gate owner is the caller above.
    (umask 077; mkdir "$1/operation.lock.d") 2>/dev/null || {
        [ -e "$1/operation.lock.d" ] || [ -L "$1/operation.lock.d" ] || return 76
        return 75
    }
    _native_gate_directory "$1/operation.lock.d" 0700 || return 76
    # Never remove partial creation on failure: a later caller sees busy.
    (umask 077; set -C; printf '%s\n' "$_ng_new_record" > "$1/operation.lock.d/owner.tmp") || return 76
    mv "$1/operation.lock.d/owner.tmp" "$1/operation.lock.d/owner" || return 76
    _ng_owned_record=$_ng_new_record
    _ng_owned_root=$1
    XKEEN_GATE_ROOT=$1
    XKEEN_GATE_TOKEN=$_ng_new_token
    export XKEEN_GATE_ROOT XKEEN_GATE_TOKEN
}

native_gate_join() {
    [ "$#" = 2 ] || return 77
    _native_gate_root "$1" || return 76
    _native_gate_read_owner "$1" || return 77
    _ng_join_record=$_ng_record
    [ "$_ng_token" = "$2" ] || return 77
    IFS= read -r _ng_current_boot < /proc/sys/kernel/random/boot_id || return 77
    [ "$_ng_boot" = "$_ng_current_boot" ] || return 77
    _native_gate_proc "$_ng_pid" || return 77
    [ "$_ng_proc_start" = "$_ng_start" ] || return 77
    _native_gate_self || return 77
    _ng_cursor=$_ng_self_pid
    _ng_depth=0
    while [ "$_ng_cursor" -gt 0 ] && [ "$_ng_depth" -lt 256 ]; do
        _native_gate_proc "$_ng_cursor" || return 77
        if [ "$_ng_cursor" = "$_ng_pid" ]; then
            [ "$_ng_proc_start" = "$_ng_start" ] || return 77
            _native_gate_read_owner "$1" || return 77
            [ "$_ng_record" = "$_ng_join_record" ] || return 77
            XKEEN_GATE_ROOT=$1
            XKEEN_GATE_TOKEN=$2
            export XKEEN_GATE_ROOT XKEEN_GATE_TOKEN
            return 0
        fi
        [ "$_ng_cursor" != "$_ng_parent" ] || return 77
        _ng_cursor=$_ng_parent
        _ng_depth=$((_ng_depth+1))
    done
    return 77
}

native_gate_release() {
    [ -n "${_ng_owned_record-}" ] && [ -n "${_ng_owned_root-}" ] || return 77
    _native_gate_root "$_ng_owned_root" || return 76
    _native_gate_read_owner "$_ng_owned_root" || return 77
    [ "$_ng_record" = "$_ng_owned_record" ] || return 77
    _native_gate_self || return 77
    IFS= read -r _ng_current_boot < /proc/sys/kernel/random/boot_id || return 77
    [ "$_ng_boot" = "$_ng_current_boot" ] && [ "$_ng_pid" = "$_ng_self_pid" ] && [ "$_ng_start" = "$_ng_self_start" ] || return 77
    # Unknown entries are not ours to delete. Keep the owner record intact.
    for _ng_entry in "$_ng_lock"/* "$_ng_lock"/.[!.]* "$_ng_lock"/..?*; do
        [ -e "$_ng_entry" ] || [ -L "$_ng_entry" ] || continue
        [ "$_ng_entry" = "$_ng_lock/owner" ] || return 77
    done
    rm "$_ng_lock/owner" && rmdir "$_ng_lock" || return 77
    unset _ng_owned_record _ng_owned_root
    native_gate_strip
}

native_gate_strip() { unset XKEEN_GATE_ROOT XKEEN_GATE_TOKEN; }
