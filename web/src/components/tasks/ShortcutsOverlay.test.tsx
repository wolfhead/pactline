import { useState } from 'react'
import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import ShortcutsOverlay from './ShortcutsOverlay'

// vitest.config's test block doesn't set `globals: true`, so
// @testing-library/react's own auto-cleanup never registers; without this, a
// component rendered by one test stays mounted and pollutes the next test's
// queries. Mirrors src/pages/tasks/TaskListPage.test.tsx.
afterEach(() => {
  cleanup()
})

/** Stands in for App.tsx: a trigger button that mounts/unmounts the overlay,
 * exactly like the "? 快捷键" button and the showShortcuts state in App.tsx. */
function Host() {
  const [open, setOpen] = useState(false)
  return (
    <div>
      <button onClick={() => setOpen(true)}>打开快捷键</button>
      {open && <ShortcutsOverlay onClose={() => setOpen(false)} />}
    </div>
  )
}

describe('ShortcutsOverlay — modal focus behaviour', () => {
  it('moves focus into the dialog on open, traps Tab inside it, and restores focus to the trigger on close', async () => {
    render(<Host />)
    const trigger = screen.getByRole('button', { name: '打开快捷键' })
    trigger.focus()
    expect(trigger).toHaveFocus()

    fireEvent.click(trigger)

    const dialog = await screen.findByRole('dialog', { name: '键盘快捷键' })
    const closeButton = within(dialog).getByRole('button', { name: '关闭' })

    // Opening the dialog must move focus into it — not leave it sitting on
    // the trigger, which is what "aria-modal" without any focus management
    // actually did before this fix.
    await waitFor(() => expect(closeButton).toHaveFocus())

    // The close button is the dialog's only focusable element, so it is
    // simultaneously first and last in the trap. Tab from it must be
    // intercepted and wrapped back to itself rather than being allowed to
    // escape to the background page — asserting on fireEvent's return value
    // (false means preventDefault was called) rather than merely on
    // "focus didn't move" avoids a false pass from jsdom's lack of native
    // Tab-driven focus traversal, which would make an untrapped dialog look
    // identical to a trapped one on this assertion alone.
    const tabResult = fireEvent.keyDown(closeButton, { key: 'Tab' })
    expect(tabResult).toBe(false)
    expect(closeButton).toHaveFocus()

    const shiftTabResult = fireEvent.keyDown(closeButton, { key: 'Tab', shiftKey: true })
    expect(shiftTabResult).toBe(false)
    expect(closeButton).toHaveFocus()

    fireEvent.click(closeButton)

    expect(screen.queryByRole('dialog', { name: '键盘快捷键' })).not.toBeInTheDocument()
    // Focus must return to whatever opened the dialog, not to <body> or
    // wherever it happened to land.
    await waitFor(() => expect(trigger).toHaveFocus())
  })
})
