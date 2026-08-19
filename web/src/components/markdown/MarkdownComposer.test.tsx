import { useState } from 'react'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import MarkdownComposer from './MarkdownComposer'

afterEach(cleanup)

function ComposerHarness({ initialValue = '' }: { initialValue?: string }) {
  const [value, setValue] = useState(initialValue)
  return (
    <>
      <MarkdownComposer
        value={value}
        onChange={setValue}
        ariaLabel="Thread Markdown"
        placeholder="Write an update"
        rows={4}
      />
      <button type="button" onClick={() => setValue('')}>Clear draft</button>
    </>
  )
}

describe('MarkdownComposer', () => {
  it('preserves the controlled Markdown source while switching between writing and preview', () => {
    render(<ComposerHarness initialValue="Initial draft" />)

    const writeTab = screen.getByRole('tab', { name: '编写' })
    const previewTab = screen.getByRole('tab', { name: '预览' })
    expect(writeTab).toHaveAttribute('aria-selected', 'true')
    expect(previewTab).toHaveAttribute('aria-selected', 'false')

    const editor = screen.getByRole('textbox', { name: 'Thread Markdown' })
    expect(editor).toHaveValue('Initial draft')
    fireEvent.change(editor, { target: { value: '# Preview heading' } })

    fireEvent.click(previewTab)
    expect(previewTab).toHaveAttribute('aria-selected', 'true')
    expect(screen.queryByRole('textbox', { name: 'Thread Markdown' })).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Preview heading' })).toBeVisible()

    fireEvent.click(writeTab)
    expect(screen.getByRole('textbox', { name: 'Thread Markdown' })).toHaveValue('# Preview heading')
  })

  it('explains an empty preview instead of showing a blank panel', () => {
    render(<ComposerHarness />)

    fireEvent.click(screen.getByRole('tab', { name: '预览' }))
    expect(screen.getByText('输入 Markdown 后可在这里预览。')).toBeVisible()
  })

  it('returns to writing when the parent clears a submitted draft', () => {
    render(<ComposerHarness initialValue="Ready to send" />)

    fireEvent.click(screen.getByRole('tab', { name: '预览' }))
    fireEvent.click(screen.getByRole('button', { name: 'Clear draft' }))

    expect(screen.getByRole('tab', { name: '编写' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('textbox', { name: 'Thread Markdown' })).toHaveValue('')
  })
})
