import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import AcceptanceChecklist from './AcceptanceChecklist'

afterEach(cleanup)

describe('AcceptanceChecklist', () => {
  it('submits one addressable criterion check with evidence', async () => {
    const onCheck = vi.fn().mockResolvedValue(undefined)
    render(
      <AcceptanceChecklist
        title="Project acceptance"
        criteria={[{
          id: 'criterion-1',
          version: 1,
          criterion: 'The release test passes',
          verification_instructions: 'Run make e2e',
          revision: 3,
          position: 0,
          current_check: null,
        }]}
        onAdd={vi.fn()}
        onCheck={onCheck}
        onUpdate={vi.fn()}
        onRemove={vi.fn()}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '检查' }))
    fireEvent.change(screen.getByPlaceholderText('检查证据或原因'), {
      target: { value: 'Playwright: 18 passed' },
    })
    fireEvent.click(screen.getByRole('button', { name: '记录' }))

    expect(onCheck).toHaveBeenCalledWith(
      expect.objectContaining({ id: 'criterion-1', revision: 3 }),
      'passed',
      'Playwright: 18 passed',
    )
  })

  it('edits and removes one criterion by stable id', async () => {
    const onUpdate = vi.fn().mockResolvedValue(undefined)
    const onRemove = vi.fn().mockResolvedValue(undefined)
    const criterion = {
      id: 'criterion-1',
      version: 1,
      criterion: 'Old proposition',
      verification_instructions: 'Old procedure',
      revision: 1,
      position: 0,
      current_check: null,
    }
    render(
      <AcceptanceChecklist
        title="Project acceptance"
        criteria={[criterion]}
        onAdd={vi.fn()}
        onCheck={vi.fn()}
        onUpdate={onUpdate}
        onRemove={onRemove}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '编辑' }))
    fireEvent.change(screen.getByDisplayValue('Old proposition'), { target: { value: 'New proposition' } })
    fireEvent.change(screen.getByDisplayValue('Old procedure'), { target: { value: 'New procedure' } })
    fireEvent.click(screen.getByRole('button', { name: '保存修改' }))
    expect(onUpdate).toHaveBeenCalledWith(criterion, 'New proposition', 'New procedure')

    fireEvent.click(screen.getByRole('button', { name: '移除' }))
    expect(onRemove).toHaveBeenCalledWith(criterion)
  })
})
