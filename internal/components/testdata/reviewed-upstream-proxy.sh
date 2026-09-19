#!/bin/sh
# XKeen: Auto-generated file. DO NOT EDIT!
_xkeen_secure_rundir() {
    d="/tmp/.xkeen"
    if [ -e "$d" ] && [ ! -d "$d" ]; then rm -f "$d" 2>/dev/null; fi
    if [ -d "$d" ]; then
        set -- $(ls -ld "$d" 2>/dev/null)
        [ "$3" = "root" ] && [ "$1" = "drwx------" ] || rm -rf "$d" 2>/dev/null
    fi
    [ -d "$d" ] || mkdir -m 700 "$d" 2>/dev/null || return 1
    chmod 700 "$d" 2>/dev/null || return 1
    printf '%s' "$d"
}
_xkeen_rundir=$(_xkeen_secure_rundir) || exit 1
[ -f "$_xkeen_rundir/ready" ] || exit 0
case "${table:-}" in filter|raw) exit 0 ;; esac
name_client='xray'
name_profile='xkeen'
mode_proxy='Hybrid'
network_redirect='tcp'
network_tproxy='udp'
networks='tcp udp'
name_chain='xkeen'
port_redirect='61219'
port_tproxy='61219'
port_dscp_force_proxy='fixture'
port_dscp_force_proxy_redirect='fixture'
port_dscp_force_proxy_tproxy='fixture'
port_donor='fixture'
port_exclude='fixture'
policy_mark='fixture'
policy_mark_full='fixture'
comment_tag='xkeen_rule'
comment='fixture'
custom_mark='fixture'
nfqws_mark='fixture'
dscp_exclude='fixture'
dscp_proxy='fixture'
dscp_force_proxy='fixture'
dscp_force_proxy_tag='fixture'
mode_dscp_force_proxy='fixture'
network_dscp_force_proxy='fixture'
network_dscp_force_proxy_redirect='fixture'
network_dscp_force_proxy_tproxy='fixture'
user_policies=''
table_redirect='nat'
table_tproxy='mangle'
table_mark='0x111'
table_id='111'
file_dns='fixture'
arm_cpu='fixture'
file_ca='fixture'
proxy_dns='fixture'
proxy_router='fixture'
directory_configs_app='/opt/etc/xray'
directory_xray_config='/opt/etc/xray/configs'
directory_xray_asset='/opt/etc/xray/dat'
iptables_supported='fixture'
ip6tables_supported='fixture'
arm64_fd='fixture'
other_fd='fixture'
aghfix='fixture'
ipv6_proxy='::1'
ipv4_proxy='127.0.0.1'
val_exclude_ip6='fixture'
val_exclude_ip4='fixture'
name_ipset_deny_mac='xkeen_deny_mac'
url_server='127.0.0.1:79'
url_hotspot='rci/show/ip/hotspot'
rci_token=''
ru_exclude_ipv4='fixture'
ru_exclude_ipv6='fixture'
gomemlimit_value='fixture'
killswitch='fixture'
restart_script() {
    exec /bin/sh "$0" "$@"
}
curl_api() {
    if [ -n "$rci_token" ]; then
        curl --connect-timeout 2 -m 5 -kfsS -H "X-Ndma-Tkn: $rci_token" "$@"
    else
        curl --connect-timeout 2 -m 5 -kfsS "$@"
    fi
}
if pidof "$name_client" >/dev/null; then
    _xkeen_nf_lock="$_xkeen_rundir/netfilter.lock.d"
    _xkeen_lock_owned=""
    _lock_try=0
    while [ "$_lock_try" -lt 50 ]; do
        if mkdir "$_xkeen_nf_lock" 2>/dev/null; then
            _xkeen_lock_owned=1
            printf '%s' "$$" > "$_xkeen_nf_lock/pid"
            trap 'rm -rf "$_xkeen_nf_lock"' EXIT INT TERM
            break
        fi
        _lock_pid=$(cat "$_xkeen_nf_lock/pid" 2>/dev/null)
        if [ -n "$_lock_pid" ] && ! kill -0 "$_lock_pid" 2>/dev/null; then
            rm -rf "$_xkeen_nf_lock" 2>/dev/null
            continue
        fi
        _lock_try=$((_lock_try + 1))
        usleep 100000 2>/dev/null || sleep 1
    done
    [ -n "$_xkeen_lock_owned" ] || exit 0
    _xkeen_sync_deny_mac_ipset() {
        command -v ipset >/dev/null 2>&1 || return 0
        ipset create "$name_ipset_deny_mac" hash:mac -exist 2>/dev/null || return 0
        _tmp="${name_ipset_deny_mac}_tmp"
        ipset create "$_tmp" hash:mac -exist 2>/dev/null
        ipset flush "$_tmp" >/dev/null 2>&1
        _hjson=$(curl_api "${url_server}/${url_hotspot}" 2>/dev/null)
        if [ -z "$_hjson" ]; then
            ipset destroy "$_tmp" 2>/dev/null
            return 0
        fi
        printf '%s' "$_hjson" | jq -r '
            ((.host // . // []) |
             (if type == "array" then .[] else . end)) |
            select((.access // "") == "deny" and (.mac // "") != "") |
            .mac
        ' 2>/dev/null | tr '[:lower:]' '[:upper:]' | while IFS= read -r _m; do
            [ -n "$_m" ] && ipset add "$_tmp" "$_m" -exist 2>/dev/null
        done
        ipset swap "$_tmp" "$name_ipset_deny_mac" 2>/dev/null
        ipset destroy "$_tmp" 2>/dev/null
    }
    command -v ipset >/dev/null 2>&1 && ipset create "$name_ipset_deny_mac" hash:mac -exist 2>/dev/null
    _xkeen_release_nf_lock() {
        if [ -n "$_xkeen_lock_owned" ]; then
            rm -rf "$_xkeen_nf_lock" 2>/dev/null
            _xkeen_lock_owned=""
            trap - EXIT INT TERM
        fi
    }
    _xkeen_v4_nat_rules=""
    _xkeen_v4_mangle_rules=""
    _xkeen_v6_nat_rules=""
    _xkeen_v6_mangle_rules=""
    ipt() {
        [ "$family" = "iptables" ] && [ "$iptables_supported" != "true" ] && return 0
        [ "$family" = "ip6tables" ] && [ "$ip6tables_supported" != "true" ] && return 0
        case "$1" in
            -A|-I|-D)
                _line=$*
                case "${family}_${table}" in
                    iptables_nat)     _xkeen_v4_nat_rules="${_xkeen_v4_nat_rules}${_line}
" ;;
                    iptables_mangle)  _xkeen_v4_mangle_rules="${_xkeen_v4_mangle_rules}${_line}
" ;;
                    ip6tables_nat)    _xkeen_v6_nat_rules="${_xkeen_v6_nat_rules}${_line}
" ;;
                    ip6tables_mangle) _xkeen_v6_mangle_rules="${_xkeen_v6_mangle_rules}${_line}
" ;;
                esac
                return 0
                ;;
            *)
                if [ "$family" = "iptables" ]; then
                    iptables -w -t "$table" "$@"
                else
                    ip6tables -w -t "$table" "$@"
                fi
                return $?
                ;;
        esac
    }
    _xkeen_apply_table() {
        _family="$1"
        _table="$2"
        _rules_var="$3"
        eval "_rules=\${$_rules_var}"
        [ -z "$_rules" ] && return 0
        save_cmd=""
        [ "$_family" = "iptables" ] && [ "$iptables_supported" = "true" ] && save_cmd="iptables-save"
        [ "$_family" = "ip6tables" ] && [ "$ip6tables_supported" = "true" ] && save_cmd="ip6tables-save"
        [ -z "$save_cmd" ] && { _deletes=""; return; }
        _deletes=$($save_cmd -t "$_table" 2>/dev/null | awk \
            -v tag="$comment_tag" \
            -v c1="$name_chain" \
            -v c2="${name_chain}_out" \
            -v c3="${name_chain}_force" '
            index($0, tag) &&
            $1 == "-A" &&
            $2 != c1 &&
            $2 != c2 &&
            $2 != c3 {
                sub(/^-A /, "-D ")
                print
            }
        ')
        _blob=$( {
            printf '*%s\n' "$_table"
            printf ':%s -\n' "$name_chain"
            { [ -n "$port_dscp_force_proxy" ] || [ -n "$policy_mark_full" ]; } && printf ':%s_force -\n' "$name_chain"
            [ "$proxy_router" = "on" ] && printf ':%s_out -\n' "$name_chain"
            [ -n "$_deletes" ] && printf '%s\n' "$_deletes"
            printf '%s' "$_rules"
            printf 'COMMIT'
        } )
        _restore_cmd="iptables-restore"
        [ "$_family" = "ip6tables" ] && _restore_cmd="ip6tables-restore"
        _attempt=1
        _max_attempts=3
        while :; do
            _restore_err=$(printf '%s\n' "$_blob" | "$_restore_cmd" --noflush 2>&1) && {
                [ "$_attempt" -gt 1 ] && command -v logger >/dev/null 2>&1 && \
                    logger -p daemon.notice -t xkeen \
                        "$_restore_cmd $_table: applied on retry $_attempt"
                break
            }
            if [ "$_attempt" -ge "$_max_attempts" ]; then
                command -v logger >/dev/null 2>&1 && \
                    logger -p daemon.err -t xkeen \
                        "$_restore_cmd --noflush failed for $_table after $_attempt attempts: $(printf '%s' "$_restore_err" | head -n1)"
                return 1
            fi
            usleep $((200000 * _attempt)) 2>/dev/null || sleep "$_attempt"
            _attempt=$((_attempt + 1))
        done
        return 0
    }
    _xkeen_apply() {
        [ "$iptables_supported" = "true" ] && _xkeen_apply_table iptables nat _xkeen_v4_nat_rules || true
        [ "$iptables_supported" = "true" ] && _xkeen_apply_table iptables mangle _xkeen_v4_mangle_rules || true
        [ "$ip6tables_supported" = "true" ] && _xkeen_apply_table ip6tables nat _xkeen_v6_nat_rules || true
        [ "$ip6tables_supported" = "true" ] && _xkeen_apply_table ip6tables mangle _xkeen_v6_mangle_rules || true
    }
    add_exclude_rules() {
        chain="$1"
        for exclude in $exclude_list; do
            if [ "$file_dns" = "true" ] && [ "$proxy_dns" = "on" ] && [ "$chain" != "${name_chain}_out" ]; then
                case "$exclude" in
                    10.0.0.0/8|172.16.0.0/12|192.168.0.0/16|fd00::/8|fe80::/10)
                    if [ "$table" = "mangle" ] && [ "$mode_proxy" = "Hybrid" ]; then
                        ipt -A "$chain" -d "$exclude" -p tcp --dport 53 $comment -j RETURN >/dev/null 2>&1
                        ipt -A "$chain" -d "$exclude" -p udp ! --dport 53 $comment -j RETURN >/dev/null 2>&1
                    elif [ "$table" = "nat" ] && [ "$mode_proxy" = "Hybrid" ]; then
                        ipt -A "$chain" -d "$exclude" -p tcp ! --dport 53 $comment -j RETURN >/dev/null 2>&1
                        ipt -A "$chain" -d "$exclude" -p udp --dport 53 $comment -j RETURN >/dev/null 2>&1
                    elif [ "$table" = "mangle" ] && [ "$mode_proxy" = "TProxy" ]; then
                        ipt -A "$chain" -d "$exclude" -p tcp ! --dport 53 $comment -j RETURN >/dev/null 2>&1
                        ipt -A "$chain" -d "$exclude" -p udp ! --dport 53 $comment -j RETURN >/dev/null 2>&1
                    fi
                    ;;
                esac
            else
                ipt -A "$chain" -d "$exclude" $comment -j RETURN >/dev/null 2>&1
            fi
        done
    }
    add_ipset_exclude() {
        base_set="$1"
        set_type="${2:-hash:net}"
        if [ "$family" = "ip6tables" ]; then
            set_name="${base_set}6"
            ipset_family="inet6"
        else
            set_name="$base_set"
            ipset_family="inet"
        fi
        ipset create "$set_name" "$set_type" family "$ipset_family" -exist || return
        ipt -I "$chain" 1 -m set --match-set "$set_name" dst $comment -j RETURN >/dev/null 2>&1
        if [ "$base_set" = "user_exclude" ]; then
            ipt -I "$chain" 1 -m set --match-set "$set_name" src $comment -j RETURN >/dev/null 2>&1
        fi
    }
    add_geo_exclude() {
        if [ "$family" = "ip6tables" ]; then
            geo_set="geo_exclude6"
            override_set="geo_override6"
            ipset_family="inet6"
        else
            geo_set="geo_exclude"
            override_set="geo_override"
            ipset_family="inet"
        fi
        ipset create "$geo_set" hash:net family "$ipset_family" -exist
        ipset create "$override_set" hash:net family "$ipset_family" -exist
        ipt -I "$chain" 1 -m set --match-set "$geo_set" dst -m set ! --match-set "$override_set" dst $comment -j RETURN >/dev/null 2>&1
    }
    add_ipt_rule() {
        family="$1"
        table="$2"
        chain="$3"
        shift 3
        [ "$family" = "iptables" ] && [ "$iptables_supported" = "false" ] && return
        [ "$family" = "ip6tables" ] && [ "$ip6tables_supported" = "false" ] && return
        add_exclude_rules "$chain"
        if [ "$table" = "$table_tproxy" ]; then
            if [ "$mode_proxy" = "Hybrid" ]; then
                set -- -p udp -m conntrack --ctstate ESTABLISHED,RELATED $comment -j CONNMARK --restore-mark
            else
                set -- -m conntrack --ctstate ESTABLISHED,RELATED $comment -j CONNMARK --restore-mark
            fi
            ipt -I "$chain" 1 "$@" >/dev/null 2>&1
        fi
        case "$mode_proxy" in
            Hybrid)
                if [ "$table" = "$table_redirect" ]; then
                    ipt -I "$chain" 1 -m conntrack --ctstate DNAT $comment -j RETURN >/dev/null 2>&1
                    add_ipset_exclude ext_exclude hash:ip
                    add_ipset_exclude user_exclude hash:net
                    add_geo_exclude
                    ipt -A "$chain" -p tcp $comment -j REDIRECT --to-port "$port_redirect" >/dev/null 2>&1
                else
                    ipt -I "$chain" 1 -m conntrack --ctstate DNAT $comment -j RETURN >/dev/null 2>&1
                    ipt -I "$chain" 1 -m conntrack --ctstate INVALID $comment -j RETURN >/dev/null 2>&1
                    add_ipset_exclude ext_exclude hash:ip
                    add_ipset_exclude user_exclude hash:net
                    add_geo_exclude
                    ipt -A "$chain" -p udp -m socket --transparent $comment -j MARK --set-mark "$table_mark" >/dev/null 2>&1
                    ipt -A "$chain" -p udp -m mark ! --mark 0 $comment -j CONNMARK --save-mark >/dev/null 2>&1
                    ipt -A "$chain" -p udp $comment -j TPROXY --on-ip "$proxy_ip" --on-port "$port_tproxy" --tproxy-mark "$table_mark" >/dev/null 2>&1
                fi
                ;;
            TProxy)
                ipt -I "$chain" 1 -m conntrack --ctstate DNAT $comment -j RETURN >/dev/null 2>&1
                ipt -I "$chain" 1 -m conntrack --ctstate INVALID $comment -j RETURN >/dev/null 2>&1
                add_ipset_exclude ext_exclude hash:ip
                add_ipset_exclude user_exclude hash:net
                add_geo_exclude
                for net in $network_tproxy; do
                    ipt -A "$chain" -p "$net" -m socket --transparent $comment -j MARK --set-mark "$table_mark" >/dev/null 2>&1
                    ipt -A "$chain" -p "$net" -m mark ! --mark 0 $comment -j CONNMARK --save-mark >/dev/null 2>&1
                    ipt -A "$chain" -p "$net" $comment -j TPROXY --on-ip "$proxy_ip" --on-port "$port_tproxy" --tproxy-mark "$table_mark" >/dev/null 2>&1
                done
                ;;
            Redirect)
                ipt -I "$chain" 1 -m conntrack --ctstate DNAT $comment -j RETURN >/dev/null 2>&1
                add_ipset_exclude ext_exclude hash:ip
                add_ipset_exclude user_exclude hash:net
                add_geo_exclude
                for net in $network_redirect; do
                    ipt -A "$chain" -p "$net" $comment -j REDIRECT --to-port "$port_redirect" >/dev/null 2>&1
                done
                ;;
            *) exit 0 ;;
        esac
        if [ -n "$dscp_exclude" ]; then
            for dscp in $dscp_exclude; do
                ipt -I "$chain" -m dscp --dscp "$dscp" $comment -j RETURN >/dev/null 2>&1
            done
        fi
        if [ "$table" = "$table_redirect" ] && [ -n "$port_dscp_force_proxy_redirect" ] && [ -n "$dscp_force_proxy" ]; then
            for net in $network_dscp_force_proxy_redirect; do
                ipt -I "$chain" 1 -p "$net" -m dscp --dscp "$dscp_force_proxy" $comment -j RETURN >/dev/null 2>&1
            done
        fi
        if [ "$table" = "$table_tproxy" ] && [ -n "$port_dscp_force_proxy_tproxy" ] && [ -n "$dscp_force_proxy" ]; then
            for net in $network_dscp_force_proxy_tproxy; do
                ipt -I "$chain" 1 -p "$net" -m dscp --dscp "$dscp_force_proxy" $comment -j RETURN >/dev/null 2>&1
            done
        fi
    }
    add_force_ipt_rule() {
        family="$1"
        table="$2"
        chain="$3"
        [ "$family" = "iptables" ] && [ "$iptables_supported" = "false" ] && return
        [ "$family" = "ip6tables" ] && [ "$ip6tables_supported" = "false" ] && return
        if [ "$table" = "$table_redirect" ]; then
            [ -n "$port_dscp_force_proxy_redirect" ] || return
        elif [ "$table" = "$table_tproxy" ]; then
            [ -n "$port_dscp_force_proxy_tproxy" ] || return
        else
            return
        fi
        add_exclude_rules "$chain"
        ipt -I "$chain" 1 -m conntrack --ctstate DNAT $comment -j RETURN >/dev/null 2>&1
        ipt -I "$chain" 1 -m conntrack --ctstate INVALID $comment -j RETURN >/dev/null 2>&1
        if [ "$table" = "$table_redirect" ]; then
            for net in $network_dscp_force_proxy_redirect; do
                ipt -A "$chain" -p "$net" $comment -j REDIRECT --to-port "$port_dscp_force_proxy_redirect" >/dev/null 2>&1
            done
        else
            ipt -I "$chain" 1 -m conntrack --ctstate ESTABLISHED,RELATED $comment -j CONNMARK --restore-mark >/dev/null 2>&1
            for net in $network_dscp_force_proxy_tproxy; do
                ipt -A "$chain" -p "$net" -m socket --transparent $comment -j MARK --set-mark "$table_mark" >/dev/null 2>&1
                ipt -A "$chain" -p "$net" -m mark ! --mark 0 $comment -j CONNMARK --save-mark >/dev/null 2>&1
                ipt -A "$chain" -p "$net" $comment -j TPROXY --on-ip "$proxy_ip" --on-port "$port_dscp_force_proxy_tproxy" --tproxy-mark "$table_mark" >/dev/null 2>&1
            done
        fi
    }
    configure_route() {
        ip_version="$1"
        if [ -n "$policy_mark" ]; then
            policy_table=$(ip rule show | awk -v policy="$policy_mark" '$0 ~ policy && /lookup/ && !/blackhole/ {print $(NF); exit}')
        fi
        source_table="${policy_table:-main}"
        check_default() {
            if [ "$ip_version" = "6" ] && ! ip -6 route show default 2>/dev/null | grep -q .; then
                return 0
            fi
            if [ "$source_table" = "main" ]; then
                ip -"$ip_version" route show default 2>/dev/null | grep -q '^default'
            else
                ip -"$ip_version" route show table "$policy_table" 2>/dev/null | grep -E '^default ' | grep -vq 'unreachable'
            fi
        }
        attempts=0
        max_attempts=4
        until check_default; do
            attempts=$((attempts + 1))
            if [ "$attempts" -ge "$max_attempts" ]; then
                [ "$ip_version" = "4" ] && touch "/tmp/noinet"
                return 1
            fi
            sleep 1
        done
        [ "$ip_version" = "4" ] && rm -f "/tmp/noinet"
        _cur_routes=$(ip -"$ip_version" route show table "$table_id" 2>/dev/null)
        _want_routes=$(ip -"$ip_version" route show table "$source_table" 2>/dev/null | \
            grep -v '^default\|^unreachable\|^blackhole')
        if [ -n "$_cur_routes" ] && \
           printf '%s\n' "$_cur_routes" | grep -q '^local default dev lo' && \
           [ "$(printf '%s\n' "$_cur_routes" | grep -v '^local default dev lo' | sort)" = \
             "$(printf '%s\n' "$_want_routes" | sort)" ] && \
           ip -"$ip_version" rule show 2>/dev/null | grep -q "fwmark $table_mark lookup $table_id"; then
            return 0
        fi
        ip -"$ip_version" rule del fwmark "$table_mark" lookup "$table_id" >/dev/null 2>&1 || true
        ip -"$ip_version" route flush table "$table_id" >/dev/null 2>&1 || true
        ip -"$ip_version" route add local default dev lo table "$table_id" >/dev/null 2>&1 || true
        ip -"$ip_version" rule add fwmark "$table_mark" lookup "$table_id" >/dev/null 2>&1 || true
        ip -"$ip_version" route show table "$source_table" 2>/dev/null | while read -r route_line; do
            case "$route_line" in
                default*|unreachable*|blackhole*) continue ;;
                *) ip -"$ip_version" route add table "$table_id" $route_line >/dev/null 2>&1 || true ;;
            esac
        done
        return 0
    }
    add_multiport_rules() {
        family="$1"
        table="$2"
        net="$3"
        mark="$4"
        ports="$5"
        target="$6"
        [ -z "$ports" ] && return
        num_ports=$(echo "$ports" | tr ',' '\n' | wc -l)
        i=1
        while [ "$i" -le "$num_ports" ]; do
            end=$((i + 6))
            chunk=$(echo "$ports" | tr ',' '\n' | sed -n "${i},${end}p" | tr '\n' ',' | sed 's/,$//')
            [ -z "$chunk" ] && break
            if [ -n "$mark" ]; then
                set -- -m connmark --mark "$mark" -m conntrack ! --ctstate INVALID -p "$net" -m multiport --dports "$chunk" $comment -j "$target"
            else
                set -- -m conntrack ! --ctstate INVALID -p "$net" -m multiport --dports "$chunk" $comment -j "$target"
            fi
            ipt -A PREROUTING "$@" >/dev/null 2>&1
            i=$((i + 7))
        done
    }
    add_prerouting() {
        family="$1"
        table="$2"
        ipt -I PREROUTING 1 -m set --match-set "$name_ipset_deny_mac" src $comment -j RETURN >/dev/null 2>&1
        if [ "$table" = "$table_redirect" ] && [ -n "$port_dscp_force_proxy_redirect" ] && [ -n "$dscp_force_proxy" ]; then
            for force_net in $network_dscp_force_proxy_redirect; do
                set -- -m conntrack ! --ctstate INVALID -p "$force_net" -m dscp --dscp "$dscp_force_proxy" $comment -j "${name_chain}_force"
                ipt -A PREROUTING "$@" >/dev/null 2>&1
            done
        fi
        if [ "$table" = "$table_tproxy" ] && [ -n "$port_dscp_force_proxy_tproxy" ] && [ -n "$dscp_force_proxy" ]; then
            for force_net in $network_dscp_force_proxy_tproxy; do
                set -- -m conntrack ! --ctstate INVALID -p "$force_net" -m dscp --dscp "$dscp_force_proxy" $comment -j "${name_chain}_force"
                ipt -A PREROUTING "$@" >/dev/null 2>&1
            done
        fi
        for net in $networks; do
            if [ "$mode_proxy" = "Hybrid" ]; then
                [ "$table" = "nat"    ] && [ "$net" != "tcp" ] && continue
                [ "$table" = "mangle" ] && [ "$net" != "udp" ] && continue
            fi
            proto_match="-p $net"
            all_ports_proto_match=""
            [ "$mode_proxy" = "TProxy" ] && all_ports_proto_match="$proto_match"
            for dscp in $dscp_proxy; do
                set -- -m conntrack ! --ctstate INVALID $proto_match -m dscp --dscp "$dscp" $comment -j "$name_chain"
                ipt -A PREROUTING "$@" >/dev/null 2>&1
            done
            if [ "$proxy_router" = "on" ]; then
                set -- -i lo -m mark --mark "$table_mark" $proto_match $comment -j "$name_chain"
                ipt -A PREROUTING "$@" >/dev/null 2>&1
            fi
            while IFS='|' read -r pname pmark pmode pports; do
                [ -z "$pmark" ] && continue
                pmark=$(echo "$pmark" | tr -d ' \r\n')
                pmode=$(echo "$pmode" | tr -d ' \r\n')
                pports=$(echo "$pports" | tr -d ' \r\n')
                if [ "$pmode" = "all" ]; then
                    set -- -m connmark --mark 0x"$pmark" -m conntrack ! --ctstate INVALID $all_ports_proto_match $comment -j "$name_chain"
                    ipt -A PREROUTING "$@" >/dev/null 2>&1
                elif [ "$pmode" = "include" ]; then
                    add_multiport_rules "$family" "$table" "$net" "0x$pmark" "$pports" "$name_chain"
                elif [ "$pmode" = "exclude" ]; then
                    add_multiport_rules "$family" "$table" "$net" "0x$pmark" "$pports" "RETURN"
                    set -- -m connmark --mark 0x"$pmark" -m conntrack ! --ctstate INVALID -p "$net" $comment -j "$name_chain"
                    ipt -A PREROUTING "$@" >/dev/null 2>&1
                fi
            done <<USER_POLICIES_EOF
USER_POLICIES_EOF
            if [ -n "$policy_mark_full" ]; then
                set -- -m connmark --mark "$policy_mark_full" -m conntrack ! --ctstate INVALID -p "$net" $comment -j "${name_chain}_force"
                ipt -A PREROUTING "$@" >/dev/null 2>&1
            fi
            if [ -n "$policy_mark" ]; then
                if [ -n "$port_donor" ]; then
                    add_multiport_rules "$family" "$table" "$net" "$policy_mark" "$port_donor" "$name_chain"
                elif [ -n "$port_exclude" ]; then
                    add_multiport_rules "$family" "$table" "$net" "$policy_mark" "$port_exclude" "RETURN"
                    set -- -m connmark --mark "$policy_mark" -m conntrack ! --ctstate INVALID -p "$net" $comment -j "$name_chain"
                    ipt -A PREROUTING "$@" >/dev/null 2>&1
                else
                    set -- -m connmark --mark "$policy_mark" -m conntrack ! --ctstate INVALID $all_ports_proto_match $comment -j "$name_chain"
                    ipt -A PREROUTING "$@" >/dev/null 2>&1
                fi
            else
                if [ -n "$port_donor" ]; then
                    add_multiport_rules "$family" "$table" "$net" "" "$port_donor" "$name_chain"
                elif [ -n "$port_exclude" ]; then
                    add_multiport_rules "$family" "$table" "$net" "" "$port_exclude" "RETURN"
                    set -- -m conntrack ! --ctstate INVALID -p "$net" $comment -j "$name_chain"
                    ipt -A PREROUTING "$@" >/dev/null 2>&1
                else
                    set -- -m conntrack ! --ctstate INVALID $all_ports_proto_match $comment -j "$name_chain"
                    ipt -A PREROUTING "$@" >/dev/null 2>&1
                fi
            fi
        done
    }
    add_output() {
        family="$1"
        table="$2"
        [ "$proxy_router" != "on" ] && return
        out_chain="${name_chain}_out"
        orig_chain="$chain"
        chain="$out_chain"
        ipt -A "$out_chain" -p udp --dport 33434:33534 $comment -j RETURN >/dev/null 2>&1
        ipt -A "$out_chain" -o lo $comment -j RETURN >/dev/null 2>&1
        ipt -A "$out_chain" -m mark --mark 255 $comment -j RETURN >/dev/null 2>&1
        policy_bypass_marks="$policy_mark"
        if [ -n "$user_policies" ]; then
            user_policy_marks=$(printf '%s\n' "$user_policies" | awk -F'|' '$2 != "" {print "0x"$2}')
            policy_bypass_marks="$policy_bypass_marks $user_policy_marks"
        fi
        for bypass_mark in $policy_bypass_marks; do
            [ -n "$bypass_mark" ] && ipt -A "$out_chain" -m mark --mark "$bypass_mark" $comment -j RETURN >/dev/null 2>&1
        done
        for nfqws_bypass_mark in $nfqws_mark; do
            [ -n "$nfqws_bypass_mark" ] || continue
            case "$nfqws_bypass_mark" in
                */*) ;;
                *) nfqws_bypass_mark="$nfqws_bypass_mark/$nfqws_bypass_mark" ;;
            esac
            ipt -A "$out_chain" -m mark --mark "$nfqws_bypass_mark" $comment -j RETURN >/dev/null 2>&1
        done
        add_exclude_rules "$out_chain"
        add_ipset_exclude ext_exclude hash:ip
        add_ipset_exclude user_exclude hash:net
        add_geo_exclude
        chain="$orig_chain"
        for net in $networks; do
            if [ "$mode_proxy" = "Hybrid" ]; then
                [ "$table" = "nat"    ] && [ "$net" != "tcp" ] && continue
                [ "$table" = "mangle" ] && [ "$net" != "udp" ] && continue
            fi
            proto_match="-p $net"
            set -- -m conntrack ! --ctstate INVALID $proto_match $comment -j "$out_chain"
            ipt -A OUTPUT "$@" >/dev/null 2>&1
            if [ "$table" = "$table_redirect" ]; then
                set -- -p "$net" $comment -j REDIRECT --to-port "$port_redirect"
                ipt -A "$out_chain" "$@" >/dev/null 2>&1
            elif [ "$table" = "$table_tproxy" ]; then
                set -- -p "$net" $comment -j MARK --set-mark "$table_mark"
                ipt -A "$out_chain" "$@" >/dev/null 2>&1
            fi
        done
    }
    dns_redir() {
        family="$1"
        table="nat"
        [ "$aghfix" != "on" ] && return
        [ "$file_dns" = "true" ] && [ "$proxy_dns" = "on" ] && return
        all_marks=""
        [ -n "$policy_mark" ] && all_marks="$policy_mark"
        [ -n "$policy_mark_full" ] && all_marks="$policy_mark_full $all_marks"
        [ -n "$custom_mark" ] && all_marks="$custom_mark $all_marks"
        if [ -n "$user_policies" ]; then
            user_marks=$(echo "$user_policies" | awk -F'|' '{if ($2 != "") print "0x"$2}')
            all_marks="$all_marks $user_marks"
        fi
        for mark in $all_marks; do
            mark=$(echo "$mark" | tr -d ' \r\n')
            [ -z "$mark" ] && continue
            for proto in udp tcp; do
                set -- -p "$proto" -m mark --mark "$mark" -m pkttype --pkt-type unicast -m "$proto" --dport 53 $comment -j REDIRECT --to-ports 53
                ipt -I _NDM_HOTSPOT_DNSREDIR "$@" >/dev/null 2>&1
            done
        done
    }
    _xkeen_hook_tables=""
    [ -n "$port_tproxy" ] && _xkeen_hook_tables="$table_tproxy"
    [ -n "$port_redirect" ] && [ "$table_redirect" != "$table_tproxy" ] && \
        _xkeen_hook_tables="$_xkeen_hook_tables $table_redirect"
    _xkeen_family_intact() {
        _bin="$1"
        for _tbl in $_xkeen_hook_tables; do
            [ "$("$_bin" -w -t "$_tbl" -S "$name_chain" 2>/dev/null | wc -l)" -gt 1 ] || return 1
            "$_bin" -w -t "$_tbl" -S PREROUTING 2>/dev/null | grep -q "$comment_tag" || return 1
            if [ "$proxy_router" = "on" ]; then
                "$_bin" -w -t "$_tbl" -S OUTPUT 2>/dev/null | grep -q "$comment_tag" || return 1
            fi
        done
        if [ "$aghfix" = "on" ] && ! { [ "$file_dns" = "true" ] && [ "$proxy_dns" = "on" ]; }; then
            "$_bin" -w -t nat -S _NDM_HOTSPOT_DNSREDIR 2>/dev/null | grep -q "$comment_tag" || return 1
        fi
        return 0
    }
    _xkeen_rules_intact() {
        [ -n "$_xkeen_hook_tables" ] || return 1
        if [ "$iptables_supported" = "true" ]; then
            _xkeen_family_intact iptables || return 1
        fi
        if [ "$ip6tables_supported" = "true" ]; then
            _xkeen_family_intact ip6tables || return 1
        fi
        return 0
    }
    _xkeen_refill_geo_if_empty() {
        _rg_set="$1"
        _rg_file="$2"
        _rg_family="$3"
        [ -s "$_rg_file" ] || return 0
        ipset save "$_rg_set" 2>/dev/null | grep -q '^add ' && return 0
        _rg_tmp="${_rg_set}_renew_tmp"
        ipset create "$_rg_tmp" hash:net family "$_rg_family" -exist 2>/dev/null || return 1
        ipset flush "$_rg_tmp" 2>/dev/null
        if sed -e 's/\r$//' -e 's/#.*//' -e '/^[[:space:]]*$/d' "$_rg_file" | \
             awk '{print "add '"$_rg_tmp"' "$1}' | ipset restore -exist; then
            ipset swap "$_rg_set" "$_rg_tmp" 2>/dev/null || return 1
        else
            logger -p daemon.warning -t xkeen "не удалось восстановить $_rg_set из $_rg_file"
        fi
        ipset destroy "$_rg_tmp" 2>/dev/null
    }
    _xkeen_wan_state="$_xkeen_rundir/wan_ip"
    _xkeen_cur_wan=$(ip -o route get 195.208.4.1 2>/dev/null | sed -n 's/.*src \([^ ]*\).*/\1/p' || \
                     ip -o route get 77.88.8.8 2>/dev/null | sed -n 's/.*src \([^ ]*\).*/\1/p')
    _xkeen_prev_wan=$(cat "$_xkeen_wan_state" 2>/dev/null)
    if [ -n "$_xkeen_cur_wan" ] && [ "$_xkeen_cur_wan" = "$_xkeen_prev_wan" ] && _xkeen_rules_intact; then
        [ "$iptables_supported" = "true" ] && _xkeen_refill_geo_if_empty geo_exclude "$ru_exclude_ipv4" inet
        [ "$ip6tables_supported" = "true" ] && _xkeen_refill_geo_if_empty geo_exclude6 "$ru_exclude_ipv6" inet6
        _xkeen_release_nf_lock
        exit 0
    fi
    if _xkeen_rules_intact; then
        [ "$iptables_supported" = "true" ] && configure_route 4
        [ "$ip6tables_supported" = "true" ] && configure_route 6
        [ -n "$_xkeen_cur_wan" ] && printf '%s' "$_xkeen_cur_wan" > "$_xkeen_wan_state"
        _xkeen_release_nf_lock
        _xkeen_sync_deny_mac_ipset
        exit 0
    fi
    _xkeen_cache_dir="$_xkeen_rundir/rules_cache"
    _xkeen_ensure_ipsets() {
        command -v ipset >/dev/null 2>&1 || return 0
        if [ "$iptables_supported" = "true" ]; then
            ipset create ext_exclude hash:ip family inet -exist 2>/dev/null
            ipset create user_exclude hash:net family inet -exist 2>/dev/null
            ipset create geo_exclude hash:net family inet -exist 2>/dev/null
            ipset create geo_override hash:net family inet -exist 2>/dev/null
        fi
        if [ "$ip6tables_supported" = "true" ]; then
            ipset create ext_exclude6 hash:ip family inet6 -exist 2>/dev/null
            ipset create user_exclude6 hash:net family inet6 -exist 2>/dev/null
            ipset create geo_exclude6 hash:net family inet6 -exist 2>/dev/null
            ipset create geo_override6 hash:net family inet6 -exist 2>/dev/null
        fi
    }
    _xkeen_cache_valid() {
        [ -s "$_xkeen_cache_dir/key" ] || return 1
        [ "$(cat "$_xkeen_cache_dir/key" 2>/dev/null)" = "$(md5sum "$0" 2>/dev/null | awk '{print $1}')" ]
    }
    _xkeen_cache_load() {
        for _cn in v4_nat v4_mangle v6_nat v6_mangle; do
            _cb=$(cat "$_xkeen_cache_dir/$_cn" 2>/dev/null)
            [ -n "$_cb" ] || continue
            case "$_cn" in
                v4_nat) _xkeen_v4_nat_rules="$_cb
" ;;
                v4_mangle) _xkeen_v4_mangle_rules="$_cb
" ;;
                v6_nat) _xkeen_v6_nat_rules="$_cb
" ;;
                v6_mangle) _xkeen_v6_mangle_rules="$_cb
" ;;
            esac
        done
    }
    _xkeen_cache_save() {
        rm -rf "${_xkeen_cache_dir}.new" 2>/dev/null
        mkdir -p "${_xkeen_cache_dir}.new" 2>/dev/null || return 0
        printf '%s' "$_xkeen_v4_nat_rules"    > "${_xkeen_cache_dir}.new/v4_nat"
        printf '%s' "$_xkeen_v4_mangle_rules" > "${_xkeen_cache_dir}.new/v4_mangle"
        printf '%s' "$_xkeen_v6_nat_rules"    > "${_xkeen_cache_dir}.new/v6_nat"
        printf '%s' "$_xkeen_v6_mangle_rules" > "${_xkeen_cache_dir}.new/v6_mangle"
        md5sum "$0" 2>/dev/null | awk '{print $1}' > "${_xkeen_cache_dir}.new/key"
        rm -rf "${_xkeen_cache_dir}.old" 2>/dev/null
        [ -d "$_xkeen_cache_dir" ] && mv "$_xkeen_cache_dir" "${_xkeen_cache_dir}.old" 2>/dev/null
        if mv "${_xkeen_cache_dir}.new" "$_xkeen_cache_dir" 2>/dev/null; then
            rm -rf "${_xkeen_cache_dir}.old" 2>/dev/null
        else
            [ -d "${_xkeen_cache_dir}.old" ] && mv "${_xkeen_cache_dir}.old" "$_xkeen_cache_dir" 2>/dev/null
        fi
    }
    if _xkeen_cache_valid; then
        _xkeen_ensure_ipsets
        [ "$iptables_supported" = "true" ] && _xkeen_refill_geo_if_empty geo_exclude "$ru_exclude_ipv4" inet
        [ "$ip6tables_supported" = "true" ] && _xkeen_refill_geo_if_empty geo_exclude6 "$ru_exclude_ipv6" inet6
        _xkeen_cache_load
        [ "$iptables_supported" = "true" ] && configure_route 4
        [ "$ip6tables_supported" = "true" ] && configure_route 6
        _xkeen_apply
        [ -n "$_xkeen_cur_wan" ] && printf '%s' "$_xkeen_cur_wan" > "$_xkeen_wan_state"
        _xkeen_release_nf_lock
        _xkeen_sync_deny_mac_ipset
        exit 0
    fi
    if [ -n "$port_donor" ] || [ -n "$port_exclude" ]; then
        [ "$file_dns" = "true" ] && [ "$proxy_dns" = "on" ] && [ -n "$port_donor" ] && port_donor="53,$port_donor"
    fi
    for family in iptables ip6tables; do
        [ "$family" = "ip6tables" ] && [ "$ip6tables_supported" != "true" ] && continue
        [ "$family" = "iptables" ] && [ "$iptables_supported" != "true" ] && continue
        if [ "$family" = "ip6tables" ]; then
            exclude_list="$val_exclude_ip6"
            proxy_ip="$ipv6_proxy"
            configure_route 6
        else
            exclude_list="$val_exclude_ip4"
            proxy_ip="$ipv4_proxy"
            configure_route 4
        fi
        if [ -n "$port_redirect" ] && [ -n "$port_tproxy" ]; then
            for table in "$table_tproxy" "$table_redirect"; do
                add_ipt_rule "$family" "$table" "$name_chain"
                add_force_ipt_rule "$family" "$table" "${name_chain}_force"
                add_prerouting "$family" "$table"
                add_output "$family" "$table"
            done
        elif [ -z "$port_redirect" ] && [ -n "$port_tproxy" ]; then
            table="$table_tproxy"
            add_ipt_rule "$family" "$table" "$name_chain"
            add_force_ipt_rule "$family" "$table" "${name_chain}_force"
            add_prerouting "$family" "$table"
            add_output "$family" "$table"
        elif [ -n "$port_redirect" ] && [ -z "$port_tproxy" ]; then
            table="$table_redirect"
            add_ipt_rule "$family" "$table" "$name_chain"
            add_prerouting "$family" "$table"
            add_output "$family" "$table"
        fi
        dns_redir "$family"
    done
    _xkeen_apply
    _xkeen_cache_save
    [ -n "$_xkeen_cur_wan" ] && printf '%s' "$_xkeen_cur_wan" > "$_xkeen_wan_state"
    _xkeen_release_nf_lock
    _xkeen_sync_deny_mac_ipset
else
    _xkeen_start_lock="$_xkeen_rundir/starting.lock.d"
    if ! mkdir "$_xkeen_start_lock" 2>/dev/null; then
        _sl_pid=$(cat "$_xkeen_start_lock/pid" 2>/dev/null)
        if [ -n "$_sl_pid" ] && kill -0 "$_sl_pid" 2>/dev/null; then
            exit 0
        fi
        rm -rf "$_xkeen_start_lock" 2>/dev/null
        mkdir "$_xkeen_start_lock" 2>/dev/null || exit 0
    fi
    printf '%s' "$$" > "$_xkeen_start_lock/pid"
    trap 'rm -rf "$_xkeen_start_lock"' EXIT INT TERM
    fd_limit="$other_fd"
    [ "$arm_cpu" = "true" ] && fd_limit="$arm64_fd"
    ulimit -SHn "$fd_limit"
    export SSL_CERT_FILE="$file_ca"
    case "$name_client" in
        xray)
            export XRAY_LOCATION_CONFDIR="$directory_xray_config"
            export XRAY_LOCATION_ASSET="$directory_xray_asset"
            "$name_client" run >/dev/null 2>&1 &
        ;;
        mihomo)
            export CLASH_HOME_DIR="$directory_configs_app"
            if [ -z "$GOMEMLIMIT" ] && [ -n "$gomemlimit_value" ]; then
                export GOMEMLIMIT="$gomemlimit_value"
            fi
            "$name_client" >/dev/null 2>&1 &
        ;;
    esac
    _probe=0
    while [ "$_probe" -lt 60 ]; do
        pidof "$name_client" >/dev/null 2>&1 && break
        _probe=$((_probe + 1))
        usleep 100000
    done
    unset _probe
    rm -rf "$_xkeen_start_lock"
    trap - EXIT INT TERM
    if pidof "$name_client" >/dev/null; then
        restart_script "$@"
    else
        exit 1
    fi
fi
