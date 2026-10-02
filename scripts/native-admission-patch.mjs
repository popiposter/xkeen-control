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

export function buildCandidates({ init, dispatcher }) {
  // Validate both sources before producing anything; accept no alternate revision.
  if (!Buffer.isBuffer(init) || digest(init) !== initSHA256 ||
      !Buffer.isBuffer(dispatcher) || digest(dispatcher) !== dispatcherSHA256) throw new Error('unsupported native source identity')
  let text = patchSource(init).toString('utf8')
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
  const candidates = { init: Buffer.from(disabled(text)), dispatcher: Buffer.from(disabled(dispatcher.toString('utf8'))) }
  return { ...candidates, manifest: {
    schema: 1, enabled: false, installed: false,
    source: { initSHA256, dispatcherSHA256 },
    candidate: { initSHA256: digest(candidates.init), dispatcherSHA256: digest(candidates.dispatcher) },
    missing: ['dispatcher/init shared admission', 'foreground hook ownership and settlement',
      'background token stripping and monitor fresh admission', 'bounded NDM event convergence',
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
