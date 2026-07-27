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

/** The `?` overlay: discoverable, never trapping — a click on the backdrop
 * or Escape (handled by the caller's keydown listener) both close it. */
export default function ShortcutsOverlay({ onClose }: ShortcutsOverlayProps) {
  return (
    <div className="overlay-backdrop" onClick={onClose}>
      <div
        className="overlay-panel"
        role="dialog"
        aria-modal="true"
        aria-label="键盘快捷键"
        onClick={(e) => e.stopPropagation()}
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
