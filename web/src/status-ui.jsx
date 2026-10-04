import { Badge } from '@/components/ui/badge'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { CSPProvider } from '@base-ui/react/csp-provider'
import { IconCircleCheck, IconAlertTriangle, IconCircleMinus, IconInfoCircle, IconShieldLock, IconWorld, IconBan } from '@tabler/icons-react'

export function StatusBadge({ tone = 'info', children }) {
  const Glyph = { success: IconCircleCheck, warning: IconAlertTriangle, muted: IconCircleMinus, danger: IconAlertTriangle, info: IconInfoCircle }[tone] || IconInfoCircle
  return <Badge variant="outline" className={`status-${tone}`}><Glyph data-icon="inline-start" aria-hidden="true" />{children}</Badge>
}

export function ActionHint({ label, description, disabled, children }) {
  const accessibleDescription = typeof description === 'string' ? description : undefined
  return <CSPProvider nonce={document.querySelector('meta[name="style-nonce"]')?.content} disableStyleElements><Tooltip>{disabled ? <TooltipTrigger render={<span className="inline-flex" tabIndex={0} aria-label={label} aria-description={accessibleDescription} />}>{children}</TooltipTrigger> : <TooltipTrigger render={children} aria-description={accessibleDescription} />}<TooltipContent><div className="flex flex-col gap-1"><strong>{label}</strong>{description && <span>{description}</span>}{disabled && <span>Unavailable in the current state.</span>}</div></TooltipContent></Tooltip></CSPProvider>
}

export function DestinationBadge({ target, tag }) {
  const kind = target?.protocol === 'freedom' ? 'direct' : target?.protocol === 'blackhole' ? 'block' : target?.kind === 'balancer' || ['vless', 'vmess', 'trojan', 'shadowsocks'].includes(target?.protocol) ? 'vpn' : 'other'
  const Glyph = { direct: IconWorld, block: IconBan, vpn: IconShieldLock, other: IconInfoCircle }[kind]
  return <Badge variant="outline" className={`status-${{ direct: 'success', block: 'danger', vpn: 'info', other: 'muted' }[kind]}`}><Glyph data-icon="inline-start" />{kind === 'other' ? tag : kind.toUpperCase()}{kind !== 'other' && <span className="font-normal opacity-80"> · {tag}</span>}</Badge>
}

export const formatRate = (value) => Number.isFinite(value) && value > 0 ? `${(value * 8 / 1e6).toFixed(1)} Mbps` : '—'
