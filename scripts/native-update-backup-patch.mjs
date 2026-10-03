// Build-time checked native backup choice/copy. No installer or runtime API.
// jameszeroX/XKeen@5aaece27a70d5bd002c615248614914ebbc4569d;
// BSD-3-Clause, see native-xkeen-stop-fix.LICENSE.
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
const metadata = JSON.parse(readFileSync(new URL('./native-update-profile-v1.json', import.meta.url)))
export const choicePath = '_xkeen/04_tools/05_tools_choice/02_choice_xkeen.sh'
export const backupPath = '_xkeen/04_tools/06_tools_backups/01_backups_xkeen.sh'
function source(entries, path) {
  const expected = metadata.files.find(file => file.path === path), bytes = entries.get(path)
  if (!expected || !Buffer.isBuffer(bytes) || bytes.length !== expected.size || createHash('sha256').update(bytes).digest('hex') !== expected.sha256) throw new Error('unsupported native backup source')
  return bytes.toString('utf8')
}
function replace(text, from, to) {
  if (text.split(from).length !== 2) throw new Error('native backup anchor changed')
  return text.replace(from, to)
}
const fence = `# SOURCE-ONLY FENCE: native backup integration incomplete. Never install.
return 76 2>/dev/null || exit 76
# END SOURCE-ONLY FENCE
`
export function buildBackupWrites(entries) {
  let choice = source(entries, choicePath)
  choice = replace(choice, `    backup_value=$(awk -F= '/^[[:space:]]*backup[[:space:]]*=/ { gsub(/"| /,"",$2); print tolower($2); exit }' "$initd_file")`,
    `    backup_value=$(awk -F= '/^[[:space:]]*backup[[:space:]]*=/ { gsub(/"| /,"",$2); print tolower($2); exit }' "$initd_file") || return 2`)
  let backup = source(entries, backupPath)
  const end = backup.indexOf('restore_backup_xkeen() {\n')
  if (end < 0) throw new Error('native backup function changed')
  let body = backup.slice(0, end)
  body = replace(body, '    if choice_backup_xkeen && [ -z "$manual_backup" ]; then', `    choice_backup_xkeen
    _backup_choice=$?
    case "$_backup_choice" in 0|1) ;; *) return 1;; esac
    if [ "$_backup_choice" = 0 ] && [ -z "$manual_backup" ]; then`)
  body = replace(body, '    mkdir -p "$backup_dir"', '    mkdir -p "$backup_dir" || return 1')
  body = replace(body, '        mv "$backup_dir/.xkeen" "$backup_dir/_xkeen"', '        mv "$backup_dir/.xkeen" "$backup_dir/_xkeen" || return 1')
  body = replace(body, '        echo -e "  ${red}Ошибка${reset} при создании резервной копии XKeen"', '        echo -e "  ${red}Ошибка${reset} при создании резервной копии XKeen"\n        return 1')
  backup = body + backup.slice(end)
  return new Map([[choicePath, Buffer.from(fence + choice)], [backupPath, Buffer.from(fence + backup)]])
}
