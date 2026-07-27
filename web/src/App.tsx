import { useEffect, useState } from 'react'
import { NavLink, Route, Routes } from 'react-router-dom'
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
  // The bounty/credit/scoring mechanism (work feed, board, portfolio, mine,
  // steward tools) moved to internal/legacy on both backend and frontend —
  // see web/src/legacy/README.md. Its routes stay mounted below, unlinked
  // from the nav but still reachable by direct URL (and drivable by the
  // Playwright suite) at their original paths ("/", "/board", ...). The
  // task product below therefore lives at its own paths ("/tasks",
  // "/tasks/board", "/tasks/:number") rather than reclaiming "/" — doing so
  // would silently break every legacy e2e spec that navigates to "/".
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

          <Route path="/" element={<WorkFeed />} />
          <Route path="/board" element={<Board />} />
          <Route path="/bounties/:id" element={<BountyDetail />} />
          <Route path="/users/:id/portfolio" element={<Portfolio />} />
          <Route path="/mine" element={<Mine />} />
          <Route path="/steward" element={<Steward />} />
        </Routes>
      </main>
      {showShortcuts && <ShortcutsOverlay onClose={() => setShowShortcuts(false)} />}
    </div>
  )
}
