// Source-only native staging experiment; never installed or executed on a router.
// Upstream: jameszeroX/XKeen@5aaece27a70d5bd002c615248614914ebbc4569d,
// BSD-3-Clause; see native-xkeen-stop-fix.LICENSE.
import { createHash } from 'node:crypto'

export const installerSHA256 = '675403b5e21c170d9ff3b1fe7f1cfd48af63e1fbb6e6b9b6904f145c2771d992'
const anchor = '        chmod +x "$stage_dir/xkeen"\n'
const guard = `        # Admission decoration must survive the native archive replacement.
        # Refuse before the first live rename when preservation is unavailable.
        if ! _na_file_ok /opt/lib/xkeen/native-update-stage.sh \\
            || ! /opt/bin/sh /opt/lib/xkeen/native-update-stage.sh "$stage_dir"; then
            rm -rf "$stage_dir" "$xkeen_archive"
            return 1
        fi
`

export function buildUpdateCandidate(source) {
  if (!Buffer.isBuffer(source) || createHash('sha256').update(source).digest('hex') !== installerSHA256) {
    throw new Error('unsupported native installer source')
  }
  const text = source.toString('utf8')
  if (text.split(anchor).length !== 2) throw new Error('native staging anchor is not unique')
  const candidate = Buffer.from(`# SOURCE-ONLY FENCE: native update integration incomplete. Never install.
return 76 2>/dev/null || exit 76
# END SOURCE-ONLY FENCE
` + text.replace(anchor, guard + anchor))
  return { candidate, manifest: {
    enabled: false, installed: false,
    sourceSHA256: installerSHA256,
    candidateSHA256: createHash('sha256').update(candidate).digest('hex'),
    missing: ['authenticated fixed staging worker', 'native updater admission and exec handoff',
      'update postconditions and event settlement', 'target qualification'],
  } }
}
