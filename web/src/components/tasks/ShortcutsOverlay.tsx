import { useEffect, useRef, type KeyboardEvent as ReactKeyboardEvent } from 'react'

const SHORTCUTS: [string, string][] = [
  ['C', '新建任务'],
  ['/', '聚焦搜索'],
  ['J / ↓', '下一条任务'],
  ['K / ↑', '上一条任务'],
  ['Enter', '打开选中任务'],
  ['Esc', '关闭弹层 / 取消编辑'],
  ['?', '显示这份快捷键说明'],
]

interface ShortcutsOverlayProps {
  onClose: () => void
}

// Anything a sighted mouse user or a screen reader's virtual cursor could
// tab to inside the panel. Kept in one place so the focus-in and the Tab
// trap agree on exactly the same set of elements.
const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), summary, [tabindex]:not([tabindex="-1"])'

/** The `?` overlay. `role="dialog" aria-modal="true"` is a promise to
 * assistive tech that the rest of the page is inert while this is open —
 * so, unlike a plain non-modal popover, this component has to actually
 * deliver that: move focus in on open, keep Tab/Shift+Tab cycling inside
 * while it's open, and hand focus back to whatever opened it on close.
 * Escape is still handled by App.tsx's global keydown listener — that
 * behaviour is unchanged, this component only owns the Tab trap and the
 * focus-in/focus-out. A click on the backdrop still closes it too. */
export default function ShortcutsOverlay({ onClose }: ShortcutsOverlayProps) {
  const panelRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    // Whatever had focus when the dialog opened (the "? 快捷键" button, or
    // whatever the "?" keyboard shortcut was pressed from) is where focus
    // must return once the dialog closes — captured here, at mount, before
    // this effect moves focus anywhere.
    const previouslyFocused = document.activeElement as HTMLElement | null

    const panel = panelRef.current
    const focusable = panel?.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)
    const target = focusable && focusable.length > 0 ? focusable[0] : panel
    target?.focus()

    return () => {
      previouslyFocused?.focus()
    }
  }, [])

  function handleKeyDown(e: ReactKeyboardEvent<HTMLDivElement>) {
    if (e.key !== 'Tab') return
    const panel = panelRef.current
    if (!panel) return
    const focusable = Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR))
    if (focusable.length === 0) return
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault()
      last.focus()
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault()
      first.focus()
    }
  }

  return (
    <div className="overlay-backdrop" onClick={onClose}>
      <div
        ref={panelRef}
        className="overlay-panel"
        role="dialog"
        aria-modal="true"
        aria-label="键盘快捷键"
        tabIndex={-1}
        onClick={(e) => e.stopPropagation()}
        onKeyDown={handleKeyDown}
      >
        <h3>键盘快捷键</h3>
        <dl className="shortcut-list">
          {SHORTCUTS.map(([key, desc]) => (
            <div key={key} className="shortcut-item">
              <dt><kbd>{key}</kbd></dt>
              <dd>{desc}</dd>
            </div>
          ))}
        </dl>
        <button onClick={onClose}>关闭</button>
      </div>
    </div>
  )
}
