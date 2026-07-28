import { Navigate, Route, Routes } from 'react-router-dom'
import AppShell from './components/tasks/AppShell'
import TaskBoardPage from './pages/tasks/TaskBoardPage'
import TaskListPage from './pages/tasks/TaskListPage'

export default function App() {
  return (
    <AppShell>
      <Routes>
        <Route path="/tasks" element={<TaskListPage />} />
        <Route path="/tasks/board" element={<TaskBoardPage />} />
        {/* The list page owns this route too: it reads :number and mounts the
         * detail beside (xl) or over (lg/md/phone) the list. */}
        <Route path="/tasks/:number" element={<TaskListPage />} />

        <Route path="/" element={<Navigate to="/tasks" replace />} />
      </Routes>
    </AppShell>
  )
}
