#!/bin/sh
# SOURCE-ONLY FENCE: complete native update integration is not enabled.
exit 76
# END SOURCE-ONLY FENCE
PATH=/opt/bin:/opt/sbin:/usr/bin:/bin; export PATH
[ "$(id -u)" = 0 ] || exit 76
# Bootstrap only the fixed protected entry library before using its helpers.
_ns_boot=/opt/lib/xkeen/native-admission-entry.sh
while :; do
    [ ! -L "$_ns_boot" ] || exit 76
    _ns_meta=$(stat -t "$_ns_boot" 2>/dev/null) || exit 76
    (
        set -- $_ns_meta
        [ "$#" -ge 9 ] && [ "$5" = 0 ] || exit 76
        case "$4" in ''|*[!0-9a-fA-F]*) exit 76;; esac
        [ "$((0x$4 & 0022))" = 0 ] || exit 76
        if [ "$_ns_boot" = /opt/lib/xkeen/native-admission-entry.sh ]; then
            [ -f "$_ns_boot" ] && [ "$9" = 1 ] || exit 76
        else [ -d "$_ns_boot" ] || exit 76; fi
    ) || exit 76
    [ "$_ns_boot" != / ] || break
    _ns_boot=${_ns_boot%/*}; [ -n "$_ns_boot" ] || _ns_boot=/
done
. /opt/lib/xkeen/native-admission-entry.sh
_na_file_ok /opt/lib/xkeen/native-operation-gate.sh || exit 76
. /opt/lib/xkeen/native-operation-gate.sh
_na_file_ok /opt/lib/xkeen/native-update-context.sh || exit 76
. /opt/lib/xkeen/native-update-context.sh

# BEGIN PROFILE STAGE FUNCTIONS
_ns_file() {
    _nu_file_hash "$1" || return 76
    [ "$_nu_hash" = "$2" ] && [ "$_ng_meta_size" = "$3" ] || return 76
}
_ns_shape() {
    return 76 # COMPILE PROFILE SHAPE
}
_ns_validate() {
    _ns_shape || return 76
    case "$1" in
        source) return 76 # COMPILE SOURCE INVENTORY
            ;;
        prepared) return 76 # COMPILE PREPARED INVENTORY
            ;;
        *) return 76;;
    esac
}
_ns_payloads() {
    return 76 # COMPILE OVERLAY INVENTORY
}
_ns_copy() {
    # Only fixed compiler-generated payload/target pairs call this function.
    _ns_tmp="$_ns_stage/.admission-overlay-$3"
    [ ! -e "$_ns_tmp" ] && [ ! -L "$_ns_tmp" ] || return 77
    (umask 077; set -C; dd if="$1" bs=4096 count="$(( ($5 + 4095) / 4096 ))" > "$_ns_tmp" 2>/dev/null) || return 77
    _ns_file "$_ns_tmp" "$4" "$5" || return 77
    mv -f "$_ns_tmp" "$_ns_stage/$2" || return 77
}
_ns_decorate() {
    return 76 # COMPILE OVERLAY WRITES
}
_ns_recheck() {
    native_update_stage_context "$_ns_stage" || return 77
    [ "$_nu_gate_record" = "$_ns_gate" ] && [ "$_nu_context" = "$_ns_context" ] &&
        [ "$_nu_body" = "$_ns_body" ] || return 77
}
native_update_stage_apply() {
    [ "$#" = 1 ] || return 76
    native_update_stage_context "$1" || return $?
    _ns_stage=$1
    _ns_gate=$_nu_gate_record; _ns_context=$_nu_context; _ns_body=$_nu_body
    _ns_validate source && _ns_payloads || return 76
    _ns_recheck || return 77
    _ns_decorate || return 77
    _ns_validate prepared && _ns_recheck || return 77
    native_update_bind_staged "$_ns_stage" || return $?
    # Verify the complete generation and original owner even after publication.
    # Failure retains receipt/admission; native install handles staging cleanup.
    _ns_validate prepared && _ns_recheck || return 77
}
# END PROFILE STAGE FUNCTIONS
native_update_stage_apply "$@"
exit $?
