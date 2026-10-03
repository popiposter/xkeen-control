#!/bin/sh
# SOURCE-ONLY FENCE: complete native update integration is not enabled.
exit 76
# END SOURCE-ONLY FENCE
PATH=/opt/bin:/opt/sbin:/usr/bin:/bin; export PATH
umask 077
[ "$(id -u)" = 0 ] && [ "$#" = 1 ] || exit 76
case "$1" in pre|post) _np_phase=$1;; *) exit 76;; esac
# Bootstrap the protected library; never source a native dispatcher/init.
_np_boot=/opt/lib/xkeen/native-admission-entry.sh
while :; do
    [ ! -L "$_np_boot" ] || exit 76
    _np_meta=$(stat -t "$_np_boot" 2>/dev/null) || exit 76
    (
        set -- $_np_meta
        [ "$#" -ge 9 ] && [ "$5" = 0 ] || exit 76
        case "$4" in ''|*[!0-9a-fA-F]*) exit 76;; esac
        [ "$((0x$4 & 0022))" = 0 ] || exit 76
        if [ "$_np_boot" = /opt/lib/xkeen/native-admission-entry.sh ]; then
            [ -f "$_np_boot" ] && [ "$9" = 1 ] || exit 76
        else [ -d "$_np_boot" ] || exit 76; fi
    ) || exit 76
    [ "$_np_boot" != / ] || break
    _np_boot=${_np_boot%/*}; [ -n "$_np_boot" ] || _np_boot=/
done
. /opt/lib/xkeen/native-admission-entry.sh
_na_file_ok /opt/lib/xkeen/native-operation-gate.sh || exit 76
. /opt/lib/xkeen/native-operation-gate.sh
_na_file_ok /opt/lib/xkeen/native-update-context.sh || exit 76
. /opt/lib/xkeen/native-update-context.sh

# BEGIN INSTALLED PROFILE FUNCTIONS
_np_file() {
    _nu_file_hash "$1" || return 76
    [ "$_nu_hash" = "$2" ] && [ "$_ng_meta_size" = "$3" ] || return 76
}
_np_shape() {
    return 76 # COMPILE INSTALLED PROFILE SHAPE
}
_np_files() {
    return 76 # COMPILE INSTALLED PROFILE INVENTORY
}
_np_list() {
    _np_list_path=/opt/lib/opkg/info/xkeen.list
    _nu_file_hash "$_np_list_path" || return 76
    [ "$_ng_meta_size" -le 32768 ] || return 76
    _np_list_hash=$_nu_hash
    _np_last=$(tail -c 1 "$_np_list_path" | od -v -b) || return 76
    set -- $_np_last
    [ "$#" = 3 ] && [ "$1:$2:$3" = 0000000:012:0000001 ] || return 76
    return 76 # COMPILE INSTALLED PACKAGE LIST
    _nu_file_hash "$_np_list_path" || return 76
    [ "$_nu_hash" = "$_np_list_hash" ] || return 76
}
_np_main() {
    native_update_observer_context "$_np_phase" || return $?
    _np_gate=$_nu_gate_record; _np_context=$_nu_context
    _np_shape && _np_files && _np_list || return 76
    native_update_observer_context "$_np_phase" || return 77
    [ "$_nu_gate_record" = "$_np_gate" ] && [ "$_nu_context" = "$_np_context" ] || return 77
    # Catch changes to early files/directories during the first inventory pass.
    # Re-authenticate again after the final bounded readback, never settle here.
    _np_shape && _np_files && _np_list || return 77
    native_update_observer_context "$_np_phase" || return 77
    [ "$_nu_gate_record" = "$_np_gate" ] && [ "$_nu_context" = "$_np_context" ] || return 77
}
# END INSTALLED PROFILE FUNCTIONS
_np_main
exit $?
