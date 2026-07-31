import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/react'
import StatusControl from './StatusControl'

describe('StatusControl', () => {
  // vitest.config's test block doesn't set `globals: true`, so
  // @testing-library/react's own auto-cleanup (which hooks a global
  // afterEach) never registers; without this, a trigger rendered by one
  // test stays mounted in the DOM and pollutes the next test's queries (see
  // the identical workaround in identity.test.tsx / theme-bridge.test.tsx).
  afterEach(() => {
    cleanup()
  })

  it('is a permanently visible control, not a hover-revealed one', () => {
    render(<StatusControl value="todo" onChange={() => {}} ariaLabel="任务 #142 状态" />)
    // The trigger must exist and be a combobox WITHOUT any interaction first.
    // A decoy that only renders a control after focus/hover fails here.
    const trigger = screen.getByRole('combobox', { name: '任务 #142 状态' })
    expect(trigger).toBeVisible()
    expect(trigger).toHaveTextContent('待办')
  })

  it('reports the chosen status', async () => {
    const onChange = vi.fn()
    render(<StatusControl value="todo" onChange={onChange} ariaLabel="任务 #142 状态" />)
    fireEvent.click(screen.getByRole('combobox', { name: '任务 #142 状态' }))
    fireEvent.click(await screen.findByRole('option', { name: '进行中' }))
    expect(onChange).toHaveBeenCalledWith('in_progress')
  })

  it('keeps the row variant icon-only without losing its accessible name', () => {
    render(
      <StatusControl
        value="in_review"
        onChange={() => {}}
        ariaLabel="任务 #142 状态"
        compact
      />,
    )
    const trigger = screen.getByRole('combobox', { name: '任务 #142 状态' })
    expect(trigger).toHaveAttribute('data-compact', 'true')
    expect(trigger).toHaveAttribute('title', '待评审')
    expect(trigger).not.toHaveTextContent('待评审')
  })

  it('offers every status, including the current one', async () => {
    render(<StatusControl value="todo" onChange={() => {}} ariaLabel="状态" />)
    fireEvent.click(screen.getByRole('combobox', { name: '状态' }))
    const names = (await screen.findAllByRole('option')).map((o) => o.textContent)
    // Five statuses, no gating: every status may move to every other status.
    expect(names).toEqual(['待办', '进行中', '待评审', '已完成', '已取消'])
  })
})
