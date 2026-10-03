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
_nu_observer_chain() {
    _nu_chain_pid=$_nu_verify_pid; _nu_chain_depth=0; _nu_chain=
    while [ "$_nu_chain_depth" -lt 16 ]; do
        _native_gate_proc "$_nu_chain_pid" || return 77
        _nu_chain="$_nu_chain $_nu_chain_pid:$_ng_proc_start"
        if [ "$_nu_chain_pid" = "$_nu_wrapper_pid" ]; then
            [ "$_nu_chain_depth" -gt 0 ] && [ "$_ng_proc_start" = "$_nu_wrapper_start" ] || return 77
            [ "$_nu_verify_mode" != direct ] || [ "$_nu_chain_depth" = 1 ] || return 77
            printf '%s' "$_nu_chain"
            return 0
        fi
        [ "$_ng_parent" -gt 0 ] && [ "$_ng_parent" != "$_nu_chain_pid" ] || return 77
        _nu_chain_pid=$_ng_parent; _nu_chain_depth=$((_nu_chain_depth + 1))
    done
    return 77
}
_nu_observation_context() {
    # Read-only verifier invocation, not body/stage/finish authority. Preflight
    # precedes body binding; postflight runs after that body has exited, so its
    # live parent is the wrapper. Never authenticate by requiring a live body.
    [ "$#" = 2 ] || return 76
    case "$1" in pre|post) ;; *) return 76;; esac
    case "$2" in direct|observer) ;; *) return 76;; esac
    _nu_verify_phase=$1; _nu_verify_mode=$2
    _nu_load_call || return $?
    _nu_verify_gate=$_nu_gate_record; _nu_verify_context=$_nu_context
    _native_gate_self || return 77
    _nu_verify_pid=$_ng_self_pid; _nu_verify_start=$_ng_self_start
    _nu_verify_chain=$(_nu_observer_chain) || return 77
    if [ "$_nu_verify_phase" = pre ]; then
        for _nu_verify_name in body staged exec.used exec.argv completed terminal.argv init-parent.argv packages.initial packages.post; do
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
    [ "$_ng_proc_start" = "$_nu_verify_start" ] || return 77
    [ "$(_nu_observer_chain)" = "$_nu_verify_chain" ] || return 77
    if [ "$_nu_verify_phase" = pre ]; then
        for _nu_verify_name in body staged exec.used exec.argv completed terminal.argv init-parent.argv packages.initial packages.post; do
            [ ! -e "$_nu_call/$_nu_verify_name" ] && [ ! -L "$_nu_call/$_nu_verify_name" ] || return 77
        done
    fi
    return 0
}
native_update_verifier_context() {
    [ "$#" = 1 ] || return 76
    _nu_observation_context "$1" direct
}
native_update_observer_context() {
    # Bounded nested read-only kernel queries may run below coreutils-timeout.
    # This never grants the strict direct-child body/stage/finish privileges.
    [ "$#" = 1 ] || return 76
    _nu_observation_context "$1" observer
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
_nu_installed_dependencies() {
    # One data reader for the three fixed update queries, never a command path.
    [ "$#" = 1 ] || return 76
    _nu_dep_query=$1
    _nu_load_call || return 77
    case "$_nu_dep_query" in
        "$_nu_call/environment.pre/packages"|"$_nu_call/packages.initial/packages"|"$_nu_call/packages.post/packages") ;;
        *) return 76;;
    esac
    _native_gate_directory "${_nu_dep_query%/*}" 0700 || return 77
    [ ! -L "$_nu_dep_query" ] && [ -f "$_nu_dep_query" ] && _na_file_ok /opt/lib/opkg/status || return 76
    _native_gate_metadata "$_nu_dep_query" || return 76
    [ "$_ng_meta_mode:$_ng_meta_uid:$_ng_meta_links" = 8180:0:1 ] && [ "$_ng_meta_size" -le 262144 ] || return 76
    _native_gate_metadata /opt/lib/opkg/status || return 76
    [ "$_ng_meta_size" -gt 0 ] && [ "$_ng_meta_size" -le 524288 ] || return 76
    for _nu_dep_file in "$_nu_dep_query" /opt/lib/opkg/status; do
        _nu_dep_last=$(tail -c 1 "$_nu_dep_file" | od -v -b) || return 76
        set -- $_nu_dep_last
        [ "$#" = 3 ] && [ "$1:$2:$3" = 0000000:012:0000001 ] || return 76
    done
    LC_ALL=C awk -v query="$_nu_dep_query" '
      BEGIN {
        split("curl jq ip-full iptables ipset ca-bundle coreutils-uname coreutils-nohup conntrack", names, " ")
        for(i in names) required[names[i]]=1
        while((read=(getline line < query))>0) {
          if(line !~ /^[A-Za-z0-9][A-Za-z0-9+_.-]* - [A-Za-z0-9][A-Za-z0-9+_.:~()-]*$/) {bad=1;continue}
          split(line, parts, " "); if(++listed[parts[1]]!=1) bad=1
          versions[parts[1]]=parts[3]
        }
        if(read<0) bad=1; close(query); RS=""
      }
      {
        count=split($0, rows, "\n"); package=""; version=""; state=""; previous=""; p=0; v=0; s=0
        for(i=1;i<=count;i++) {
          if(rows[i] ~ /^[ \t]/ && previous ~ /^(Package|Version|Status): /) bad=1
          if(rows[i] ~ /^Package: /) {package=substr(rows[i],10);p++}
          if(rows[i] ~ /^Version: /) {version=substr(rows[i],10);v++}
          if(rows[i] ~ /^Status: /) {state=substr(rows[i],9);s++}
          if(rows[i] !~ /^[ \t]/) previous=rows[i]
        }
        if(p!=1 || rows[1] !~ /^Package: [A-Za-z0-9][A-Za-z0-9+_.-]*$/ || ++seen[package]!=1) bad=1
        if(package in required) {
          if(p!=1 || v!=1 || s!=1 || ++healthy[package]!=1 || listed[package]!=1 || version!=versions[package] || (state!="install ok installed" && state!="install user installed")) bad=1
        }
      }
      END {for(package in required) if(healthy[package]!=1) bad=1; if(bad)exit 76}
    ' /opt/lib/opkg/status
}
_nu_package_body_context() {
    _nu_load_call || return 77
    _nu_read_record "$_nu_call/body" || return 77
    _nu_pkg_body=$_nu_record
    _native_gate_self || return 77
    _nu_pkg_self=$_ng_self_pid
    [ "$_nu_pkg_body" = "v1 $_ng_self_pid $_ng_self_start $_nu_nonce" ] || return 77
    _native_gate_proc "$_ng_self_pid" || return 77
    [ "$_ng_parent" = "$_nu_wrapper_pid" ] || return 77
    _native_gate_proc "$_nu_wrapper_pid" || return 77
    [ "$_ng_proc_start" = "$_nu_wrapper_start" ] || return 77
    if [ -e "$_nu_call/exec.used" ] || [ -L "$_nu_call/exec.used" ]; then
        _nu_init_body_live || return 77
        _nu_pkg_phase=post
    else
        for _nu_pkg_absent in staged exec.argv completed terminal.argv init-parent.argv; do
            [ ! -e "$_nu_call/$_nu_pkg_absent" ] && [ ! -L "$_nu_call/$_nu_pkg_absent" ] || return 77
        done
        _nu_pkg_phase=initial
    fi
}
native_update_packages_cache() {
    # Feed the actual native info_packages classifier; never run an installer.
    # One bounded query per actual bound body phase in the existing RAM scope.
    [ "$#" = 0 ] || return 76
    _nu_package_body_context || return 77
    _nu_pkg_gate=$_nu_gate_record; _nu_pkg_context=$_nu_context
    _nu_pkg_original_body=$_nu_pkg_body; _nu_pkg_original_phase=$_nu_pkg_phase
    _nu_pkg_inputs=
    for _nu_pkg_file in /opt/bin/opkg /opt/libexec/timeout-coreutils /opt/lib/opkg/status /opt/etc/opkg.conf; do
        _nu_file_hash "$_nu_pkg_file" || return 76
        _nu_pkg_inputs="$_nu_pkg_inputs $_nu_hash:$_ng_meta_mode:$_ng_meta_uid:$_ng_meta_links"
    done
    [ -x /opt/bin/opkg ] && [ -x /opt/libexec/timeout-coreutils ] || return 76
    _nu_pkg_query=$_nu_call/packages.$_nu_pkg_phase
    (umask 077; mkdir "$_nu_pkg_query") 2>/dev/null || return 77
    _native_gate_directory "$_nu_pkg_query" 0700 || return 77
    if [ "$_nu_pkg_phase" = post ]; then
        (umask 077; set -C; dd if="/proc/$_nu_pkg_self/cmdline" bs=128 count=1 > "$_nu_pkg_query/argv.before" 2>/dev/null) || return 77
        _na_small_file "$_nu_pkg_query/argv.before" || return 77
        _nu_pkg_argv=$(sha256sum "$_nu_pkg_query/argv.before" 2>/dev/null) || return 77
        [ "${_nu_pkg_argv%% *}" = f7549ee949d9d02fa5ba2e6586f286394c31c2243d32d7ed1b520aa37d5439fe ] || return 77
    fi
    (umask 077; set -C; ulimit -f 512 || exit 76
        native_admission_strip
        /opt/libexec/timeout-coreutils -s KILL 15 /opt/bin/opkg list-installed > "$_nu_pkg_query/packages" 2>/dev/null) || return 77
    _nu_installed_dependencies "$_nu_pkg_query/packages" || return 77
    _nu_pkg_query_hash=$(sha256sum "$_nu_pkg_query/packages" 2>/dev/null) || return 77
    _packages_cache=$(cat "$_nu_pkg_query/packages") || return 77
    if [ "$_nu_pkg_original_phase" = post ]; then
        (umask 077; set -C; dd if="/proc/$_nu_pkg_self/cmdline" bs=128 count=1 > "$_nu_pkg_query/argv.after" 2>/dev/null) || return 77
    fi
    _nu_installed_dependencies "$_nu_pkg_query/packages" || return 77
    _nu_pkg_after=
    for _nu_pkg_file in /opt/bin/opkg /opt/libexec/timeout-coreutils /opt/lib/opkg/status /opt/etc/opkg.conf; do
        _nu_file_hash "$_nu_pkg_file" || return 77
        _nu_pkg_after="$_nu_pkg_after $_nu_hash:$_ng_meta_mode:$_ng_meta_uid:$_ng_meta_links"
    done
    [ "$_nu_pkg_inputs" = "$_nu_pkg_after" ] && [ -x /opt/bin/opkg ] && [ -x /opt/libexec/timeout-coreutils ] || return 77
    _native_gate_directory "$_nu_pkg_query" 0700 || return 77
    [ ! -L "$_nu_pkg_query/packages" ] && [ -f "$_nu_pkg_query/packages" ] || return 77
    _native_gate_metadata "$_nu_pkg_query/packages" || return 77
    [ "$_ng_meta_mode:$_ng_meta_uid:$_ng_meta_links" = 8180:0:1 ] && [ "$_ng_meta_size" -le 262144 ] || return 77
    [ "$(sha256sum "$_nu_pkg_query/packages" 2>/dev/null)" = "$_nu_pkg_query_hash" ] || return 77
    if [ "$_nu_pkg_original_phase" = post ]; then
        for _nu_pkg_argv_name in argv.before argv.after; do
            _na_small_file "$_nu_pkg_query/$_nu_pkg_argv_name" || return 77
            _nu_pkg_argv=$(sha256sum "$_nu_pkg_query/$_nu_pkg_argv_name" 2>/dev/null) || return 77
            [ "${_nu_pkg_argv%% *}" = f7549ee949d9d02fa5ba2e6586f286394c31c2243d32d7ed1b520aa37d5439fe ] || return 77
        done
    fi
    _nu_package_body_context || return 77
    [ "$_nu_gate_record" = "$_nu_pkg_gate" ] && [ "$_nu_context" = "$_nu_pkg_context" ] &&
        [ "$_nu_pkg_body" = "$_nu_pkg_original_body" ] && [ "$_nu_pkg_phase" = "$_nu_pkg_original_phase" ] || return 77
}
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
_nu_terminal_context() {
    _nu_load_call || return $?
    _native_gate_self || return 77
    _nu_term_pid=$_ng_self_pid; _nu_term_start=$_ng_self_start
    _nu_read_record "$_nu_call/body" || return 77
    _nu_term_body=$_nu_record
    [ "$_nu_term_body" = "v1 $_nu_term_pid $_nu_term_start $_nu_nonce" ] || return 77
    _native_gate_proc "$_nu_term_pid" || return 77
    [ "$_ng_parent" = "$_nu_wrapper_pid" ] || return 77
    _native_gate_proc "$_nu_wrapper_pid" || return 77
    [ "$_ng_proc_start" = "$_nu_wrapper_start" ] || return 77
    _nu_read_record "$_nu_call/staged" || return 77
    _nu_term_staged=$_nu_record
    _nu_dispatcher_hash || return 77
    _nu_term_hash=$_nu_hash
    [ "$_nu_term_staged" = "$_nu_term_body $_nu_term_hash" ] || return 77
    _native_gate_directory "$_nu_call/exec.used" 0700 || return 77
    _na_small_file "$_nu_call/exec.argv" || return 77
    _nu_term_argv=$(sha256sum "$_nu_call/exec.argv" 2>/dev/null) || return 77
    [ "${_nu_term_argv%% *}" = f7549ee949d9d02fa5ba2e6586f286394c31c2243d32d7ed1b520aa37d5439fe ] || return 77
}
_nu_terminal_recheck() {
    _nu_terminal_context || return 77
    [ "$_nu_gate_record" = "$_nu_done_gate" ] && [ "$_nu_context" = "$_nu_done_context" ] &&
        [ "$_nu_term_body" = "$_nu_done_body" ] && [ "$_nu_term_staged" = "$_nu_done_staged" ] &&
        [ "$_nu_term_hash" = "$_nu_done_hash" ] || return 77
    _na_small_file "$_nu_call/terminal.argv" || return 77
    _nu_done_argv=$(sha256sum "$_nu_call/terminal.argv" 2>/dev/null) || return 77
    [ "${_nu_done_argv%% *}" = f7549ee949d9d02fa5ba2e6586f286394c31c2243d32d7ed1b520aa37d5439fe ] || return 77
}
native_update_complete() {
    # Future fixed native successful terminal only. This producer does not
    # perform postconditions, release admission or authorize another writer.
    # Wiring it before/after arbitrary statements is not updater acceptance.
    [ "$#" = 1 ] && [ "$1" = 0 ] || return 77
    _nu_terminal_context || return $?
    _nu_done_gate=$_nu_gate_record; _nu_done_context=$_nu_context
    _nu_done_body=$_nu_term_body; _nu_done_staged=$_nu_term_staged; _nu_done_hash=$_nu_term_hash
    for _nu_done_name in completed terminal.argv; do
        [ ! -e "$_nu_call/$_nu_done_name" ] && [ ! -L "$_nu_call/$_nu_done_name" ] || return 77
    done
    # Prove actual post-exec argv again, not merely the stored phase receipt.
    (umask 077; set -C; dd if="/proc/$_nu_term_pid/cmdline" bs=128 count=1 > "$_nu_call/terminal.argv" 2>/dev/null) || return 77
    _nu_terminal_recheck || return 77
    _nu_done_record="$_nu_done_body updated $_nu_done_hash"
    (umask 077; set -C; printf '%s\n' "$_nu_done_record" > "$_nu_call/completed") 2>/dev/null || return 77
    # On late drift retain all evidence. Never replace a receipt or retry it.
    _nu_terminal_recheck || return 77
    _nu_read_record "$_nu_call/completed" || return 77
    [ "$_nu_record" = "$_nu_done_record" ] || return 77
}

_nu_init_body_live() {
    # Called only with the authenticated fixed update context. The native body
    # must still be executing; an already completed/terminal body cannot borrow.
    _nu_read_record "$_nu_call/body" || return 77
    _nu_init_body=$_nu_record
    set -- $_nu_init_body
    [ "$#" = 4 ] && [ "$1" = v1 ] && [ "$4" = "$_nu_nonce" ] || return 77
    _native_gate_decimal "$2" && _native_gate_decimal "$3" || return 77
    _nu_init_body_pid=$2; _nu_init_body_start=$3
    [ "$_nu_init_body" = "v1 $_nu_init_body_pid $_nu_init_body_start $_nu_nonce" ] || return 77
    _native_gate_proc "$_nu_init_body_pid" || return 77
    [ "$_ng_proc_start" = "$_nu_init_body_start" ] && [ "$_ng_parent" = "$_nu_wrapper_pid" ] || return 77
    _native_gate_proc "$_nu_wrapper_pid" || return 77
    [ "$_ng_proc_start" = "$_nu_wrapper_start" ] || return 77
    _nu_read_record "$_nu_call/staged" || return 77
    _nu_init_staged=$_nu_record
    _nu_dispatcher_hash || return 77
    [ "$_nu_init_staged" = "$_nu_init_body $_nu_hash" ] || return 77
    _native_gate_directory "$_nu_call/exec.used" 0700 || return 77
    _na_small_file "$_nu_call/exec.argv" || return 77
    _nu_init_argv=$(sha256sum "$_nu_call/exec.argv" 2>/dev/null) || return 77
    [ "${_nu_init_argv%% *}" = f7549ee949d9d02fa5ba2e6586f286394c31c2243d32d7ed1b520aa37d5439fe ] || return 77
    for _nu_init_absent in completed terminal.argv; do
        [ ! -e "$_nu_call/$_nu_init_absent" ] && [ ! -L "$_nu_call/$_nu_init_absent" ] || return 77
    done
}
native_update_init_enter() {
    # One direct forced init wrapper, before it creates the existing call.init.
    # This is not a general descendant writer privilege or a second executor.
    [ "$#" = 0 ] || return 76
    _nu_load_call && _nu_init_body_live || return 77
    _nu_init_gate=$_nu_gate_record; _nu_init_context=$_nu_context
    _nu_init_original_body=$_nu_init_body; _nu_init_original_staged=$_nu_init_staged
    _native_gate_self || return 77
    _nu_init_pid=$_ng_self_pid; _nu_init_start=$_ng_self_start
    _native_gate_proc "$_nu_init_pid" || return 77
    [ "$_ng_parent" = "$_nu_init_body_pid" ] || return 77
    [ ! -e "$_nu_call/init-parent.argv" ] && [ ! -L "$_nu_call/init-parent.argv" ] || return 77
    (umask 077; set -C; dd if="/proc/$_nu_init_body_pid/cmdline" bs=128 count=1 > "$_nu_call/init-parent.argv" 2>/dev/null) || return 77
    _na_small_file "$_nu_call/init-parent.argv" || return 77
    _nu_init_parent_argv=$(sha256sum "$_nu_call/init-parent.argv" 2>/dev/null) || return 77
    [ "${_nu_init_parent_argv%% *}" = f7549ee949d9d02fa5ba2e6586f286394c31c2243d32d7ed1b520aa37d5439fe ] || return 77
    _nu_load_call && _nu_init_body_live || return 77
    [ "$_nu_gate_record" = "$_nu_init_gate" ] && [ "$_nu_context" = "$_nu_init_context" ] &&
        [ "$_nu_init_body" = "$_nu_init_original_body" ] && [ "$_nu_init_staged" = "$_nu_init_original_staged" ] || return 77
    _native_gate_proc "$_nu_init_pid" || return 77
    [ "$_ng_proc_start" = "$_nu_init_start" ] && [ "$_ng_parent" = "$_nu_init_body_pid" ] || return 77
}
native_update_init_observer_context() (
    # Read-only proof for the existing init/hook wrappers and their verifiers.
    # Environment substitution is confined to this subshell, never inherited by
    # the native init/core. Fixed call.init still owns subordinate completion.
    [ "$#" = 0 ] || return 76
    _nu_init_hint_role=${XKEEN_ADMISSION_ROLE-}; _nu_init_hint_call=${XKEEN_ADMISSION_CALL-}
    case "$_nu_init_hint_role:${XKEEN_ADMISSION_ACTION-}" in
        update:update-xkeen|init:restart|hook:restart) ;;
        *) return 77;;
    esac
    _nu_read_record /tmp/.xkeen-admission/operation.lock.d/call.update/context || return 77
    set -- $_nu_record
    [ "$#" = 8 ] || return 77
    XKEEN_ADMISSION_ROLE=update; XKEEN_ADMISSION_ACTION=update-xkeen; XKEEN_ADMISSION_CALL=$4
    export XKEEN_ADMISSION_ROLE XKEEN_ADMISSION_ACTION XKEEN_ADMISSION_CALL
    _nu_load_call && _nu_init_body_live || return 77
    _nu_init_ob_gate=$_nu_gate_record; _nu_init_ob_context=$_nu_context
    _nu_init_ob_body=$_nu_init_body; _nu_init_ob_staged=$_nu_init_staged
    _na_small_file "$_nu_call/init-parent.argv" || return 77
    _nu_init_ob_argv=$(sha256sum "$_nu_call/init-parent.argv" 2>/dev/null) || return 77
    [ "${_nu_init_ob_argv%% *}" = f7549ee949d9d02fa5ba2e6586f286394c31c2243d32d7ed1b520aa37d5439fe ] || return 77
    _native_gate_self || return 77
    _nu_verify_pid=$_ng_self_pid; _nu_verify_mode=observer
    if [ "$_nu_init_hint_role" = update ]; then
        # Only during creation: this proof subshell's direct parent is the init
        # wrapper, which must itself be the post-exec body's immediate child.
        [ "$_nu_init_hint_call" = "$_nu_nonce" ] || return 77
        _native_gate_proc "$_nu_verify_pid" || return 77
        _nu_init_wrapper=$_ng_parent
        _native_gate_proc "$_nu_init_wrapper" || return 77
        _nu_init_wrapper_start=$_ng_proc_start
    else
        _native_gate_directory /tmp/.xkeen-admission/operation.lock.d/call.init 0700 || return 77
        _nu_read_record /tmp/.xkeen-admission/operation.lock.d/call.init/context || return 77
        _nu_init_record=$_nu_record
        set -- $_nu_record
        [ "$#" = 8 ] && [ "$1:$5:$6:$7:$8" = "v1:init:restart:forced:$XKEEN_GATE_TOKEN" ] || return 77
        _native_gate_decimal "$2" && _native_gate_decimal "$3" || return 77
        [ "${#4}" = 32 ] || return 77
        case "$4" in *[!0-9a-f]*) return 77;; esac
        [ "$_nu_init_record" = "v1 $2 $3 $4 init restart forced $XKEEN_GATE_TOKEN" ] || return 77
        [ "$_nu_init_hint_role" != init ] || [ "$4" = "$_nu_init_hint_call" ] || return 77
        _nu_init_wrapper=$2; _nu_init_wrapper_start=$3
    fi
    _native_gate_proc "$_nu_init_wrapper" || return 77
    [ "$_ng_proc_start" = "$_nu_init_wrapper_start" ] && [ "$_ng_parent" = "$_nu_init_body_pid" ] || return 77
    _nu_init_chain=$(_nu_observer_chain) || return 77
    case "$_nu_init_chain " in *" $_nu_init_wrapper:$_nu_init_wrapper_start "*) ;; *) return 77;; esac
    _nu_load_call && _nu_init_body_live || return 77
    [ "$_nu_gate_record" = "$_nu_init_ob_gate" ] && [ "$_nu_context" = "$_nu_init_ob_context" ] &&
        [ "$_nu_init_body" = "$_nu_init_ob_body" ] && [ "$_nu_init_staged" = "$_nu_init_ob_staged" ] || return 77
    _na_small_file "$_nu_call/init-parent.argv" || return 77
    [ "$(sha256sum "$_nu_call/init-parent.argv" 2>/dev/null)" = "$_nu_init_ob_argv" ] || return 77
    [ "$(_nu_observer_chain)" = "$_nu_init_chain" ] || return 77
    if [ "$_nu_init_hint_role" != update ]; then
        _native_gate_directory /tmp/.xkeen-admission/operation.lock.d/call.init 0700 || return 77
        _nu_read_record /tmp/.xkeen-admission/operation.lock.d/call.init/context || return 77
        [ "$_nu_record" = "$_nu_init_record" ] || return 77
    fi
)
