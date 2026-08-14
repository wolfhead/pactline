import { lazy, Suspense } from 'react'
import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import AppShell from './components/tasks/AppShell'
import { TaskComposerProvider } from './components/tasks/TaskComposer'
import TaskListPage from './pages/tasks/TaskListPage'
import AttachmentPreviewPage from './pages/tasks/AttachmentPreviewPage'
import ProjectListPage from './pages/projects/ProjectListPage'
import ProjectDetailPage from './pages/projects/ProjectDetailPage'
import LoginPage from './pages/auth/LoginPage'
import InvitePage from './pages/auth/InvitePage'
import AccessDecisionPage from './pages/auth/AccessDecisionPage'
import AdminUsersPage from './pages/admin/AdminUsersPage'
import APITokensPage from './pages/account/APITokensPage'
import AgentConversationsPage from './pages/agent/AgentConversationsPage'
import AdminAPIAuditPage from './pages/admin/AdminAPIAuditPage'
import AdminToolsPage from './pages/admin/AdminToolsPage'
import AdminConnectionsPage from './pages/admin/AdminConnectionsPage'
import { useIdentity } from './identity'

const APIDocsPage = lazy(() => import('./pages/APIDocsPage'))

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

  if (actor?.access_status !== 'APPROVED') {
    return <AccessDecisionPage />
  }

  if (/^\/tasks\/\d+\/attachments\/[^/]+\/preview$/.test(location.pathname)) {
    return (
      <Routes>
        <Route path="/tasks/:number/attachments/:attachmentID/preview" element={<AttachmentPreviewPage />} />
      </Routes>
    )
  }

  const adminVisible = actor?.platform_role === 'ADMIN' && !impersonation
  return (
    <TaskComposerProvider>
      <AppShell>
        <Routes>
          <Route path="/tasks" element={<TaskListPage />} />
          <Route path="/tasks/:number" element={<TaskListPage />} />
          <Route path="/projects" element={<ProjectListPage />} />
          <Route path="/projects/:number" element={<Navigate to="overview" replace />} />
          <Route path="/projects/:number/overview" element={<ProjectDetailPage view="overview" />} />
          <Route path="/projects/:number/milestones" element={<ProjectDetailPage view="milestones" />} />
          <Route path="/projects/:number/milestones/:milestoneID" element={<ProjectDetailPage view="milestones" />} />
          <Route path="/projects/:number/backlog" element={<ProjectDetailPage view="backlog" />} />
          <Route path="/account/api-tokens" element={!impersonation ? <APITokensPage /> : <Navigate to="/" replace />} />
          <Route path="/agent/conversations" element={<AgentConversationsPage />} />
          <Route
            path="/api-docs"
            element={(
              <Suspense fallback={<p className="p-5 text-sm text-fg-muted">正在加载 API 文档…</p>}>
                <APIDocsPage />
              </Suspense>
            )}
          />
          <Route path="/admin/users" element={adminVisible ? <AdminUsersPage /> : <Navigate to="/" replace />} />
          <Route path="/admin/api-audit" element={adminVisible ? <AdminAPIAuditPage /> : <Navigate to="/" replace />} />
          <Route path="/admin/tools" element={adminVisible ? <AdminToolsPage /> : <Navigate to="/" replace />} />
          <Route path="/admin/connections" element={adminVisible ? <AdminConnectionsPage /> : <Navigate to="/" replace />} />
          <Route path="/" element={<Navigate to="/tasks" replace />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </AppShell>
    </TaskComposerProvider>
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
