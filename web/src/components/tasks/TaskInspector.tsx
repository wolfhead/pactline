import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'
import type { Task, UserRef } from '@/task-types'
import TaskDetail from './TaskDetail'

export default function TaskInspector({
  number,
  users,
  syncedTask,
  onPatched,
  onClose,
}: {
  number: number | null
  users: UserRef[]
  syncedTask: Task | null
  onPatched: (task: Task) => void
  onClose: () => void
}) {
  return (
    <Sheet
      open={number !== null}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <SheetContent
        className="w-full gap-0 overflow-y-auto p-0 outline-none sm:max-w-[36rem]"
        showCloseButton={false}
        overlayClassName="bg-fg/10"
        aria-describedby={undefined}
        onEscapeKeyDown={(event) => {
          const active = document.activeElement
          if (
            active instanceof HTMLInputElement
            || active instanceof HTMLTextAreaElement
            || (active instanceof HTMLElement && active.isContentEditable)
          ) {
            event.preventDefault()
          }
        }}
      >
        <SheetTitle className="sr-only">任务详情</SheetTitle>
        {number !== null && (
          <TaskDetail
            number={number}
            users={users}
            syncedTask={syncedTask}
            onPatched={onPatched}
            onClose={onClose}
          />
        )}
      </SheetContent>
    </Sheet>
  )
}
