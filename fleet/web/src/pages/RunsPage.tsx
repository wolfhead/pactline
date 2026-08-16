import { ArrowRight, Filter } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { useAPI } from '../data'
import type { ListData, RunSummary } from '../types'
import { elapsedTime, EmptyState, ErrorState, label, LoadingState, PageHeading, StaleNotice, StateBadge } from '../ui'

export function RunsPage(): JSX.Element {
  const [scope, setScope] = useState('all')
  const path = useMemo(() => scope === 'all' ? '/api/v1/runs?limit=100' : `/api/v1/runs?state=${scope}&limit=100`, [scope])
  const runs = useAPI<ListData<RunSummary>>(path)
  return <>
    <PageHeading title="Runs" description="Active and retained attempts across every local Fleet." aside={<div className="filter-control"><Filter size={15} /><label htmlFor="run-scope">State</label><select id="run-scope" value={scope} onChange={event => setScope(event.target.value)}><option value="all">All states</option><option value="running_harness">Running Harness</option><option value="validating">Validating</option><option value="completed">Completed</option><option value="released">Released</option><option value="quarantined">Quarantined</option><option value="failed">Failed</option></select></div>} />
    <StaleNotice visible={runs.stale} />
    {runs.loading && runs.data === undefined ? <LoadingState /> : runs.data === undefined ? <ErrorState message={runs.error ?? 'Runs are unavailable.'} retry={runs.refresh} /> : runs.data.items.length === 0 ? <EmptyState title="No matching Runs" detail="Change the state filter or wait for Fleet Service to admit work." /> : <div className="data-table runs-table">
      <div className="table-head"><span>Task / Run</span><span>Fleet</span><span>Stage</span><span>Adapter</span><span>Age</span><span>Checkpoint</span><span>State</span><span /></div>
      {runs.data.items.map(run => <Link className="table-row" to={`/runs/${run.runId}`} key={run.runId}>
        <span data-label="Task"><strong>{run.taskNumber === undefined ? 'Pending Task' : `Task #${String(run.taskNumber)}`}</strong><small>{run.runId}</small></span><span data-label="Fleet"><strong>{run.fleetId}</strong><small>Project {String(run.projectNumber)}</small></span><span data-label="Stage">{label(run.stage)}</span><span data-label="Adapter">{run.adapter ?? 'Pending'}</span><span data-label="Age">{elapsedTime(run.createdAt)}</span><span data-label="Checkpoint">{label(run.checkpoint)}</span><span data-label="State"><StateBadge value={run.state} /></span><ArrowRight className="row-arrow" size={16} />
      </Link>)}
    </div>}
  </>
}
