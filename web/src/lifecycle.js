// Mutations start only when the server explicitly reports both lifecycle
// flags as false. A missing, malformed or true flag blocks them.
export function lifecycleBlocked(lifecycle) {
  return typeof lifecycle?.maintenance !== 'boolean' || typeof lifecycle?.applying !== 'boolean' || lifecycle.maintenance || lifecycle.applying
}
