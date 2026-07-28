import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import AppShell from './components/tasks/AppShell'
import TaskListPage from './pages/tasks/TaskListPage'
import ProjectListPage from './pages/projects/ProjectListPage'
import ProjectDetailPage from './pages/projects/ProjectDetailPage'
import LoginPage from './pages/auth/LoginPage'
import InvitePage from './pages/auth/InvitePage'
import AdminUsersPage from './pages/admin/AdminUsersPage'
import AdminInvitationsPage from './pages/admin/AdminInvitationsPage'
import { useIdentity } from './identity'

function ProtectedApplication() {
  const { status, error, actor, impersonation } = useIdentity()
  const location = useLocation()

  if (status === 'loading') {
    return <p className="p-5 text-sm text-fg-muted">正在确认登录状态…</p>
  }
  if (status === 'error') {
    return <p role="alert" className="p-5 text-sm text-danger">{error}</p>
  }
  if (status === 'unauthenticated') {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }

  const adminVisible = actor?.platform_role === 'ADMIN' && !impersonation
  return (
    <AppShell>
      <Routes>
        <Route path="/tasks" element={<TaskListPage />} />
        <Route path="/tasks/:number" element={<TaskListPage />} />
        <Route path="/projects" element={<ProjectListPage />} />
        <Route path="/projects/:number" element={<ProjectDetailPage />} />
        <Route path="/admin/users" element={adminVisible ? <AdminUsersPage /> : <Navigate to="/" replace />} />
        <Route path="/admin/invitations" element={adminVisible ? <AdminInvitationsPage /> : <Navigate to="/" replace />} />
        <Route path="/" element={<Navigate to="/tasks" replace />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </AppShell>
  )
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/invite" element={<InvitePage />} />
      <Route path="/*" element={<ProtectedApplication />} />
    </Routes>
  )
}
