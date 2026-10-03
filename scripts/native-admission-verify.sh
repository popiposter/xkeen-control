#!/bin/sh
# Native-owned preflight/readback. Source-only: the complete candidate stays fenced.
# No panel executable/API, lifecycle execution, persistent receipt or repair here.
PATH=/opt/bin:/opt/sbin:/usr/bin:/bin
export PATH
umask 077

_nv_path_ok() {
    [ "$#" = 2 ] || return 76
    case "$2" in file|directory) ;; *) return 76;; esac
    _nv_kind=$2
    _nv_file=$1
    _nv_walk=$1
    while :; do
        [ ! -L "$_nv_walk" ] || return 76
        _nv_stat=$(stat -t "$_nv_walk" 2>/dev/null) || return 76
        set -- $_nv_stat
        [ "$#" -ge 9 ] && [ "$5" = 0 ] || return 76
        case "$4" in ''|*[!0-9a-fA-F]*) return 76;; esac
        [ "$((0x$4 & 0022))" -eq 0 ] || return 76
        if [ "$_nv_walk" = "$_nv_file" ]; then
            if [ "$_nv_kind" = file ]; then
                [ -f "$_nv_walk" ] && [ "$9" = 1 ] || return 76
            else
                [ -d "$_nv_walk" ] || return 76
            fi
        else
            [ -d "$_nv_walk" ] || return 76
        fi
        [ "$_nv_walk" != / ] || break
        _nv_walk=${_nv_walk%/*}; [ -n "$_nv_walk" ] || _nv_walk=/
    done
}
_nv_file_ok() { _nv_path_ok "$1" file; }
_nv_directory_ok() { _nv_path_ok "$1" directory; }
_nv_hash() {
    _nv_line=$(sha256sum "$1" 2>/dev/null) || return 76
    _nv_digest=${_nv_line%% *}
    [ "${#_nv_digest}" = 64 ] || return 76
    case "$_nv_digest" in *[!0-9a-f]*) return 76;; esac
    printf '%s' "$_nv_digest"
}
_nv_ram_file() {
    # Gate Join already validates /tmp and every private ancestor. /tmp itself
    # is intentionally sticky/world-writable, unlike native code ancestors.
    [ ! -L "$1" ] && [ -f "$1" ] || return 77
    _native_gate_metadata "$1" || return 77
    [ "$_ng_meta_mode:$_ng_meta_uid:$_ng_meta_links" = 8180:0:1 ] && [ "$_ng_meta_size" -le "$2" ]
}
_nv_pending() {
    # Historical fallback is never an implicit second authority.
    [ ! -e /opt/etc/xkeen-control/secrets/previous/.pending ] &&
        [ ! -L /opt/etc/xkeen-control/secrets/previous/.pending ] || return 75
    _nv_pending_path=/opt/etc/xkeen-control/previous/.pending
    if [ ! -e "$_nv_pending_path" ] && [ ! -L "$_nv_pending_path" ]; then return 0; fi
    [ "$_ng_action" = config-change ] || return 75
    _nv_file_ok "$_nv_pending_path" || return 75
    _nv_meta=$(stat -t "$_nv_pending_path" 2>/dev/null) || return 75
    set -- $_nv_meta
    [ "$#" -ge 9 ] && [ "$4:$5:$9" = 8180:0:1 ] && [ "$2" = 23 ] || return 75
    case "$7" in ''|*[!0-9a-fA-F]*) return 75;; esac
    [ "${#7}" -le 15 ] || return 75
    _native_gate_decimal "$8" || return 75
    _nv_dev=$((0x$7)); _nv_ino=$8; _nv_size=$2
    _nv_pending_hash=$(_nv_hash "$_nv_pending_path") || return 75
    _nv_owner_hash=$(_nv_hash /tmp/.xkeen-admission/operation.lock.d/owner) || return 75
    _nv_proof=/tmp/.xkeen-admission/operation.lock.d/node-intent
    _nv_ram_file "$_nv_proof" 256 || return 75
    IFS= read -r _nv_binding < "$_nv_proof" || return 75
    [ "$_nv_binding" = "v1 $_nv_owner_hash $_nv_dev $_nv_ino $_nv_size $_nv_pending_hash" ] &&
        [ "$_ng_meta_size" = "$(( ${#_nv_binding} + 1 ))" ] || return 75
    # Detect replaced same-content intent after metadata/hash reads.
    _nv_after=$(stat -t "$_nv_pending_path" 2>/dev/null) || return 75
    set -- $_nv_after
    [ "$#" -ge 9 ] && [ "$2:$4:$5:$7:$8:$9" = "$_nv_size:8180:0:$(printf '%x' "$_nv_dev"):$_nv_ino:1" ] || return 75
}
_nv_input_file() {
    _nv_file_ok "$1" || return 76
    _native_gate_metadata "$1" || return 76
    [ "$_ng_meta_size" -le 8388608 ] || return 76
    _nv_total=$((_nv_total + _ng_meta_size)); _nv_count=$((_nv_count + 1))
    [ "$_nv_total" -le 33554432 ] && [ "$_nv_count" -le 64 ] || return 76
    _nv_hash "$1" || return 76
    printf ' %s\n' "$1"
}
_nv_inputs() {
    _nv_total=0; _nv_count=0; _nv_json=0
    # Supported native Xray layout is flat. Unknown subtrees and links refuse,
    # rather than silently omitting configuration from the frozen graph.
    [ -d /opt/etc/xray/configs ] && [ ! -L /opt/etc/xray/configs ] || return 76
    for _nv_path in /opt/etc/xray/configs/* /opt/etc/xray/configs/.[!.]* /opt/etc/xray/configs/..?*; do
        [ -e "$_nv_path" ] || { [ ! -L "$_nv_path" ] || return 76; continue; }
        _nv_name=${_nv_path##*/}
        case "$_nv_name" in *[!a-zA-Z0-9_.-]*) return 76;; esac
        case "$_nv_name" in *.json) _nv_json=$((_nv_json + 1));; esac
        _nv_input_file "$_nv_path" || return 76
    done
    [ "$_nv_json" -gt 0 ] || return 76
    # Stable native settings only; native-owned generated cache/last-policy-mark
    # must not be mistaken for authoritative config changes during Start.
    for _nv_path in /opt/etc/xkeen/xkeen.json /opt/etc/xkeen/port_proxying.lst /opt/etc/xkeen/port_exclude.lst /opt/etc/xkeen/ip_exclude.lst /opt/etc/xkeen/ipset/ru_exclude_ipv4.lst /opt/etc/xkeen/ipset/ru_exclude_ipv6.lst /opt/etc/xkeen/ipset/ru_exclude_override.lst; do
        if [ -e "$_nv_path" ] || [ -L "$_nv_path" ]; then
            _nv_input_file "$_nv_path" || return 76
        else
            printf 'absent %s\n' "$_nv_path"
        fi
    done
}
_nv_core() {
    _nv_pids=$(/opt/bin/pidof xray 2>/dev/null)
    _nv_pid_rc=$?
    if [ -z "$_nv_pids" ]; then [ "$_nv_pid_rc" = 1 ] || return 77; printf absent; return; fi
    [ "$_nv_pid_rc" = 0 ] || return 77
    _native_gate_decimal "$_nv_pids" || return 77
    [ "$(readlink "/proc/$_nv_pids/exe")" = /opt/sbin/xray ] || return 77
    _nv_proc_hash=$(_nv_hash "/proc/$_nv_pids/exe") || return 77
    [ "$_nv_proc_hash" = "$_nv_binary_hash" ] || return 77
    _nv_args=$(tr '\000' '\n' < "/proc/$_nv_pids/cmdline") || return 77
    [ "$_nv_args" = "$(printf '%s\n' xray run)" ] || return 77
    # Pinned native init launches PATH-resolved `xray run`, with these settings.
    # Read only the selected environment keys; never print the process environment.
    _nv_env=$(tr '\000' '\n' < "/proc/$_nv_pids/environ" | sed -n '/^XRAY_LOCATION_CONFDIR=/p; /^XRAY_LOCATION_ASSET=/p') || return 77
    [ "$(printf '%s\n' "$_nv_env" | sort)" = "$(printf '%s\n' XRAY_LOCATION_ASSET=/opt/etc/xray/dat XRAY_LOCATION_CONFDIR=/opt/etc/xray/configs)" ] || return 77
    IFS= read -r _nv_proc < "/proc/$_nv_pids/stat" || return 77
    _nv_rest=${_nv_proc##*) }
    [ "$_nv_proc" != "$_nv_rest" ] || return 77
    set -- $_nv_rest
    [ "$#" -ge 20 ] || return 77
    case "$1" in Z|X) return 77;; esac
    shift 19
    _native_gate_decimal "$1" || return 77
    _nv_start=$1
    [ "$(readlink "/proc/$_nv_pids/exe")" = /opt/sbin/xray ] || return 77
    IFS= read -r _nv_final_proc < "/proc/$_nv_pids/stat" || return 77
    _nv_rest=${_nv_final_proc##*) }; set -- $_nv_rest
    [ "$#" -ge 20 ] || return 77
    case "$1" in Z|X) return 77;; esac
    shift 19
    [ "$1" = "$_nv_start" ] || return 77
    printf '%s:%s:%s' "$_nv_pids" "$_nv_start" "$_nv_proc_hash"
}
# BEGIN UPDATE INIT IDENTITY
_nv_update_init_file() {
    _nv_file_ok "$1" || return 76
    _native_gate_metadata "$1" || return 76
    [ "$_ng_meta_size" -gt 0 ] && [ "$_ng_meta_size" -le 524288 ] || return 76
    # awk reconstructs LF records: require the original final LF first.
    _nv_update_last=$(tail -c 1 "$1" | od -v -b) || return 76
    set -- $_nv_update_last
    [ "$#" = 3 ] && [ "$1:$2:$3" = 0000000:012:0000001 ] || return 76
}
_nv_update_init_identity() {
    # Read-only primitive for the future fixed update branch. It authenticates
    # its wrapper context itself; successful projection is not update success.
    [ "$#" = 1 ] || return 76
    case "$1" in pre|post) ;; *) return 76;; esac
    _nv_update_phase=$1
    native_update_verifier_context "$_nv_update_phase" || return $?
    _nv_update_gate=$_nu_gate_record; _nv_update_context=$_nu_context
    _nv_update_query=$_nu_call/init-identity.$_nv_update_phase
    _nv_update_live=/opt/etc/init.d/S05xkeen
    _nv_update_template=/opt/lib/xkeen/native-profile-v1/overlay-1.disabled.sh
    _nv_update_parser=/opt/lib/xkeen/native-update-init.awk
    # The caller also verifies the complete supported prepared profile. This
    # primitive refuses unsafe dependencies and never sources either init.
    for _nv_update_path in "$_nv_update_live" "$_nv_update_template" "$_nv_update_parser"; do
        _nv_update_init_file "$_nv_update_path" || return 76
    done
    _nv_update_live_hash=$(_nv_hash "$_nv_update_live") || return 76
    _nv_update_template_hash=$(_nv_hash "$_nv_update_template") || return 76
    _nv_update_parser_hash=$(_nv_hash "$_nv_update_parser") || return 76
    (umask 077; mkdir "$_nv_update_query") 2>/dev/null || return 77
    _native_gate_directory "$_nv_update_query" 0700 || return 77
    # Bound producer output before reading/hashing it. No raw init/settings
    # enters diagnostics or a public result. Failed queries remain for readback.
    for _nv_update_kind in live template; do
        case "$_nv_update_kind" in live) _nv_update_input=$_nv_update_live;; template) _nv_update_input=$_nv_update_template;; esac
        for _nv_update_mode in code settings; do
            _nv_update_output=$_nv_update_query/$_nv_update_kind-$_nv_update_mode
            (umask 077; set -C; ulimit -f 1024 || exit 76
                LC_ALL=C awk -v mode="$_nv_update_mode" -f "$_nv_update_parser" "$_nv_update_input" > "$_nv_update_output") || return 76
            _nv_ram_file "$_nv_update_output" 1048576 || return 77
        done
    done
    _nv_update_code=$(_nv_hash "$_nv_update_query/live-code") || return 77
    [ "$_nv_update_code" = "$(_nv_hash "$_nv_update_query/template-code")" ] || return 76
    # This supported lifecycle profile requires the native exact literals used
    # by the existing kernel/core verifier, not just parser-safe scalar syntax.
    [ "$(sed -n 's/^name_client="\([^"]*\)"$/\1/p' "$_nv_update_query/live-settings")" = xray ] || return 76
    _nv_update_auto=$(sed -n 's/^start_auto="\([^"]*\)"$/\1/p' "$_nv_update_query/live-settings")
    case "$_nv_update_auto" in on|off) ;; *) return 76;; esac
    _nv_update_settings=$(_nv_hash "$_nv_update_query/live-settings") || return 77
    # Content identity alone is insufficient: same-content links, changed
    # ownership/modes or an unsafe ancestor must not qualify after the query.
    for _nv_update_path in "$_nv_update_live" "$_nv_update_template" "$_nv_update_parser"; do
        _nv_update_init_file "$_nv_update_path" || return 77
    done
    [ "$(_nv_hash "$_nv_update_live")" = "$_nv_update_live_hash" ] &&
        [ "$(_nv_hash "$_nv_update_template")" = "$_nv_update_template_hash" ] &&
        [ "$(_nv_hash "$_nv_update_parser")" = "$_nv_update_parser_hash" ] || return 77
    native_update_verifier_context "$_nv_update_phase" || return 77
    [ "$_nu_gate_record" = "$_nv_update_gate" ] && [ "$_nu_context" = "$_nv_update_context" ] || return 77
}
# END UPDATE INIT IDENTITY
# BEGIN UPDATE ENVIRONMENT
_nv_update_environment_inputs() {
    # This supported update path permits native regeneration/registration, not
    # first-install/new-feature/legacy migration effects. Check before body.
    for _nv_env_absent in /opt/etc/init.d/S99xkeen /opt/etc/init.d/S24xray /opt/etc/init.d/S99xkeenstart /opt/etc/xkeen_exclude.lst; do
        [ ! -e "$_nv_env_absent" ] && [ ! -L "$_nv_env_absent" ] || return 76
    done
    _nv_directory_ok /opt/etc/xkeen/ipset && _nv_directory_ok /opt/var/spool/cron/crontabs || return 76
    for _nv_env_path in /opt/etc/xkeen/xkeen.json /opt/etc/xkeen/port_proxying.lst /opt/etc/xkeen/port_exclude.lst /opt/etc/xkeen/ip_exclude.lst /opt/lib/opkg/status; do
        _nv_file_ok "$_nv_env_path" || return 76
        _native_gate_metadata "$_nv_env_path" || return 76
        [ "$_ng_meta_size" -le 2097152 ] || return 76
        _nv_hash "$_nv_env_path" || return 76
        printf ' %s\n' "$_nv_env_path"
    done
    jq -e 'type == "object" and (.xkeen == null or (.xkeen | type) == "object")' /opt/etc/xkeen/xkeen.json >/dev/null 2>&1 || return 76
    _nv_env_cron_init=/opt/etc/init.d/S05crond
    if [ -e "$_nv_env_cron_init" ] || [ -L "$_nv_env_cron_init" ]; then
        _nv_file_ok "$_nv_env_cron_init" || return 76
        _native_gate_metadata "$_nv_env_cron_init" || return 76
        [ "$_ng_meta_size" -le 65536 ] || return 76
        _nv_hash "$_nv_env_cron_init" || return 76
        printf ' cron-init\n'
    else
        printf 'absent cron-init\n'
    fi
    _nv_env_cron=/opt/var/spool/cron/crontabs/root
    if [ -e "$_nv_env_cron" ] || [ -L "$_nv_env_cron" ]; then
        _nv_file_ok "$_nv_env_cron" || return 76
        _native_gate_metadata "$_nv_env_cron" || return 76
        [ "$_ng_meta_size" -le 65536 ] || return 76
        if [ "$_ng_meta_size" -gt 0 ]; then
            _nv_update_init_file "$_nv_env_cron" || return 76
        fi
        # Match the actual pinned native removal expression, including comments
        # and prefix matches; its grep-v rewrite must not remove any line.
        grep -E '(/opt/sbin/xkeen[[:space:]]+-(ugi|ugs|ux|uk))' "$_nv_env_cron" >/dev/null
        _nv_env_grep=$?
        [ "$_nv_env_grep" = 1 ] || return 76
        _nv_hash "$_nv_env_cron" || return 76
        printf ' cron\n'
    else
        printf 'absent cron\n'
    fi
}
_nv_update_environment_pre() {
    [ "$#" = 0 ] || return 76
    native_update_verifier_context pre || return $?
    _nv_env_gate=$_nu_gate_record; _nv_env_context=$_nu_context
    _nv_env_before=$(_nv_update_environment_inputs) || return 76
    _nv_file_ok /opt/bin/opkg && _nv_file_ok /opt/libexec/timeout-coreutils || return 76
    [ -x /opt/bin/opkg ] && [ -x /opt/libexec/timeout-coreutils ] || return 76
    _nv_env_opkg_hash=$(_nv_hash /opt/bin/opkg) || return 76
    _nv_env_timeout_hash=$(_nv_hash /opt/libexec/timeout-coreutils) || return 76
    _nv_env_query=$_nu_call/environment.pre
    (umask 077; mkdir "$_nv_env_query") 2>/dev/null || return 77
    _native_gate_directory "$_nv_env_query" 0700 || return 77
    (umask 077; set -C; ulimit -f 512 || exit 76
        /opt/libexec/timeout-coreutils -s KILL 15 /opt/bin/opkg list-installed > "$_nv_env_query/packages" 2>/dev/null) || return 76
    _nv_ram_file "$_nv_env_query/packages" 262144 || return 77
    _nv_env_cron_count=$(LC_ALL=C awk '
      /^[A-Za-z0-9][A-Za-z0-9+_.-]* - [A-Za-z0-9][A-Za-z0-9+_.:~()-]*$/ {if($1=="cron")cron++;next}
      {bad=1} END {if(bad || cron>1)exit 76; print cron+0}' "$_nv_env_query/packages") || return 76
    case "$_nv_env_cron_count" in
        1) ;; # Native register_cron_initd returns before touching its script.
        0)
            # Native BusyBox crond profile: exact pinned script version0.6 makes
            # registration skip replacement, without installing a cron package.
            # The pinned public native echo-e payload is1711 bytes; its fixture
            # derives these bytes and tests the actual registration function.
            [ "$(_nv_hash /opt/etc/init.d/S05crond)" = 516226b527a140fc733d349c42dd8e92b3182dbc7ac3df38c78e75bbb7a713e2 ] || return 76
            ;;
        *) return 76;;
    esac
    _nv_env_after=$(_nv_update_environment_inputs) || return 77
    [ "$_nv_env_before" = "$_nv_env_after" ] || return 77
    _nv_file_ok /opt/bin/opkg && _nv_file_ok /opt/libexec/timeout-coreutils || return 77
    [ "$(_nv_hash /opt/bin/opkg)" = "$_nv_env_opkg_hash" ] &&
        [ "$(_nv_hash /opt/libexec/timeout-coreutils)" = "$_nv_env_timeout_hash" ] || return 77
    native_update_verifier_context pre || return 77
    [ "$_nu_gate_record" = "$_nv_env_gate" ] && [ "$_nu_context" = "$_nv_env_context" ] || return 77
    _nv_env_digest=$(printf '%s\n' "$_nv_env_before" | sha256sum) || return 77
    _nv_env_digest=${_nv_env_digest%% *}
}
# END UPDATE ENVIRONMENT
# BEGIN UPDATE PACKAGE IDENTITY
_nv_update_package_identity() {
    [ "$#" = 1 ] || return 76
    case "$1" in pre|post) _nv_pkg_phase=$1;; *) return 76;; esac
    native_update_verifier_context "$_nv_pkg_phase" || return $?
    _nv_pkg_gate=$_nu_gate_record; _nv_pkg_context=$_nu_context
    _nv_pending || return $?
    _nv_pkg_status=/opt/lib/opkg/status
    _nv_pkg_control=/opt/lib/opkg/info/xkeen.control
    _nv_pkg_parser=/opt/lib/xkeen/native-update-packages.awk
    _nv_update_init_file "$_nv_pkg_status" && _nv_file_ok "$_nv_pkg_parser" || return 76
    _native_gate_metadata "$_nv_pkg_parser" || return 76
    [ "$_ng_meta_size" -gt 0 ] && [ "$_ng_meta_size" -le 65536 ] || return 76
    _nv_update_init_file "$_nv_pkg_control" || return 76
    _native_gate_metadata "$_nv_pkg_control" || return 76
    [ "$_ng_meta_size" -le 16384 ] || return 76
    _nv_pkg_status_hash=$(_nv_hash "$_nv_pkg_status") || return 76
    _nv_pkg_parser_hash=$(_nv_hash "$_nv_pkg_parser") || return 76
    _nv_pkg_control_hash=$(_nv_hash "$_nv_pkg_control") || return 76
    _nv_pkg_query=$_nu_call/package-identity.$_nv_pkg_phase
    (umask 077; mkdir "$_nv_pkg_query") 2>/dev/null || return 77
    _native_gate_directory "$_nv_pkg_query" 0700 || return 77
    for _nv_pkg_projection in other identity control; do
        _nv_pkg_input=$_nv_pkg_status
        [ "$_nv_pkg_projection" != control ] || _nv_pkg_input=$_nv_pkg_control
        (umask 077; set -C; ulimit -f 1024 || exit 76
            LC_ALL=C awk -v mode="$_nv_pkg_projection" -v version=2.0.1 -f "$_nv_pkg_parser" "$_nv_pkg_input" > "$_nv_pkg_query/$_nv_pkg_projection") || return 76
        _nv_ram_file "$_nv_pkg_query/$_nv_pkg_projection" 524288 || return 77
    done
    _nv_pkg_other_hash=$(_nv_hash "$_nv_pkg_query/other") || return 77
    _nv_pkg_identity_hash=$(_nv_hash "$_nv_pkg_query/identity") || return 77
    [ "$(_nv_hash "$_nv_pkg_query/control")" = "$_nv_pkg_identity_hash" ] || return 76
    # Repeat metadata/protection checks even if file contents remain identical.
    _nv_update_init_file "$_nv_pkg_status" && _nv_file_ok "$_nv_pkg_parser" || return 77
    _native_gate_metadata "$_nv_pkg_parser" || return 77
    [ "$_ng_meta_size" -gt 0 ] && [ "$_ng_meta_size" -le 65536 ] || return 77
    _nv_update_init_file "$_nv_pkg_control" || return 77
    _native_gate_metadata "$_nv_pkg_control" || return 77
    [ "$_ng_meta_size" -le 16384 ] || return 77
    [ "$(_nv_hash "$_nv_pkg_status")" = "$_nv_pkg_status_hash" ] &&
        [ "$(_nv_hash "$_nv_pkg_parser")" = "$_nv_pkg_parser_hash" ] &&
        [ "$(_nv_hash "$_nv_pkg_control")" = "$_nv_pkg_control_hash" ] || return 77
    _nv_pending || return $?
    native_update_verifier_context "$_nv_pkg_phase" || return 77
    [ "$_nu_gate_record" = "$_nv_pkg_gate" ] && [ "$_nu_context" = "$_nv_pkg_context" ] || return 77
}
# END UPDATE PACKAGE IDENTITY
_nv_main() {
    [ "$#" = 4 ] && [ "$(id -u)" = 0 ] || return 76
    _nv_phase=$1; _na_role=$2; _na_action=$3; _na_mode=$4
    case "$_nv_phase:$_na_role:$_na_action:$_na_mode" in
        pre:dispatcher:start:forced|pre:dispatcher:stop:forced|pre:dispatcher:restart:forced|pre:init:start:forced|pre:init:stop:forced|pre:init:restart:forced|pre:init:start:automatic|pre:init:restart:automatic|post:dispatcher:start:forced|post:dispatcher:stop:forced|post:dispatcher:restart:forced|post:init:start:forced|post:init:stop:forced|post:init:restart:forced|post:init:start:automatic|post:init:restart:automatic|pre:hook:start:forced|pre:hook:restart:forced|pre:hook:start:automatic|pre:hook:restart:automatic|post:hook:start:forced|post:hook:restart:forced|post:hook:start:automatic|post:hook:restart:automatic) ;;
        pre:event:start:forced|pre:event:stop:forced|post:event:start:forced|post:event:stop:forced) ;;
        *) return 76;;
    esac
    _nv_file_ok /opt/lib/xkeen/native-operation-gate.sh && _nv_file_ok /opt/lib/xkeen/native-admission-entry.sh || return 76
    . /opt/lib/xkeen/native-operation-gate.sh
    . /opt/lib/xkeen/native-admission-entry.sh
    _na_call_dir=/tmp/.xkeen-admission/operation.lock.d/call.$_na_role
    _na_child_ok || return 77
    if [ "$_ng_action" = reconcile ]; then
        _nv_ready=$(_na_event_state) || return 77
        case "$_na_action:$_nv_ready" in start:running|stop:stopped) ;; *) return 77;; esac
    fi
    _nv_context=$(_nv_hash "$_na_call_dir/context") || return 77
    _nv_pending || return $?
    _nv_file_ok /opt/etc/init.d/S05xkeen && _nv_file_ok /opt/sbin/xray || return 76
    # Read only exact native literal settings, never source the native init.
    [ "$(sed -n 's/^name_client="\([^"]*\)"$/\1/p' /opt/etc/init.d/S05xkeen)" = xray ] || return 76
    _nv_auto=$(sed -n 's/^start_auto="\([^"]*\)"$/\1/p' /opt/etc/init.d/S05xkeen)
    case "$_nv_auto" in on|off) ;; *) return 76;; esac
    _nv_expect=running
    if [ "$_na_action" = stop ] || [ "$_na_mode:$_nv_auto:$_na_action" = automatic:off:restart ]; then _nv_expect=stopped; fi
    [ "$_na_mode:$_nv_auto:$_na_action" != automatic:off:start ] || _nv_expect=unchanged
    # This required native hook proof is deliberately not supplied as a success
    # placeholder. Missing/unsupported Hybrid proof rejects BEFORE native body.
    _nv_file_ok /opt/lib/xkeen/native-admission-hook-verify.sh || return 76
    # Explicit Entware coreutils-timeout prerequisite; no unbounded fallback.
    # Installation/target qualification remains a separate native enable step.
    _nv_file_ok /opt/libexec/timeout-coreutils || return 76
    [ -x /opt/libexec/timeout-coreutils ] && [ -x /opt/bin/pidof ] || return 76
    _nv_init_hash=$(_nv_hash /opt/etc/init.d/S05xkeen) || return 76
    _nv_binary_hash=$(_nv_hash /opt/sbin/xray) || return 76
    _nv_manifest=$(_nv_inputs) || return 76
    _nv_config_hash=$(printf '%s\n' "$_nv_manifest" | sha256sum) || return 76
    _nv_config_hash=${_nv_config_hash%% *}
    _nv_runtime=$(_nv_core) || return 77
    _nv_frozen="v1 $_nv_context $_nv_init_hash $_nv_binary_hash $_nv_config_hash $_nv_expect"
    _nv_baseline=$_na_call_dir/preflight
    if [ "$_nv_phase" = pre ]; then
        if [ "$_nv_expect" = running ]; then
            (native_admission_strip; export XRAY_LOCATION_ASSET=/opt/etc/xray/dat;
                /opt/libexec/timeout-coreutils -s KILL 15 /opt/sbin/xray run -test -confdir /opt/etc/xray/configs >/dev/null 2>&1) || return 76
        fi
    else
        _nv_ram_file "$_nv_baseline" 512 || return 77
        IFS= read -r _nv_saved < "$_nv_baseline" || return 77
        [ "$_ng_meta_size" = "$(( ${#_nv_saved} + 1 ))" ] || return 77
        _nv_previous=${_nv_saved##* }
        [ "$_nv_saved" = "$_nv_frozen $_nv_previous" ] || return 77
        case "$_nv_expect:$_nv_runtime" in running:absent) return 77;; stopped:absent) ;; stopped:*) return 77;; esac
        [ "$_nv_expect" != unchanged ] || [ "$_nv_runtime" = "$_nv_previous" ] || return 77
    fi
    /opt/libexec/timeout-coreutils -s KILL 15 /opt/bin/sh /opt/lib/xkeen/native-admission-hook-verify.sh "$_nv_phase" "$_na_role" "$_na_action" "$_na_mode" "$_nv_expect" >/dev/null 2>&1 || return 77
    _na_child_ok || return 77
    _nv_pending || return $?
    if [ "$_ng_action" = reconcile ]; then
        [ "$(_na_event_state)" = "$_nv_ready" ] || return 77
    fi
    # Validation/readback can take time. Freeze only unchanged inputs and require
    # the independently observed process identity to survive the hook readback.
    [ "$(_nv_hash /opt/etc/init.d/S05xkeen)" = "$_nv_init_hash" ] &&
        [ "$(_nv_hash /opt/sbin/xray)" = "$_nv_binary_hash" ] || return 77
    _nv_final_manifest=$(_nv_inputs) || return 77
    [ "$_nv_final_manifest" = "$_nv_manifest" ] || return 77
    [ "$(_nv_core)" = "$_nv_runtime" ] || return 77
    if [ "$_nv_phase" = pre ]; then
        (set -C; printf '%s\n' "$_nv_frozen $_nv_runtime" > "$_nv_baseline") || return 77
    else
        rm "$_nv_baseline" || return 77
    fi
}
_nv_main "$@"
exit $?
