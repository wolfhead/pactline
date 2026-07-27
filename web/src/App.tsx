import { useEffect, useState } from 'react'
import { Navigate, NavLink, Route, Routes } from 'react-router-dom'
import ShortcutsOverlay from './components/tasks/ShortcutsOverlay'
import { UserSwitcher } from './identity'
import { isTypingTarget } from './keyboard'
import { ThemeToggle } from './theme'
import TaskBoardPage from './pages/tasks/TaskBoardPage'
import TaskDetailPage from './pages/tasks/TaskDetailPage'
import TaskListPage from './pages/tasks/TaskListPage'

function navLinkClass({ isActive }: { isActive: boolean }): string {
  return isActive ? 'active' : ''
}

export default function App() {
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
        </Routes>
      </main>
      {showShortcuts && <ShortcutsOverlay onClose={() => setShowShortcuts(false)} />}
    </div>
  )
}
