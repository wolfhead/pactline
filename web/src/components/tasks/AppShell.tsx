import { useState, type ReactNode } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { Columns3, FolderKanban, LayoutList, LogOut, Menu, Plus, User } from 'lucide-react'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'
import { ThemeToggle } from '@/theme'
import { useIdentity } from '@/identity'
import { useBreakpoint } from '@/hooks/useBreakpoint'
import { cn } from '@/lib/utils'
import NavSidebar from './NavSidebar'

const BOTTOM_TABS = [
  { to: '/tasks', label: '列表', icon: LayoutList, end: true },
  { to: '/tasks/board', label: '看板', icon: Columns3, end: false },
  { to: '/projects', label: '项目', icon: FolderKanban, end: false },
] as const

// One shell, four arrangements — not four code paths. Navigation is a
// permanent column at lg and up, a drawer at md, and a bottom tab bar on a
// phone, because a 172px column and a 44px-tall thumb target cannot both fit
// on a 375px screen.
export default function AppShell({ children }: { children: ReactNode }) {
  const { actor, subject, impersonation, isReadOnly, logout, endImpersonation } = useIdentity()
  const tier = useBreakpoint()
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [meOpen, setMeOpen] = useState(false)

  const showPermanentNav = tier === 'lg' || tier === 'xl'
  const showDrawer = tier === 'md'
  const showBottomTabs = tier === 'phone'
  const showAdminLinks = actor?.platform_role === 'ADMIN' && !impersonation

  const accountControls = (
    <>
      <ThemeToggle />
      <div className="flex min-w-0 items-center gap-2">
        {subject?.avatar_url ? (
          <img src={subject.avatar_url} alt="" className="size-7 shrink-0 rounded-full object-cover" />
        ) : (
          <span className="grid size-7 shrink-0 place-items-center rounded-full bg-accent-subtle text-xs font-medium text-accent">
            {subject?.name.slice(0, 1).toUpperCase()}
          </span>
        )}
        <span className="max-w-32 truncate text-sm">{subject?.name}</span>
        <button
          type="button"
          aria-label="退出登录"
          onClick={() => void logout()}
          className="rounded-md p-1.5 text-fg-muted hover:bg-surface-subtle hover:text-fg"
        >
          <LogOut className="size-4" aria-hidden="true" />
        </button>
      </div>
    </>
  )

  return (
    <div className="flex h-dvh flex-col bg-surface text-fg">
      {impersonation && (
        <div role="status" className="flex shrink-0 items-center justify-center gap-3 bg-accent-subtle px-3 py-2 text-sm text-accent">
          <span>管理员 {actor?.name} 正以 {subject?.name} 身份只读查看</span>
          <button
            type="button"
            onClick={() => void endImpersonation()}
            className="rounded-md border border-accent px-2 py-1 font-medium"
          >
            退出只读查看
          </button>
        </div>
      )}
      <header
        // One row on every tier now. The phone header used to stack the two
        // switchers onto a full-width row of their own below the title,
        // which cost ~90px of a 844px screen before a single task was
        // visible; they live behind the 我的 tab instead (see below), so
        // there is nothing left to stack and the 390px mid-word wrap that
        // forced the stacking cannot happen either.
        className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-b border-border px-3 py-2 sm:px-4"
      >
        <div className="flex items-center gap-2">
          {showDrawer && (
            <Sheet open={drawerOpen} onOpenChange={setDrawerOpen}>
              <button
                type="button"
                aria-label="打开导航"
                onClick={() => setDrawerOpen(true)}
                className="flex h-8 items-center justify-center rounded-md border border-border-strong px-2.5 text-fg"
              >
                <Menu className="size-5" aria-hidden="true" />
              </button>
              <SheetContent side="left" className="w-64 p-0">
                <SheetTitle className="sr-only">主导航</SheetTitle>
                <NavSidebar onNavigate={() => setDrawerOpen(false)} />
              </SheetContent>
            </Sheet>
          )}
          <span className="text-sm font-semibold">任务面板</span>
        </div>
        {!showBottomTabs && <div className="flex items-center gap-3">{accountControls}</div>}
      </header>

      <div className="flex min-h-0 flex-1">
        {showPermanentNav && (
          <div className="w-44 shrink-0 overflow-y-auto border-r border-border">
            <NavSidebar />
          </div>
        )}
        <main className="min-w-0 flex-1 overflow-y-auto" data-read-only={isReadOnly || undefined}>{children}</main>
      </div>

      {showBottomTabs && (
        <nav
          aria-label="底部导航"
          // The safe-area bottom padding is not decoration: the iOS
          // home-indicator area sits right under this bar, and without it
          // the last row of tab labels is clipped on notched phones.
          // (Spelling the utility out in prose here would make Tailwind's
          // source scanner emit a broken rule for it.)
          className={cn(
            'grid shrink-0 border-t border-border bg-surface pb-[env(safe-area-inset-bottom)]',
            isReadOnly ? 'grid-cols-4' : 'grid-cols-5',
          )}
        >
          {BOTTOM_TABS.map((tab) => (
            <NavLink
              key={tab.to}
              to={tab.to}
              end={tab.end}
              className={({ isActive }) =>
                cn(
                  'flex min-h-11 flex-col items-center justify-center gap-0.5 py-1.5 text-[11px]',
                  isActive ? 'text-accent' : 'text-fg-muted',
                )
              }
            >
              <tab.icon className="size-5" aria-hidden="true" />
              {tab.label}
            </NavLink>
          ))}
          {!isReadOnly && (
            <Link
              to="/tasks"
              state={{ focusCreate: true }}
              className="flex min-h-11 flex-col items-center justify-center gap-0.5 py-1.5 text-[11px] text-fg-muted"
            >
              <Plus className="size-5" aria-hidden="true" />
              新建
            </Link>
          )}
          {/* 我的 is where the two switchers went. Permanently parked in the
           * phone header they cost ~90px — two full-width 44px selects and a
           * gap — above every task, on the one tier that has the least room
           * and the least reason to change theme or identity mid-scroll.
           * A sheet is the right trade: still one tap away, zero standing
           * cost. (A dedicated "my tasks" list stays out of scope — see
           * NavSidebar's tags-view note.) */}
          <Sheet open={meOpen} onOpenChange={setMeOpen}>
            <button
              type="button"
              onClick={() => setMeOpen(true)}
              className="flex min-h-11 flex-col items-center justify-center gap-0.5 py-1.5 text-[11px] text-fg-muted"
            >
              <User className="size-5" aria-hidden="true" />
              我的
            </button>
            <SheetContent
              side="bottom"
              // The same home-indicator reasoning as the tab bar above, and
              // now actually load-bearing: index.html declares
              // viewport-fit=cover, so the inset resolves to a real value on
              // a notched phone instead of always 0.
              className="gap-4 p-4 pb-[calc(1rem+env(safe-area-inset-bottom))]"
            >
              <SheetTitle className="text-sm">我的</SheetTitle>
              <div className="flex flex-col gap-3">{accountControls}</div>
              {showAdminLinks && (
                <div className="flex flex-col gap-1 border-t border-border pt-3">
                  <Link to="/admin/users" onClick={() => setMeOpen(false)} className="rounded-md px-3 py-2 text-sm hover:bg-surface-subtle">
                    用户管理
                  </Link>
                  <Link to="/admin/invitations" onClick={() => setMeOpen(false)} className="rounded-md px-3 py-2 text-sm hover:bg-surface-subtle">
                    邀请成员
                  </Link>
                </div>
              )}
            </SheetContent>
          </Sheet>
        </nav>
      )}
    </div>
  )
}
