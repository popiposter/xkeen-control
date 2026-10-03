// Build-time decoration of pinned upstream registration helpers. No executor.
// Upstream: jameszeroX/XKeen@5aaece27a70d5bd002c615248614914ebbc4569d;
// BSD-3-Clause, see native-xkeen-stop-fix.LICENSE.
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'

const metadata = JSON.parse(readFileSync(new URL('./native-update-profile-v1.json', import.meta.url)))
export const commonPath = '_xkeen/02_install/07_install_register/00_register_common.sh'
export const deletePath = '_xkeen/03_delete/05_delete_register.sh'
export const registrationPath = '_xkeen/02_install/07_install_register/02_register_xkeen.sh'
const fence = `# SOURCE-ONLY FENCE: native registration integration incomplete. Never install.
return 76 2>/dev/null || exit 76
# END SOURCE-ONLY FENCE
`
function pinned(entries, path) {
  const expected = metadata.files.find(file => file.path === path), bytes = entries.get(path)
  if (!expected || !Buffer.isBuffer(bytes) || bytes.length !== expected.size || createHash('sha256').update(bytes).digest('hex') !== expected.sha256) {
    throw new Error('unsupported native registration source')
  }
  return bytes.toString('utf8')
}
function replace(text, from, to) {
  if (text.split(from).length !== 2) throw new Error('native registration anchor changed')
  return text.replace(from, to)
}
function checkWrites(text) {
  return text.replace(/^(\s*echo .*|\s*cat .*|\s*rm -f .*|\s*sed -i .*|\s*mv -f .*)$/gm, '$1 || return 1')
}
export function buildRegistrationWrites(entries) {
  let common = pinned(entries, commonPath)
  common = replace(common, '    _installed_size=$(du -s "$install_dir" | cut -f1)\n', `    _installed_size=$(du -s "$install_dir") || return 1
    _installed_size=\${_installed_size%%[[:space:]]*}
    case "$_installed_size" in ''|*[!0-9]*) return 1;; esac
`)
  common = replace(common, '    _source_date_epoch=$(date +%s)', '    _source_date_epoch=$(date +%s) || return 1')
  common = replace(common, '    status_entry="$(mktemp)"', `    status_entry="$(mktemp)" || return 1
    [ -n "$status_entry" ] && [ -f "$status_entry" ] && [ ! -L "$status_entry" ] || return 1
    _installed_time=$(date +%s) || return 1`)
  common = replace(common, '        echo "Installed-Time: $(date +%s)"', '        echo "Installed-Time: $_installed_time"')
  // An absent Depends/status input is optional; a failed write/read is not.
  common = common.replaceAll('[ -n "$package_depends" ] && echo "Depends: $package_depends"', 'if [ -n "$package_depends" ]; then echo "Depends: $package_depends" || return 1; fi')
  common = replace(common, '        [ -f "$status_file" ] && cat "$status_file"', '        if [ -f "$status_file" ]; then cat "$status_file" || return 1; fi')
  common = checkWrites(common)
  common = common.replace(/^(\s*} > .*?)$/gm, '$1 || return 1')
  let deletion = pinned(entries, deletePath)
  const start = deletion.indexOf('delete_register_xkeen() {\n')
  if (start < 0 || deletion.indexOf('delete_register_xkeen() {\n', start + 1) >= 0) throw new Error('native deletion function changed')
  // Leave the Xray/Mihomo/Yq helpers byte-for-byte upstream-owned.
  let body = deletion.slice(start)
  body = replace(body, ' "$status_file" > "$status_tmp"', ' "$status_file" > "$status_tmp" || return 1')
  body = checkWrites(body)
  deletion = deletion.slice(0, start) + body
  let registration = pinned(entries, registrationPath)
  const listStart = registration.indexOf('register_xkeen_list() {\n'), listEnd = registration.indexOf('register_xkeen_status() {\n', listStart)
  if (listStart < 0 || listEnd < listStart) throw new Error('native list function changed')
  registration = registration.slice(0, listStart) + `register_xkeen_list() {
    # Preserve native inventory and extra paths; publish only complete output.
    list_tmp="$register_dir/xkeen.list.tmp.$$"
    (set -C; find "$xkeen_dir" -mindepth 1 > "$list_tmp") || return 1
    echo "$install_dir/xkeen" >> "$list_tmp" || return 1
    echo "$xkeen_dir" >> "$list_tmp" || return 1
    echo "$initd_file" >> "$list_tmp" || return 1
    echo "$log_dir/xkeen-detached.log" >> "$list_tmp" || return 1
    mv -f "$list_tmp" "$register_dir/xkeen.list" || return 1
}

` + registration.slice(listEnd)
  registration = replace(registration, '    current_datetime=$(date "+%Y-%m-%d_%H-%M-%S")', '    current_datetime=$(date "+%Y-%m-%d_%H-%M-%S") || return 1')
  // Existing checked template copy/rename retain their native failure branches.
  registration = registration.replace(/^(\s*cp [^\n]+)$/gm, line => line.includes('||') ? line : line + ' || return 1')
  registration = registration.replace(/^(\s*sed -i [^\n]+|\s*chmod \+x [^\n]+)$/gm, '$1 || return 1')
  for (const [variable, field, file] of [
    ['autostart_val', 'autostart', 'source_start_backup'], ['start_delay_val', 'start_delay', 'source_start_backup'],
    ['autostart_val', 'start_auto', 'source_main_backup'], ['start_delay_val', 'start_delay', 'source_main_backup'],
  ]) {
    const old = `${variable}=$(grep '^${field}=' "$${file}" | head -n 1 | cut -d'=' -f2)`
    const checked = `${variable}=$(awk -F= '/^${field}=/ {print $2; exit}' "$${file}") || return 1`
    if (file === 'source_main_backup') {
      registration = replace(registration, `[ -z "$${variable}" ] && ${old}`, `if [ -z "$${variable}" ]; then ${checked}; fi`)
    } else registration = replace(registration, old, checked)
  }
  registration = replace(registration, '                value=$(grep -m1 "^${var}=" "$source_main_backup") || continue', `                value=$(grep -m1 "^\${var}=" "$source_main_backup")
                read_result=$?
                case "$read_result" in 0) ;; 1) continue;; *) return 1;; esac`)
  registration = replace(registration, "                escaped_value=$(printf '%s\\n' \"$value\" | sed 's:[&#/]:\\\\&:g')", `                escaped_value=$(sed 's:[&#/]:\\\\&:g' <<NATIVE_SETTING
$value
NATIVE_SETTING
                ) || return 1`)
  registration = replace(registration, '                position=$(grep -n "^${var}=" "$initd_tmp_file" | head -n 1 | cut -d: -f1)', `                position=$(awk -v name="$var" 'index($0,name "=")==1 {print NR; exit}' "$initd_tmp_file") || return 1`)
  registration = replace(registration, '                [ -n "$position" ] && sed -i "${position}s#.*#${escaped_value}#" "$initd_tmp_file"', '                if [ -n "$position" ]; then sed -i "${position}s#.*#${escaped_value}#" "$initd_tmp_file" || return 1; fi')
  return new Map([[commonPath, Buffer.from(fence + common)], [deletePath, Buffer.from(fence + deletion)], [registrationPath, Buffer.from(fence + registration)]])
}
