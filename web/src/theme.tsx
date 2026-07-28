import { useEffect, useState } from 'react'

/**
 * Theme preference. "system" follows the OS; the other two override it.
 *
 * The choice is written to the document root as data-theme, which index.css's
 * `dark` custom variant and its color-scheme overrides both key off. Under
 * "system" the attribute is removed entirely rather than resolved to a value
 * here, so prefers-color-scheme keeps handling it — that way the OS switching
 * while the app is open is picked up with no listener of ours.
 */
export type ThemePreference = 'system' | 'light' | 'dark'

/**
 * Versioned key. The previous key holds values that were auto-written on mount
 * rather than chosen, so honouring them would pin existing users to a
 * preference they never expressed. A new key discards them once; anything
 * stored under this one is a real choice.
 */
const STORAGE_KEY = 'bountyboard.theme.v2'

function isPreference(v: string | null): v is ThemePreference {
  return v === 'system' || v === 'light' || v === 'dark'
}

/**
 * Light, not "system", is the default.
 *
 * Following the OS is the conventional choice, but it is a choice about who
 * decides — and here it silently handed the decision to a machine setting that
 * disagreed with the person using the tool. Light is a decision; "system" is a
 * deferral. Anyone who wants dark or OS-following picks it once and it sticks.
 */
const DEFAULT_PREFERENCE: ThemePreference = 'light'

function readStored(): ThemePreference {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return isPreference(raw) ? raw : DEFAULT_PREFERENCE
  } catch {
    // Private mode or a blocked store must not break rendering.
    return DEFAULT_PREFERENCE
  }
}

function apply(preference: ThemePreference): void {
  const root = document.documentElement
  if (preference === 'system') {
    root.removeAttribute('data-theme')
  } else {
    root.setAttribute('data-theme', preference)
  }
}

export function useTheme(): [ThemePreference, (next: ThemePreference) => void] {
  const [preference, setPreference] = useState<ThemePreference>(readStored)

  // Apply on every change, but never write on mount. Writing the default here
  // was a real bug: it persisted the default as though the user had chosen it,
  // so changing the default afterwards had no effect on anyone who had ever
  // loaded the page — they were pinned to a preference they never expressed.
  useEffect(() => {
    apply(preference)
  }, [preference])

  // Only an explicit choice is written. That is what makes it a choice.
  function choose(next: ThemePreference): void {
    setPreference(next)
    try {
      localStorage.setItem(STORAGE_KEY, next)
    } catch {
      // Persisting is a convenience; a blocked store must not break the toggle.
    }
  }

  return [preference, choose]
}

const LABELS: Record<ThemePreference, string> = {
  system: '跟随系统',
  light: '浅色',
  dark: '深色',
}

const ORDER: ThemePreference[] = ['system', 'light', 'dark']

export function ThemeToggle() {
  const [preference, setPreference] = useTheme()

  return (
    <label className="flex min-w-0 items-center gap-2 text-xs whitespace-nowrap text-fg-muted">
      主题
      {/* A real, native <select> rather than the shadcn/Radix one. Two
       * reasons, both concrete: theme-bridge.test.tsx drives this control
       * with fireEvent.change, which only a real <select> answers to; and
       * this control lives in the header, which on a phone is exactly where
       * the OS's own picker sheet beats a re-implemented listbox. The
       * classes below deliberately mirror ui/select.tsx's SelectTrigger so
       * the two read as the same control. */}
      <select
        value={preference}
        aria-label="主题"
        onChange={(e) => setPreference(e.target.value as ThemePreference)}
        className="min-h-11 min-w-0 flex-1 rounded-md border border-border-strong bg-surface px-2 py-1.5 text-sm text-fg shadow-xs transition-[color,box-shadow] outline-none focus-visible:border-accent focus-visible:ring-[3px] focus-visible:ring-accent/50 pointer-coarse:min-h-11 sm:min-h-8 sm:flex-none"
      >
        {ORDER.map((p) => (
          <option key={p} value={p}>
            {LABELS[p]}
          </option>
        ))}
      </select>
    </label>
  )
}
