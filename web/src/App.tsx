import { useEffect, useState } from 'react'
import { Navigate, NavLink, Route, Routes } from 'react-router-dom'
import ShortcutsOverlay from './components/tasks/ShortcutsOverlay'
import { UserSwitcher } from './identity'
import { isTypingTarget } from './keyboard'
import { ThemeToggle } from './theme'
import TaskBoardPage from './pages/tasks/TaskBoardPage'
import TaskDetailPage from './pages/tasks/TaskDetailPage'
import TaskListPage from './pages/tasks/TaskListPage'
import WorkFeed from './legacy/pages/WorkFeed'
import Board from './legacy/pages/Board'
import BountyDetail from './legacy/pages/BountyDetail'
import Portfolio from './legacy/pages/Portfolio'
import Mine from './legacy/pages/Mine'
import Steward from './legacy/pages/Steward'

function navLinkClass({ isActive }: { isActive: boolean }): string {
  return isActive ? 'active' : ''
}

export default function App() {
  // The task product owns "/". The retired bounty/credit/scoring mechanism
  // (work feed, board, portfolio, mine, steward tools) lives under /legacy —
  // see web/src/legacy/README.md. It used to sit at "/" so its e2e specs would
  // not need updating, which had the product's front door opening onto the
  // retired product; the specs were updated instead.
  const [showShortcuts, setShowShortcuts] = useState(false)

  // Global, reachable from every view: "?" opens the shortcuts overlay,
  // Escape closes it. Never fires while typing in a field.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (showShortcuts && e.key === 'Escape') {
        e.preventDefault()
        setShowShortcuts(false)
        return
      }
      if (e.key === '?' && !isTypingTarget(e.target)) {
        e.preventDefault()
        setShowShortcuts((v) => !v)
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [showShortcuts])

  return (
    <div className="app">
      <header>
        <nav>
          <NavLink to="/tasks" className={navLinkClass} end>列表</NavLink>
          <NavLink to="/tasks/board" className={navLinkClass}>看板</NavLink>
        </nav>
        <div className="row">
          <button type="button" onClick={() => setShowShortcuts(true)} title="键盘快捷键">
            ? 快捷键
          </button>
          <ThemeToggle />
          <UserSwitcher />
        </div>
      </header>
      <main>
        <Routes>
          <Route path="/tasks" element={<TaskListPage />} />
          <Route path="/tasks/board" element={<TaskBoardPage />} />
          <Route path="/tasks/:number" element={<TaskDetailPage />} />

          <Route path="/" element={<Navigate to="/tasks" replace />} />

          {/* Retired mechanism product. Reachable, out of the way, clearly marked. */}
          <Route path="/legacy" element={<WorkFeed />} />
          <Route path="/legacy/board" element={<Board />} />
          <Route path="/legacy/bounties/:id" element={<BountyDetail />} />
          <Route path="/legacy/users/:id/portfolio" element={<Portfolio />} />
          <Route path="/legacy/mine" element={<Mine />} />
          <Route path="/legacy/steward" element={<Steward />} />
        </Routes>
      </main>
      {showShortcuts && <ShortcutsOverlay onClose={() => setShowShortcuts(false)} />}
    </div>
  )
}
