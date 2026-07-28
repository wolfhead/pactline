import { describe, expect, it, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import { ThemeToggle } from './theme'

/**
 * Guards the ONE thing that would silently break the whole rewrite: the dark
 * variant in index.css must key off the data-theme attribute that theme.tsx
 * actually writes, and "system" must leave the attribute off entirely.
 *
 * jsdom does not evaluate @custom-variant, so this asserts the contract
 * theme.tsx upholds — the CSS half is verified for real in Step 6's browser
 * check, which is why that step is not optional.
 */
describe('theme bridge', () => {
  // vitest.config's test block doesn't set `globals: true`, so
  // @testing-library/react's own auto-cleanup (which hooks a global
  // afterEach) never registers; without this, a component rendered by one
  // test stays mounted in the DOM and pollutes the next test's queries (see
  // the identical workaround in identity.test.tsx).
  afterEach(() => {
    cleanup()
    document.documentElement.removeAttribute('data-theme')
    localStorage.clear()
  })

  it('defaults to light and writes data-theme="light", never a .dark class', () => {
    render(<ThemeToggle />)
    expect(document.documentElement.getAttribute('data-theme')).toBe('light')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('removes the attribute under "system" so prefers-color-scheme takes over', async () => {
    render(<ThemeToggle />)
    const select = screen.getByLabelText('主题') as HTMLSelectElement
    const { fireEvent } = await import('@testing-library/react')
    fireEvent.change(select, { target: { value: 'system' } })
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false)
  })

  it('writes data-theme="dark" for an explicit dark choice', async () => {
    render(<ThemeToggle />)
    const select = screen.getByLabelText('主题') as HTMLSelectElement
    const { fireEvent } = await import('@testing-library/react')
    fireEvent.change(select, { target: { value: 'dark' } })
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
    expect(localStorage.getItem('bountyboard.theme.v2')).toBe('dark')
  })
})
