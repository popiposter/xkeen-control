#!/bin/sh
# Fixed source-only NDM worker. No Start/Stop command, installer or panel call.
# It borrows elected reconcile admission and reads current native ready state.
[ "$#" = 0 ] && [ "$(id -u)" = 0 ] || exit 76
PATH=/opt/bin:/opt/sbin:/usr/bin:/bin; export PATH
_ne_entry=/opt/lib/xkeen/native-admission-entry.sh
_ne_walk=$_ne_entry
while :; do
    [ ! -L "$_ne_walk" ] || exit 76
    _ne_meta=$(stat -t "$_ne_walk" 2>/dev/null) || exit 76
    set -- $_ne_meta
    [ "$#" -ge 9 ] && [ "$5" = 0 ] || exit 76
    case "$4" in ''|*[!0-9a-fA-F]*) exit 76;; esac
    [ "$((0x$4 & 0022))" -eq 0 ] || exit 76
    if [ "$_ne_walk" = "$_ne_entry" ]; then
        [ -f "$_ne_walk" ] && [ "$9" = 1 ] || exit 76
    else
        [ -d "$_ne_walk" ] || exit 76
    fi
    [ "$_ne_walk" != / ] || break
    _ne_walk=${_ne_walk%/*}; [ -n "$_ne_walk" ] || _ne_walk=/
done
. /opt/lib/xkeen/native-admission-entry.sh
# The bootstrap parsed metadata only; the public worker always has zero args.
native_admission_event_enter || exit $?
[ "$_na_body" = 1 ] || exit 0
case "$_na_action" in
    start)
        _na_file_ok /opt/etc/ndm/netfilter.d/proxy.sh || exit 76
        /opt/bin/sh /opt/etc/ndm/netfilter.d/proxy.sh || exit $?
        ;;
    stop) ;; # No native effect. Independent stopped proof still follows.
    *) exit 77;;
esac
native_admission_finish 0
exit $?
