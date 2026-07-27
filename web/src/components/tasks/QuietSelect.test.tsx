import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import QuietSelect from './QuietSelect'

// vitest.config's test block doesn't set `globals: true`, so
// @testing-library/react's own auto-cleanup never registers; without this, a
// component rendered by one test stays mounted and pollutes the next test's
// queries. Mirrors src/components/tasks/ShortcutsOverlay.test.tsx.
afterEach(() => {
  cleanup()
})

type Fruit = 'apple' | 'banana'
const OPTIONS: Fruit[] = ['apple', 'banana']
const LABELS: Record<Fruit, string> = { apple: '苹果', banana: '香蕉' }

/**
 * QuietSelect is the interaction this whole redesign turns on: every value
 * that used to be a permanently-open <select> (status/priority/assignee in
 * the list, board and detail views) now reads as plain text until it's
 * interacted with. These tests exercise that generically, independent of
 * any one field.
 */
describe('QuietSelect', () => {
  it('reads as a quiet button, not a <select>, until interacted with', () => {
    render(<QuietSelect value="apple" options={OPTIONS} labels={LABELS} onChange={vi.fn()} ariaLabel="水果" />)

    expect(screen.getByRole('button', { name: '水果' })).toHaveTextContent('苹果')
    expect(screen.queryByRole('combobox', { name: '水果' })).not.toBeInTheDocument()
  })

  it('reveals the real <select> on click, and commits the instant an option is chosen', () => {
    const onChange = vi.fn()
    render(<QuietSelect value="apple" options={OPTIONS} labels={LABELS} onChange={onChange} ariaLabel="水果" />)

    fireEvent.click(screen.getByRole('button', { name: '水果' }))

    const select = screen.getByRole('combobox', { name: '水果' })
    expect(select).toHaveValue('apple')
    // The quiet button is gone while the control is open — there is only
    // ever one of the two mounted at a time.
    expect(screen.queryByRole('button', { name: '水果' })).not.toBeInTheDocument()

    fireEvent.change(select, { target: { value: 'banana' } })

    expect(onChange).toHaveBeenCalledWith('banana')
    // Choosing an option collapses the control straight back to the quiet
    // display — no separate "save" step, and no <select> left open.
    expect(screen.queryByRole('combobox', { name: '水果' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '水果' })).toBeInTheDocument()
  })

  it('reveals the real <select> on Enter too, for a keyboard-only user', () => {
    render(<QuietSelect value="apple" options={OPTIONS} labels={LABELS} onChange={vi.fn()} ariaLabel="水果" />)

    const trigger = screen.getByRole('button', { name: '水果' })
    trigger.focus()
    fireEvent.keyDown(trigger, { key: 'Enter' })

    expect(screen.getByRole('combobox', { name: '水果' })).toBeInTheDocument()
  })

  it('reverts on Escape without calling onChange, since nothing was chosen', () => {
    const onChange = vi.fn()
    render(<QuietSelect value="apple" options={OPTIONS} labels={LABELS} onChange={onChange} ariaLabel="水果" />)

    fireEvent.click(screen.getByRole('button', { name: '水果' }))
    const select = screen.getByRole('combobox', { name: '水果' })

    fireEvent.keyDown(select, { key: 'Escape' })

    // Back to the quiet display, showing the original (unchanged) value —
    // Escape is a cancel, not a revert-a-draft, since a <select> only ever
    // "changes" by an explicit choice; nothing here was ever committed.
    expect(screen.queryByRole('combobox', { name: '水果' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '水果' })).toHaveTextContent('苹果')
    expect(onChange).not.toHaveBeenCalled()
  })

  it('renders nothing for a value the caller marks as genuinely empty', () => {
    render(
      <QuietSelect
        value="apple"
        options={OPTIONS}
        labels={LABELS}
        onChange={vi.fn()}
        ariaLabel="水果"
        renderQuiet={() => null}
      />,
    )

    const trigger = screen.getByRole('button', { name: '水果' })
    expect(trigger).toHaveTextContent('')
  })
})
