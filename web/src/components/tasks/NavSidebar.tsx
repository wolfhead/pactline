import { useEffect, useState } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { useIdentity } from '@/identity'
import { listProjects, type Project } from '@/api/projects'
import { Plus } from 'lucide-react'
import { useTaskComposer } from './TaskComposer'

const ITEMS = [
  { to: '/tasks', label: '我的工作', end: false },
  { to: '/account/api-tokens', label: 'API Token', end: false },
  { to: '/api-docs', label: 'API 文档', end: false },
] as const

// Label management belongs to the tags-and-relations plan, not this pass —
// see task-6-brief.md.

/** The primary navigation, shared between the permanent lg/xl column and the
 * md drawer's `<Sheet>` body — one nav, two homes. `onNavigate` lets the
 * drawer close itself the instant a link is picked, since a drawer that
 * stays open after navigating reads as broken. */
export default function NavSidebar({ onNavigate }: { onNavigate?: () => void }) {
  const { actor, impersonation, me, isReadOnly } = useIdentity()
  const { openTaskComposer } = useTaskComposer()
  const location = useLocation()
  const [projects, setProjects] = useState<Project[]>([])
  useEffect(() => {
    let cancelled = false
    listProjects()
      .then((items) => { if (!cancelled) setProjects(items) })
      .catch(() => { if (!cancelled) setProjects([]) })
    return () => { cancelled = true }
  }, [me?.id, location.pathname])
  const baseItems = impersonation
    ? ITEMS.filter((item) => item.to !== '/account/api-tokens')
    : ITEMS
  const items = actor?.platform_role === 'ADMIN' && !impersonation
    ? [
        ...baseItems,
        { to: '/admin/users', label: '用户', end: false },
        { to: '/admin/invitations', label: '邀请', end: false },
        { to: '/admin/api-audit', label: 'API 审计', end: false },
      ]
    : baseItems
  return (
    <nav aria-label="主导航" className="flex flex-col gap-1 p-3">
      {!isReadOnly && (
        <button
          type="button"
          onClick={() => {
            openTaskComposer()
            onNavigate?.()
          }}
          className="mb-2 flex min-h-10 items-center justify-center gap-2 rounded-md bg-accent px-3 py-2 text-sm font-medium text-accent-fg shadow-sm hover:opacity-90"
        >
          <Plus className="size-4" aria-hidden="true" />
          新建任务
        </button>
      )}
      {items.slice(0, 1).map((item) => (
        <NavLink
          key={item.to}
          to={item.to}
          end={item.end}
          onClick={onNavigate}
          className={({ isActive }) =>
            cn(
              // flex, not the default inline: min-height does nothing to an
              // inline box, which is why the previous `min-h-11` here still
              // measured 36px on a coarse-pointer tablet. The 44px floor
              // itself comes from index.css's `nav a` coarse-pointer rule.
              'flex items-center rounded-md px-3 py-2 text-sm font-medium',
              isActive
                ? 'bg-accent text-accent-fg shadow-sm'
                : 'text-fg-muted hover:bg-surface/70 hover:text-fg',
            )
          }
        >
          {item.label}
        </NavLink>
      ))}
      <div className="mt-3 flex items-center justify-between px-3 text-xs font-medium uppercase tracking-wide text-fg-subtle">
        <span>项目</span>
        <NavLink to="/projects" onClick={onNavigate} className="text-accent">全部</NavLink>
      </div>
      {projects.map((project) => (
        <NavLink
          key={project.id}
          to={`/projects/${project.number}/overview`}
          onClick={onNavigate}
          title={project.name}
          className={({ isActive }) => cn(
            'line-clamp-2 min-h-11 rounded-md px-3 py-2 text-sm font-medium leading-5',
            isActive ? 'bg-accent text-accent-fg shadow-sm' : 'text-fg-muted hover:bg-surface/70 hover:text-fg',
          )}
        >
          {project.name}
        </NavLink>
      ))}
      <div className="my-2 border-t border-border" />
      {items.slice(1).map((item) => (
        <NavLink
          key={item.to}
          to={item.to}
          end={item.end}
          onClick={onNavigate}
          className={({ isActive }) => cn(
            'flex items-center rounded-md px-3 py-2 text-sm font-medium',
            isActive ? 'bg-accent text-accent-fg shadow-sm' : 'text-fg-muted hover:bg-surface/70 hover:text-fg',
          )}
        >
          {item.label}
        </NavLink>
      ))}
    </nav>
  )
}
