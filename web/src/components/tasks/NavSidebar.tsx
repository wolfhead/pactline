import { useEffect, useMemo, useState, type ComponentType } from 'react'
import { Link, NavLink, useLocation } from 'react-router-dom'
import {
  BookOpen,
  Bot,
  ChevronRight,
  ClipboardList,
  Code2,
  FileClock,
  FolderKanban,
  KeyRound,
  MessageSquareText,
  Plus,
  ShieldCheck,
  Users,
} from 'lucide-react'
import { listProjects, type Project } from '@/api/projects'
import { useIdentity } from '@/identity'
import { cn } from '@/lib/utils'
import { useTaskComposer } from './TaskComposer'

interface NavigationItem {
  to: string
  label: string
  icon: ComponentType<{ className?: string; 'aria-hidden'?: boolean | 'true' | 'false' }>
  end?: boolean
}

const PRIMARY_ITEMS: NavigationItem[] = [
  { to: '/tasks', label: '我的工作', icon: ClipboardList },
]

const DEVELOPER_ITEMS: NavigationItem[] = [
  { to: '/account/api-tokens', label: 'API Token', icon: KeyRound },
  { to: '/api-docs', label: 'API 文档', icon: BookOpen },
]

const AGENT_ITEMS: NavigationItem[] = [
  { to: '/agent/conversations', label: '群聊配置', icon: MessageSquareText },
]

const ADMIN_ITEMS: NavigationItem[] = [
  { to: '/admin/users', label: '用户', icon: Users },
  { to: '/admin/api-audit', label: 'API 审计', icon: FileClock },
]

const PROJECT_LIMIT = 6

const activeLinkClass = 'bg-surface text-accent shadow-[0_2px_8px_rgb(23_43_61/0.06)]'
const inactiveLinkClass = 'text-fg-muted hover:bg-surface/70 hover:text-fg'

function NavigationLink({
  item,
  onNavigate,
  nested = false,
}: {
  item: NavigationItem
  onNavigate?: () => void
  nested?: boolean
}) {
  const Icon = item.icon
  return (
    <NavLink
      to={item.to}
      end={item.end}
      onClick={onNavigate}
      className={({ isActive }) => cn(
        'flex items-center gap-2 rounded-md px-2.5 py-2 text-sm font-medium',
        nested && 'pl-8 text-[13px]',
        isActive ? activeLinkClass : inactiveLinkClass,
      )}
    >
      <Icon className="size-4 shrink-0" aria-hidden="true" />
      <span className="truncate">{item.label}</span>
    </NavLink>
  )
}

function CollapsibleNavigationGroup({
  id,
  label,
  icon: Icon,
  items,
  open,
  onOpenChange,
  onNavigate,
}: {
  id: string
  label: string
  icon: NavigationItem['icon']
  items: NavigationItem[]
  open: boolean
  onOpenChange: (open: boolean) => void
  onNavigate?: () => void
}) {
  const location = useLocation()
  const containsActiveItem = items.some((item) => location.pathname.startsWith(item.to))

  return (
    <div>
      <button
        type="button"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => onOpenChange(!open)}
        className={cn(
          'flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-sm font-medium',
          containsActiveItem ? 'text-accent' : 'text-fg-muted hover:bg-surface/70 hover:text-fg',
        )}
      >
        <Icon className="size-4 shrink-0" aria-hidden="true" />
        <span className="min-w-0 flex-1 text-left">{label}</span>
        <ChevronRight
          className={cn('size-3.5 shrink-0 transition-transform', open && 'rotate-90')}
          aria-hidden="true"
        />
      </button>
      {open && (
        <div id={id} className="mt-0.5 flex flex-col gap-0.5">
          {items.map((item) => (
            <NavigationLink
              key={item.to}
              item={item}
              nested
              onNavigate={onNavigate}
            />
          ))}
        </div>
      )}
    </div>
  )
}

export default function NavSidebar({ onNavigate }: { onNavigate?: () => void }) {
  const { actor, impersonation, me, isReadOnly } = useIdentity()
  const { openTaskComposer } = useTaskComposer()
  const location = useLocation()
  const [projects, setProjects] = useState<Project[]>([])
  const developerItems = useMemo(
    () => impersonation
      ? DEVELOPER_ITEMS.filter((item) => item.to !== '/account/api-tokens')
      : DEVELOPER_ITEMS,
    [impersonation],
  )
  const showAdmin = actor?.platform_role === 'ADMIN' && !impersonation
  const [developerOpen, setDeveloperOpen] = useState(
    developerItems.some((item) => location.pathname.startsWith(item.to)),
  )
  const [agentOpen, setAgentOpen] = useState(
    AGENT_ITEMS.some((item) => location.pathname.startsWith(item.to)),
  )
  const [adminOpen, setAdminOpen] = useState(
    ADMIN_ITEMS.some((item) => location.pathname.startsWith(item.to)),
  )

  useEffect(() => {
    let cancelled = false
    listProjects()
      .then((items) => { if (!cancelled) setProjects(items) })
      .catch(() => { if (!cancelled) setProjects([]) })
    return () => { cancelled = true }
  }, [me?.id, location.pathname])

  useEffect(() => {
    if (developerItems.some((item) => location.pathname.startsWith(item.to))) {
      setDeveloperOpen(true)
    }
    if (AGENT_ITEMS.some((item) => location.pathname.startsWith(item.to))) {
      setAgentOpen(true)
    }
    if (ADMIN_ITEMS.some((item) => location.pathname.startsWith(item.to))) {
      setAdminOpen(true)
    }
  }, [developerItems, location.pathname])

  const projectNumber = Number(location.pathname.match(/^\/projects\/(\d+)/)?.[1])
  const currentProject = Number.isInteger(projectNumber)
    ? projects.find((project) => project.number === projectNumber)
    : undefined
  const visibleProjects = projects.slice(0, PROJECT_LIMIT)
  if (
    currentProject
    && !visibleProjects.some((project) => project.id === currentProject.id)
  ) {
    visibleProjects.splice(PROJECT_LIMIT - 1, 1, currentProject)
  }

  return (
    <nav aria-label="主导航" className="flex min-h-full flex-col gap-1 p-3">
      {!isReadOnly && (
        <button
          type="button"
          onClick={() => {
            openTaskComposer()
            onNavigate?.()
          }}
          className="mb-2 flex items-center justify-center gap-2 rounded-md bg-accent px-3 py-2 text-sm font-medium text-accent-fg shadow-[0_4px_12px_rgb(37_99_235/0.18)] hover:bg-accent/90"
        >
          <Plus className="size-4" aria-hidden="true" />
          新建任务
        </button>
      )}

      {PRIMARY_ITEMS.map((item) => (
        <NavigationLink key={item.to} item={item} onNavigate={onNavigate} />
      ))}

      <section aria-labelledby="project-navigation-title" className="mt-4">
        <div className="mb-1.5 flex items-center gap-2 px-2.5">
          <FolderKanban className="size-4 shrink-0 text-fg-subtle" aria-hidden="true" />
          <h2
            id="project-navigation-title"
            className="min-w-0 flex-1 text-xs font-semibold text-fg-muted"
          >
            项目
          </h2>
          <NavLink
            to="/projects"
            end
            onClick={onNavigate}
            className={({ isActive }) => cn(
              'rounded px-1 py-0.5 text-xs font-medium',
              isActive ? 'bg-accent-subtle text-accent' : 'text-fg-subtle hover:text-accent',
            )}
          >
            全部
          </NavLink>
        </div>

        <div className="flex flex-col gap-0.5">
          {visibleProjects.map((project) => (
            <Link
              key={project.id}
              to={`/projects/${project.number}/overview`}
              onClick={onNavigate}
              title={project.name}
              aria-current={project.number === projectNumber ? 'page' : undefined}
              className={cn(
                'flex items-start gap-2 rounded-md px-2.5 py-2 text-sm font-medium leading-5',
                project.number === projectNumber ? activeLinkClass : inactiveLinkClass,
              )}
            >
              <span
                className="mt-2 size-1.5 shrink-0 rounded-full bg-current opacity-70"
                aria-hidden="true"
              />
              <span className="line-clamp-2 min-w-0">{project.name}</span>
            </Link>
          ))}
          {projects.length === 0 && (
            <p className="px-2.5 py-2 text-xs text-fg-subtle">暂无项目</p>
          )}
          {projects.length > PROJECT_LIMIT && (
            <NavLink
              to="/projects"
              onClick={onNavigate}
              className="rounded-md px-6 py-1.5 text-xs font-medium text-fg-subtle hover:bg-surface/70 hover:text-accent"
            >
              查看全部 {projects.length} 个项目
            </NavLink>
          )}
        </div>
      </section>

      <div className="mt-auto border-t border-border pt-2">
        <CollapsibleNavigationGroup
          id="agent-navigation"
          label="Agent"
          icon={Bot}
          items={AGENT_ITEMS}
          open={agentOpen}
          onOpenChange={setAgentOpen}
          onNavigate={onNavigate}
        />
        <CollapsibleNavigationGroup
          id="developer-navigation"
          label="开发者工具"
          icon={Code2}
          items={developerItems}
          open={developerOpen}
          onOpenChange={setDeveloperOpen}
          onNavigate={onNavigate}
        />
        {showAdmin && (
          <CollapsibleNavigationGroup
            id="admin-navigation"
            label="系统管理"
            icon={ShieldCheck}
            items={ADMIN_ITEMS}
            open={adminOpen}
            onOpenChange={setAdminOpen}
            onNavigate={onNavigate}
          />
        )}
      </div>
    </nav>
  )
}
