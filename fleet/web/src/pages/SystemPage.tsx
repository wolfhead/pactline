import { Boxes, Database, FileCog, Link2, Server, TimerReset } from 'lucide-react'
import { useAPI } from '../data'
import type { AdapterHealth, ListData, ServiceHealth } from '../types'
import { CopyValue, ErrorState, label, LoadingState, PageHeading, relativeTime, Section, StaleNotice, StateBadge } from '../ui'

function DependencyRow({ icon, title, status, detail, checkedAt }: { icon: JSX.Element; title: string; status: 'unknown' | 'ok' | 'error'; detail: string; checkedAt?: string }): JSX.Element {
  return <div className="dependency-row"><span className="dependency-icon">{icon}</span><div><strong>{title}</strong><p>{detail}</p></div><div><StateBadge value={status} /><small>{relativeTime(checkedAt)}</small></div></div>
}

export function SystemPage(): JSX.Element {
  const service = useAPI<ServiceHealth>('/api/v1/service')
  const adapters = useAPI<ListData<AdapterHealth>>('/api/v1/adapters')
  if (service.loading && service.data === undefined) return <LoadingState label="Loading system health" />
  if (service.data === undefined) return <ErrorState message={service.error ?? 'System health is unavailable.'} retry={service.refresh} />
  const value = service.data
  return <>
    <PageHeading title="System" description="Service dependencies, configuration, and Adapter capability probes." aside={<StateBadge value={value.mode} />} />
    <StaleNotice visible={service.stale || adapters.stale} />
    <Section title="Dependencies" description="The latest local checks. A scoped failure does not erase healthy dependencies.">
      <div className="dependency-list"><DependencyRow icon={<Link2 size={18} />} title="Pactline" status={value.pactline.status} detail={`${value.pactline.server}${value.pactline.cliVersion === undefined ? '' : ` · CLI ${value.pactline.cliVersion}`}`} checkedAt={value.pactline.checkedAt} /><DependencyRow icon={<Database size={18} />} title="Registry" status={value.registry.status} detail={`Schema ${String(value.registry.schemaVersion)} · ${String(value.registry.nonTerminalRuns)} non-terminal Runs`} checkedAt={value.registry.checkedAt} /></div>
    </Section>
    <Section title="Configuration" description="Only revision identity and reload outcome are projected." aside={<FileCog size={18} />}>
      <dl className="system-ledger"><div><dt>Revision</dt><dd><CopyValue value={value.config.revision} /></dd></div><div><dt>Loaded</dt><dd>{relativeTime(value.config.loadedAt)}</dd></div><div><dt>Last reload</dt><dd>{relativeTime(value.config.lastReloadAt)}</dd></div><div><dt>Reload result</dt><dd className={value.config.lastReloadError === undefined ? '' : 'danger-text'}>{value.config.lastReloadError ?? 'Accepted'}</dd></div></dl>
    </Section>
    <Section title="Harness Adapters" description="Capabilities are probe results, not inferred from model names." aside={<Boxes size={18} />}>
      <div className="adapter-list">{(adapters.data?.items ?? value.adapters).map(adapter => <div className="adapter-row" key={adapter.id}><div><strong>{adapter.id}</strong><span>{adapter.version ?? 'Version unavailable'}</span></div><StateBadge value={adapter.status} /><div className="capability-list">{adapter.capabilities === undefined ? <span>No capability projection</span> : Object.entries(adapter.capabilities).map(([key, item]) => <span key={key}><small>{label(key)}</small><strong>{Array.isArray(item) ? item.join(', ') : String(item)}</strong></span>)}</div>{adapter.message === undefined ? null : <p className="adapter-message">{adapter.message}</p>}</div>)}</div>
    </Section>
    <Section title="Service identity" description="Useful when correlating local logs and diagnostic bundles." aside={<Server size={18} />}>
      <dl className="system-ledger"><div><dt>Service ID</dt><dd><CopyValue value={value.serviceId} /></dd></div><div><dt>Version</dt><dd>{value.version}</dd></div><div><dt>Started</dt><dd><TimerReset size={14} />{relativeTime(value.startedAt)}</dd></div><div><dt>Registry path</dt><dd><code>{value.registry.path}</code></dd></div></dl>
    </Section>
  </>
}
