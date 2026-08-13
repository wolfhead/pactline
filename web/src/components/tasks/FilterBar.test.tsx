import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/react'
import FilterBar, { DEFAULT_FILTERS } from './FilterBar'

// vitest.config's test block doesn't set `globals: true`, so
// @testing-library/react's own auto-cleanup never registers; without this a
// popover/select left open by one test would still be in the DOM for the
// next test's queries. See the identical workaround throughout
// controls/*.test.tsx.
afterEach(() => {
  cleanup()
})

describe('FilterBar', () => {
  it('combines two phase choices instead of replacing the first', async () => {
    const onChange = vi.fn()
    render(<FilterBar filters={{ ...DEFAULT_FILTERS, phases: ['backlog'] }} onChange={onChange}
      labels={[]} onLabelsChanged={() => {}} />)
    fireEvent.click(screen.getByRole('button', { name: /阶段/ }))
    fireEvent.click(await screen.findByRole('checkbox', { name: '执行中' }))
    // Decoy: an implementation that sets statuses to just the clicked one.
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ phases: ['backlog', 'in_progress'] }))
  })

  it('marks a filter chip as active only when it actually narrows', () => {
    const { rerender } = render(<FilterBar filters={DEFAULT_FILTERS} onChange={() => {}}
      labels={[]} onLabelsChanged={() => {}} />)
    expect(screen.getByRole('button', { name: /阶段/ })).toHaveAttribute('aria-pressed', 'false')
    rerender(<FilterBar filters={{ ...DEFAULT_FILTERS, phases: ['backlog'] }} onChange={() => {}}
      labels={[]} onLabelsChanged={() => {}} />)
    expect(screen.getByRole('button', { name: /阶段/ })).toHaveAttribute('aria-pressed', 'true')
  })

  // Routed from Task 9's dropped no-regression coverage: filters must
  // combine into one filters object per change, and clearing one must never
  // touch the others — a naive "last filter touched wins" implementation, or
  // one that rebuilds the whole filters object from scratch on every toggle,
  // would fail this.
  it('combines filters of different kinds into one request, and removing one leaves the others intact', async () => {
    const onChange = vi.fn()
    const { rerender } = render(<FilterBar filters={DEFAULT_FILTERS} onChange={onChange} labels={[]}
      onLabelsChanged={() => {}} />)

    fireEvent.click(screen.getByRole('button', { name: /阶段/ }))
    fireEvent.click(await screen.findByRole('checkbox', { name: '执行中' }))
    expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ phases: ['in_progress'], priorities: [] }))

    // The page would have committed that change and re-rendered with it —
    // simulate that before adding the second filter kind.
    rerender(<FilterBar filters={{ ...DEFAULT_FILTERS, phases: ['in_progress'] }} onChange={onChange}
      labels={[]} onLabelsChanged={() => {}} />)
    fireEvent.click(screen.getByRole('button', { name: /优先级/ }))
    fireEvent.click(await screen.findByRole('checkbox', { name: '高' }))
    expect(onChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ phases: ['in_progress'], priorities: ['high'] }),
    )

    // Now remove the status filter — the priority filter set alongside it
    // must survive untouched.
    rerender(<FilterBar filters={{ ...DEFAULT_FILTERS, phases: ['in_progress'], priorities: ['high'] }}
      onChange={onChange} labels={[]} onLabelsChanged={() => {}} />)
    fireEvent.click(screen.getByRole('button', { name: /阶段/ }))
    fireEvent.click(await screen.findByRole('checkbox', { name: '执行中' }))
    expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ phases: [], priorities: ['high'] }))
  })
})
