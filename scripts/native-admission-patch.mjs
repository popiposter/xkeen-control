// Disabled source experiment for jameszeroX/XKeen@5aaece27a70d5bd002c615248614914ebbc4569d.
// Upstream fragments: Copyright (c) 2026, jameszeroX; BSD-3-Clause.
// See native-xkeen-stop-fix.LICENSE. No installation, fetching or execution.
import { createHash } from 'node:crypto'
import { closeSync, fstatSync, mkdirSync, openSync, readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { pathToFileURL } from 'node:url'
import { patchSource, sourceSHA256 as initSHA256 } from './native-xkeen-stop-fix.mjs'

export const dispatcherSHA256 = 'feda355231551c2776da78453b950bfe10b59225b9a5c32c18c333649cc2e542'
const digest = bytes => createHash('sha256').update(bytes).digest('hex')
const fence = `# SOURCE-ONLY FENCE: incomplete admission candidate. Never install.
# No environment variable or command argument enables this artifact.
printf '%s\\n' 'native admission candidate disabled: hook/monitor/recovery integration incomplete' >&2
exit 76
# END SOURCE-ONLY FENCE
`
function replaceOnce(text, before, after) {
  if (text.split(before).length !== 2) throw new Error('native anchor is not unique')
  return text.replace(before, after)
}
function disabled(text) {
  if (!text.startsWith('#!/bin/sh\n')) throw new Error('missing script header')
  return '#!/bin/sh\n' + fence + text.slice('#!/bin/sh\n'.length)
}
function entryPrelude(role) {
  const normalize = role === 'hook' ? `
[ "$#" = 0 ] || exit 76
` : role === 'dispatcher' ? `
[ "$#" = 1 ] || exit 76
case "$1" in
    -start|-stop|-restart) _na_entry_action=\${1#-};;
    -uk|-uk_post_update) _na_entry_action=update-xkeen;;
    *) exit 76;;
esac
_na_entry_mode=forced
` : `
case "$#:$1:\${2-}" in
    1:start:|1:restart:) _na_entry_action=$1; _na_entry_mode=automatic;;
    1:stop:|2:stop:on) _na_entry_action=stop; _na_entry_mode=forced;;
    2:start:on|2:restart:on) _na_entry_action=$1; _na_entry_mode=forced;;
    *) exit 76;;
esac
`
  return `# BEGIN NATIVE ADMISSION ENTRY
${normalize}
PATH=/opt/bin:/opt/sbin:/usr/bin:/bin; export PATH
[ "$(id -u)" = 0 ] || exit 76
# Validate the fixed entry library before sourcing any of its code.
_na_boot_path=/opt/lib/xkeen/native-admission-entry.sh
while :; do
    [ ! -L "$_na_boot_path" ] || exit 76
    _na_boot_meta=$(stat -t "$_na_boot_path" 2>/dev/null) || exit 76
    # Parsing in a subshell preserves original native positional arguments.
    (
        set -- $_na_boot_meta
        [ "$#" -ge 9 ] && [ "$5" = 0 ] || exit 76
        case "$4" in ''|*[!0-9a-fA-F]*) exit 76;; esac
        [ "$((0x$4 & 0022))" -eq 0 ] || exit 76
        if [ "$_na_boot_path" = /opt/lib/xkeen/native-admission-entry.sh ]; then
            [ -f "$_na_boot_path" ] && [ "$9" = 1 ] || exit 76
        else
            [ -d "$_na_boot_path" ] || exit 76
        fi
    ) || exit 76
    [ "$_na_boot_path" != / ] || break
    _na_boot_path=\${_na_boot_path%/*}
    [ -n "$_na_boot_path" ] || _na_boot_path=/
done
. /opt/lib/xkeen/native-admission-entry.sh
${role === 'hook' ? 'native_admission_hook_enter' : role === 'dispatcher' ? `if [ "$_na_entry_action" = update-xkeen ]; then
    _na_file_ok /opt/lib/xkeen/native-update-context.sh || exit 76
    . /opt/lib/xkeen/native-update-context.sh
    native_update_enter "$1"
else
    native_admission_enter dispatcher "$_na_entry_action" "$_na_entry_mode"
fi` : `native_admission_enter ${role} "$_na_entry_action" "$_na_entry_mode"`} || exit $?
[ "$_na_body" = 1 ] || exit 0
XKEEN_FOREGROUND=1; export XKEEN_FOREGROUND
# END NATIVE ADMISSION ENTRY
`
}
function addEntry(text, role) { return '#!/bin/sh\n' + entryPrelude(role) + text.slice('#!/bin/sh\n'.length) }

function patchUpdaterFailures(text) {
  // Healthy installed dependencies are required before the admitted update
  // body. Native -uk uses GitHub archives, never the refreshed Entware feeds.
  const updateBegin = text.indexOf('        -uk)    # Обновление XKeen\n')
  const updateEnd = text.indexOf('        -uk_post_update)\n', updateBegin)
  if (updateBegin < 0 || updateEnd <= updateBegin) throw new Error('native update prerequisite anchor missing')
  const initial = replaceOnce(text.slice(updateBegin, updateEnd), '            test_entware\n',
    '            # Entware feed refresh is unused with all dependencies installed.\n')
  const checkedInitial = replaceOnce(initial, '            backup_xkeen\n', '            backup_xkeen || exit 1\n')
  text = text.slice(0, updateBegin) + checkedInitial + text.slice(updateEnd)
  // Patch only the pinned post-update branch. Do not reinterpret optional
  // native probes/no-op return codes or claim to repair internal module errors.
  const begin = text.indexOf('        -uk_post_update)\n')
  const end = text.indexOf('        -ux)', begin)
  if (begin < 0 || end < begin) throw new Error('native updater branch missing')
  let post = text.slice(begin, end)
  for (const command of ['register_cron_initd', 'register_xkeen_initd', 'create_xkeen_cfg',
    'delete_register_xkeen', 'register_xkeen_list', 'register_xkeen_control', 'register_xkeen_status', 'fixed_register_packages']) {
    post = replaceOnce(post, `            ${command}\n`, `            ${command} || exit 1\n`)
  }
  for (const command of ['chmod 700 "$xkeen_cfg" 2>/dev/null', 'chmod 600 "$xkeen_config" 2>/dev/null',
    '"$initd_file" restart on >/dev/null 2>&1']) {
    post = replaceOnce(post, command + '\n', command + ' || exit 1\n')
  }
  text = text.slice(0, begin) + post + text.slice(end)
  const installedFlags = ['curl', 'jq', 'ip_full', 'iptables', 'ipset', 'cabundle', 'uname', 'nohup', 'conntrack']
  text = replaceOnce(text, '    *)\n        _load_packages_info\n        _ensure_installed_packages\n',
    `    -uk|-uk_post_update)
        # The checked native loader keeps its classifier but cannot self-heal.
        _load_packages_info || exit 1
        [ "${installedFlags.map(name => `$info_packages_${name}`).join(':')}" = "${installedFlags.map(() => 'installed').join(':')}" ] || exit 1
        _ensure_installed_packages || exit 1
        ;;
    *)
        _load_packages_info
        _ensure_installed_packages
`)
  return replaceOnce(text,
    '            grep -E "^[[:space:]]*-uk_post_update[[:space:]]*\\)" "$0" > /dev/null && exec /opt/bin/sh /opt/sbin/xkeen -uk_post_update\n',
    `            # A missing/failed handoff must not fall through to success.
            grep -E "^[[:space:]]*-uk_post_update[[:space:]]*\\)" /opt/sbin/xkeen > /dev/null || exit 1
            exec /opt/bin/sh /opt/sbin/xkeen -uk_post_update
            exit 1
`)
}

function patchForegroundHook(text) {
  text = replaceOnce(text,
    '# XKeen: Auto-generated file. DO NOT EDIT!\nPATH="/opt/bin:/opt/sbin:/sbin:/bin:/usr/sbin:/usr/bin"\n',
    '# XKeen: Auto-generated file. DO NOT EDIT!\n' + entryPrelude('hook') + 'PATH="/opt/bin:/opt/sbin:/sbin:/bin:/usr/sbin:/usr/bin"\n')
  // Only these native successful terminals have completed all hook writers.
  // Early ready/lock/no-op exits retain an incomplete parent, never settle it.
  for (const [start, end] of [
    ['    if [ -n "$_xkeen_cur_wan" ]', '\n    if _xkeen_rules_intact; then'],
    ['    if _xkeen_rules_intact; then', '\n    # Кэш готовых'],
    ['    if _xkeen_cache_valid; then', '\n    if [ -n "$port_donor" ]'],
  ]) {
    const a = text.indexOf(start), b = text.indexOf(end, a + start.length)
    if (a < 0 || b < a) throw new Error('hook finish anchor missing')
    const body = replaceOnce(text.slice(a, b), '        exit 0\n', '        native_admission_finish 0 || exit $?\n        exit 0\n')
    text = text.slice(0, a) + body + text.slice(b)
  }
  text = replaceOnce(text, '\nelse\n    # mkdir-lock', '\n    native_admission_finish 0 || exit $?\nelse\n    # mkdir-lock')
  return text
}

function patchNativeHookErrors(text) {
  const section = (start, end, edit) => {
    const a = text.indexOf(start), b = text.indexOf(end, a + start.length)
    if (a < 0 || b < a) throw new Error('native error propagation anchor missing')
    text = text.slice(0, a) + edit(text.slice(a, b)) + text.slice(b)
  }
  section('    _xkeen_sync_deny_mac_ipset() {\n', '\n    command -v ipset', () => `    _xkeen_sync_deny_mac_ipset() {
        # Keep native atomic set replacement, but never publish a failed API
        # read or failed JSON producer as an intentional empty deny list.
        (
            umask 077
            _dm_dir="$_xkeen_rundir/deny-read.$$"
            mkdir "$_dm_dir" || exit 1
            trap 'rm -f "$_dm_dir/api" "$_dm_dir/macs" "$_dm_dir/upper" "$_dm_dir/restore" && rmdir "$_dm_dir" || exit 1' EXIT
            # POSIX file-size bound is applied only in this read subprocess.
            # The explicit byte check also covers implementations with 1KiB units.
            ulimit -f 64 || exit 1
            command -v ipset >/dev/null 2>&1 || exit 1
            ipset create "$name_ipset_deny_mac" hash:mac -exist 2>/dev/null || exit 1
            _tmp="\${name_ipset_deny_mac}_tmp"
            ipset create "$_tmp" hash:mac -exist 2>/dev/null || exit 1
            ipset flush "$_tmp" >/dev/null 2>&1 || exit 1
            curl_api "\${url_server}/\${url_hotspot}" > "$_dm_dir/api" 2>/dev/null || exit 1
            [ "$(wc -c < "$_dm_dir/api")" -le 65536 ] && [ -s "$_dm_dir/api" ] || exit 1
            jq -sr '
                if length != 1 then error("invalid hotspot response") else .[0] end |
                (if type == "object" and has("host") then .host else . end) |
                (if type == "array" then . elif type == "object" and has("mac") then [.] else error("invalid host collection") end) |
                if all(.[]; type == "object" and (.mac | type) == "string" and (.mac | length) > 0 and
                    ((has("access") | not) or (.access | type) == "string")) then .[] else error("invalid host record") end |
                select((.access // "") == "deny") | .mac
            ' < "$_dm_dir/api" > "$_dm_dir/macs" 2>/dev/null || exit 1
            tr '[:lower:]' '[:upper:]' < "$_dm_dir/macs" > "$_dm_dir/upper" || exit 1
            awk -v set="$_tmp" '
                /^[0-9A-F][0-9A-F]:[0-9A-F][0-9A-F]:[0-9A-F][0-9A-F]:[0-9A-F][0-9A-F]:[0-9A-F][0-9A-F]:[0-9A-F][0-9A-F]$/ {print "add " set " " $0 " -exist"; next}
                {exit 1}
            ' < "$_dm_dir/upper" > "$_dm_dir/restore" || exit 1
            ipset restore -exist < "$_dm_dir/restore" || exit 1
            ipset swap "$_tmp" "$name_ipset_deny_mac" 2>/dev/null || exit 1
            ipset destroy "$_tmp" 2>/dev/null || exit 1
        )
    }
`)
  text = replaceOnce(text,
    '    command -v ipset >/dev/null 2>&1 && ipset create "$name_ipset_deny_mac" hash:mac -exist 2>/dev/null\n',
    '    command -v ipset >/dev/null 2>&1 && ipset create "$name_ipset_deny_mac" hash:mac -exist 2>/dev/null || exit 1\n')
  section('    configure_route() {\n', '\n    # Добавление', route => {
    route = replaceOnce(route,
      '            policy_table=$(ip rule show | awk -v policy="$policy_mark" \'$0 ~ policy && /lookup/ && !/blackhole/ {print $(NF); exit}\')\n',
      '            _policy_rules=$(ip rule show) || return 1\n            policy_table=$(printf \'%s\\n\' "$_policy_rules" | awk -v policy="$policy_mark" \'$0 ~ policy && /lookup/ && !/blackhole/ {print $(NF); exit}\') || return 1\n')
    route = replaceOnce(route, '        if [ -n "$policy_mark" ]; then\n', '        policy_table=\n        if [ -n "$policy_mark" ]; then\n')
    route = replaceOnce(route,
      '            if [ "$ip_version" = "6" ] && ! ip -6 route show default 2>/dev/null | grep -q .; then\n',
      '            _default_routes=$(ip -"$ip_version" route show default 2>/dev/null) || return 1\n            if [ "$ip_version" = "6" ] && [ -z "$_default_routes" ]; then\n')
    route = replaceOnce(route,
      '        _cur_routes=$(ip -"$ip_version" route show table "$table_id" 2>/dev/null)\n',
      '        _cur_routes=$(ip -"$ip_version" route show table "$table_id" 2>/dev/null) || return 1\n')
    route = replaceOnce(route,
      '        _want_routes=$(ip -"$ip_version" route show table "$source_table" 2>/dev/null | \\\n            grep -v \'^default\\|^unreachable\\|^blackhole\')\n',
      '        _source_routes=$(ip -"$ip_version" route show table "$source_table" 2>/dev/null) || return 1\n        _want_routes=$(printf \'%s\\n\' "$_source_routes" | grep -v \'^default\\|^unreachable\\|^blackhole\' || true)\n        _route_rules=$(ip -"$ip_version" rule show 2>/dev/null) || return 1\n')
    route = replaceOnce(route,
      '           ip -"$ip_version" rule show 2>/dev/null | grep -q "fwmark $table_mark lookup $table_id"; then\n',
      '           printf \'%s\\n\' "$_route_rules" | grep -Fq "fwmark $table_mark lookup $table_id"; then\n')
    route = replaceOnce(route,
      '        ip -"$ip_version" rule del fwmark "$table_mark" lookup "$table_id" >/dev/null 2>&1 || true\n',
      '        if printf \'%s\\n\' "$_route_rules" | grep -Fq "fwmark $table_mark lookup $table_id"; then\n            ip -"$ip_version" rule del fwmark "$table_mark" lookup "$table_id" >/dev/null 2>&1 || return 1\n        fi\n')
    route = route.replaceAll('>/dev/null 2>&1 || true', '>/dev/null 2>&1 || return 1')
    route = replaceOnce(route,
      '        ip -"$ip_version" route show table "$source_table" 2>/dev/null | while read -r route_line; do\n',
      '        printf \'%s\\n\' "$_source_routes" | while read -r route_line; do\n')
    route = replaceOnce(route, '                default*|unreachable*|blackhole*) continue ;;', '                \'\'|default*|unreachable*|blackhole*) continue ;;')
    route = replaceOnce(route, '        done\n        return 0\n', '        done || return 1\n        return 0\n')
    return route
  })
  section('    _xkeen_ensure_ipsets() {\n', '\n    _xkeen_cache_valid()', body =>
    body.replace('|| return 0', '|| return 1')
      .replaceAll('-exist 2>/dev/null\n', '-exist 2>/dev/null || return 1\n')
      .replace('        fi\n    }', '        fi\n        return 0\n    }'))
  section('    _xkeen_refill_geo_if_empty() {\n', '\n    # Текущий WAN', () => `    _xkeen_refill_geo_if_empty() {
        # Keep native refill-only-if-empty behavior. A failed producer is never
        # an empty set/list. Do not capture a populated geo set in a shell value.
        (
            _rg_set="$1"; _rg_file="$2"; _rg_family="$3"
            [ -s "$_rg_file" ] || exit 0
            umask 077
            _rg_dir="$_xkeen_rundir/geo-read.$$"
            mkdir "$_rg_dir" || exit 1
            trap 'rm -f "$_rg_dir/read" "$_rg_dir/restore" && rmdir "$_rg_dir" || exit 1' EXIT
            # At most two bounded RAM files, never a persistent cache/journal.
            # Cap overflow refuses rather than treating a truncated list as empty.
            ulimit -f 8192 || exit 1
            ipset save "$_rg_set" > "$_rg_dir/read" 2>/dev/null || exit 1
            [ "$(wc -c < "$_rg_dir/read")" -le 8388608 ] || exit 1
            grep -q '^add ' "$_rg_dir/read"
            _rg_scan=$?
            case "$_rg_scan" in 0) exit 0;; 1) ;; *) exit 1;; esac
            _rg_tmp="\${_rg_set}_renew_tmp"
            ipset create "$_rg_tmp" hash:net family "$_rg_family" -exist 2>/dev/null || exit 1
            ipset flush "$_rg_tmp" 2>/dev/null || exit 1
            [ "$(wc -c < "$_rg_file")" -le 8388608 ] || exit 1
            sed -e 's/\\r$//' -e 's/#.*//' -e '/^[[:space:]]*$/d' "$_rg_file" > "$_rg_dir/read" || exit 1
            awk -v set="$_rg_tmp" '{print "add " set " " $1}' < "$_rg_dir/read" > "$_rg_dir/restore" || exit 1
            [ "$(wc -c < "$_rg_dir/restore")" -le 8388608 ] || exit 1
            ipset restore -exist < "$_rg_dir/restore" || exit 1
            ipset swap "$_rg_set" "$_rg_tmp" 2>/dev/null || exit 1
            ipset destroy "$_rg_tmp" 2>/dev/null || exit 1
        )
    }
`)
  // Every terminal branch must observe these helper failures. Slow deny-MAC
  // synchronization stays after nf-lock release, before success cache/state.
  const wan = '        [ -n "$_xkeen_cur_wan" ] && printf \'%s\' "$_xkeen_cur_wan" > "$_xkeen_wan_state"\n'
  for (const [start, end] of [
    ['    if [ -n "$_xkeen_cur_wan" ]', '\n    if _xkeen_rules_intact; then'],
    ['    if _xkeen_rules_intact; then', '\n    # Кэш готовых'],
    ['    if _xkeen_cache_valid; then', '\n    if [ -n "$port_donor" ]'],
    ['\n    _xkeen_apply || exit 1\n', '\nelse\n    # mkdir-lock'],
  ]) section(start, end, body => {
    const indent = start.startsWith('\n') ? '    ' : '        '
    const publish = wan.replace(/^ {8}/, indent)
    const cache = `${indent}_xkeen_cache_save\n`
    const hasWan = body.includes(publish), hasCache = body.includes(cache)
    body = body.replace(publish, '').replace(cache, '')
    body = replaceOnce(body, `${indent}_xkeen_sync_deny_mac_ipset`, `${indent}_xkeen_sync_deny_mac_ipset || exit 1\n${hasCache ? cache : ''}${hasWan ? publish : ''}`)
    return body
  })
  // Optional families are skipped successfully; enabled family work must pass.
  text = text.replace(/^( +)\[ "\$(iptables|ip6tables)_supported" = "true" \] && (configure_route [46]|_xkeen_refill_geo_if_empty [^\n]+)$/gm,
    (_, indent, family, call) => `${indent}if [ "$${family}_supported" = "true" ]; then ${call} || exit 1; fi`)
  text = text.replace(/^( +)configure_route ([46])$/gm, '$1configure_route $2 || exit 1')
  text = replaceOnce(text, '        _xkeen_ensure_ipsets\n', '        _xkeen_ensure_ipsets || exit 1\n')
  return text
}

export function buildCandidates({ init, dispatcher }) {
  // Validate both sources before producing anything; accept no alternate revision.
  if (!Buffer.isBuffer(init) || digest(init) !== initSHA256 ||
      !Buffer.isBuffer(dispatcher) || digest(dispatcher) !== dispatcherSHA256) throw new Error('unsupported native source identity')
  let text = patchSource(init).toString('utf8')
  // Native per-table retries already return failure. Preserve that result at
  // both aggregation and terminal paths, without changing rendered rules.
  for (const family of ['iptables', 'ip6tables']) {
    for (const table of ['nat', 'mangle']) {
      const rules = `_xkeen_v${family === 'iptables' ? '4' : '6'}_${table}_rules`
      text = replaceOnce(text,
        `        [ "$${family}_supported" = "true" ] && _xkeen_apply_table ${family} ${table} ${rules} || true\n`,
        `        if [ "$${family}_supported" = "true" ]; then\n            _xkeen_apply_table ${family} ${table} ${rules} || return 1\n        fi\n`)
    }
  }
  text = replaceOnce(text, '        _xkeen_apply\n', '        _xkeen_apply || exit 1\n')
  text = replaceOnce(text, '    _xkeen_apply\n', '    _xkeen_apply || exit 1\n')
  text = replaceOnce(text,
    '        _xkeen_release_nf_lock\n        exit 0\n',
    '        _xkeen_release_nf_lock\n        _xkeen_sync_deny_mac_ipset\n        exit 0\n')
  text = patchNativeHookErrors(text)
  text = patchForegroundHook(text)
  // Exhausted native attempts otherwise fall through successful mutex cleanup.
  // Keep native firewall/killswitch behavior and release only a mutex we acquired.
  text = replaceOnce(text,
    '        log_error_terminal "Не удалось остановить прокси-клиент"\n',
    `        log_error_terminal "Не удалось остановить прокси-клиент"
        if [ "$_pstop_mutex_rc" -eq 0 ]; then
            _release_proxy_mutex
            trap - INT TERM HUP
        fi
        return 1
`)
  text = replaceOnce(text,
    '            log_error_terminal "Не удалось запустить прокси-клиент"\n            _release_coldstart_guard\n',
    `            log_error_terminal "Не удалось запустить прокси-клиент"
            _release_coldstart_guard
            if [ "$_ps_mutex_rc" -eq 0 ]; then
                _release_proxy_mutex
                trap - INT TERM HUP
            fi
            return 1
`)
  text = replaceOnce(text, '    restart) proxy_stop; proxy_start "$2"; _cmd_rc=$? ;;',
    '    restart) proxy_stop && proxy_start "$2"; _cmd_rc=$? ;;')

  // Preserve the existing native cold-start procedure, but join it in this shell.
  const coldBegin = text.indexOf('    cold_start)\n')
  const coldEnd = text.indexOf('        ;;\n', coldBegin)
  if (coldBegin < 0 || coldEnd < coldBegin) throw new Error('missing cold-start anchor')
  let cold = text.slice(coldBegin + '    cold_start)\n'.length, coldEnd)
  cold = replaceOnce(cold, '        wait_for_ready\n', '        wait_for_ready || return $?\n')
  // Old comments described a detached child; the executable procedure is retained.
  cold = cold.split('\n').filter(line => !line.trimStart().startsWith('#')).join('\n')
  text = text.slice(0, coldBegin) + '    cold_start) _native_cold_start; _cmd_rc=$?\n' + text.slice(coldEnd)
  text = replaceOnce(text, '\n_cmd_rc=0\n', '\n_cmd_rc=0\n_native_cold_start() {\n' + cold + '}\n')
  const startBegin = text.indexOf('            # Атомарный guard ДО spawn')
  const startEnd = text.indexOf('        _cmd_rc=$?\n', startBegin) + '        _cmd_rc=$?\n'.length
  if (startBegin < 0 || startEnd < startBegin) throw new Error('missing autostart anchor')
  text = text.slice(0, startBegin) + `            _acquire_coldstart_guard || exit 75
            log_info_router "Подготовка к запуску прокси-клиента"
            _native_cold_start
            _cmd_rc=$?
        else
            proxy_start "$2"
            _cmd_rc=$?
        fi
` + text.slice(startEnd)
  text = replaceOnce(text, `            [ "$start_auto" != "on" ] && exit 0
            _acquire_coldstart_guard || exit 75
            log_info_router "Подготовка к запуску прокси-клиента"
            _native_cold_start
            _cmd_rc=$?
`, `            if [ "$start_auto" = "on" ]; then
                if _acquire_coldstart_guard; then
                    log_info_router "Подготовка к запуску прокси-клиента"
                    _native_cold_start
                    _cmd_rc=$?
                else
                    _cmd_rc=75
                fi
            fi
`)

  // Same shell/PID preserves native proxy-mutex reentrancy in emergency_clear.
  const crashComment = text.indexOf('                        # Даём ядру прокси')
  const crashBegin = text.lastIndexOf('                    (\n', crashComment)
  const crashEnd = text.indexOf('                    ) &\n', crashComment)
  if (crashComment < 0 || crashBegin < 0 || crashEnd < crashComment) throw new Error('missing crash-check anchor')
  let crash = text.slice(crashBegin, crashEnd + '                    ) &\n'.length)
  crash = replaceOnce(crash, '                    (\n', '                    {\n')
  crash = replaceOnce(crash, '                    ) &\n', '                    }\n')
  // Readiness must still hold after the settling interval, including a vanished
  // PID before the first pidof. Native emergency_clear owns rule/killswitch policy.
  const conditionBegin = crash.indexOf('                        if [ -n "$_crash_pid" ]')
  const conditionEnd = crash.indexOf(' then\n', conditionBegin) + ' then\n'.length
  if (conditionBegin < 0 || conditionEnd < conditionBegin) throw new Error('missing crash condition')
  crash = crash.slice(0, conditionBegin) + '                        if ! proxy_status; then\n' + crash.slice(conditionEnd)
  crash = replaceOnce(crash, "                            printf '\\n~ # '\n", `                            if [ "$_ps_mutex_rc" -eq 0 ]; then
                                _release_proxy_mutex
                                trap - INT TERM HUP
                            fi
                            return 1
`)
  text = text.slice(0, crashBegin) + crash + text.slice(crashEnd + '                    ) &\n'.length)
  // Success announcement belongs after the joined check, like success logging.
  const announcement = '                    echo -e "  Прокси-клиент ${green}запущен${reset} в режиме ${light_blue}${mode_proxy}${reset}"\n'
  text = replaceOnce(text, announcement, '')
  text = replaceOnce(text, '                    if [ -n "$api_policy_json" ]; then\n', announcement + '                    if [ -n "$api_policy_json" ]; then\n')
  // Eight pinned core launches: six init variants and two generated-hook paths.
  // exec keeps the background PID tied to the core (including native nohup).
  const coreLaunch = /^([ \t]+)((?:nohup )?"\$name_client"(?: run)?)( >\/dev\/null 2>&1)? &$/gm
  if ([...text.matchAll(coreLaunch)].length !== 8) throw new Error('native core launch inventory changed')
  text = text.replace(coreLaunch, (_, indent, command, redirect = '') =>
    `${indent}(native_admission_strip; exec ${command}${redirect}) &`)
  text = replaceOnce(text, '                        monitor_fd &\n', '                        (native_admission_strip; monitor_fd) &\n')
  // A generated hook is a separate shell; it cannot inherit the sourced init
  // function. Embed only this exact source-owned strip function, not an executor.
  const entryHelper = readFileSync(new URL('./native-admission-entry.sh', import.meta.url), 'utf8').replaceAll('\r\n', '\n')
  const stripFunctions = [...entryHelper.matchAll(/^native_admission_strip\(\) \{\n[\s\S]*?^\}\n/gm)]
  if (stripFunctions.length !== 1) throw new Error('native strip helper identity is ambiguous')
  const hookHeader = '    cat > "$file_netfilter_hook" <<\'EOL\'\n#!/bin/sh\n'
  text = replaceOnce(text, hookHeader, hookHeader + stripFunctions[0][0])
  const monitorBegin = text.indexOf('\nmonitor_fd() {\n')
  const monitorEnd = text.indexOf('\nload_ipset() {\n', monitorBegin)
  if (monitorBegin < 0 || monitorEnd < monitorBegin) throw new Error('native monitor anchor missing')
  text = replaceOnce(text, text.slice(monitorBegin, monitorEnd), `
_native_monitor_sample() {
    _nm_core_pid=$(pidof "$name_client" | awk '{print $1}')
    [ -n "$_nm_core_pid" ] && [ -d "/proc/$_nm_core_pid/fd" ] || return 1
    _native_gate_proc "$_nm_core_pid" || return 1
    _nm_core_start=$_ng_proc_start
    _nm_limit=$(awk '/Max open files/ {print $4}' "/proc/$_nm_core_pid/limits")
    case "$_nm_limit" in ''|*[!0-9]*) return 1;; esac
    set -- /proc/$_nm_core_pid/fd/*
    [ -e "$1" ] || set --
    [ "$_nm_limit" -gt 0 ] && [ "$#" -gt $((_nm_limit * 90 / 100)) ]
}

_native_monitor_owns_marker() {
    [ ! -L "$file_pid_fd" ] && [ -f "$file_pid_fd" ] || return 1
    _native_gate_metadata "$file_pid_fd" || return 1
    [ "$_ng_meta_uid" = 0 ] && [ "$_ng_meta_links" = 1 ] &&
        [ "$_ng_meta_size" -le 32 ] && [ "$((0x$_ng_meta_mode & 0022))" -eq 0 ] || return 1
    set -- $_ng_meta
    _nm_read_identity="$7:$8"
    [ -z "$_nm_marker_identity" ] || [ "$_nm_marker_identity" = "$_nm_read_identity" ] || return 1
    IFS= read -r _nm_marker_pid < "$file_pid_fd" || return 1
    _native_gate_decimal "$_nm_marker_pid" || return 1
    [ "$_nm_marker_pid" = "$_nm_monitor_pid" ] &&
        [ "$_ng_meta_size" -eq "$(( \${#_nm_marker_pid} + 1 ))" ] || return 1
    _native_gate_self || return 1
    [ "$_ng_self_pid" = "$_nm_monitor_pid" ] && [ "$_ng_self_start" = "$_nm_monitor_start" ] || return 1
    _nm_marker_identity=$_nm_read_identity
}

monitor_fd() {
    # This long-lived worker inherited no operation context. Each threshold
    # trigger starts a fresh native restart owner; busy uses the native cadence.
    _native_gate_self || return 77
    _nm_monitor_pid=$_ng_self_pid; _nm_monitor_start=$_ng_self_start
    while true; do
        if _native_monitor_sample; then
            _nm_observed="$_nm_core_pid:$_nm_core_start"
            native_gate_acquire /tmp/.xkeen-admission restart
            _nm_admission=$?
            case "$_nm_admission" in
                0)
                    if ! _native_monitor_sample || [ "$_nm_observed" != "$_nm_core_pid:$_nm_core_start" ]; then
                        # No mutation occurred: known stale evidence is safe to dismiss.
                        native_gate_release || return 77
                    else
                        _nm_marker_identity=
                        if ! _native_monitor_owns_marker; then
                            native_gate_release || return 77
                            return 77
                        fi
                        native_gate_join /tmp/.xkeen-admission "$XKEEN_GATE_TOKEN" || return 77
                        [ "$_ng_record" = "$_ng_owned_record" ] || return 77
                        _na_file_ok /opt/etc/init.d/S05xkeen || return 76
                        # Remove only our verified marker; native cleanup must
                        # not kill this foreground gate owner. Never touch it
                        # again: successful startup may publish a new monitor.
                        if ! _native_monitor_owns_marker; then
                            native_gate_release || return 77
                            return 77
                        fi
                        rm -f "$file_pid_fd" || return 77
                        fd_out=true /opt/bin/sh /opt/etc/init.d/S05xkeen restart on || return 77
                        native_gate_release || return 77
                        return 0
                    fi
                    ;;
                75) ;;
                *) return "$_nm_admission";;
            esac
        fi
        sleep "$delay_fd"
    done
}
`)
  text = replaceOnce(text, '\nexit "$_cmd_rc"\n', '\nnative_admission_finish "$_cmd_rc"\nexit $?\n')
  let dispatcherText = replaceOnce(dispatcher.toString('utf8'), '\nexit "$xkeen_rc"\n', `
case "$_na_entry_action" in
    update-xkeen) exit "$xkeen_rc";;
    *) native_admission_finish "$xkeen_rc"; exit $?;;
esac
`)
  // The post-update proof pins the actual interpreter/path/argument vector;
  // PATH lookup and a caller-selected $0 must not alter that handoff.
  dispatcherText = replaceOnce(dispatcherText, 'exec sh "$0" -uk_post_update', 'exec /opt/bin/sh /opt/sbin/xkeen -uk_post_update')
  dispatcherText = patchUpdaterFailures(dispatcherText)
  // Lifecycle is not installation. These exact classified actions must neither
  // rename an installation nor invoke the package manager under the gate.
  dispatcherText = replaceOnce(dispatcherText, '\ninstall_xkeen_rename\n', '\ncase "$1" in -start|-stop|-restart|-uk|-uk_post_update) ;; *) install_xkeen_rename;; esac\n')
  dispatcherText = replaceOnce(dispatcherText,
    '    ""|-sbt|-h|-help|-v|-version|-about|-ad|-donate|-af|-feedback) ;;',
    '    -start|-stop|-restart|""|-sbt|-h|-help|-v|-version|-about|-ad|-donate|-af|-feedback) ;;')
  const candidates = { init: Buffer.from(disabled(addEntry(text, 'init'))), dispatcher: Buffer.from(disabled(addEntry(dispatcherText, 'dispatcher'))) }
  // Native register_xkeen_initd copies this source template and reapplies its
  // declared settings. Decorate the template too; changing only the live init
  // cannot survive native registration. This remains an uninstalled fenced file.
  candidates.registrationTemplate = Buffer.from(candidates.init)
  return { ...candidates, manifest: {
    schema: 1, enabled: false, installed: false,
    source: { initSHA256, dispatcherSHA256, registrationTemplateSHA256: initSHA256 },
    candidate: { initSHA256: digest(candidates.init), dispatcherSHA256: digest(candidates.dispatcher), registrationTemplateSHA256: digest(candidates.registrationTemplate) },
    missing: ['native verifier/hook target integration qualification', 'bounded NDM event convergence',
      'standalone recovery/readback', 'native update persistence', 'BusyBox and hardware qualification'],
  } }
}

function boundedRead(path) {
  const fd = openSync(path, 'r')
  try {
    const stat = fstatSync(fd)
    if (!stat.isFile() || stat.size > 512 * 1024) throw new Error('input must be bounded regular file')
    return readFileSync(fd)
  } finally { closeSync(fd) }
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    if (process.argv.length !== 5) throw new Error('usage: PUBLIC_DISPATCHER PUBLIC_INIT NEW_DIRECTORY')
    const candidates = buildCandidates({ dispatcher: boundedRead(process.argv[2]), init: boundedRead(process.argv[3]) })
    // A new directory, never an installed destination. Manifest is written last;
    // interrupted/failed publication is not a complete candidate set.
    mkdirSync(process.argv[4], { mode: 0o700 })
    for (const name of ['dispatcher', 'init']) writeFileSync(join(process.argv[4], name + '.disabled.sh'), candidates[name], { flag: 'wx', mode: 0o600 })
    writeFileSync(join(process.argv[4], 'registration-template.disabled.sh'), candidates.registrationTemplate, { flag: 'wx', mode: 0o600 })
    writeFileSync(join(process.argv[4], 'manifest.json'), JSON.stringify(candidates.manifest, null, 2) + '\n', { flag: 'wx', mode: 0o600 })
    console.log(JSON.stringify(candidates.manifest))
  } catch {
    console.error('native admission candidate rejected; no runtime installation performed')
    process.exitCode = 1
  }
}
