import { describe, expect, it, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useBreakpoint } from './useBreakpoint'

function setWidth(px: number) {
  Object.defineProperty(window, 'innerWidth', { writable: true, configurable: true, value: px })
  window.dispatchEvent(new Event('resize'))
}

describe('useBreakpoint', () => {
  beforeEach(() => setWidth(1440))

  it.each([
    [375, 'phone'],
    [767, 'phone'],
    [768, 'md'],
    [1023, 'md'],
    [1024, 'lg'],
    [1279, 'lg'],
    [1280, 'xl'],
    [1920, 'xl'],
  ])('reports %s px as %s', (px, want) => {
    setWidth(px as number)
    const { result } = renderHook(() => useBreakpoint())
    expect(result.current).toBe(want)
  })

  it('follows a resize without a remount', () => {
    setWidth(1440)
    const { result } = renderHook(() => useBreakpoint())
    expect(result.current).toBe('xl')
    act(() => setWidth(800))
    // Boundary values above already pin the thresholds; this pins that the
    // hook actually listens, rather than reading width once on mount.
    expect(result.current).toBe('md')
  })
})
