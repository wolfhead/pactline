import { ArrowLeft, ExternalLink, FolderGit2, Route as RouteIcon } from 'lucide-react'
import { Link, useParams } from 'react-router-dom'
import { useAPI } from '../data'
import type { Fleet, ListData, RunSummary, ServiceHealth } from '../types'
import { EmptyState, ErrorState, label, LoadingState, PageHeading, Section, StaleNotice, StateBadge } from '../ui'

export function FleetDetailPage(): JSX.Element {
  const { fleetId = '' } = useParams()
  const fleet = useAPI<Fleet>(`/api/v1/fleets/${encodeURIComponent(fleetId)}`)
  const runs = useAPI<ListData<RunSummary>>(`/api/v1/runs?fleet=${encodeURIComponent(fleetId)}&limit=50`)
  const service = useAPI<ServiceHealth>('/api/v1/service')
  if (fleet.loading && fleet.data === undefined) return <LoadingState label="Loading Fleet" />
  if (fleet.data === undefined) return <ErrorState message={fleet.error ?? 'Fleet was not found.'} retry={fleet.refresh} />
  const value = fleet.data
  return <>
    <Link className="back-link" to="/"><ArrowLeft size={15} />Overview</Link>
    <PageHeading title={`Project ${String(value.projectNumber)}`} description={`Fleet ${value.id}`} aside={<StateBadge value={value.status} />} />
    <StaleNotice visible={fleet.stale || runs.stale || service.stale} />
    {value.message === undefined ? null : <div className="scope-message"><strong>This Fleet is degraded.</strong><span>{value.message}</span></div>}
    <div className="detail-ledger">
      <div><span>Status</span><strong>{value.enabled ? 'Enabled for discovery' : 'Disabled'}</strong></div>
      <div><span>Concurrency</span><strong>{String(value.maxConcurrentRuns)} Run{value.maxConcurrentRuns === 1 ? '' : 's'}</strong></div>
      <div><span>Delivery boundary</span><strong>{value.workPluginConfigured ? 'Work plugin configured' : 'Observation only'}</strong></div>
      <div><span>Discovery</span><strong>{label(value.discovery.status)} · {String(value.discovery.candidateCount)} candidates</strong></div>
    </div>
    <Section title="Routing policy" description="The configured Adapter route for each future Run stage." aside={<RouteIcon size={18} />}>
      <div className="route-grid">{Object.entries(value.routing).map(([stage, route]) => <div className="route-row" key={stage}><span>{label(stage)}</span><strong>{route.adapter}</strong><span>{route.model}</span><code>{route.reasoning ?? 'default'}</code></div>)}</div>
    </Section>
    <Section title="Discovery state" description="The latest completed Project poll recorded by the resident scheduler.">
      <dl className="definition-list"><div><dt>Status</dt><dd>{label(value.discovery.status)}</dd></div><div><dt>Last check</dt><dd>{value.discovery.checkedAt === undefined ? 'No discovery cycle recorded' : value.discovery.checkedAt}</dd></div><div><dt>Candidate queue</dt><dd>{String(value.discovery.candidateCount)} candidates at the last check</dd></div>{value.discovery.retryAt === undefined ? null : <div><dt>Retry after</dt><dd>{value.discovery.retryAt}</dd></div>}</dl>
    </Section>
    <Section title="Repository readiness" description="Local delivery authority remains outside Pactline task prose." aside={<FolderGit2 size={18} />}>
      <dl className="definition-list"><div><dt>Workspace root</dt><dd><code>{value.workspaceRoot}</code></dd></div><div><dt>Work plugin</dt><dd>{value.workPluginConfigured ? 'Configured and eligible for scheduling' : 'Not configured; this Fleet is observation-only'}</dd></div>{service.data === undefined ? null : <div><dt>Authoritative Project</dt><dd><a href={`${service.data.pactline.server}/projects/${String(value.projectNumber)}`} target="_blank" rel="noreferrer">Open in Pactline <ExternalLink size={14} /></a></dd></div>}</dl>
    </Section>
    <Section title="Runs" description="Active work first, followed by recent terminal outcomes.">
      {runs.data === undefined && runs.loading ? <LoadingState /> : runs.data?.items.length === 0 ? <EmptyState title="No local Runs" detail="This Fleet has not admitted work into the local registry." /> : <div className="compact-run-table">{runs.data?.items.map(run => <Link to={`/runs/${run.runId}`} key={run.runId}><span><strong>Task {run.taskNumber === undefined ? '—' : `#${String(run.taskNumber)}`}</strong><small>{label(run.stage)} · {run.adapter ?? 'Pending Adapter'}</small></span><span>{label(run.checkpoint)}</span><StateBadge value={run.state} /></Link>)}</div>}
    </Section>
  </>
}
