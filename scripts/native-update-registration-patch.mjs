// Build-time decoration of pinned upstream registration helpers. No executor.
// Upstream: jameszeroX/XKeen@5aaece27a70d5bd002c615248614914ebbc4569d;
// BSD-3-Clause, see native-xkeen-stop-fix.LICENSE.
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'

const metadata = JSON.parse(readFileSync(new URL('./native-update-profile-v1.json', import.meta.url)))
export const commonPath = '_xkeen/02_install/07_install_register/00_register_common.sh'
export const deletePath = '_xkeen/03_delete/05_delete_register.sh'
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
  return new Map([[commonPath, Buffer.from(fence + common)], [deletePath, Buffer.from(fence + deletion)]])
}
