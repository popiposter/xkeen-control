import { spawnSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// Iteration only. Final/release checks always use ./... without this selector.
export function selectGoPackages(changed, graph) {
  const selected = new Set()
  for (const file of changed) {
    if (/^(docs|plan|web)\//.test(file) || /^[^/]+\.md$/.test(file)) continue
    if (!/^(internal|cmd)\//.test(file) || file.startsWith('internal/webassets/')) return ['./...']
    const owner = graph.filter(p => file.startsWith(`${p.dir}/`)).sort((a, b) => b.dir.length - a.dir.length)[0]
    // A removed/new package cannot be inferred from the current graph.
    if (!owner || (!file.slice(owner.dir.length + 1).startsWith('testdata/') && file.slice(owner.dir.length + 1).includes('/'))) return ['./...']
    selected.add(owner.name)
  }
  if (!selected.size) return ['./...']
  let added
  do {
    added = false
    for (const pkg of graph) {
      if (!selected.has(pkg.name) && pkg.imports.some(dependency => selected.has(dependency))) {
        selected.add(pkg.name)
        added = true
      }
    }
  } while (added)
  return [...selected].sort()
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  let result = ['./...']
  try {
    const changed = JSON.parse(process.env.XKEEN_CHECK_PATHS || '[]')
    if (!Array.isArray(changed) || changed.some(p => typeof p !== 'string')) throw new Error('invalid paths')
    const listed = spawnSync('go', ['list', '-f', '{{.ImportPath}}|{{.Dir}}|{{join .Imports ","}}|{{join .TestImports ","}}|{{join .XTestImports ","}}', './...'], { encoding: 'utf8', maxBuffer: 16 * 1024 * 1024 })
    if (listed.status !== 0) throw new Error('package graph unavailable')
    const graph = listed.stdout.trim().split('\n').map(line => {
      const [name, dir, ...imports] = line.trim().split('|')
      return { name, dir: path.relative(process.cwd(), dir).replaceAll('\\', '/'), imports: imports.flatMap(value => value.split(',')) }
    })
    result = selectGoPackages(changed, graph)
  } catch {
    console.error('Go selection unavailable; checking all packages')
  }
  console.log(result.join('\n'))
}
