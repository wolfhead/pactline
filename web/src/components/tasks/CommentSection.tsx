import { useEffect, useState, type FormEvent } from 'react'
import { createComment, deleteComment, listComments, updateComment } from '../../api/tasks'
import { useIdentity } from '../../identity'
import type { Comment } from '../../task-types'
import InlineEditable from './InlineEditable'

interface CommentSectionProps {
  taskNumber: number
}

/** Comments: add, and edit/delete of one's own — enforced by the store, but
 * the controls are hidden for anyone else's comment too so there is no
 * button to click that would just 403. */
export default function CommentSection({ taskNumber }: CommentSectionProps) {
  const { me } = useIdentity()
  const [comments, setComments] = useState<Comment[]>([])
  const [body, setBody] = useState('')
  const [posting, setPosting] = useState(false)
  const [error, setError] = useState('')
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({})

  // Fetches whenever the task changes or identity changes, guarded against
  // out-of-order resolution — mirrors the cancelled-flag idiom in
  // identity.tsx / WorkFeed.tsx / Board.tsx.
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
      const created = await createComment(taskNumber, trimmed)
      setComments((cs) => [...cs, created])
      setBody('')
    } catch (err) {
      setError(String((err as Error).message))
    } finally {
      setPosting(false)
    }
  }

  function editComment(id: string, next: string) {
    if (!next.trim()) return
    const previous = comments
    setComments((cs) => cs.map((c) => (c.id === id ? { ...c, body: next } : c)))
    updateComment(taskNumber, id, next)
      .then((updated) => setComments((cs) => cs.map((c) => (c.id === id ? updated : c))))
      .catch((err) => {
        setComments(previous)
        setRowErrors((r) => ({ ...r, [id]: `保存失败：${(err as Error).message}，已恢复原内容` }))
      })
  }

  function removeComment(id: string) {
    const previous = comments
    setComments((cs) => cs.filter((c) => c.id !== id))
    deleteComment(taskNumber, id).catch((err) => {
      setComments(previous)
      setRowErrors((r) => ({ ...r, [id]: `删除失败：${(err as Error).message}` }))
    })
  }

  return (
    <section className="comments">
      <h3>评论</h3>
      {comments.length === 0 && <p className="hint">还没有评论。</p>}
      <ul className="comment-list">
        {comments.map((c) => {
          const mine = me?.id === c.author_id
          return (
            <li key={c.id} className="comment-item">
              {mine ? (
                <InlineEditable
                  value={c.body}
                  onCommit={(next) => editComment(c.id, next)}
                  multiline
                  ariaLabel="编辑评论"
                  className="comment-body"
                />
              ) : (
                <p className="comment-body">{c.body}</p>
              )}
              <div className="row">
                <span className="hint">{new Date(c.created_at).toLocaleString()}</span>
                {mine && (
                  <button type="button" onClick={() => removeComment(c.id)}>
                    删除
                  </button>
                )}
              </div>
              {rowErrors[c.id] && <span className="error">{rowErrors[c.id]}</span>}
            </li>
          )
        })}
      </ul>

      <form className="row comment-composer" onSubmit={submit}>
        <textarea
          value={body}
          onChange={(e) => setBody(e.target.value)}
          placeholder="添加评论…"
          aria-label="新评论内容"
          rows={2}
        />
        <button type="submit" disabled={!body.trim() || posting}>
          {posting ? '提交中…' : '评论'}
        </button>
      </form>
      {error && <p className="error">{error}</p>}
    </section>
  )
}
