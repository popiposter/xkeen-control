// Source-only native cron-registration no-op decoration, not a cron updater.
// jameszeroX/XKeen@5aaece27a70d5bd002c615248614914ebbc4569d;
// BSD-3-Clause, see native-xkeen-stop-fix.LICENSE.
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
const metadata = JSON.parse(readFileSync(new URL('./native-update-profile-v1.json', import.meta.url)))
export const cronPath = '_xkeen/02_install/07_install_register/03_register_cron.sh'
export function buildCronRegistration(entries) {
  const bytes = entries.get(cronPath), expected = metadata.files.find(file => file.path === cronPath)
  if (!Buffer.isBuffer(bytes) || bytes.length !== expected.size || createHash('sha256').update(bytes).digest('hex') !== expected.sha256) throw new Error('unsupported native cron source')
  const anchor = 'register_cron_initd() {\n', text = bytes.toString('utf8')
  if (text.split(anchor).length !== 2) throw new Error('native cron anchor changed')
  return Buffer.from(`# SOURCE-ONLY FENCE: native cron integration incomplete. Never install.
return 76 2>/dev/null || exit 76
# END SOURCE-ONLY FENCE
` + text.replace(anchor, anchor + `    case "\${XKEEN_ADMISSION_ROLE-}:\${XKEEN_ADMISSION_ACTION-}" in
        update:update-xkeen) native_update_cron_noop || return $?; return 0;;
        update:*|*:update-xkeen) return 77;;
    esac
`))
}
