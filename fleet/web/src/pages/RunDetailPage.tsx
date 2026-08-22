import { AlertTriangle, ArrowLeft, CheckCircle2, ExternalLink, GitBranch, History, ShieldCheck } from 'lucide-react'
import { Link, useParams } from 'react-router-dom'
import { useAPI } from '../data'
import type { RunDetail } from '../types'
import { CopyValue, ErrorState, label, LoadingState, PageHeading, relativeTime, Section, shortId, StaleNotice, StateBadge } from '../ui'

export function RunDetailPage(): JSX.Element {
  const { runId = '' } = useParams()
  const run = useAPI<RunDetail>(`/api/v1/runs/${encodeURIComponent(runId)}`)
  if (run.loading && run.data === undefined) return <LoadingState label="Loading Run evidence" />
  if (run.data === undefined) return <ErrorState message={run.error ?? 'Run was not found.'} retry={run.refresh} />
  const value = run.data
  const currentTimeline = value.timeline.at(-1)?.sequence
  return <>
    <Link className="back-link" to="/runs"><ArrowLeft size={15} />Runs</Link>
    <PageHeading title={value.taskNumber === undefined ? 'Pending Run' : `Task #${String(value.taskNumber)}`} description={`${label(value.stage)} · Project ${String(value.projectNumber)} · ${value.fleetId}`} aside={<StateBadge value={value.state} />} />
    <StaleNotice visible={run.stale} />
    <div className={`run-lead state-lead-${value.state}`}><div><span>Current state</span><strong>{label(value.state)}</strong><p>{value.error ?? value.disposition ?? 'The Run is progressing from its latest durable checkpoint.'}</p></div><div><span>Last safe checkpoint</span><strong>{label(value.checkpoint)}</strong><time>{relativeTime(value.updatedAt)}</time></div></div>
    <div className="identity-ledger">
      <div><span>Run</span><CopyValue value={value.runId} /></div><div><span>Claim</span>{value.claimId === undefined ? <strong>Not created</strong> : <CopyValue value={value.claimId} />}</div><div><span>Adapter Session</span>{value.runtimeSessionId === undefined ? <strong>Not started</strong> : <CopyValue value={value.runtimeSessionId} />}</div><div><span>Configuration</span><CopyValue value={value.configRevision} /></div>
      <div><span>Task versions</span><strong>{value.taskVersion ?? '—'} → {value.claimTaskVersion ?? '—'}</strong></div><div><span>Claim version</span><strong>{value.claimVersion ?? '—'}</strong></div><div><span>Adapter route</span><strong>{value.adapter ?? 'Pending'} / {value.model ?? 'Pending'}{value.reasoning === undefined ? '' : ` / ${value.reasoning}`}</strong></div><div><span>Updated</span><strong>{relativeTime(value.updatedAt)}</strong></div>
    </div>
    {value.verificationMismatch === undefined ? null : <Section title="Verification mismatch" description={`${label(value.verificationMismatch.stage)} · ${label(value.verificationMismatch.role)} · retained ${relativeTime(value.verificationMismatch.at)}${value.verificationMismatch.detailsOmitted === undefined ? '' : ` · ${String(value.verificationMismatch.detailsOmitted)} additional differences omitted`}`} aside={<AlertTriangle size={18} />}>
      <div className="mismatch-list">{value.verificationMismatch.details.map((item, index) => <article key={`${item.category}-${String(index)}`}>
        <header><strong>{label(item.category)}</strong>{item.command === undefined ? null : <code>{item.command}</code>}</header>
        <div className="mismatch-results">
          <div><span>Harness result</span>{item.harness === undefined ? <p>Not reported</p> : <><b>{label(item.harness.outcome)}</b><pre>{item.harness.summary}</pre></>}</div>
          <div><span>Fleet result</span>{item.fleet === undefined ? <p>Not observed</p> : <><b>{label(item.fleet.outcome)} · exit {item.fleet.exitCode ?? 'none'}</b><pre>{item.fleet.summary}</pre></>}</div>
        </div>
        {item.harnessChangedPaths === undefined && item.fleetChangedPaths === undefined ? null : <div className="mismatch-paths"><div><span>Harness paths{item.harnessChangedPathsOmitted === undefined ? '' : ` · ${String(item.harnessChangedPathsOmitted)} omitted`}</span><code>{item.harnessChangedPaths?.join('\n') || 'None'}</code></div><div><span>Fleet paths{item.fleetChangedPathsOmitted === undefined ? '' : ` · ${String(item.fleetChangedPathsOmitted)} omitted`}</span><code>{item.fleetChangedPaths?.join('\n') || 'None'}</code></div></div>}
      </article>)}</div>
    </Section>}
    <div className="run-columns">
      <Section title="Timeline" description="Durable state transitions in causal order." aside={<History size={18} />}>
        <ol className="timeline">{value.timeline.map(item => <li className={item.sequence === currentTimeline ? 'current' : ''} key={item.sequence}><span className="timeline-marker">{item.sequence === currentTimeline ? <CheckCircle2 size={14} /> : null}</span><div><div><strong>{item.title}</strong><time>{relativeTime(item.at)}</time></div>{item.detail === undefined ? null : <p>{item.detail}</p>}{item.checkpoint === undefined ? null : <code>{item.checkpoint}</code>}</div></li>)}</ol>
      </Section>
      <div className="evidence-column">
        <Section title="Workspace" description="Frozen local repository identity." aside={<GitBranch size={18} />}>
          <dl className="definition-list">{value.workspace === undefined ? <div><dt>Status</dt><dd>Workspace not recorded</dd></div> : Object.entries(value.workspace).map(([key, item]) => <div key={key}><dt>{label(key)}</dt><dd><code>{item}</code></dd></div>)}</dl>
        </Section>
        <Section title="External effects" description="Intent and observed facts; payloads are deliberately bounded." aside={<ShieldCheck size={18} />}>
          <div className="effect-list">{value.effects.map(item => <div key={item.kind}><span className={`effect-status ${item.status}`}>{item.status}</span><div><strong>{item.title}</strong>{item.detail === undefined ? null : <dl>{Object.entries(item.detail).map(([key, detail]) => <div key={key}><dt>{label(key)}</dt><dd>{key.toLowerCase().includes('url') ? <a href={String(detail)} target="_blank" rel="noreferrer">Open code change <ExternalLink size={13} /></a> : <code>{typeof detail === 'string' ? shortId(detail, 24) : String(detail)}</code>}</dd></div>)}</dl>}</div></div>)}</div>
        </Section>
      </div>
    </div>
  </>
}
