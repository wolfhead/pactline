import { NavLink } from 'react-router-dom'
import { cn } from '@/lib/utils'

const ITEMS = [
  { to: '/tasks', label: '列表', end: true },
  { to: '/tasks/board', label: '看板', end: false },
  { to: '/projects', label: '项目', end: false },
] as const

// Label management belongs to the tags-and-relations plan, not this pass —
// see task-6-brief.md.

/** The primary navigation, shared between the permanent lg/xl column and the
 * md drawer's `<Sheet>` body — one nav, two homes. `onNavigate` lets the
 * drawer close itself the instant a link is picked, since a drawer that
 * stays open after navigating reads as broken. */
export default function NavSidebar({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <nav aria-label="主导航" className="flex flex-col gap-1 p-3">
      {ITEMS.map((item) => (
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
                ? 'bg-accent-subtle text-accent'
                : 'text-fg-muted hover:bg-surface-subtle hover:text-fg',
            )
          }
        >
          {item.label}
        </NavLink>
      ))}
    </nav>
  )
}
