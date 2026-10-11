import { IconCircleCheck, IconCircleDashed, IconCircleX, IconLoader2 } from '@tabler/icons-react'

export const triggerLabel = (status) => ({ nightly: 'Nightly review', operator: 'Test all and apply', manual: 'Speed test' })[status?.reviewTrigger] || (status?.manualSample ? 'Speed test' : 'Review')

const stopped = (state) => ['failed', 'deferred', 'cancelled', 'cleanup-pending'].includes(state)
const finished = (state) => ['completed', 'applying', 'consumed'].includes(state)

// reviewSteps derives the three visible stages of one review from its status:
// the latency probe of every enabled node, the speed test of the fastest that
// answered, and ranking with either an Apply or a recommendation.
export function reviewSteps(status) {
  const state = status?.state
  const phase = status?.reviewPhase
  const probes = status?.probes || []
  const answered = probes.filter((probe) => probe.answered).length
  const eligible = status?.eligibleCount || status?.totalEligible || 0
  const probed = status?.rttProbed || probes.length
  const planned = status?.selectedForSpeed || 0
  const measured = status?.attemptedCount || 0
  const batches = status?.batchTotal || 0
  const batch = status?.batchCount || 0
  const speedStarted = phase === 'speed' || batch > 0
  const probe = {
    key: 'probe', title: 'Latency probe',
    state: phase === 'rtt' && state === 'running' ? 'active' : phase === 'rtt' && stopped(state) ? 'failed' : probed > 0 || speedStarted ? 'done' : 'pending',
    detail: `${probed} of ${eligible} nodes probed${probed ? `, ${answered} answered` : ''}`,
    fraction: eligible ? probed / eligible : 0,
  }
  const speed = {
    key: 'speed', title: 'Speed test',
    state: !speedStarted ? (stopped(state) && phase !== 'rtt' ? 'failed' : 'pending') : state === 'running' ? 'active' : stopped(state) ? 'failed' : 'done',
    detail: !speedStarted ? (planned ? `${planned} fastest will be tested` : 'the fastest answering nodes') : `batch ${Math.min(batch + (state === 'running' ? 1 : 0), batches || 1)} of ${batches || 1} · ${measured} of ${planned} tested, ${status?.validCount || 0} valid`,
    fraction: planned ? measured / planned : 0,
  }
  const recommend = status?.manualSample
  const rank = {
    key: 'rank', title: recommend ? 'Ranking' : 'Ranking and Apply',
    state: state === 'applying' ? 'active' : state === 'completed' || state === 'consumed' ? (!recommend && !['applied', 'no-op'].includes(status?.appliedState) ? 'failed' : 'done') : stopped(state) && speedStarted ? 'failed' : 'pending',
    detail: state === 'applying' ? 'applying the best pool and verifying it…' : finished(state) ? (recommend ? (status?.canStage ? 'recommendation ready: Apply it below' : 'measured; no recommendation to apply') : status?.appliedState === 'applied' ? 'best pool applied and verified' : status?.appliedState === 'no-op' ? 'running pool is already the best' : 'pool kept') : `best ${status?.settings?.poolSize || 6} are chosen`,
    fraction: finished(state) && state !== 'applying' ? 1 : 0,
  }
  const overall = state === 'completed' || state === 'consumed' ? 1 : 0.3 * probe.fraction + 0.6 * (speed.state === 'done' ? 1 : speed.fraction) + (state === 'applying' ? 0.05 : 0)
  return { steps: [probe, speed, rank], overall: Math.max(0, Math.min(1, overall)) }
}

const icons = {
  done: <IconCircleCheck className="text-success" aria-hidden="true" />,
  active: <IconLoader2 className="animate-spin text-info" aria-hidden="true" />,
  failed: <IconCircleX className="text-destructive" aria-hidden="true" />,
  pending: <IconCircleDashed className="text-muted-foreground" aria-hidden="true" />,
}
const stateText = { done: 'done', active: 'in progress', failed: 'stopped', pending: 'waiting' }

export function ReviewProgress({ status }) {
  if (!status?.reviewPhase || !status.state || status.state === 'idle') return null
  const { steps, overall } = reviewSteps(status)
  const percent = Math.round(overall * 100)
  return <section className="flex flex-col gap-3 rounded-lg border p-3" aria-label={`${triggerLabel(status)} progress`}>
    <div className="flex items-center justify-between gap-2 text-sm"><strong className="font-medium">{triggerLabel(status)}</strong><span className="text-muted-foreground">{percent}%</span></div>
    <div className="h-2 overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent} aria-label="Review progress"><div className="h-full rounded-full bg-primary transition-[width]" style={{ width: `${percent}%` }} /></div>
    <ol className="grid gap-2 sm:grid-cols-3">
      {steps.map((step, index) => <li key={step.key} className="flex items-start gap-2 rounded-md bg-muted/40 p-2" data-step-state={step.state}>
        {icons[step.state]}
        <div className="flex min-w-0 flex-col"><span className="text-sm font-medium">{index + 1}. {step.title} <span className="sr-only">({stateText[step.state]})</span></span><span className="text-xs text-muted-foreground">{step.detail}</span></div>
      </li>)}
    </ol>
  </section>
}
