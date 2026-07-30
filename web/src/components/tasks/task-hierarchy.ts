import type { Task } from '@/task-types'

/** Keeps each visible child immediately after its parent without inventing
 * additional nesting. Children whose parent is outside the current filtered
 * collection retain their server order and still render their parent cue. */
export function orderTasksWithChildren(tasks: Task[]): Task[] {
  const childrenByParent = new Map<number, Task[]>()
  for (const task of tasks) {
    if (!task.parent) continue
    const children = childrenByParent.get(task.parent.number) ?? []
    children.push(task)
    childrenByParent.set(task.parent.number, children)
  }

  const ordered: Task[] = []
  const emitted = new Set<number>()
  for (const task of tasks) {
    if (task.parent) continue
    ordered.push(task)
    emitted.add(task.number)
    for (const child of childrenByParent.get(task.number) ?? []) {
      ordered.push(child)
      emitted.add(child.number)
    }
  }
  for (const task of tasks) {
    if (!emitted.has(task.number)) ordered.push(task)
  }
  return ordered
}
