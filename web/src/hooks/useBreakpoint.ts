import { useEffect, useState } from 'react'

/** The four layout tiers AppShell arranges around, keyed off viewport width
 * alone (`window.innerWidth`, not `matchMedia`, so tests can drive it by
 * setting the property directly). Boundaries: phone < 768 ≤ md < 1024 ≤ lg
 * < 1280 ≤ xl — each threshold belongs to the tier above it. */
export type Tier = 'phone' | 'md' | 'lg' | 'xl'

function tierFor(width: number): Tier {
  if (width < 768) return 'phone'
  if (width < 1024) return 'md'
  if (width < 1280) return 'lg'
  return 'xl'
}

/** Tracks the current breakpoint tier and re-renders on resize. One shared
 * source of truth so AppShell's four arrangements — and anything else that
 * needs to branch on tier — never fall out of sync with each other. */
export function useBreakpoint(): Tier {
  const [tier, setTier] = useState<Tier>(() => tierFor(window.innerWidth))

  useEffect(() => {
    function onResize() {
      setTier(tierFor(window.innerWidth))
    }
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])

  return tier
}
