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
  const normalize = role === 'dispatcher' ? `
[ "$#" = 1 ] || exit 76
case "$1" in -start|-stop|-restart) _na_entry_action=\${1#-};; *) exit 76;; esac
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
native_admission_enter ${role} "$_na_entry_action" "$_na_entry_mode" || exit $?
[ "$_na_body" = 1 ] || exit 0
XKEEN_FOREGROUND=1; export XKEEN_FOREGROUND
# END NATIVE ADMISSION ENTRY
`
}
function addEntry(text, role) { return '#!/bin/sh\n' + entryPrelude(role) + text.slice('#!/bin/sh\n'.length) }

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
  let dispatcherText = replaceOnce(dispatcher.toString('utf8'), '\nexit "$xkeen_rc"\n', '\nnative_admission_finish "$xkeen_rc"\nexit $?\n')
  // Lifecycle is not installation. These exact classified actions must neither
  // rename an installation nor invoke the package manager under the gate.
  dispatcherText = replaceOnce(dispatcherText, '\ninstall_xkeen_rename\n', '\ncase "$1" in -start|-stop|-restart) ;; *) install_xkeen_rename;; esac\n')
  dispatcherText = replaceOnce(dispatcherText,
    '    ""|-sbt|-h|-help|-v|-version|-about|-ad|-donate|-af|-feedback) ;;',
    '    -start|-stop|-restart|""|-sbt|-h|-help|-v|-version|-about|-ad|-donate|-af|-feedback) ;;')
  const candidates = { init: Buffer.from(disabled(addEntry(text, 'init'))), dispatcher: Buffer.from(disabled(addEntry(dispatcherText, 'dispatcher'))) }
  return { ...candidates, manifest: {
    schema: 1, enabled: false, installed: false,
    source: { initSHA256, dispatcherSHA256 },
    candidate: { initSHA256: digest(candidates.init), dispatcherSHA256: digest(candidates.dispatcher) },
    missing: ['native postcondition verifier', 'foreground hook ownership and settlement',
      'bounded NDM event convergence',
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
    writeFileSync(join(process.argv[4], 'manifest.json'), JSON.stringify(candidates.manifest, null, 2) + '\n', { flag: 'wx', mode: 0o600 })
    console.log(JSON.stringify(candidates.manifest))
  } catch {
    console.error('native admission candidate rejected; no runtime installation performed')
    process.exitCode = 1
  }
}
