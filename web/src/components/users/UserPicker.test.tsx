import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import UserPicker from './UserPicker'

afterEach(cleanup)

const OPTIONS = [
  { id: 'u1', name: 'Alex Chen', email: 'alex@example.test', description: '项目管理员' },
  { id: 'u2', name: 'Blair Zhang', email: 'blair@example.test', description: '项目成员' },
]

describe('UserPicker', () => {
  it('filters candidates by name or email and reports mouse selection', () => {
    const onSelect = vi.fn()
    render(
      <UserPicker
        id="people"
        options={OPTIONS}
        query="blair@"
        activeOptionID="u2"
        selectedOptionIDs={[]}
        onActiveOptionChange={vi.fn()}
        onSelect={onSelect}
      />,
    )

    expect(screen.queryByRole('option', { name: /Alex Chen/ })).not.toBeInTheDocument()
    const option = screen.getByRole('option', { name: /Blair Zhang/ })
    fireEvent.mouseDown(option)
    expect(onSelect).toHaveBeenCalledWith(OPTIONS[1])
  })

  it('keeps an already selected candidate visible but unavailable', () => {
    render(
      <UserPicker
        id="people"
        options={OPTIONS}
        query=""
        activeOptionID="u1"
        selectedOptionIDs={['u2']}
        onActiveOptionChange={vi.fn()}
        onSelect={vi.fn()}
        selectedLabel="已提及"
      />,
    )

    expect(screen.getByRole('option', { name: /Blair Zhang.*已提及/ })).toHaveAttribute('aria-disabled', 'true')
  })
})
