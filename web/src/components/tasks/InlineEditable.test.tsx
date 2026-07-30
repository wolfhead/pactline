import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import InlineEditable from './InlineEditable'

describe('InlineEditable', () => {
  it('discards a draft on Escape without closing a parent inspector', () => {
    const onCommit = vi.fn()
    const onParentKeyDown = vi.fn()

    render(
      <div onKeyDown={onParentKeyDown}>
        <InlineEditable
          value="Committed title"
          ariaLabel="Task title"
          onCommit={onCommit}
        />
      </div>,
    )

    const field = screen.getByRole('textbox', { name: 'Task title' })
    fireEvent.change(field, { target: { value: 'Discarded draft' } })
    fireEvent.keyDown(field, { key: 'Escape' })

    expect(field).toHaveValue('Committed title')
    expect(onCommit).not.toHaveBeenCalled()
    expect(onParentKeyDown).not.toHaveBeenCalled()
  })
})
