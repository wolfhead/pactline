import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import AppShell from './AppShell'

vi.mock('./TaskComposer', () => ({
  useTaskComposer: () => ({ openTaskComposer: vi.fn() }),
}))

function setWidth(px: number) {
  Object.defineProperty(window, 'innerWidth', { writable: true, configurable: true, value: px })
  window.dispatchEvent(new Event('resize'))
}

function renderShell(path = '/tasks') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <AppShell><p>内容</p></AppShell>
    </MemoryRouter>,
  )
}

describe('AppShell', () => {
  // vitest.config's test block doesn't set `globals: true`, so
  // @testing-library/react's own auto-cleanup never registers. Without this,
  // a previous test's still-mounted AppShell keeps its resize listener live
  // and reacts to a later test's setWidth(), rendering a second matching
  // landmark/button and breaking that test's query — see the identical
  // workaround in StatusControl.test.tsx.
  afterEach(() => {
    cleanup()
  })

  it('keeps navigation permanently on screen at lg and above', () => {
    setWidth(1280)
    renderShell()
    // navigation landmark present without opening anything
    expect(screen.getByRole('navigation', { name: '主导航' })).toBeVisible()
    expect(screen.queryByRole('button', { name: '打开导航' })).not.toBeInTheDocument()
  })

  it('keeps the task navigation active while a task detail is open', () => {
    setWidth(1280)
    renderShell('/tasks/678')
    expect(screen.getByRole('link', { name: '我的工作' })).toHaveAttribute('aria-current', 'page')
  })

  it('collapses navigation into a drawer below lg', () => {
    setWidth(900)
    renderShell()
    expect(screen.getByRole('button', { name: '打开导航' })).toBeVisible()
    expect(screen.queryByRole('navigation', { name: '主导航' })).not.toBeInTheDocument()
  })

  it('uses a bottom tab bar on a phone, not a drawer', () => {
    setWidth(375)
    renderShell()
    expect(screen.getByRole('navigation', { name: '底部导航' })).toBeVisible()
    expect(screen.queryByRole('button', { name: '打开导航' })).not.toBeInTheDocument()
  })

  it('keeps account controls in the header above a phone', () => {
    setWidth(900)
    renderShell()
    expect(screen.getByRole('button', { name: '退出登录' })).toBeVisible()
  })

  it('moves account controls off the phone header and behind the 我的 tab', () => {
    setWidth(390)
    renderShell()
    expect(screen.queryByRole('button', { name: '退出登录' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '我的' }))
    expect(screen.getByRole('button', { name: '退出登录' })).toBeVisible()
  })

  it('renders its children in every tier', () => {
    for (const w of [375, 900, 1100, 1440]) {
      setWidth(w)
      const { unmount } = renderShell()
      expect(screen.getByText('内容')).toBeVisible()
      unmount()
    }
  })
})
