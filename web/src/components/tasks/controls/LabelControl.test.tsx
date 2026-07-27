import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/react'
import LabelControl from './LabelControl'

// Label's wire shape is ID/Name/CreatedAt — capitalised — because
// domain.Label carries no json struct tags. See task-types.ts.
const ALL = [
  { ID: 'l1', Name: 'DSP', CreatedAt: '' },
  { ID: 'l2', Name: 'ADX', CreatedAt: '' },
  { ID: 'l3', Name: '降延迟', CreatedAt: '' },
]

describe('LabelControl', () => {
  // vitest.config's test block doesn't set `globals: true`, so
  // @testing-library/react's own auto-cleanup never registers; see the
  // identical workaround in StatusControl.test.tsx / AssigneeControl.test.tsx.
  afterEach(() => {
    cleanup()
  })

  it('adds to the existing selection instead of replacing it', async () => {
    const onChange = vi.fn()
    render(<LabelControl value={[ALL[0]]} all={ALL} onChange={onChange} ariaLabel="标签" />)
    fireEvent.click(screen.getByRole('button', { name: '标签' }))
    fireEvent.click(await screen.findByRole('checkbox', { name: 'ADX' }))
    // Decoy this catches: an implementation that sends only the just-clicked
    // id and silently drops DSP.
    expect(onChange).toHaveBeenCalledWith(['l1', 'l2'])
  })

  it('removes one without dropping the others', async () => {
    const onChange = vi.fn()
    render(<LabelControl value={[ALL[0], ALL[1]]} all={ALL} onChange={onChange} ariaLabel="标签" />)
    fireEvent.click(screen.getByRole('button', { name: '标签' }))
    fireEvent.click(await screen.findByRole('checkbox', { name: 'DSP' }))
    expect(onChange).toHaveBeenCalledWith(['l2'])
  })

  it('shows the current labels on the trigger without opening it', () => {
    render(<LabelControl value={[ALL[0], ALL[2]]} all={ALL} onChange={() => {}} ariaLabel="标签" />)
    const trigger = screen.getByRole('button', { name: '标签' })
    expect(trigger).toHaveTextContent('DSP')
    expect(trigger).toHaveTextContent('降延迟')
  })
})
