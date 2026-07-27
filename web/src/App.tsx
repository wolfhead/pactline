import { useEffect, useState } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'
import AppShell from './components/tasks/AppShell'
import ShortcutsOverlay from './components/tasks/ShortcutsOverlay'
import { isTypingTarget } from './keyboard'
import TaskBoardPage from './pages/tasks/TaskBoardPage'
import TaskListPage from './pages/tasks/TaskListPage'

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
    <AppShell>
      <Routes>
        <Route path="/tasks" element={<TaskListPage />} />
        <Route path="/tasks/board" element={<TaskBoardPage />} />
        {/* Task 9 makes this the three-column list+detail view; until then
         * TaskListPage mounts here too and simply ignores :number. */}
        <Route path="/tasks/:number" element={<TaskListPage />} />

        <Route path="/" element={<Navigate to="/tasks" replace />} />
      </Routes>
      {showShortcuts && <ShortcutsOverlay onClose={() => setShowShortcuts(false)} />}
    </AppShell>
  )
}
