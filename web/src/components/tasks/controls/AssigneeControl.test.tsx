import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import { fireEvent } from '@testing-library/react'
import AssigneeControl from './AssigneeControl'

const USERS = [
  { id: 'u1', name: '张沁', email: 'a@x.com' },
  { id: 'u2', name: '王溪', email: 'b@x.com' },
]

describe('AssigneeControl', () => {
  // See StatusControl.test.tsx: no `globals: true` in vitest.config, so RTL's
  // own auto-cleanup never registers.
  afterEach(() => {
    cleanup()
  })

  it('shows 未分配 as a real state, not an empty gap', () => {
    render(<AssigneeControl value={null} users={USERS} onChange={() => {}} ariaLabel="负责人" />)
    expect(screen.getByRole('combobox', { name: '负责人' })).toHaveTextContent('未分配')
  })

  it('reports null when 未分配 is chosen, not the empty string', async () => {
    const onChange = vi.fn()
    render(<AssigneeControl value="u1" users={USERS} onChange={onChange} ariaLabel="负责人" />)
    fireEvent.click(screen.getByRole('combobox', { name: '负责人' }))
    fireEvent.click(await screen.findByRole('option', { name: '未分配' }))
    // null clears the assignee (TaskPatchBody.assignee_id); '' would be sent
    // as a literal empty id and the PATCH would fail.
    expect(onChange).toHaveBeenCalledWith(null)
  })

  it('reports a plain user id for a real person', async () => {
    const onChange = vi.fn()
    render(<AssigneeControl value={null} users={USERS} onChange={onChange} ariaLabel="负责人" />)
    fireEvent.click(screen.getByRole('combobox', { name: '负责人' }))
    fireEvent.click(await screen.findByRole('option', { name: '王溪' }))
    expect(onChange).toHaveBeenCalledWith('u2')
  })
})
