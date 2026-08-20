import { Link } from 'react-router-dom'
import { MoreHorizontal } from 'lucide-react'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'
import { CONTROL_TRIGGER_CLASS } from './controls/trigger'
import type { TaskNavigationState } from './task-navigation'

/** The row's "⋯" action menu. Offers 归档 or 恢复, mutually exclusive on
 * `archived` — never both. Archiving never opens a confirmation dialog: it
 * is a reversible action and the list surface's undo affordance (e2e
 * 18-archive-undo) is the safety net, so a confirm step would only add
 * friction to something already safe to reverse. */
export default function RowActionsMenu({
  taskNumber, archived, onArchive, onRestore, onCopyLink, linkState,
}: {
  taskNumber: number
  archived: boolean
  onArchive: () => void
  onRestore: () => void
  onCopyLink: () => void
  linkState?: TaskNavigationState
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label={`任务 #${taskNumber} 更多操作`}
        className={cn(
          CONTROL_TRIGGER_CLASS,
          'w-8 justify-center px-0',
        )}
      >
        <MoreHorizontal className="size-4" aria-hidden="true" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem asChild>
          <Link to={`/tasks/${taskNumber}`} state={linkState}>打开详情</Link>
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={onCopyLink}>复制链接</DropdownMenuItem>
        {archived ? (
          <DropdownMenuItem onSelect={onRestore}>恢复</DropdownMenuItem>
        ) : (
          <DropdownMenuItem onSelect={onArchive}>归档</DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
