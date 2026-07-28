import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import DueDateControl from './DueDateControl'

afterEach(cleanup)

describe('DueDateControl', () => {
  it('uses the product calendar instead of a native date input', async () => {
    render(<DueDateControl value="2026-07-30" onChange={() => {}} ariaLabel="截止日期" />)

    expect(screen.getByRole('button', { name: '截止日期' })).toHaveTextContent('7月30日')
    fireEvent.click(screen.getByRole('button', { name: '截止日期' }))

    expect(await screen.findByLabelText('选择截止日期')).toBeVisible()
    expect(screen.queryByDisplayValue('2026-07-30')).not.toBeInTheDocument()
    expect(screen.getByRole('gridcell', { name: '2026年7月30日' })).toHaveAttribute('aria-selected', 'true')
  })

  it('returns an ISO date and closes after selecting a day', async () => {
    const onChange = vi.fn()
    render(<DueDateControl value="2026-07-30" onChange={onChange} ariaLabel="截止日期" />)

    fireEvent.click(screen.getByRole('button', { name: '截止日期' }))
    fireEvent.click(await screen.findByRole('gridcell', { name: '2026年7月31日' }))

    expect(onChange).toHaveBeenCalledWith('2026-07-31')
    expect(screen.queryByLabelText('选择截止日期')).not.toBeInTheDocument()
  })

  it('clears an existing date', async () => {
    const onChange = vi.fn()
    render(<DueDateControl value="2026-07-30" onChange={onChange} ariaLabel="截止日期" />)

    fireEvent.click(screen.getByRole('button', { name: '截止日期' }))
    fireEvent.click(await screen.findByRole('button', { name: '清除日期' }))

    expect(onChange).toHaveBeenCalledWith(null)
  })
})
