import { Check, Copy, LoaderCircle, RefreshCw, TriangleAlert } from 'lucide-react'
import { type ReactNode, useState } from 'react'
import type { RunState, ServiceMode } from './types'

export function relativeTime(value?: string): string {
  if (value === undefined) return 'Not checked'
  const delta = Date.now() - new Date(value).getTime()
  if (!Number.isFinite(delta)) return 'Unknown time'
  const seconds = Math.max(0, Math.round(delta / 1_000))
  if (seconds < 60) return seconds < 5 ? 'just now' : `${String(seconds)}s ago`
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${String(minutes)}m ago`
  const hours = Math.round(minutes / 60)
  if (hours < 48) return `${String(hours)}h ago`
  return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(new Date(value))
}

export function elapsedTime(value: string): string {
  const seconds = Math.max(0, Math.round((Date.now() - new Date(value).getTime()) / 1_000))
  if (seconds < 60) return `${String(seconds)}s`
  if (seconds < 3600) return `${String(Math.floor(seconds / 60))}m ${String(seconds % 60)}s`
  return `${String(Math.floor(seconds / 3600))}h ${String(Math.floor((seconds % 3600) / 60))}m`
}

export function shortId(value?: string, length = 10): string { return value === undefined ? '—' : value.length > length ? `${value.slice(0, length)}…` : value }
export function label(value?: string): string {
  return value === undefined ? 'Unknown' : value.replace(/([a-z0-9])([A-Z])/g, '$1 $2').replaceAll('_', ' ').replace(/\b\w/g, match => match.toUpperCase())
}

export function StateBadge({ value }: { value: RunState | ServiceMode | 'healthy' | 'disabled' | 'ok' | 'error' | 'unknown' }): JSX.Element {
  const tone = ['completed', 'ready', 'healthy', 'ok'].includes(value) ? 'positive'
    : ['failed', 'error', 'stopped'].includes(value) ? 'critical'
      : ['quarantined', 'degraded', 'draining', 'released'].includes(value) ? 'warning'
        : ['running_harness', 'validating', 'delivering', 'settling', 'checking'].includes(value) ? 'active'
          : 'neutral'
  return <span className={`state-badge state-${tone}`}><span className="state-dot" aria-hidden="true" />{label(value)}</span>
}

export function PageHeading({ title, description, aside }: { title: string; description?: string; aside?: ReactNode }): JSX.Element {
  return <header className="page-heading"><div><h1>{title}</h1>{description === undefined ? null : <p>{description}</p>}</div>{aside}</header>
}

export function Section({ title, description, aside, children, className = '' }: { title: string; description?: string; aside?: ReactNode; children: ReactNode; className?: string }): JSX.Element {
  return <section className={`section ${className}`}><div className="section-heading"><div><h2>{title}</h2>{description === undefined ? null : <p>{description}</p>}</div>{aside}</div>{children}</section>
}

export function LoadingState({ label: text = 'Loading operational state' }: { label?: string }): JSX.Element {
  return <div className="state-panel" role="status"><LoaderCircle className="spin" size={20} /><span>{text}</span></div>
}

export function EmptyState({ title, detail }: { title: string; detail: string }): JSX.Element {
  return <div className="empty-state"><div className="empty-line" aria-hidden="true" /><h3>{title}</h3><p>{detail}</p></div>
}

export function ErrorState({ message, retry }: { message: string; retry(): void }): JSX.Element {
  return <div className="error-state" role="alert"><TriangleAlert size={20} /><div><strong>Observation unavailable</strong><p>{message}</p></div><button className="quiet-button" onClick={retry}><RefreshCw size={15} />Retry</button></div>
}

export function CopyValue({ value, children }: { value: string; children?: ReactNode }): JSX.Element {
  const [copied, setCopied] = useState(false)
  return <button className="copy-value" title="Copy value" onClick={() => {
    void navigator.clipboard.writeText(value).then(() => { setCopied(true); window.setTimeout(() => setCopied(false), 1_500) })
  }}>{children ?? <code>{shortId(value, 16)}</code>}{copied ? <Check size={14} /> : <Copy size={14} />}<span className="sr-only">{copied ? 'Copied' : 'Copy'}</span></button>
}

export function StaleNotice({ visible }: { visible: boolean }): JSX.Element | null {
  return visible ? <div className="stale-notice"><TriangleAlert size={15} />Showing the last valid snapshot while the service reconnects.</div> : null
}
