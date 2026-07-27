import { useEffect, useState } from 'react'

/**
 * Theme preference. "system" follows the OS; the other two override it.
 *
 * The choice is written to the document root as data-theme, which styles.css
 * uses to beat its own prefers-color-scheme rule. Under "system" the attribute
 * is removed entirely rather than resolved to a value here, so the media query
 * keeps handling it — that way the OS switching while the app is open is picked
 * up with no listener of ours.
 */
export type ThemePreference = 'system' | 'light' | 'dark'

const STORAGE_KEY = 'bountyboard.theme'

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

  useEffect(() => {
    apply(preference)
    try {
      localStorage.setItem(STORAGE_KEY, preference)
    } catch {
      // Persisting is a convenience; a blocked store must not break the toggle.
    }
  }, [preference])

  return [preference, setPreference]
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
    <label className="switcher">
      主题
      <select
        value={preference}
        aria-label="主题"
        onChange={(e) => setPreference(e.target.value as ThemePreference)}
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
