// One pinned public native profile. Build-time only; no router/network execution.
// Upstream payload remains upstream-owned; only three admission overlays change.
import { createHash } from 'node:crypto'
import { constants, closeSync, fstatSync, lstatSync, mkdirSync, openSync, readFileSync, readdirSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { pathToFileURL } from 'node:url'
import { buildCandidates } from './native-admission-patch.mjs'
import { buildUpdateCandidate } from './native-admission-update-patch.mjs'

const metadata = JSON.parse(readFileSync(new URL('./native-update-profile-v1.json', import.meta.url), 'utf8'))
export const profile = Object.freeze({ ...metadata, files: Object.freeze(metadata.files.map(file => Object.freeze(file))) })
const digest = value => createHash('sha256').update(value).digest('hex')
const template = '_xkeen/02_install/07_install_register/04_register_init.sh'
const installer = '_xkeen/02_install/03_install_xkeen.sh'
const directories = new Set([''])
const sourceFiles = new Map(profile.files.map(file => [file.path, file]))
for (const { path } of profile.files) {
  let parent = dirname(path)
  while (parent !== '.') { directories.add(parent); parent = dirname(parent) }
}

export function readProfileDirectory(root) {
  const entries = new Map()
  let visited = 0
  function visit(relative) {
    const path = join(root, relative)
    const stat = lstatSync(path)
    if (stat.isSymbolicLink() || !stat.isDirectory() || !directories.has(relative)) throw new Error('unsupported profile directory')
    for (const name of readdirSync(path)) {
      if (++visited > 128 || !/^[A-Za-z0-9_.-]+$/.test(name)) throw new Error('unsupported profile entry')
      const child = relative ? `${relative}/${name}` : name
      const info = lstatSync(join(root, child))
      if (info.isDirectory()) { visit(child); continue }
      if (!info.isFile() || info.isSymbolicLink() || info.nlink !== 1 || info.size !== sourceFiles.get(child)?.size) throw new Error('unsafe profile file')
      const fd = openSync(join(root, child), constants.O_RDONLY | constants.O_NOFOLLOW)
      try {
        const opened = fstatSync(fd)
        if (!opened.isFile() || opened.dev !== info.dev || opened.ino !== info.ino || opened.size !== info.size) throw new Error('changed profile file')
        entries.set(child, readFileSync(fd))
      } finally { closeSync(fd) }
    }
  }
  visit('')
  return entries
}

export function buildUpdateProfile(entries) {
  if (!(entries instanceof Map) || entries.size !== profile.files.length) throw new Error('unsupported native profile inventory')
  for (const file of profile.files) {
    const bytes = entries.get(file.path)
    if (!Buffer.isBuffer(bytes) || bytes.length !== file.size || digest(bytes) !== file.sha256) throw new Error('unsupported native profile bytes')
  }
  const admission = buildCandidates({ dispatcher: entries.get('xkeen'), init: entries.get(template) })
  const update = buildUpdateCandidate(entries.get(installer))
  const overlays = new Map([
    ['xkeen', admission.dispatcher], [template, admission.registrationTemplate], [installer, update.candidate],
  ])
  const prepared = profile.files.map(file => {
    const bytes = overlays.get(file.path)
    return bytes ? { path: file.path, size: bytes.length, sha256: digest(bytes) } : { ...file }
  })
  return { overlays, manifest: {
    schema: 1, enabled: false, installed: false,
    upstreamCommit: profile.upstreamCommit, archiveSHA256: profile.archiveSHA256,
    source: profile.files.map(file => ({ ...file })), prepared,
    missing: ['native updater entry and final verification', 'stage worker target qualification'],
  } }
}

export function buildStageWorker(built) {
  // Render fixed shell checks, never interpret a router-supplied manifest.
  // All paths come from this repository's pinned public inventory.
  const payloadRoot = '/opt/lib/xkeen/native-profile-v1'
  const replace = (text, anchor, value) => {
    if (text.split(anchor).length !== 2) throw new Error('stage worker anchor changed')
    return text.replace(anchor, value)
  }
  const checks = files => files.map(file => `            _ns_file "$_ns_stage/${file.path}" ${file.sha256} ${file.size} || return 76`).join('\n')
  const shape = [...directories].sort().map(directory => {
    const children = [...sourceFiles.keys(), ...directories].filter(path => path && (dirname(path) === '.' ? '' : dirname(path)) === directory)
      .map(path => path.split('/').at(-1))
    const path = directory ? `$_ns_stage/${directory}` : '$_ns_stage'
    return `    [ -d "${path}" ] && [ ! -L "${path}" ] || return 76
    for _ns_item in "${path}"/* "${path}"/.[!.]* "${path}"/..?*; do
        [ -e "$_ns_item" ] || [ -L "$_ns_item" ] || continue
        [ ! -L "$_ns_item" ] || return 76
        case "\${_ns_item##*/}" in ${children.join('|')}) ;; *) return 76;; esac
    done`
  }).join('\n') + '\n    return 0'
  const overlays = [...built.overlays].map(([path, bytes], index) => ({ path, size: bytes.length, sha256: digest(bytes), index }))
  let text = readFileSync(new URL('./native-update-stage.sh', import.meta.url), 'utf8')
  text = replace(text, 'return 76 # COMPILE PROFILE SHAPE', shape)
  text = replace(text, 'return 76 # COMPILE SOURCE INVENTORY', '\n' + checks(profile.files))
  text = replace(text, 'return 76 # COMPILE PREPARED INVENTORY', '\n' + checks(built.manifest.prepared))
  text = replace(text, 'return 76 # COMPILE OVERLAY INVENTORY', overlays.map(file =>
    `    _ns_file ${payloadRoot}/overlay-${file.index}.disabled.sh ${file.sha256} ${file.size} || return 76`).join('\n'))
  text = replace(text, 'return 76 # COMPILE OVERLAY WRITES', overlays.map(file =>
    `    _ns_copy ${payloadRoot}/overlay-${file.index}.disabled.sh ${file.path} ${file.index} ${file.sha256} ${file.size} || return 77`).join('\n'))
  return Buffer.from(text)
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    if (process.argv.length !== 4) throw new Error('usage: PUBLIC_PROFILE_ROOT NEW_DIRECTORY')
    const built = buildUpdateProfile(readProfileDirectory(process.argv[2]))
    // Never replace an existing output. All executable payloads retain fences.
    mkdirSync(process.argv[3], { mode: 0o700 })
    for (const [index, [path, bytes]] of [...built.overlays].entries()) {
      writeFileSync(join(process.argv[3], `overlay-${index}.disabled.sh`), bytes, { flag: 'wx', mode: 0o600 })
      built.manifest.prepared.find(file => file.path === path).overlay = `overlay-${index}.disabled.sh`
    }
    const worker = buildStageWorker(built)
    writeFileSync(join(process.argv[3], 'stage-worker.disabled.sh'), worker, { flag: 'wx', mode: 0o600 })
    built.manifest.worker = { file: 'stage-worker.disabled.sh', size: worker.length, sha256: digest(worker) }
    writeFileSync(join(process.argv[3], 'manifest.json'), JSON.stringify(built.manifest, null, 2) + '\n', { flag: 'wx', mode: 0o600 })
    console.log(JSON.stringify({ enabled: false, files: built.manifest.source.length, overlays: built.overlays.size }))
  } catch {
    console.error('unsupported native profile; no installation performed')
    process.exitCode = 1
  }
}
