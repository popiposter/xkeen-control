// Validation/preparation can take 125s on MIPS. Keep reads and prompt delivery
// short; a timeout never authorizes replay of an unconfirmed mutation.
export function configRequestTimeout(path, body) {
  return body !== undefined && ['text', 'save-set', 'restore-saved', 'restore-previous', 'apply'].includes(path) ? 150_000 : 10_000
}

export function commandRequestTimeout(path, body) {
  return path === 'jobs/start' && ['start', 'restart'].includes(body?.action) ? 150_000 : 10_000
}
