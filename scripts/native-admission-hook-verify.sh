#!/bin/sh
# Source-only bounded native Hybrid kernel proof. Never executes the generated hook.
PATH=/opt/bin:/opt/sbin:/usr/bin:/bin
export PATH
umask 077
_hv_file() {
    _hv_path=$1; _hv_walk=$1
    while :; do
        [ ! -L "$_hv_walk" ] || return 76
        _hv_meta=$(stat -t "$_hv_walk" 2>/dev/null) || return 76
        set -- $_hv_meta
        [ "$#" -ge 9 ] && [ "$5" = 0 ] || return 76
        case "$4" in ''|*[!0-9a-fA-F]*) return 76;; esac
        [ "$((0x$4 & 0022))" -eq 0 ] || return 76
        if [ "$_hv_walk" = "$_hv_path" ]; then
            [ -f "$_hv_walk" ] && [ "$9" = 1 ] && [ "$2" -le 262144 ] || return 76
        else [ -d "$_hv_walk" ] || return 76; fi
        [ "$_hv_walk" != / ] || break
        _hv_walk=${_hv_walk%/*}; [ -n "$_hv_walk" ] || _hv_walk=/
    done
}
_hv_literal() {
    # Only literal, bounded scalar assignments. No eval/source, escapes or expansion.
    awk -v key="$2" -v quote="$3" '
        index($0,key "=")==1 {n++;v=substr($0,length(key)+2)
            if(substr(v,1,1)!=quote || substr(v,length(v),1)!=quote)bad=1
            v=substr(v,2,length(v)-2)
            if(length(v)>128 || v~/[^a-zA-Z0-9_.:\/ -]/)bad=1
            result=v}
        END{if(n!=1 || bad)exit 1;print result}' "$1"
}
_hv_base() {
    _hv_init=/opt/etc/init.d/S05xkeen
    _hv_file "$_hv_init" || return 76
    _hv_chain=$(_hv_literal "$_hv_init" name_chain '"') || return 76
    _hv_tag=$(_hv_literal "$_hv_init" comment_tag '"') || return 76
    _hv_mark=$(_hv_literal "$_hv_init" table_mark '"') || return 76
    _hv_table=$(_hv_literal "$_hv_init" table_id '"') || return 76
    _hv_deny=$(_hv_literal "$_hv_init" name_ipset_deny_mac '"') || return 76
    # Fixed native identity profile keeps readback bounded and rejects drift.
    [ "$_hv_chain:$_hv_tag:$_hv_mark:$_hv_table:$_hv_deny" = xkeen:xkeen_rule:0x111:111:xkeen_deny_mac ] || return 76
    [ "$(_hv_literal "$_hv_init" name_client '"')" = xray ] &&
        [ "$(_hv_literal "$_hv_init" proxy_router '"')" = off ] &&
        [ "$(_hv_literal "$_hv_init" aghfix '"')" = off ] || return 76
    _hv_v4=true; _hv_v6=true; _hv_policy=
}
_hv_profile() {
    _hv_hook=/opt/etc/ndm/netfilter.d/proxy.sh
    _hv_file "$_hv_hook" || return 76
    [ "$(_hv_literal "$_hv_hook" mode_proxy "'")" = Hybrid ] &&
        [ "$(_hv_literal "$_hv_hook" name_client "'")" = xray ] || return 76
    for _hv_key in name_chain comment_tag table_mark table_id name_ipset_deny_mac; do
        [ "$(_hv_literal "$_hv_hook" "$_hv_key" "'")" = "$(_hv_literal "$_hv_init" "$_hv_key" '"')" ] || return 76
    done
    [ "$(_hv_literal "$_hv_hook" table_redirect "'")" = nat ] &&
        [ "$(_hv_literal "$_hv_hook" table_tproxy "'")" = mangle ] &&
        [ "$(_hv_literal "$_hv_hook" iptables_supported "'")" = true ] &&
        [ "$(_hv_literal "$_hv_hook" ip6tables_supported "'")" = true ] &&
        [ "$(_hv_literal "$_hv_hook" proxy_router "'")" = off ] &&
        [ "$(_hv_literal "$_hv_hook" aghfix "'")" = off ] || return 76
    _hv_full=$(_hv_literal "$_hv_hook" policy_mark_full "'") || return 76
    [ -z "$_hv_full" ] || return 76
    _hv_redirect=$(_hv_literal "$_hv_hook" port_redirect "'") || return 76
    _hv_tproxy=$(_hv_literal "$_hv_hook" port_tproxy "'") || return 76
    _hv_force_redirect=$(_hv_literal "$_hv_hook" port_dscp_force_proxy_redirect "'") || return 76
    _hv_force_tproxy=$(_hv_literal "$_hv_hook" port_dscp_force_proxy_tproxy "'") || return 76
    _hv_force_dscp=$(_hv_literal "$_hv_hook" dscp_force_proxy "'") || return 76
    _hv_policy=$(_hv_literal "$_hv_hook" policy_mark "'") || return 76
    case "$_hv_policy" in ''|0x[0-9a-fA-F]*) ;;*) return 76;;esac
    case "$_hv_policy" in *[!0-9a-fA-Fx]*) return 76;;esac
    _hv_ip4=$(_hv_literal "$_hv_hook" ipv4_proxy "'") || return 76
    _hv_ip6=$(_hv_literal "$_hv_hook" ipv6_proxy "'") || return 76
    [ "$_hv_ip4:$_hv_ip6" = '127.0.0.1:::1' ] || return 76
    for _hv_port in "$_hv_redirect" "$_hv_tproxy" "$_hv_force_redirect" "$_hv_force_tproxy"; do
        case "$_hv_port" in ''|*[!0-9]*) return 76;; esac
        [ "${#_hv_port}" -le 5 ] && [ "$_hv_port" -gt 0 ] && [ "$_hv_port" -le 65535 ] || return 76
    done
    case "$_hv_force_dscp" in ''|*[!0-9]*) return 76;; esac
    [ "${#_hv_force_dscp}" -le 2 ] && [ "$_hv_force_dscp" -le 63 ] || return 76
    [ "$(_hv_literal "$_hv_hook" network_dscp_force_proxy_redirect "'")" = tcp ] &&
        [ "$(_hv_literal "$_hv_hook" network_dscp_force_proxy_tproxy "'")" = udp ] || return 76
    [ "${1-}" = pre ] || [ "$_hv_redirect $_hv_tproxy $_hv_force_redirect $_hv_force_tproxy" = "$_hv_input_ports" ] || return 77
}
_hv_inputs() {
    # Restricted unambiguous native Xray Hybrid input profile. The existing
    # core verifier freezes/validates the complete config; this selects only
    # the four native transparent listeners, never an independent renderer.
    _hv_force_tag=$(_hv_literal "$_hv_init" dscp_force_proxy_tag '"') || return 76
    [ -n "$_hv_force_tag" ] || return 76
    _hv_input_ports=$(jq -ser --arg tag "$_hv_force_tag" '
      [.[] | .inbounds[]? | select((.protocol=="dokodemo-door" or .protocol=="tunnel") and .settings.followRedirect==true) |
       {port:.port, tag:(.tag//""), mode:(if .streamSettings.sockopt.tproxy=="tproxy" then "tproxy" else "redirect" end),
        network:(.settings.allowedNetwork//.settings.network//"")}] as $all |
      def one(mode; forced; proto):
        [$all[] | select(.mode==mode) |
         select(if forced then (.tag==($tag+"-"+mode) or .tag==$tag) else (.tag!=$tag and .tag!=($tag+"-redirect") and .tag!=($tag+"-tproxy")) end)] |
        if length==1 and (.[0].port|type)=="number" and .[0].port>=1 and .[0].port<=65535 and (.[0].port|floor)==.[0].port and
          (.[0].network|split(",")|index(proto))!=null then .[0].port else error("unsupported transparent inputs") end;
      [one("redirect";false;"tcp"),one("tproxy";false;"udp"),one("redirect";true;"tcp"),one("tproxy";true;"udp")] | join(" ")
      ' /opt/etc/xray/configs/*.json 2>/dev/null) || return 76
}
_hv_query() {
    # Finite read-only operations, one bounded RAM file in the current call.
    # Limit producer output BEFORE bringing any bytes into a shell variable.
    _na_descendant_ok || return 77
    _hv_qfile=$_na_call_dir/hook-query
    (set -C; : > "$_hv_qfile") 2>/dev/null || return 77
    (
        ulimit -f 128 || exit 76
        case "$1:$2" in
            table4:nat|table4:mangle) exec iptables-save -t "$2";;
            table6:nat|table6:mangle) exec ip6tables-save -t "$2";;
            rules4:) exec ip -4 rule show;; rules6:) exec ip -6 rule show;;
            sets:) exec ipset list -terse;;
            routes4:*|routes6:*)
                case "$2" in ''|*[!a-zA-Z0-9_-]*) exit 76;;esac
                case "$1" in routes4) exec ip -4 route show table "$2";; routes6) exec ip -6 route show table "$2";;esac;;
            *) exit 76;;
        esac
    ) > "$_hv_qfile" 2>/dev/null
    _hv_qrc=$?
    _na_descendant_ok || return 77
    _native_gate_metadata "$_hv_qfile" || return 77
    [ ! -L "$_hv_qfile" ] && [ "$_ng_meta_mode:$_ng_meta_uid:$_ng_meta_links" = 8180:0:1 ] && [ "$_ng_meta_size" -le 131072 ] || return 77
    if [ "$_hv_qrc" != 0 ]; then rm "$_hv_qfile" || return 77; return 77; fi
    cat "$_hv_qfile"
    _hv_qrc=$?
    rm "$_hv_qfile" || return 77
    return "$_hv_qrc"
}
_hv_read_table() { _hv_query "table$1" "$2"; }
_hv_read_ipsets() {
    _hv_metadata=$(_hv_query sets '') || return 77
    printf '%s\n' "$_hv_metadata" | awk '
        /^Name: / {names++;name=$2;type="";next}
        /^Type: / {type=$2;next}
        /^Header: / {family="";for(i=2;i<NF;i++)if($i=="family")family=$(i+1)
            if(name=="" || type=="")exit 1
            headers++;printf "create %s %s%s\n",name,type,(family==""?"":" family " family);next}
        /^Revision: |^Size in memory: |^References: |^Number of entries: |^[[:space:]]*$/ {next}
        {bad=1}
        END{if(bad || names!=headers)exit 1}'
}
_hv_read_rules() { _hv_query "rules$1" ''; }
_hv_read_routes() { _hv_query "routes$1" "$2"; }
_hv_ipsets() {
    _hv_sets=$(_hv_read_ipsets) || return 77
    [ "${#_hv_sets}" -le 262144 ] || return 77
    printf '%s\n' "$_hv_sets" | awk -v deny="$_hv_deny" -v v4="$_hv_v4" -v v6="$_hv_v6" '
        BEGIN {want[deny]="hash:mac"; if(v4=="true") families[""]="inet"; if(v6=="true") families["6"]="inet6"
            for(s in families) {want["ext_exclude" s]="hash:ip"; want["user_exclude" s]="hash:net"; want["geo_exclude" s]="hash:net"; want["geo_override" s]="hash:net"}}
        $1=="create" && ($2 in want) {seen[$2]++; if($3!=want[$2]) bad=1
            if($2!=deny) {f="";for(i=4;i<NF;i++)if($i=="family")f=$(i+1);expected=($2~/6$/?"inet6":"inet");if(f!=expected)bad=1}}
        END {for(n in want)if(seen[n]!=1)bad=1;exit bad?1:0}' || return 77
}
_hv_routes() {
    _hv_source=main
    if [ -n "$_hv_policy" ]; then
        _hv_policy_rules=$(_hv_read_rules 4) || return 77
        _hv_source=$(printf '%s\n' "$_hv_policy_rules" | awk -v p="$_hv_policy" '$0 ~ p && /lookup/ && !/blackhole/ {print $NF;exit}')
        case "$_hv_source" in ''|*[!a-zA-Z0-9_-]*) return 76;; esac
    fi
    for _hv_f in 4 6; do
        case "$_hv_f" in 4) [ "$_hv_v4" = true ] || continue;;6) [ "$_hv_v6" = true ] || continue;;esac
        _hv_rules=$(_hv_read_rules "$_hv_f") || return 77
        _hv_routes=$(_hv_read_routes "$_hv_f" "$_hv_table") || return 77
        _hv_source_routes=$(_hv_read_routes "$_hv_f" "$_hv_source") || return 77
        [ "${#_hv_rules}" -le 65536 ] && [ "${#_hv_routes}" -le 65536 ] && [ "${#_hv_source_routes}" -le 65536 ] || return 77
        printf '%s\n' "$_hv_rules" | awk -v m="$_hv_mark" -v t="$_hv_table" '
            {mark="";tab="";for(i=1;i<NF;i++){if($i=="fwmark")mark=$(i+1);if($i=="lookup")tab=$(i+1)}
             split(mark,parts,"/")
             if(parts[1]==m || tab==t) {n++
                 if(NF!=7 || $1!~/^[0-9]+:$/ || $2!="from" || $3!="all" || $4!="fwmark" || ($5!=m && $5!=m "/0xffffffff") || $6!="lookup" || $7!=t)bad=1}}
            END{exit (n==1 && !bad)?0:1}' || return 77
        [ "$(printf '%s\n' "$_hv_routes" | grep -c '^local default dev lo')" = 1 ] || return 77
        _hv_have=$(printf '%s\n' "$_hv_routes" | grep -v '^local default dev lo' | sort)
        _hv_want=$(printf '%s\n' "$_hv_source_routes" | grep -v '^default\|^unreachable\|^blackhole' | sort)
        [ "$_hv_have" = "$_hv_want" ] || return 77
    done
}
_hv_running() {
    # This bounded proof checks native capture prerequisites, not rule equivalence.
    for _hv_family in 4 6; do
        case "$_hv_family" in 4) [ "$_hv_v4" = true ] || continue; _hv_ip=$_hv_ip4;;
            6) [ "$_hv_v6" = true ] || continue; _hv_ip=$_hv_ip6;; esac
        for _hv_kind in nat mangle; do
            _hv_view=$(_hv_read_table "$_hv_family" "$_hv_kind") || return 77
            [ "${#_hv_view}" -le 262144 ] || return 77
            [ -n "$_hv_view" ] || return 77
            printf '%s\n' "$_hv_view" | awk -v chain="$_hv_chain" -v tag="$_hv_tag" \
                -v kind="$_hv_kind" -v redirect="$_hv_redirect" -v tproxy="$_hv_tproxy" \
                -v fr="$_hv_force_redirect" -v ft="$_hv_force_tproxy" -v dscp="$_hv_force_dscp" \
                -v mark="$_hv_mark" -v ip="$_hv_ip" -v deny="$_hv_deny" '
                function val(key, i, v) { for(i=1;i<NF;i++) if($i==key) {v=$(i+1); gsub(/"/,"",v); return v} return "" }
                function has(key, i) {for(i=1;i<=NF;i++) if($i==key) return 1; return 0}
                function negated(key, i) {for(i=2;i<=NF;i++) if($i==key && $(i-1)=="!") return 1; return 0}
                function marked(v) {return v==mark || v==mark "/0xffffffff"}
                function order(c) {return restoreline[c]<socketline[c] && socketline[c]<saveline[c] && saveline[c]<captureline[c]}
                function decimal(v, i,n,d) {
                    if(v !~ /^0x/) return v+0
                    n=0; for(i=3;i<=length(v);i++){d=index("0123456789abcdef",substr(v,i,1))-1;if(d<0)return -1;n=n*16+d} return n
                }
                BEGIN {proto=(kind=="nat"?"tcp":"udp"); target=(kind=="nat"?"REDIRECT":"TPROXY"); forced=(kind=="nat"?fr:ft)}
                $1==":" chain {decl++}
                $1==":" chain "_force" {fdecl++}
                $1=="-A" && val("--comment")==tag {
                    c=$2;j=val("-j");p=val("-p")
                    if(negated("--comment") || negated("-p"))bad=1
                    if(c=="PREROUTING") {
                        if(j=="RETURN" && val("--match-set")==deny && has("src")) {denycount++; denyline=NR;if(negated("--match-set"))bad=1}
                        if(j==chain || j==chain "_force") {
                            if(!firstjump) firstjump=NR
                            if(j==chain) {jumps++;if(p!="" && p!=proto)bad=1}
                            else {fjumps++; if(p!=proto || negated("--dscp") || val("--dscp")=="" || decimal(val("--dscp"))!=dscp+0) bad=1}
                        }
                    }
                    if(c!=chain && c!=chain "_force") next
                    if(j==target) {
                        captures[c]++;captureline[c]=NR; want=(c==chain?(kind=="nat"?redirect:tproxy):forced)
                        if(p!=proto || want=="") bad=1
                        if(kind=="nat") {if(val("--to-ports")!=want) bad=1}
                        else if(val("--on-port")!=want || val("--on-ip")!=ip || !marked(val("--tproxy-mark"))) bad=1
                    }
                    if(kind=="mangle") {
                        if(j=="MARK" && has("--transparent")) {sockets[c]++;socketline[c]=NR; if(p!="udp" || negated("--transparent") || !marked(val("--set-xmark"))) bad=1}
                        if(j=="CONNMARK" && has("--restore-mark")) {
                            restores[c]++;restoreline[c]=NR;state=val("--ctstate")
                            if(negated("--ctstate") || (state!="RELATED,ESTABLISHED" && state!="ESTABLISHED,RELATED") || val("--nfmask")!="0xffffffff" || val("--ctmask")!="0xffffffff")bad=1
                            if(c==chain && p!="udp")bad=1
                            if(c==chain "_force" && p!="" && p!="udp")bad=1
                        }
                        if(j=="CONNMARK" && has("--save-mark")) {
                            saves[c]++;saveline[c]=NR
                            if(p!="udp" || !negated("--mark") || (val("--mark")!="0x0" && val("--mark")!="0x0/0xffffffff") || val("--nfmask")!="0xffffffff" || val("--ctmask")!="0xffffffff")bad=1
                        }
                    }
                }
                END {
                    if(bad || decl!=1 || captures[chain]!=1 || jumps<1 || denycount!=1 || denyline>=firstjump) exit 1
                    if(kind=="mangle" && (sockets[chain]!=1 || restores[chain]!=1 || saves[chain]!=1 || !order(chain))) exit 1
                    if(forced!="" && (fdecl!=1 || captures[chain "_force"]!=1 || fjumps<1)) exit 1
                    if(forced!="" && kind=="mangle" && (sockets[chain "_force"]!=1 || restores[chain "_force"]!=1 || saves[chain "_force"]!=1 || !order(chain "_force"))) exit 1
                }' || return 77
        done
    done
    _hv_ipsets && _hv_routes
}
_hv_stopped() {
    for _hv_f in 4 6; do
        for _hv_t in nat mangle; do
            _hv_view=$(_hv_read_table "$_hv_f" "$_hv_t") || return 77
            [ "${#_hv_view}" -le 262144 ] || return 77
            printf '%s\n' "$_hv_view" | awk -v c="$_hv_chain" -v tag="$_hv_tag" '
              $1==":" c || index($1,":" c "_")==1 {bad=1}
              $1=="-A" {for(i=1;i<NF;i++)if($i=="--comment"){v=$(i+1);gsub(/"/,"",v);if(v==tag)bad=1}}
              END{exit bad?1:0}' || return 77
        done
        _hv_rules=$(_hv_read_rules "$_hv_f") || return 77
        printf '%s\n' "$_hv_rules" | awk -v m="$_hv_mark" -v t="$_hv_table" '{for(i=1;i<NF;i++){if($i=="fwmark"){split($(i+1),p,"/");if(p[1]==m)bad=1}if($i=="lookup" && $(i+1)==t)bad=1}}END{exit bad?1:0}' || return 77
        _hv_r=$(_hv_read_routes "$_hv_f" "$_hv_table") || return 77
        [ -z "$_hv_r" ] || return 77
    done
    _hv_sets=$(_hv_read_ipsets) || return 77
    [ "${#_hv_sets}" -le 262144 ] || return 77
    printf '%s\n' "$_hv_sets" | awk -v deny="$_hv_deny" '$1=="create" && ($2==deny || $2~/^(geo_override|geo_exclude|user_exclude)6?$/){bad=1}END{exit bad?1:0}' || return 77
    [ ! -L /opt/etc/ndm/netfilter.d/proxy.sh ] || return 77
    if [ -e /opt/etc/ndm/netfilter.d/proxy.sh ]; then
        _hv_file /opt/etc/ndm/netfilter.d/proxy.sh || return 77
        [ ! -s /opt/etc/ndm/netfilter.d/proxy.sh ] || return 77
    fi
    [ ! -e /opt/etc/ndm/schedule.d/00-xkeen-hotspot-sync.sh ] && [ ! -L /opt/etc/ndm/schedule.d/00-xkeen-hotspot-sync.sh ]
}
_hv_snapshot() {
    # Fixed bounded read-only views, no rule rendering or mutable counters.
    for _hv_f in 4 6; do
        for _hv_t in nat mangle; do
            _hv_v=$(_hv_read_table "$_hv_f" "$_hv_t") || return 77
            [ "${#_hv_v}" -le 262144 ] || return 77
            printf '%s\n' "$_hv_v" | awk -v c="$_hv_chain" -v tag="$_hv_tag" '
              $1==":" c || index($1,":" c "_")==1 {gsub(/\[[0-9]+:[0-9]+\]/,"[0:0]");print;next}
              $1=="-A" {for(i=1;i<NF;i++)if($i=="--comment"){v=$(i+1);gsub(/"/,"",v);if(v==tag){print;break}}}'
        done
        _hv_r=$(_hv_read_routes "$_hv_f" "$_hv_table") || return 77
        _hv_u=$(_hv_read_rules "$_hv_f") || return 77
        [ "${#_hv_r}" -le 65536 ] && [ "${#_hv_u}" -le 65536 ] || return 77
        printf '%s\n' "$_hv_r"
        printf '%s\n' "$_hv_u" | awk -v m="$_hv_mark" -v t="$_hv_table" '{owned=0;for(i=1;i<NF;i++){if($i=="fwmark"){split($(i+1),p,"/");if(p[1]==m)owned=1}if($i=="lookup" && $(i+1)==t)owned=1}if(owned)print}'
    done
    _hv_v=$(_hv_read_ipsets) || return 77
    [ "${#_hv_v}" -le 262144 ] || return 77
    printf '%s\n' "$_hv_v" | awk -v deny="$_hv_deny" '$1=="create" && ($2==deny || $2~/^(ext_exclude|user_exclude|geo_exclude|geo_override)6?$/){print}'
    for _hv_p in /opt/etc/ndm/netfilter.d/proxy.sh /opt/etc/ndm/schedule.d/00-xkeen-hotspot-sync.sh; do
        if [ -e "$_hv_p" ] || [ -L "$_hv_p" ]; then _hv_file "$_hv_p" || return 77; sha256sum "$_hv_p" || return 77
        else printf 'absent %s\n' "$_hv_p"; fi
    done
}
_hv_main() {
    [ "$#" = 5 ] && [ "$(id -u)" = 0 ] || return 76
    _hv_phase=$1; _na_role=$2; _na_action=$3; _na_mode=$4; _hv_expect=$5
    case "$_hv_phase:$_hv_expect" in pre:running|pre:stopped|pre:unchanged|post:running|post:stopped|post:unchanged) ;;*) return 76;;esac
    _hv_file /opt/lib/xkeen/native-operation-gate.sh && _hv_file /opt/lib/xkeen/native-admission-entry.sh || return 76
    . /opt/lib/xkeen/native-operation-gate.sh
    . /opt/lib/xkeen/native-admission-entry.sh
    _na_descendant_ok || return 77
    _hv_base || return $?
    _hv_auto=$(_hv_literal "$_hv_init" start_auto '"') || return 76
    case "$_hv_auto" in on|off) ;;*) return 76;;esac
    _hv_expected=running
    if [ "$_na_action" = stop ] || [ "$_na_mode:$_hv_auto:$_na_action" = automatic:off:restart ]; then _hv_expected=stopped; fi
    [ "$_na_mode:$_hv_auto:$_na_action" != automatic:off:start ] || _hv_expected=unchanged
    [ "$_hv_expect" = "$_hv_expected" ] || return 76
    _hv_context=$(sha256sum "$_na_call_dir/context") || return 77
    _hv_context=${_hv_context%% *}
    _hv_baseline=$_na_call_dir/hook-preflight
    _hv_saved="v1 $_hv_context $_hv_expect"
    if [ "$_hv_phase" = pre ]; then
        # Capability queries permit absent running state, but never skip a family.
        for _hv_f in 4 6; do
            for _hv_t in nat mangle; do _hv_cap=$(_hv_read_table "$_hv_f" "$_hv_t") || return 76; [ "${#_hv_cap}" -le 262144 ] || return 76; done
            _hv_cap=$(_hv_read_rules "$_hv_f") || return 76
            [ "${#_hv_cap}" -le 65536 ] || return 76
        done
        _hv_cap=$(_hv_read_ipsets) || return 76
        [ "${#_hv_cap}" -le 262144 ] || return 76
        [ "$_hv_expect" != running ] || _hv_inputs || return 76
        if [ "$_hv_expect" = running ] && { [ -e /opt/etc/ndm/netfilter.d/proxy.sh ] || [ -L /opt/etc/ndm/netfilter.d/proxy.sh ]; }; then
            _hv_file /opt/etc/ndm/netfilter.d/proxy.sh || return 76
            [ ! -s /opt/etc/ndm/netfilter.d/proxy.sh ] || _hv_profile pre || return 76
        fi
        _hv_prior=none
        if [ "$_hv_expect" = unchanged ]; then
            _hv_state=$(_hv_snapshot) || return 77
            _hv_prior=$(printf '%s\n' "$_hv_state" | sha256sum); _hv_prior=${_hv_prior%% *}
        fi
        _na_descendant_ok || return 77
        (set -C; printf '%s %s\n' "$_hv_saved" "$_hv_prior" > "$_hv_baseline") || return 77
    else
        _na_small_file "$_hv_baseline" || return 77
        IFS= read -r _hv_line < "$_hv_baseline" || return 77
        [ "$_ng_meta_size" = "$(( ${#_hv_line} + 1 ))" ] || return 77
        _hv_prior=${_hv_line##* }
        [ "$_hv_line" = "$_hv_saved $_hv_prior" ] || return 77
        case "$_hv_expect" in
            running) [ "$_hv_prior" = none ] && _hv_inputs && _hv_profile && _hv_running || return 77;;
            stopped) [ "$_hv_prior" = none ] && _hv_stopped || return 77;;
            unchanged)
                _hv_state=$(_hv_snapshot) || return 77
                _hv_now=$(printf '%s\n' "$_hv_state" | sha256sum); _hv_now=${_hv_now%% *}
                [ "$_hv_now" = "$_hv_prior" ] || return 77;;
        esac
        _na_descendant_ok || return 77
        rm "$_hv_baseline" || return 77
    fi
}
_hv_main "$@"
exit $?
