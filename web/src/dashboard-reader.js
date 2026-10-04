// One reader owns dashboard and diagnostic refreshes. A refresh requested while
// a read is running executes afterward, so post-mutation reads cannot reuse a
// pre-mutation snapshot. Queued refreshes share one subsequent read.
export function createDashboardReader({ api, onData, onError, onDone }) {
  let epoch = 0
  let active = null
  let queued = []

  const invalidate = () => {
    epoch++
    active?.abort()
    active = null
    for (const item of queued) item.resolve()
    queued = []
  }
  const refresh = (full = true) => new Promise((resolve) => {
    queued.push({ full, resolve })
    drain()
  })
  async function drain() {
    if (active || !queued.length) return
    const items = queued
    queued = []
    const full = items.some((item) => item.full)
    const generation = epoch
    const controller = new AbortController()
    active = controller
    try {
      const options = { signal: controller.signal }
      let data
      if (full) {
        const [status, nodes, performance] = await Promise.all([
          api('/api/v1/status', options), api('/api/v1/nodes', options), api('/api/v1/performance', options),
        ])
        if (!status.controlPlane || !Array.isArray(nodes.nodes) || !Array.isArray(performance.nodes)) throw new Error('Invalid dashboard response. Refresh its status.')
        data = { status, nodes, performance }
      } else {
        const performance = await api('/api/v1/performance', options)
        if (!Array.isArray(performance.nodes)) throw new Error('Invalid diagnostic response. Refresh its status.')
        data = { performance }
      }
      if (generation === epoch) onData(data, full)
    } catch (error) {
      // Stop sibling requests after Promise.all failure as well.
      controller.abort()
      if (generation === epoch && error.name !== 'AbortError') onError(error)
    } finally {
      for (const item of items) item.resolve()
      if (generation === epoch) {
        active = null
        onDone()
        drain()
      }
    }
  }
  return { refresh, invalidate }
}
