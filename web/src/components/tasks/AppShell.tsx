import { useState, type ReactNode } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { Columns3, LayoutList, Menu, Plus, User } from 'lucide-react'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'
import { ThemeToggle } from '@/theme'
import { UserSwitcher } from '@/identity'
import { useBreakpoint } from '@/hooks/useBreakpoint'
import { cn } from '@/lib/utils'
import NavSidebar from './NavSidebar'

const BOTTOM_TABS = [
  { to: '/tasks', label: '列表', icon: LayoutList, end: true },
  { to: '/tasks/board', label: '看板', icon: Columns3, end: false },
] as const

// One shell, four arrangements — not four code paths. Navigation is a
// permanent column at lg and up, a drawer at md, and a bottom tab bar on a
// phone, because a 172px column and a 44px-tall thumb target cannot both fit
// on a 375px screen.
export default function AppShell({ children }: { children: ReactNode }) {
  const tier = useBreakpoint()
  const [drawerOpen, setDrawerOpen] = useState(false)

  const showPermanentNav = tier === 'lg' || tier === 'xl'
  const showDrawer = tier === 'md'
  const showBottomTabs = tier === 'phone'

  return (
    <div className="flex h-dvh flex-col bg-surface text-fg">
      <header
        className={cn(
          'flex shrink-0 border-b border-border px-3 py-2 sm:px-4',
          // A phone header stacks: title first, switchers on their own
          // full-width row below. Sharing one row forced 当前身份's label
          // into single-character line wraps at 390px — Chinese text has no
          // spaces to break on, so a squeezed flex item wraps mid-word
          // instead of just truncating (caught in the Step 6 screenshots).
          tier === 'phone' ? 'flex-col items-stretch gap-2' : 'flex-wrap items-center justify-between gap-2',
        )}
      >
        <div className="flex items-center gap-2">
          {showDrawer && (
            <Sheet open={drawerOpen} onOpenChange={setDrawerOpen}>
              <button
                type="button"
                aria-label="打开导航"
                onClick={() => setDrawerOpen(true)}
                className="flex min-h-11 items-center justify-center rounded-md border border-border px-2.5 text-fg sm:min-h-8"
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
        <div className={cn('flex items-center gap-3', tier === 'phone' && 'flex-col items-stretch gap-2')}>
          <ThemeToggle />
          <UserSwitcher />
        </div>
      </header>

      <div className="flex min-h-0 flex-1">
        {showPermanentNav && (
          <div className="w-44 shrink-0 overflow-y-auto border-r border-border">
            <NavSidebar />
          </div>
        )}
        <main className="min-w-0 flex-1 overflow-y-auto">{children}</main>
      </div>

      {showBottomTabs && (
        <nav
          aria-label="底部导航"
          className="grid shrink-0 grid-cols-4 border-t border-border bg-surface"
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
          <Link
            to="/tasks"
            state={{ focusCreate: true }}
            className="flex min-h-11 flex-col items-center justify-center gap-0.5 py-1.5 text-[11px] text-fg-muted"
          >
            <Plus className="size-5" aria-hidden="true" />
            新建
          </Link>
          {/* A dedicated "my tasks" view is out of scope here (see
           * NavSidebar's tags-view note) — this degrades to the plain list
           * for now rather than staying inert, and deliberately isn't a
           * NavLink so it never doubles up the active indicator with 列表. */}
          <Link
            to="/tasks"
            className="flex min-h-11 flex-col items-center justify-center gap-0.5 py-1.5 text-[11px] text-fg-muted"
          >
            <User className="size-5" aria-hidden="true" />
            我的
          </Link>
        </nav>
      )}
    </div>
  )
}
