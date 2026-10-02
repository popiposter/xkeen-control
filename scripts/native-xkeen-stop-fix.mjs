// Source-only correction for jameszeroX/XKeen@5aaece27a70d5bd002c615248614914ebbc4569d.
// Upstream fragments: Copyright (c) 2026, jameszeroX; BSD-3-Clause.
// See native-xkeen-stop-fix.LICENSE. Never executes or installs native code.
import { createHash } from 'node:crypto'
import { closeSync, fstatSync, openSync, readFileSync, writeFileSync } from 'node:fs'
import { pathToFileURL } from 'node:url'

export const sourceSHA256 = 'fbdba1f1cca6e1923e0c43113b4b6fafc51f92248cad70818f7ac92c937e32e3'
const digest = (bytes) => createHash('sha256').update(bytes).digest('hex')

export function patchSource(source) {
  if (!Buffer.isBuffer(source) || digest(source) !== sourceSHA256) throw new Error('unsupported native source identity')
  const text = source.toString('utf8')
  const needle = '        cleanup_fd_monitor\n    else\n        [ -f "$xkeen_rundir/coldstart.lock" ]'
  if (text.split(needle).length !== 2) throw new Error('native stop anchor is not unique')
  return Buffer.from(text.replace(needle, '        clean_firewall\n' + needle))
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    if (process.argv.length !== 4) throw new Error('usage: node scripts/native-xkeen-stop-fix.mjs PUBLIC_SOURCE NEW_OUTPUT')
    const fd = openSync(process.argv[2], 'r')
    let source
    try {
      const stat = fstatSync(fd)
      if (!stat.isFile() || stat.size > 512 * 1024) throw new Error('native source must be a bounded regular file')
      source = readFileSync(fd)
    } finally { closeSync(fd) }
    const candidate = patchSource(source)
    // Exclusive output: never overwrite an installed init, original, or earlier candidate.
    writeFileSync(process.argv[3], candidate, { flag: 'wx', mode: 0o600 })
    console.log(JSON.stringify({ sourceSHA256, candidateSHA256: digest(candidate), bytes: candidate.length, installed: false }))
  } catch (error) {
    console.error(error.code === 'EEXIST' ? 'output already exists' : 'native source patch rejected')
    process.exitCode = 1
  }
}
