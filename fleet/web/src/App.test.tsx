import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { App } from './App'
import { LiveObservationProvider, useLiveObservation } from './data'
import type { AdapterHealth, Fleet, ListData, Overview, RunDetail, RunSummary, ServiceHealth } from './types'

const service: ServiceHealth = {
  serviceId: 'service-test', version: 'test', mode: 'ready', live: true, ready: true,
  startedAt: '2026-08-16T10:00:00.000Z', updatedAt: '2026-08-16T10:02:00.000Z',
  config: { revision: 'a'.repeat(64), loadedAt: '2026-08-16T10:00:00.000Z' },
  registry: { status: 'ok', path: '/tmp/fleet.sqlite3', schemaVersion: 3, nonTerminalRuns: 1 },
  pactline: { status: 'ok', server: 'http://localhost:8080' }, adapters: [],
  fleets: [{ id: 'development', projectNumber: 5, enabled: true, status: 'healthy', adapters: ['deepseek'], discovery: { status: 'ok', candidateCount: 1 } }],
}

function response<T>(data: T): Response {
  return { ok: true, status: 200, json: async () => ({ ok: true, data }) } as Response
}

afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe('Fleet Operations Console', () => {
  it('falls back to polling without discarding the observation surface', async () => {
    class FakeEventSource {
      static instance: FakeEventSource
      onopen: (() => void) | null = null
      onerror: (() => void) | null = null
      constructor() { FakeEventSource.instance = this }
      addEventListener(): void {}
      close(): void {}
    }
    function Probe(): JSX.Element { return <span>{useLiveObservation().mode}</span> }
    vi.stubGlobal('EventSource', FakeEventSource)
    render(<LiveObservationProvider><Probe /></LiveObservationProvider>)
    act(() => { FakeEventSource.instance.onerror?.() })
    expect(await screen.findByText('polling')).toBeInTheDocument()
  })

  it('puts ready state, Project-bound Fleets, and active Runs in the first operating flow', async () => {
    const overview: Overview = {
      attention: [],
      fleets: [{ id: 'development', projectNumber: 5, enabled: true, status: 'healthy', maxConcurrentRuns: 1, workPluginConfigured: true, workspaceRoot: '/tmp/work', routing: {}, activeRunCount: 1, recentRunCount: 2, discovery: { status: 'ok', candidateCount: 1 } }],
      activeRuns: [{ runId: 'run-41', fleetId: 'development', projectNumber: 5, taskNumber: 41, stage: 'execution', state: 'running_harness', adapter: 'deepseek', model: 'deepseek-v4-pro', checkpoint: 'adapter_session_observed', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }],
      recentRuns: [],
    }
    vi.stubGlobal('fetch', vi.fn(async (input: string | URL | Request) => String(input).includes('/overview') ? response(overview) : response(service)))
    render(<MemoryRouter initialEntries={['/']}><LiveObservationProvider><App /></LiveObservationProvider></MemoryRouter>)
    expect(await screen.findByRole('heading', { name: 'Operations overview' })).toBeInTheDocument()
    expect(screen.getByText('Ready for admission')).toBeInTheDocument()
    expect(screen.getByText('Project 5')).toBeInTheDocument()
    expect(screen.getByText('deepseek')).toBeInTheDocument()
    expect(screen.queryByText('Healthy idle')).not.toBeInTheDocument()
  })

  it('leads Run detail with current state, safe checkpoint, timeline, and bounded effects', async () => {
    const run: RunDetail = {
      runId: 'run-41', serviceId: 'service-test', fleetId: 'development', projectNumber: 5,
      configRevision: 'b'.repeat(64), taskNumber: 41, taskVersion: 7, claimId: 'claim-41', claimVersion: 1,
      claimTaskVersion: 8, runtimeSessionId: 'session-41', stage: 'execution', state: 'validating',
      adapter: 'deepseek', model: 'deepseek-v4-pro', reasoning: 'max', checkpoint: 'harness_result_observed',
      createdAt: '2026-08-16T10:00:00.000Z', updatedAt: '2026-08-16T10:03:00.000Z',
      verificationMismatch: {
        at: '2026-08-16T10:02:30.000Z', stage: 'execution', role: 'implementer',
        detailsOmitted: 6,
        details: [{
          category: 'test_failure', command: 'npm test',
          harness: { outcome: 'passed', summary: 'All passed.' },
          fleet: { outcome: 'failed', exitCode: 1, summary: 'One test failed.' },
        }],
      },
      timeline: [{ sequence: 1, at: '2026-08-16T10:00:00.000Z', kind: 'run.admitted', title: 'Run admitted', state: 'admitted' }, { sequence: 2, at: '2026-08-16T10:03:00.000Z', kind: 'run.transitioned', title: 'Run entered validating', state: 'validating', checkpoint: 'harness_result_observed' }],
      effects: [{ kind: 'harness_result', status: 'observed', title: 'Harness result', detail: { terminalState: 'success' }, updatedAt: '2026-08-16T10:03:00.000Z' }],
    }
    vi.stubGlobal('fetch', vi.fn(async (input: string | URL | Request) => String(input).includes('/runs/run-41') ? response(run) : response(service)))
    render(<MemoryRouter initialEntries={['/runs/run-41']}><LiveObservationProvider><App /></LiveObservationProvider></MemoryRouter>)
    expect(await screen.findByText('Last safe checkpoint')).toBeInTheDocument()
    expect(screen.getAllByText('Harness Result Observed').length).toBeGreaterThan(0)
    expect(screen.getByText('Run entered validating')).toBeInTheDocument()
    expect(screen.getAllByText('Harness result').length).toBeGreaterThan(0)
    expect(screen.getByText('Verification mismatch')).toBeInTheDocument()
    expect(screen.getByText('Test Failure')).toBeInTheDocument()
    expect(screen.getByText('Failed · exit 1')).toBeInTheDocument()
    expect(screen.getByText(/6 additional differences omitted/)).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByText('Loading Run evidence')).not.toBeInTheDocument())
  })

  it('keeps one Fleet scoped to one Project and exposes its discovery boundary', async () => {
    const fleet: Fleet = {
      id: 'development', projectNumber: 5, enabled: true, status: 'healthy', maxConcurrentRuns: 1,
      workPluginConfigured: true, workspaceRoot: '/tmp/work/project-5',
      routing: { execution: { adapter: 'deepseek', model: 'deepseek-v4-pro', reasoning: 'max' } },
      activeRunCount: 0, recentRunCount: 0,
      discovery: { status: 'ok', candidateCount: 2, checkedAt: '2026-08-16T10:02:00.000Z' },
    }
    const runs: ListData<RunSummary> = { items: [] }
    vi.stubGlobal('fetch', vi.fn(async (input: string | URL | Request) => {
      const url = String(input)
      if (url.includes('/fleets/development')) return response(fleet)
      if (url.includes('/runs?fleet=')) return response(runs)
      return response(service)
    }))
    render(<MemoryRouter initialEntries={['/fleets/development']}><LiveObservationProvider><App /></LiveObservationProvider></MemoryRouter>)
    expect(await screen.findByRole('heading', { name: 'Project 5' })).toBeInTheDocument()
    expect(screen.getByText('Fleet development')).toBeInTheDocument()
    expect(screen.getByText('Ok · 2 candidates')).toBeInTheDocument()
    expect(screen.getByText('Configured and eligible for scheduling')).toBeInTheDocument()
  })

  it('shows bounded dependency and Adapter diagnostics on the System route', async () => {
    const adapter: AdapterHealth = {
      id: 'deepseek', status: 'ok', version: '0.1.0-rc.6', checkedAt: '2026-08-16T10:02:00.000Z',
      capabilities: { sessionResume: false, terminalStates: ['success', 'failure'] },
    }
    const healthyService: ServiceHealth = { ...service, adapters: [adapter] }
    vi.stubGlobal('fetch', vi.fn(async (input: string | URL | Request) => String(input).includes('/adapters') ? response({ items: [adapter] }) : response(healthyService)))
    render(<MemoryRouter initialEntries={['/system']}><LiveObservationProvider><App /></LiveObservationProvider></MemoryRouter>)
    expect(await screen.findByRole('heading', { name: 'System' })).toBeInTheDocument()
    expect(screen.getAllByText('Pactline').length).toBeGreaterThan(0)
    expect(screen.getByText('Harness Adapters')).toBeInTheDocument()
    expect(screen.getByText('0.1.0-rc.6')).toBeInTheDocument()
    expect(screen.getByText('Session Resume')).toBeInTheDocument()
  })
})
