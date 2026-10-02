import test from 'node:test'
import assert from 'node:assert/strict'
import { selectGoPackages } from './dev-check-go.mjs'

const graph = [
  { name: 'app/a', dir: 'internal/a', imports: [] },
  { name: 'app/b', dir: 'internal/b', imports: ['app/a'] },
  { name: 'app/c', dir: 'cmd/c', imports: ['app/b'] },
  { name: 'app/d', dir: 'internal/d', imports: [] },
]
test('selects changed package and all reverse dependencies, including test imports', () => {
  assert.deepEqual(selectGoPackages(['internal/a/a.go'], graph), ['app/a', 'app/b', 'app/c'])
  assert.deepEqual(selectGoPackages(['internal/b/b_test.go'], graph), ['app/b', 'app/c'])
  assert.deepEqual(selectGoPackages(['internal/a/testdata/input.txt'], graph), ['app/a', 'app/b', 'app/c'])
})
test('new/deleted packages, module and build inputs broaden rather than skip', () => {
  for (const path of ['internal/gone/a.go', 'go.mod', 'scripts/build-control-plane.sh', 'config/a.json', 'internal/webassets/dist/index.html']) {
    assert.deepEqual(selectGoPackages([path], graph), ['./...'])
  }
})
test('unrelated docs do not broaden known package edits; empty selection fails broad', () => {
  assert.deepEqual(selectGoPackages(['docs/README.md', 'internal/d/a.go'], graph), ['app/d'])
  assert.deepEqual(selectGoPackages([], graph), ['./...'])
})
