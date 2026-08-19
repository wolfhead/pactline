import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import MarkdownEditableField from './MarkdownEditableField'

afterEach(cleanup)

describe('MarkdownEditableField', () => {
  it('reads as rendered Markdown and commits the exact edited source', () => {
    const onCommit = vi.fn()
    render(
      <MarkdownEditableField
        label="背景 / 问题"
        value="# Current context"
        onCommit={onCommit}
        placeholder="补充任务背景"
        required
      />,
    )

    const field = screen.getByRole('region', { name: '背景 / 问题' })
    expect(within(field).getByRole('heading', { name: 'Current context' })).toBeVisible()

    fireEvent.click(within(field).getByRole('button', { name: '编辑背景 / 问题' }))
    const editor = within(field).getByRole('textbox', { name: '背景 / 问题 Markdown' })
    fireEvent.change(editor, { target: { value: '## Updated context\n\n- verified' } })
    fireEvent.click(within(field).getByRole('tab', { name: '预览' }))
    expect(within(field).getByRole('heading', { name: 'Updated context' })).toBeVisible()

    fireEvent.click(within(field).getByRole('button', { name: '保存背景 / 问题' }))
    expect(onCommit).toHaveBeenCalledWith('## Updated context\n\n- verified')
  })

  it('cancels a draft and prevents required fields from committing blank content', () => {
    const onCommit = vi.fn()
    render(
      <MarkdownEditableField
        label="期望结果"
        value="Stable release"
        onCommit={onCommit}
        placeholder="补充期望结果"
        required
      />,
    )

    const field = screen.getByRole('region', { name: '期望结果' })
    fireEvent.click(within(field).getByRole('button', { name: '编辑期望结果' }))
    const editor = within(field).getByRole('textbox', { name: '期望结果 Markdown' })
    fireEvent.change(editor, { target: { value: '   ' } })
    expect(within(field).getByRole('button', { name: '保存期望结果' })).toBeDisabled()

    fireEvent.change(editor, { target: { value: 'Unsaved result' } })
    fireEvent.keyDown(editor, { key: 'Escape' })

    expect(onCommit).not.toHaveBeenCalled()
    expect(within(field).getByText('Stable release')).toBeVisible()

    fireEvent.click(within(field).getByRole('button', { name: '编辑期望结果' }))
    expect(within(field).getByRole('textbox', { name: '期望结果 Markdown' })).toHaveValue('Stable release')
  })
})
