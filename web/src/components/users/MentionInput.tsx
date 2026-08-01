import {
  forwardRef,
  useEffect,
  useId,
  useImperativeHandle,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ClipboardEvent,
  type FormEvent,
  type KeyboardEvent,
  type MouseEvent,
} from 'react'
import { AtSign, Trash2 } from 'lucide-react'
import {
  Popover,
  PopoverAnchor,
  PopoverContent,
} from '@/components/ui/popover'
import { cn } from '@/lib/utils'
import UserPicker, {
  filterUserPickerOptions,
  type UserPickerOption,
} from './UserPicker'

export interface MentionInputValue {
  body: string
  mentionedUserIDs: string[]
}

export interface MentionInputHandle {
  focus: () => void
}

interface MentionInputProps {
  value: MentionInputValue
  options: UserPickerOption[]
  onChange: (value: MentionInputValue) => void
  ariaLabel: string
  placeholder?: string
  disabled?: boolean
  className?: string
}

const MENTION_SELECTOR = '[data-mention-id]'
const MENTION_CLASS = [
  'mx-0.5 inline-flex rounded-sm bg-accent/10 px-1 py-0.5',
  'font-medium text-accent ring-1 ring-inset ring-accent/20',
  'cursor-pointer select-none',
].join(' ')

const MentionInput = forwardRef<MentionInputHandle, MentionInputProps>(function MentionInput({
  value,
  options,
  onChange,
  ariaLabel,
  placeholder = '输入内容，使用 @ 提及项目成员…',
  disabled = false,
  className,
}, forwardedRef) {
  const editorRef = useRef<HTMLDivElement>(null)
  const triggerRangeRef = useRef<Range | null>(null)
  const replacingMentionRef = useRef<HTMLElement | null>(null)
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [activeOptionID, setActiveOptionID] = useState('')
  const pickerID = `mention-picker-${useId().replaceAll(':', '')}`
  const visibleOptions = useMemo(
    () => filterUserPickerOptions(options, query),
    [options, query],
  )
  const replacementID = replacingMentionRef.current?.dataset.mentionId ?? ''
  const unavailableIDs = value.mentionedUserIDs.filter((id) => id !== replacementID)
  const availableOptions = visibleOptions.filter((option) => !unavailableIDs.includes(option.id))

  useImperativeHandle(forwardedRef, () => ({
    focus: () => editorRef.current?.focus(),
  }), [])

  useLayoutEffect(() => {
    const editor = editorRef.current
    if (!editor) return
    const current = serializeEditor(editor)
    if (sameValue(current, value)) return
    hydrateEditor(editor, value, options)
  }, [options, value])

  useEffect(() => {
    if (!open) return
    if (availableOptions.some(({ id }) => id === activeOptionID)) return
    setActiveOptionID(availableOptions[0]?.id ?? '')
  }, [activeOptionID, availableOptions, open])

  function emitChange() {
    const editor = editorRef.current
    if (!editor) return
    const next = serializeEditor(editor)
    if (!sameValue(next, value)) onChange(next)
  }

  function closePicker() {
    setOpen(false)
    setQuery('')
    setActiveOptionID('')
    triggerRangeRef.current = null
    replacingMentionRef.current = null
  }

  function inspectMentionQuery() {
    const editor = editorRef.current
    const selection = window.getSelection()
    if (!editor || !selection?.isCollapsed || selection.rangeCount === 0) {
      closePicker()
      return
    }
    const node = selection.anchorNode
    const offset = selection.anchorOffset
    if (!node || node.nodeType !== Node.TEXT_NODE || !editor.contains(node)) {
      closePicker()
      return
    }
    const textBeforeCaret = node.textContent?.slice(0, offset) ?? ''
    const match = /(^|[\s([{，。！？、：；])@([^\s@]*)$/u.exec(textBeforeCaret)
    if (!match) {
      closePicker()
      return
    }
    const atOffset = match.index + match[1].length
    const range = document.createRange()
    range.setStart(node, atOffset)
    range.setEnd(node, offset)
    triggerRangeRef.current = range
    replacingMentionRef.current = null
    setQuery(match[2])
    setOpen(true)
  }

  function selectOption(option: UserPickerOption) {
    const editor = editorRef.current
    if (!editor) return
    const replacing = replacingMentionRef.current
    if (replacing?.isConnected) {
      configureMentionNode(replacing, option)
      placeCaretAfter(replacing)
    } else {
      const range = usableRange(triggerRangeRef.current, editor) ?? rangeAtEnd(editor)
      range.deleteContents()
      const mention = createMentionNode(option)
      const trailingSpace = document.createTextNode(' ')
      const fragment = document.createDocumentFragment()
      fragment.append(mention, trailingSpace)
      range.insertNode(fragment)
      placeCaretAfter(trailingSpace)
    }
    editor.focus()
    closePicker()
    emitChange()
  }

  function removeReplacingMention(event: MouseEvent<HTMLButtonElement>) {
    event.preventDefault()
    const editor = editorRef.current
    const mention = replacingMentionRef.current
    if (!editor || !mention?.isConnected) return
    const nextNode = mention.nextSibling
    mention.remove()
    editor.normalize()
    if (nextNode?.isConnected) placeCaretBefore(nextNode)
    else placeCaretAtEnd(editor)
    editor.focus()
    closePicker()
    emitChange()
  }

  function handleInput(_event: FormEvent<HTMLDivElement>) {
    emitChange()
    inspectMentionQuery()
  }

  function handleKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.nativeEvent.isComposing) return
    if (open && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
      event.preventDefault()
      if (availableOptions.length === 0) return
      const currentIndex = availableOptions.findIndex(({ id }) => id === activeOptionID)
      const step = event.key === 'ArrowDown' ? 1 : -1
      const nextIndex = currentIndex < 0
        ? (step === 1 ? 0 : availableOptions.length - 1)
        : (currentIndex + step + availableOptions.length) % availableOptions.length
      setActiveOptionID(availableOptions[nextIndex].id)
      return
    }
    if (open && (event.key === 'Enter' || event.key === 'Tab')) {
      const active = availableOptions.find(({ id }) => id === activeOptionID)
      if (active) {
        event.preventDefault()
        selectOption(active)
        return
      }
    }
    if (open && event.key === 'Escape') {
      event.preventDefault()
      closePicker()
      return
    }
    if (event.key === 'Backspace' || event.key === 'Delete') {
      const mention = adjacentMention(editorRef.current, event.key === 'Backspace' ? -1 : 1)
      if (mention) {
        event.preventDefault()
        mention.remove()
        editorRef.current?.normalize()
        closePicker()
        emitChange()
        return
      }
    }
    if (event.key === 'Enter') {
      event.preventDefault()
      insertPlainText('\n')
    }
  }

  function handlePaste(event: ClipboardEvent<HTMLDivElement>) {
    event.preventDefault()
    insertPlainText(event.clipboardData.getData('text/plain'))
  }

  function insertPlainText(text: string) {
    const editor = editorRef.current
    if (!editor) return
    const selection = window.getSelection()
    const range = selection && selection.rangeCount > 0 && editor.contains(selection.anchorNode)
      ? selection.getRangeAt(0)
      : rangeAtEnd(editor)
    range.deleteContents()
    const node = document.createTextNode(text)
    range.insertNode(node)
    placeCaretAfter(node)
    editor.focus()
    emitChange()
    inspectMentionQuery()
  }

  function startMention(event: MouseEvent<HTMLButtonElement>) {
    event.preventDefault()
    if (disabled) return
    const editor = editorRef.current
    if (!editor) return
    editor.focus()
    const selection = window.getSelection()
    if (!selection || !selection.anchorNode || !editor.contains(selection.anchorNode)) {
      placeCaretAtEnd(editor)
    }
    insertPlainText('@')
  }

  function handleEditorMouseDown(event: MouseEvent<HTMLDivElement>) {
    if (disabled) return
    const target = event.target as HTMLElement
    const mention = target.closest<HTMLElement>(MENTION_SELECTOR)
    if (!mention || !editorRef.current?.contains(mention)) return
    event.preventDefault()
    replacingMentionRef.current = mention
    triggerRangeRef.current = null
    setQuery('')
    setOpen(true)
    editorRef.current.focus()
  }

  return (
    <Popover open={open} onOpenChange={(next) => !next && closePicker()}>
      <PopoverAnchor asChild>
        <div className={cn('relative', className)}>
          <div
            ref={editorRef}
            contentEditable={!disabled}
            suppressContentEditableWarning
            role="combobox"
            aria-label={ariaLabel}
            aria-multiline="true"
            aria-autocomplete="list"
            aria-expanded={open}
            aria-controls={open ? pickerID : undefined}
            aria-activedescendant={open && activeOptionID ? `${pickerID}-option-${activeOptionID}` : undefined}
            data-placeholder={placeholder}
            onInput={handleInput}
            onKeyDown={handleKeyDown}
            onPaste={handlePaste}
            onMouseDown={handleEditorMouseDown}
            className={cn(
              'min-h-20 w-full whitespace-pre-wrap rounded-md border border-border-strong bg-surface',
              'px-3 py-2 pr-10 text-sm text-fg outline-hidden',
              'empty:before:pointer-events-none empty:before:text-fg-muted empty:before:content-[attr(data-placeholder)]',
              'focus:border-accent focus:ring-2 focus:ring-accent/20',
              disabled && 'cursor-not-allowed bg-surface-subtle opacity-60',
            )}
          />
          <button
            type="button"
            aria-label="提及项目成员"
            title="提及项目成员"
            disabled={disabled || options.length === 0}
            onMouseDown={startMention}
            className={cn(
              'absolute bottom-2 right-2 flex size-7 items-center justify-center rounded-md text-fg-muted',
              'hover:bg-accent/10 hover:text-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/30',
              'disabled:cursor-not-allowed disabled:opacity-40',
            )}
          >
            <AtSign aria-hidden="true" className="size-4" />
          </button>
        </div>
      </PopoverAnchor>
      <PopoverContent
        align="start"
        sideOffset={6}
        onOpenAutoFocus={(event) => event.preventDefault()}
        onCloseAutoFocus={(event) => event.preventDefault()}
        className="w-80 max-w-[calc(100vw-2rem)] p-1.5 shadow-lg"
      >
        <div className="border-b border-border px-2.5 py-2">
          <p className="text-xs font-medium text-fg">选择项目成员</p>
          <p className="mt-0.5 truncate text-xs text-fg-muted">
            {query ? `搜索“${query}”` : '输入姓名或邮箱继续筛选'}
          </p>
        </div>
        {replacingMentionRef.current && (
          <button
            type="button"
            aria-label={`移除 @${replacingMentionRef.current.dataset.mentionName}`}
            onMouseDown={removeReplacingMention}
            className="mt-1 flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm text-danger hover:bg-danger/10"
          >
            <Trash2 aria-hidden="true" className="size-4" />
            <span>移除提及</span>
          </button>
        )}
        <UserPicker
          id={pickerID}
          options={options}
          query={query}
          activeOptionID={activeOptionID}
          selectedOptionIDs={unavailableIDs}
          onActiveOptionChange={setActiveOptionID}
          onSelect={selectOption}
          ariaLabel="选择项目成员"
          emptyLabel="没有匹配的项目成员"
          selectedLabel="已提及"
        />
      </PopoverContent>
    </Popover>
  )
})

export default MentionInput

function createMentionNode(option: UserPickerOption): HTMLSpanElement {
  const mention = document.createElement('span')
  mention.className = MENTION_CLASS
  mention.setAttribute('contenteditable', 'false')
  mention.setAttribute('role', 'button')
  mention.setAttribute('aria-label', `@${option.name}，点击更改`)
  configureMentionNode(mention, option)
  return mention
}

function configureMentionNode(mention: HTMLElement, option: UserPickerOption) {
  mention.dataset.mentionId = option.id
  mention.dataset.mentionName = option.name
  mention.textContent = `@${option.name}`
}

function serializeEditor(editor: HTMLElement): MentionInputValue {
  const mentionedUserIDs: string[] = []
  let body = ''

  function visit(node: Node) {
    if (node.nodeType === Node.TEXT_NODE) {
      body += (node.textContent ?? '').replaceAll('\u00a0', ' ')
      return
    }
    if (!(node instanceof HTMLElement)) return
    const mentionID = node.dataset.mentionId
    if (mentionID) {
      body += `@${node.dataset.mentionName ?? ''}`
      if (!mentionedUserIDs.includes(mentionID)) mentionedUserIDs.push(mentionID)
      return
    }
    if (node.tagName === 'BR') {
      body += '\n'
      return
    }
    const startsBlock = node !== editor && (node.tagName === 'DIV' || node.tagName === 'P')
    if (startsBlock && body && !body.endsWith('\n')) body += '\n'
    node.childNodes.forEach(visit)
  }

  editor.childNodes.forEach(visit)
  return { body, mentionedUserIDs }
}

function hydrateEditor(
  editor: HTMLElement,
  value: MentionInputValue,
  options: UserPickerOption[],
) {
  const matches = value.mentionedUserIDs.flatMap((id) => {
    const option = options.find((candidate) => candidate.id === id)
    if (!option) return []
    const token = `@${option.name}`
    const start = value.body.indexOf(token)
    return start >= 0 ? [{ start, end: start + token.length, option }] : []
  }).sort((left, right) => left.start - right.start)

  editor.replaceChildren()
  let cursor = 0
  for (const match of matches) {
    if (match.start < cursor) continue
    if (match.start > cursor) editor.append(document.createTextNode(value.body.slice(cursor, match.start)))
    editor.append(createMentionNode(match.option))
    cursor = match.end
  }
  if (cursor < value.body.length) editor.append(document.createTextNode(value.body.slice(cursor)))
}

function sameValue(left: MentionInputValue, right: MentionInputValue): boolean {
  return left.body === right.body
    && left.mentionedUserIDs.length === right.mentionedUserIDs.length
    && left.mentionedUserIDs.every((id, index) => id === right.mentionedUserIDs[index])
}

function usableRange(range: Range | null, editor: HTMLElement): Range | null {
  if (!range || !range.startContainer.isConnected || !editor.contains(range.commonAncestorContainer)) return null
  return range
}

function rangeAtEnd(editor: HTMLElement): Range {
  const range = document.createRange()
  range.selectNodeContents(editor)
  range.collapse(false)
  return range
}

function placeCaretAtEnd(editor: HTMLElement) {
  applySelection(rangeAtEnd(editor))
}

function placeCaretAfter(node: Node) {
  const range = document.createRange()
  if (node.nodeType === Node.TEXT_NODE) {
    range.setStart(node, node.textContent?.length ?? 0)
  } else {
    range.setStartAfter(node)
  }
  range.collapse(true)
  applySelection(range)
}

function placeCaretBefore(node: Node) {
  const range = document.createRange()
  range.setStartBefore(node)
  range.collapse(true)
  applySelection(range)
}

function applySelection(range: Range) {
  const selection = window.getSelection()
  selection?.removeAllRanges()
  selection?.addRange(range)
}

function adjacentMention(editor: HTMLElement | null, direction: -1 | 1): HTMLElement | null {
  const selection = window.getSelection()
  if (!editor || !selection?.isCollapsed || !selection.anchorNode || !editor.contains(selection.anchorNode)) return null
  let candidate: Node | null = null
  const node = selection.anchorNode
  const offset = selection.anchorOffset
  if (node.nodeType === Node.TEXT_NODE) {
    const length = node.textContent?.length ?? 0
    if (direction === -1 && offset === 0) candidate = node.previousSibling
    if (direction === 1 && offset === length) candidate = node.nextSibling
  } else if (node === editor) {
    candidate = editor.childNodes[offset + (direction === -1 ? -1 : 0)] ?? null
  }
  while (candidate?.nodeType === Node.TEXT_NODE && !(candidate.textContent ?? '').length) {
    candidate = direction === -1 ? candidate.previousSibling : candidate.nextSibling
  }
  return candidate instanceof HTMLElement && candidate.matches(MENTION_SELECTOR) ? candidate : null
}
