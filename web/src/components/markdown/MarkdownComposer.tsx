import { useEffect, useId, useRef, useState } from 'react'
import MarkdownContent from './MarkdownContent'

interface MarkdownComposerProps {
  value: string
  onChange: (value: string) => void
  ariaLabel: string
  placeholder?: string
  rows?: number
  autoFocus?: boolean
  disabled?: boolean
}

export default function MarkdownComposer({
  value,
  onChange,
  ariaLabel,
  placeholder,
  rows = 3,
  autoFocus = false,
  disabled = false,
}: MarkdownComposerProps) {
  const [mode, setMode] = useState<'write' | 'preview'>('write')
  const previousValue = useRef(value)
  const id = useId()
  const writePanelID = `${id}-write`
  const previewPanelID = `${id}-preview`

  useEffect(() => {
    if (previousValue.current.trim() && !value.trim()) {
      setMode('write')
    }
    previousValue.current = value
  }, [value])

  return (
    <div className="overflow-hidden rounded-md border border-border-strong bg-surface focus-within:border-accent">
      <div role="tablist" aria-label="Markdown 编辑模式" className="flex border-b border-border bg-surface-subtle px-1 pt-1">
        <ModeTab
          selected={mode === 'write'}
          panelID={writePanelID}
          onSelect={() => setMode('write')}
        >
          编写
        </ModeTab>
        <ModeTab
          selected={mode === 'preview'}
          panelID={previewPanelID}
          onSelect={() => setMode('preview')}
        >
          预览
        </ModeTab>
      </div>

      {mode === 'write' ? (
        <div id={writePanelID} role="tabpanel" aria-label="Markdown 编写区">
          <textarea
            value={value}
            onChange={(event) => onChange(event.target.value)}
            aria-label={ariaLabel}
            placeholder={placeholder}
            rows={rows}
            autoFocus={autoFocus}
            disabled={disabled}
            className="block w-full resize-y bg-surface px-3 py-2 text-sm leading-6 text-fg outline-none placeholder:text-fg-subtle disabled:opacity-60"
          />
        </div>
      ) : (
        <div id={previewPanelID} role="tabpanel" aria-label="Markdown 预览区" className="min-h-20 px-3 py-2">
          {value.trim()
            ? <MarkdownContent source={value} />
            : <p className="text-sm text-fg-muted">输入 Markdown 后可在这里预览。</p>}
        </div>
      )}
    </div>
  )
}

function ModeTab({
  selected,
  panelID,
  onSelect,
  children,
}: {
  selected: boolean
  panelID: string
  onSelect: () => void
  children: string
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={selected}
      aria-controls={panelID}
      onClick={onSelect}
      className={selected
        ? '-mb-px rounded-t-md border border-border border-b-surface bg-surface px-3 py-1.5 text-xs font-medium text-fg'
        : 'px-3 py-1.5 text-xs font-medium text-fg-muted hover:text-fg'}
    >
      {children}
    </button>
  )
}
