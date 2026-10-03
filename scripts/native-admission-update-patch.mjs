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
  const archiveAnchor = '    xkeen_archive="$tmp_ram/xkeen.tar.gz"\n'
  if (text.split(archiveAnchor).length !== 2) throw new Error('native archive anchor is not unique')
  const protectedText = text.replace(archiveAnchor, archiveAnchor + `
    # Refuse unsupported bytes/destinations before ANY archive operation.
    native_update_archive_ready "$xkeen_archive" || return 1
`)
  const extractAnchor = 'tar -xzf "$xkeen_archive" -C "$stage_dir" xkeen _xkeen'
  if (protectedText.split(extractAnchor).length !== 2) throw new Error('native extraction anchor is not unique')
  // -o is supported by the actual Entware BusyBox tar: keep the extracting
  // root owner rather than restoring the public archive's CI runner UID.
  const extractedText = protectedText.replace(extractAnchor, 'tar -xozf "$xkeen_archive" -C "$stage_dir" xkeen _xkeen')
  let checkedText = extractedText.replace(anchor, guard + anchor)
  const check = (before, after) => {
    if (checkedText.split('\n' + before).length !== 2) throw new Error('native promotion anchor is not unique')
    checkedText = checkedText.replace('\n' + before, '\n' + after)
  }
  // Preserve the native promotion order. A partial replacement or failed
  // retirement is an error; it must never become the updater's terminal zero.
  check(anchor, '        chmod +x "$stage_dir/xkeen" || return 1\n')
  check('        rm -rf "$stage_dir"\n', '        rm -rf "$stage_dir" || return 1\n')
  check('        rm -f "$install_dir/xkeen.old"\n', '        rm -f "$install_dir/xkeen.old" || return 1\n')
  check('        [ -f "$install_dir/xkeen" ] && mv "$install_dir/xkeen" "$install_dir/xkeen.old"\n',
    '        if [ -f "$install_dir/xkeen" ]; then mv "$install_dir/xkeen" "$install_dir/xkeen.old" || return 1; fi\n')
  check('        rm -rf "$install_dir/.xkeen.old"\n', '        rm -rf "$install_dir/.xkeen.old" || return 1\n')
  check('        [ -d "$install_dir/.xkeen" ] && mv "$install_dir/.xkeen" "$install_dir/.xkeen.old"\n',
    '        if [ -d "$install_dir/.xkeen" ]; then mv "$install_dir/.xkeen" "$install_dir/.xkeen.old" || return 1; fi\n')
  check('            rm -f "$install_dir/xkeen.old"\n', '            rm -f "$install_dir/xkeen.old" || return 1\n')
  check('            rm -rf "$install_dir/.xkeen.old" "$stage_dir"\n',
    '            rm -rf "$install_dir/.xkeen.old" "$stage_dir" || return 1\n')
  check('    [ -d "$log_dir/xkeen" ] && rm -rf "$log_dir/xkeen"\n',
    '    if [ -d "$log_dir/xkeen" ]; then rm -rf "$log_dir/xkeen" || return 1; fi\n')
  const candidate = Buffer.from(`# SOURCE-ONLY FENCE: native update integration incomplete. Never install.
return 76 2>/dev/null || exit 76
# END SOURCE-ONLY FENCE
` + checkedText)
  return { candidate, manifest: {
    enabled: false, installed: false,
    sourceSHA256: installerSHA256,
    candidateSHA256: createHash('sha256').update(candidate).digest('hex'),
    missing: ['authenticated fixed staging worker', 'native updater admission and exec handoff',
      'update postconditions and event settlement', 'target qualification'],
  } }
}
