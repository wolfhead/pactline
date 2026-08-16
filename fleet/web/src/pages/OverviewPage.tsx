import { ArrowRight, CircleAlert, Clock3, RadioTower } from 'lucide-react'
import { Link } from 'react-router-dom'
import { useAPI } from '../data'
import type { Overview, ServiceHealth } from '../types'
import { elapsedTime, EmptyState, ErrorState, label, LoadingState, PageHeading, relativeTime, Section, shortId, StaleNotice, StateBadge } from '../ui'

export function OverviewPage(): JSX.Element {
  const service = useAPI<ServiceHealth>('/api/v1/service')
  const overview = useAPI<Overview>('/api/v1/overview')
  if ((service.loading || overview.loading) && (service.data === undefined || overview.data === undefined)) return <LoadingState />
  if (service.data === undefined) return <ErrorState message={service.error ?? 'Service state is unavailable.'} retry={service.refresh} />
  if (overview.data === undefined) return <ErrorState message={overview.error ?? 'Overview is unavailable.'} retry={overview.refresh} />
  const state = overview.data
  return <>
    <PageHeading title="Operations overview" description="Current scheduling scope and the latest durable Fleet facts." aside={<div className="headline-status"><span>{service.data.ready ? 'Scheduling is ready' : 'Scheduling needs attention'}</span><StateBadge value={service.data.mode} /></div>} />
    <StaleNotice visible={service.stale || overview.stale} />
    <div className="operating-summary" aria-label="Service summary">
      <div><span className="summary-label">Service</span><strong>{service.data.ready ? 'Ready for admission' : label(service.data.mode)}</strong></div>
      <div><span className="summary-label">Fleet scope</span><strong>{String(state.fleets.length)} Projects</strong></div>
      <div><span className="summary-label">Active Runs</span><strong>{String(state.activeRuns.length)}</strong></div>
      <div><span className="summary-label">Configuration</span><strong><code>{shortId(service.data.config.revision, 9)}</code></strong></div>
    </div>

    {state.attention.length === 0 ? null : <Section title="Attention" description="Conditions that narrow where to investigate next." className="attention-section">
      <div className="attention-list">{state.attention.map(item => <div className={`attention-row ${item.severity}`} key={item.id}>
        <CircleAlert size={19} /><div><strong>{item.title}</strong><p>{item.detail}</p></div><div className="attention-scope"><span>{label(item.scope)}</span><small>{relativeTime(item.checkedAt)}</small>{item.runId === undefined ? null : <Link to={`/runs/${item.runId}`}>Inspect Run <ArrowRight size={14} /></Link>}</div>
      </div>)}</div>
    </Section>}

    <Section title="Fleets" description="One local Fleet for each configured Pactline Project." aside={<span className="section-count">{String(state.fleets.length)} configured</span>}>
      {state.fleets.length === 0 ? <EmptyState title="No Fleets configured" detail="Add a Project-bound Fleet to the service YAML, then reload the service." /> : <div className="data-table fleet-table">
        <div className="table-head"><span>Project / Fleet</span><span>Discovery</span><span>Queue</span><span>Active</span><span>Health</span><span /></div>
        {state.fleets.map(fleet => <Link className="table-row" to={`/fleets/${encodeURIComponent(fleet.id)}`} key={fleet.id}>
          <span data-label="Project"><strong>Project {String(fleet.projectNumber)}</strong><small>{fleet.id} · {fleet.workPluginConfigured ? 'Work plugin ready' : 'Observation only'}</small></span>
          <span data-label="Discovery"><strong>{label(fleet.discovery.status)}</strong><small>{relativeTime(fleet.discovery.checkedAt)}</small></span>
          <span data-label="Queue">{String(fleet.discovery.candidateCount)}</span><span data-label="Active">{String(fleet.activeRunCount)}</span>
          <span data-label="Health"><StateBadge value={fleet.status} /></span><span className="row-arrow"><ArrowRight size={16} /></span>
        </Link>)}
      </div>}
    </Section>

    <Section title="Active Runs" description="Work currently inside the local durable state machine." aside={<span className="section-count">{String(state.activeRuns.length)} active</span>}>
      {state.activeRuns.length === 0 ? <EmptyState title="Healthy idle" detail="No local Run is active. Fleet Service will continue discovery on its configured interval." /> : <div className="run-list">{state.activeRuns.map(run => <Link className="run-row" to={`/runs/${run.runId}`} key={run.runId}>
        <span className="run-glyph"><RadioTower size={17} /></span><span><strong>Task {run.taskNumber === undefined ? 'pending' : `#${String(run.taskNumber)}`}</strong><small>{run.fleetId} · Project {String(run.projectNumber)}</small></span>
        <span><small>Stage</small><strong>{label(run.stage)}</strong></span><span><small>Adapter</small><strong>{run.adapter ?? 'Pending'}</strong></span>
        <span><small>Age</small><strong><Clock3 size={14} />{elapsedTime(run.createdAt)}</strong></span><span><small>Safe checkpoint</small><strong>{label(run.checkpoint)}</strong></span><StateBadge value={run.state} /><div className="run-mobile-meta">{label(run.stage)} · {run.adapter ?? 'Pending Adapter'} · {label(run.checkpoint)}</div><ArrowRight size={16} />
      </Link>)}</div>}
    </Section>

    <Section title="Recent outcomes" description="The latest locally retained terminal Runs.">
      {state.recentRuns.length === 0 ? <EmptyState title="No outcomes yet" detail="Completed, released, quarantined, and failed Runs will appear here." /> : <div className="outcome-list">{state.recentRuns.map(run => <Link to={`/runs/${run.runId}`} key={run.runId}><span><strong>Task {run.taskNumber === undefined ? '—' : `#${String(run.taskNumber)}`}</strong><small>{run.fleetId} · {label(run.stage)}</small></span><span>{run.disposition ?? label(run.state)}</span><StateBadge value={run.state} /><time>{relativeTime(run.updatedAt)}</time><ArrowRight size={15} /></Link>)}</div>}
    </Section>
  </>
}
