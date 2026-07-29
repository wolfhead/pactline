import { useEffect, useState, type FormEvent } from 'react'
import { createComment, deleteComment, listComments, updateComment } from '../../api/tasks'
import { useIdentity } from '../../identity'
import type { Comment } from '../../task-types'
import InlineEditable from './InlineEditable'

interface CommentSectionProps {
  taskNumber: number
  taskVersion: number
  onTaskChanged: () => Promise<void>
}

/** Comments: add, and edit/delete of one's own — enforced by the store, but
 * the controls are hidden for anyone else's comment too so there is no
 * button to click that would just 403. */
export default function CommentSection({
  taskNumber,
  taskVersion,
  onTaskChanged,
}: CommentSectionProps) {
  const { me } = useIdentity()
  const [comments, setComments] = useState<Comment[]>([])
  const [body, setBody] = useState('')
  const [posting, setPosting] = useState(false)
  const [error, setError] = useState('')
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({})

  // Fetches whenever the task changes or identity changes, guarded against
  // out-of-order resolution — mirrors the cancelled-flag idiom in
  // identity.tsx / WorkFeed.tsx.
  useEffect(() => {
    let cancelled = false
    listComments(taskNumber)
      .then((loaded) => {
        if (cancelled) return
        setComments(loaded)
      })
      .catch((err) => {
        if (cancelled) return
        setError(String((err as Error).message))
      })
    return () => {
      cancelled = true
    }
  }, [taskNumber, me?.id])

  async function submit(e: FormEvent) {
    e.preventDefault()
    const trimmed = body.trim()
    if (!trimmed || posting) return
    setPosting(true)
    setError('')
    try {
      const created = await createComment(taskNumber, taskVersion, trimmed)
      setComments((cs) => [...cs, created])
      setBody('')
      await onTaskChanged()
    } catch (err) {
      setError(String((err as Error).message))
    } finally {
      setPosting(false)
    }
  }

  function editComment(id: string, next: string) {
    if (!next.trim()) return
    const current = comments.find((comment) => comment.id === id)
    if (!current) return
    const previous = comments
    setComments((cs) => cs.map((c) => (c.id === id ? { ...c, body: next } : c)))
    updateComment(taskNumber, id, current.version, next)
      .then((updated) => setComments((cs) => cs.map((c) => (c.id === id ? updated : c))))
      .catch((err) => {
        setComments(previous)
        setRowErrors((r) => ({ ...r, [id]: `保存失败：${(err as Error).message}，已恢复原内容` }))
      })
  }

  function removeComment(id: string) {
    const current = comments.find((comment) => comment.id === id)
    if (!current) return
    const previous = comments
    setComments((cs) => cs.filter((c) => c.id !== id))
    deleteComment(taskNumber, id, current.version).catch((err) => {
      setComments(previous)
      setRowErrors((r) => ({ ...r, [id]: `删除失败：${(err as Error).message}` }))
    })
  }

  return (
    // role/aria-label rather than relying on the <h3> below: a landmark
    // with a stable name is what lets a caller (and the e2e suite) scope to
    // "the comments block" without walking the DOM upwards from a heading,
    // which breaks the moment this section grows a wrapper.
    <section role="region" aria-label="评论" className="flex flex-col gap-3">
      <h3 className="text-sm font-medium text-fg">评论</h3>
      {comments.length === 0 && <p className="text-sm text-fg-muted">还没有评论。</p>}
      <ul className="flex flex-col gap-3">
        {comments.map((c) => {
          const mine = me?.id === c.author_id
          return (
            <li key={c.id} className="rounded-md border border-border p-3">
              {mine ? (
                <InlineEditable
                  value={c.body}
                  onCommit={(next) => editComment(c.id, next)}
                  multiline
                  ariaLabel="编辑评论"
                  className="mb-2 w-full text-sm text-fg"
                />
              ) : (
                <p className="mb-2 text-sm text-fg">{c.body}</p>
              )}
              <div className="flex items-center gap-2">
                <span className="text-xs text-fg-muted">{new Date(c.created_at).toLocaleString()}</span>
                {mine && (
                  <button
                    type="button"
                    className="text-xs text-fg-muted hover:text-danger"
                    onClick={() => removeComment(c.id)}
                  >
                    删除
                  </button>
                )}
              </div>
              {rowErrors[c.id] && <span className="text-xs text-danger">{rowErrors[c.id]}</span>}
            </li>
          )
        })}
      </ul>

      <form className="flex flex-col gap-2 rounded-md border border-border bg-surface-subtle p-3" onSubmit={submit}>
        <textarea
          value={body}
          onChange={(e) => setBody(e.target.value)}
          placeholder="添加评论…"
          aria-label="新评论内容"
          rows={2}
          className="rounded-md border border-border-strong bg-surface px-2 py-1.5 text-sm text-fg placeholder:text-fg-muted"
        />
        <button
          type="submit"
          disabled={!body.trim() || posting}
          className="self-end rounded-md bg-accent px-3 py-1.5 text-xs font-medium text-accent-fg disabled:cursor-not-allowed disabled:opacity-50"
        >
          {posting ? '提交中…' : '评论'}
        </button>
      </form>
      {error && <p className="text-sm text-danger">{error}</p>}
    </section>
  )
}
