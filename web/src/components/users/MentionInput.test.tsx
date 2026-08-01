import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import MentionInput, { type MentionInputValue } from './MentionInput'

afterEach(cleanup)

const OPTIONS = [
  { id: 'u1', name: 'Alex Chen', email: 'alex@example.test' },
  { id: 'u2', name: 'Blair Zhang', email: 'blair@example.test' },
]

function Harness() {
  const [value, setValue] = useState<MentionInputValue>({ body: '', mentionedUserIDs: [] })
  return (
    <>
      <MentionInput
        value={value}
        options={OPTIONS}
        onChange={setValue}
        ariaLabel="评论内容"
        placeholder="添加评论…"
      />
      <output data-testid="body">{value.body}</output>
      <output data-testid="mentions">{value.mentionedUserIDs.join(',')}</output>
    </>
  )
}

function setCaretAtEnd(element: HTMLElement) {
  const selection = window.getSelection()
  const range = document.createRange()
  const node = element.lastChild ?? element
  range.selectNodeContents(node)
  range.collapse(false)
  selection?.removeAllRanges()
  selection?.addRange(range)
}

describe('MentionInput', () => {
  it('opens from an @ query and inserts a structured atomic mention', () => {
    render(<Harness />)
    const editor = screen.getByRole('combobox', { name: '评论内容' })
    editor.textContent = 'Please @bl'
    setCaretAtEnd(editor)
    fireEvent.input(editor)

    const option = screen.getByRole('option', { name: /Blair Zhang/ })
    fireEvent.mouseDown(option)

    expect(screen.getByTestId('body')).toHaveTextContent('Please @Blair Zhang')
    expect(screen.getByTestId('mentions')).toHaveTextContent('u2')
    expect(editor.querySelector('[data-mention-id="u2"]')).toHaveAttribute('contenteditable', 'false')
  })

  it('can replace or remove an existing mention from the inline token', () => {
    render(<Harness />)
    const editor = screen.getByRole('combobox', { name: '评论内容' })
    editor.textContent = '@bl'
    setCaretAtEnd(editor)
    fireEvent.input(editor)
    fireEvent.mouseDown(screen.getByRole('option', { name: /Blair Zhang/ }))

    fireEvent.mouseDown(editor.querySelector('[data-mention-id="u2"]') as HTMLElement)
    fireEvent.mouseDown(screen.getByRole('button', { name: '移除 @Blair Zhang' }))

    expect(screen.getByTestId('mentions')).toBeEmptyDOMElement()
    expect(editor.querySelector('[data-mention-id]')).not.toBeInTheDocument()
  })

  it('supports arrow-key navigation and Enter selection without leaving the editor', () => {
    render(<Harness />)
    const editor = screen.getByRole('combobox', { name: '评论内容' })
    editor.textContent = '@'
    setCaretAtEnd(editor)
    fireEvent.input(editor)

    fireEvent.keyDown(editor, { key: 'ArrowDown' })
    fireEvent.keyDown(editor, { key: 'Enter' })

    expect(screen.getByTestId('body')).toHaveTextContent('@Blair Zhang')
    expect(screen.getByTestId('mentions')).toHaveTextContent('u2')
    expect(document.activeElement).toBe(editor)
  })
})
