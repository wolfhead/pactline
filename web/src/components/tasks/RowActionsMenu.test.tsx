import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import RowActionsMenu from './RowActionsMenu'

// Radix's DropdownMenuTrigger (unlike SelectTrigger) opens exclusively on
// `pointerdown`, with no `click` fallback — see the PointerEvent polyfill
// note in src/test/setup.ts. `fireEvent.click` alone never opens it, so
// every test here opens the menu through a real pointerdown first.
function openMenu(trigger: HTMLElement) {
  fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false, pointerType: 'mouse' })
}

describe('RowActionsMenu', () => {
  // vitest.config's test block doesn't set `globals: true`, so
  // @testing-library/react's own auto-cleanup never registers; see the
  // identical workaround in StatusControl.test.tsx / AssigneeControl.test.tsx.
  afterEach(() => {
    cleanup()
  })

  it('offers 归档 for a live task and 恢复 for an archived one, never both', async () => {
    const { rerender } = render(
      <MemoryRouter>
        <RowActionsMenu taskNumber={142} archived={false}
          onArchive={() => {}} onRestore={() => {}} onCopyLink={() => {}} />
      </MemoryRouter>,
    )
    openMenu(screen.getByRole('button', { name: '任务 #142 更多操作' }))
    expect(await screen.findByRole('menuitem', { name: '归档' })).toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '恢复' })).not.toBeInTheDocument()

    fireEvent.keyDown(document.activeElement!, { key: 'Escape' })
    rerender(
      <MemoryRouter>
        <RowActionsMenu taskNumber={142} archived
          onArchive={() => {}} onRestore={() => {}} onCopyLink={() => {}} />
      </MemoryRouter>,
    )
    openMenu(screen.getByRole('button', { name: '任务 #142 更多操作' }))
    expect(await screen.findByRole('menuitem', { name: '恢复' })).toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '归档' })).not.toBeInTheDocument()
  })

  it('archives without a confirmation dialog — undo is the affordance', async () => {
    const onArchive = vi.fn()
    render(
      <MemoryRouter>
        <RowActionsMenu taskNumber={7} archived={false}
          onArchive={onArchive} onRestore={() => {}} onCopyLink={() => {}} />
      </MemoryRouter>,
    )
    openMenu(screen.getByRole('button', { name: '任务 #7 更多操作' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: '归档' }))
    expect(onArchive).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })
})
