/*
THESIS: Recovery should read as a causal operating ledger, not a wall of health cards.
OWN-WORLD: Glacier canvas, white ledgers, slate typography, blue location, teal completion, amber attention.
STORY: The operator sees service scope, finds the narrowest issue, and follows a Run to its last safe fact.
FIRST VIEWPORT: A compact service rail and state header lead directly into conditional Attention, Fleet rows, and active Runs.
FORM: Established Converging Workbench extended as a restrained, layered Operate console; no concept seed was needed because architecture fixed the staging.
*/
import { Activity, Blocks, Cable, LayoutDashboard, ListChecks, Radio, ServerCog } from 'lucide-react'
import { NavLink, Navigate, Route, Routes } from 'react-router-dom'
import { useAPI, useLiveObservation } from './data'
import { FleetDetailPage } from './pages/FleetDetailPage'
import { OverviewPage } from './pages/OverviewPage'
import { RunDetailPage } from './pages/RunDetailPage'
import { RunsPage } from './pages/RunsPage'
import { SystemPage } from './pages/SystemPage'
import type { ServiceHealth } from './types'
import { relativeTime, StateBadge } from './ui'

const navigation = [
  { to: '/', label: 'Overview', icon: LayoutDashboard, end: true },
  { to: '/runs', label: 'Runs', icon: ListChecks, end: false },
  { to: '/system', label: 'System', icon: ServerCog, end: false },
]

function AppShell(): JSX.Element {
  const service = useAPI<ServiceHealth>('/api/v1/service')
  const live = useLiveObservation()
  return <div className="app-shell">
    <aside className="sidebar">
      <div className="brand"><img src="/pactline-mark.svg" alt="" /><div><strong>Pactline</strong><span>Fleet operations</span></div></div>
      <nav aria-label="Primary navigation">
        {navigation.map(item => <NavLink key={item.to} to={item.to} end={item.end} className={({ isActive }) => isActive ? 'nav-link active' : 'nav-link'}><item.icon size={17} /><span>{item.label}</span></NavLink>)}
      </nav>
      <div className="sidebar-status">
        <div className="connection-label"><Radio size={14} /><span>{live.mode === 'live' ? 'Live updates' : live.mode === 'polling' ? 'Polling fallback' : 'Connecting'}</span></div>
        <p>{service.data === undefined ? 'Waiting for service' : `${service.data.fleets.length} Fleets · ${service.data.registry.nonTerminalRuns} active`}</p>
      </div>
    </aside>
    <main className="main-area">
      <div className="service-strip">
        <div className="service-identity"><Activity size={17} /><strong>Fleet Service</strong>{service.data === undefined ? null : <StateBadge value={service.data.mode} />}</div>
        <div className="service-meta"><span><Cable size={14} />{service.data?.pactline.server ?? 'Pactline pending'}</span><span>Updated {relativeTime(service.data?.updatedAt)}</span></div>
      </div>
      <div className="page-area">
        <Routes>
          <Route path="/" element={<OverviewPage />} />
          <Route path="/fleets/:fleetId" element={<FleetDetailPage />} />
          <Route path="/runs" element={<RunsPage />} />
          <Route path="/runs/:runId" element={<RunDetailPage />} />
          <Route path="/system" element={<SystemPage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </div>
    </main>
    <nav className="mobile-nav" aria-label="Mobile navigation">
      {navigation.map(item => <NavLink key={item.to} to={item.to} end={item.end} className={({ isActive }) => isActive ? 'mobile-nav-link active' : 'mobile-nav-link'}><item.icon size={20} /><span>{item.label}</span></NavLink>)}
    </nav>
  </div>
}

export function App(): JSX.Element { return <AppShell /> }
