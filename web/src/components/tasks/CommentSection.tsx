import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import {
  answerTaskClaimQuestion,
  createComment,
  deleteComment,
  listComments,
  listTaskAgentConversations,
  releaseTaskClaim,
  updateComment,
} from '../../api/tasks'
import { listProjectMembers, type ProjectMembership } from '@/api/projects'
import { useIdentity } from '../../identity'
import type {
  Comment,
  TaskClaimConversation,
  TaskClaimMessage,
  TaskStatus,
} from '../../task-types'
import type { AcceptanceCriterion, AcceptanceOutcome } from '@/api/acceptance'
import MentionInput, {
  type MentionInputHandle,
  type MentionInputValue,
} from '@/components/users/MentionInput'
import InlineEditable from './InlineEditable'
import SubmissionReview from './SubmissionReview'

interface CommentSectionProps {
  taskNumber: number
  projectNumber: number
  taskVersion: number
  taskStatus: TaskStatus
  acceptanceCriteria: AcceptanceCriterion[]
  onReviewCheck: (
    criterion: AcceptanceCriterion,
    outcome: AcceptanceOutcome,
    evidence: string,
  ) => Promise<void>
  onCompleteReview: () => Promise<void>
  onReturnForChanges: () => Promise<void>
  onTaskChanged: () => Promise<void>
}

/** Comments: add, and edit/delete of one's own — enforced by the store, but
 * the controls are hidden for anyone else's comment too so there is no
 * button to click that would just 403. */
export default function CommentSection({
  taskNumber,
  projectNumber,
  taskVersion,
  taskStatus,
  acceptanceCriteria,
  onReviewCheck,
  onCompleteReview,
  onReturnForChanges,
  onTaskChanged,
}: CommentSectionProps) {
  const { me } = useIdentity()
  const [comments, setComments] = useState<Comment[]>([])
  const [members, setMembers] = useState<ProjectMembership[]>([])
  const [conversations, setConversations] = useState<TaskClaimConversation[]>([])
  const [commentDraft, setCommentDraft] = useState<MentionInputValue>({
    body: '',
    mentionedUserIDs: [],
  })
  const [replyToCommentID, setReplyToCommentID] = useState('')
  const composerRef = useRef<MentionInputHandle>(null)
  const [answers, setAnswers] = useState<Record<string, string>>({})
  const [answeringClaimID, setAnsweringClaimID] = useState('')
  const [releasingClaimID, setReleasingClaimID] = useState('')
  const [posting, setPosting] = useState(false)
  const [reviewingSubmissionID, setReviewingSubmissionID] = useState('')
  const [error, setError] = useState('')
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({})

  // Fetches whenever the task changes or identity changes, guarded against
  // out-of-order resolution — mirrors the cancelled-flag idiom in
  // identity.tsx / WorkFeed.tsx.
  useEffect(() => {
    let cancelled = false
    Promise.all([
      listComments(taskNumber),
      listTaskAgentConversations(taskNumber),
      listProjectMembers(projectNumber),
    ])
      .then(([loadedComments, loadedConversations, loadedMembers]) => {
        if (cancelled) return
        setComments(loadedComments)
        setConversations(loadedConversations ?? [])
        setMembers(loadedMembers.filter(({ active }) => active))
      })
      .catch((err) => {
        if (cancelled) return
        setError(String((err as Error).message))
      })
    return () => {
      cancelled = true
    }
  }, [taskNumber, projectNumber, me?.id])

  const memberByID = useMemo(() => new Map(members.map((member) => [member.user.id, member])), [members])
  const mentionOptions = useMemo(() => members
    .filter(({ user }) => user.id !== me?.id)
    .map(({ user, role }) => ({
      ...user,
      description: role === 'admin' ? '项目管理员' : '项目成员',
    })), [me?.id, members])

  const timeline = useMemo(() => {
    const entries: Array<
      | { type: 'comment'; createdAt: string; comment: Comment }
      | {
        type: 'agent'
        createdAt: string
        message: TaskClaimMessage
        conversation: TaskClaimConversation
      }
    > = comments.map((comment) => ({
      type: 'comment' as const, createdAt: comment.created_at, comment,
    }))
    for (const conversation of conversations) {
      for (const message of conversation.messages) {
        entries.push({
          type: 'agent', createdAt: message.created_at, message, conversation,
        })
      }
    }
    return entries.sort((left, right) => (
      new Date(left.createdAt).getTime() - new Date(right.createdAt).getTime()
    ))
  }, [comments, conversations])

  const unfinishedConversation = useMemo(() => (
    [...conversations].reverse().find(({ claim }) => (
      claim.status === 'active' || claim.status === 'waiting_human'
    ))
  ), [conversations])

  const latestSubmissionID = useMemo(() => {
    if (taskStatus !== 'in_review') return ''
    const submissions = conversations.flatMap((conversation) => (
      conversation.claim.status === 'submitted'
        ? conversation.messages
          .filter((message) => message.kind === 'submission')
          .map((message) => ({ message, conversation }))
        : []
    ))
    submissions.sort((left, right) => (
      new Date(right.message.created_at).getTime()
      - new Date(left.message.created_at).getTime()
    ))
    return submissions[0]?.message.id ?? ''
  }, [conversations, taskStatus])

  async function reloadConversations() {
    const loaded = await listTaskAgentConversations(taskNumber)
    setConversations(loaded ?? [])
  }

  async function submit(e: FormEvent) {
    e.preventDefault()
    const trimmed = commentDraft.body.trim()
    if (!trimmed || posting) return
    setPosting(true)
    setError('')
    try {
      const created = await createComment(
        taskNumber,
        taskVersion,
        trimmed,
        replyToCommentID || undefined,
        commentDraft.mentionedUserIDs,
      )
      setComments((cs) => [...cs, created])
      setCommentDraft({ body: '', mentionedUserIDs: [] })
      setReplyToCommentID('')
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
    updateComment(taskNumber, id, current.version, next, current.mentioned_user_ids ?? [])
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
    setComments((cs) => cs.map((comment) => (
      comment.id === id
        ? { ...comment, body: '', deleted: true, mentioned_user_ids: [] }
        : comment
    )))
    deleteComment(taskNumber, id, current.version).catch((err) => {
      setComments(previous)
      setRowErrors((r) => ({ ...r, [id]: `删除失败：${(err as Error).message}` }))
    })
  }

  function startReply(comment: Comment) {
    setReplyToCommentID(comment.id)
    window.requestAnimationFrame(() => composerRef.current?.focus())
  }

  async function answerQuestion(conversation: TaskClaimConversation) {
    const answer = (answers[conversation.claim.id] ?? '').trim()
    if (!answer || answeringClaimID) return
    setAnsweringClaimID(conversation.claim.id)
    setError('')
    try {
      await answerTaskClaimQuestion(conversation.claim.id, conversation.claim.version, answer)
      await reloadConversations()
      setAnswers((current) => ({ ...current, [conversation.claim.id]: '' }))
    } catch (err) {
      setError(`回复失败：${(err as Error).message}`)
    } finally {
      setAnsweringClaimID('')
    }
  }

  async function releaseClaim(conversation: TaskClaimConversation) {
    if (releasingClaimID) return
    setReleasingClaimID(conversation.claim.id)
    setError('')
    try {
      await releaseTaskClaim(conversation.claim.id, conversation.claim.version)
      await Promise.all([reloadConversations(), onTaskChanged()])
    } catch (err) {
      setError(`释放失败：${(err as Error).message}`)
    } finally {
      setReleasingClaimID('')
    }
  }

  return (
    // role/aria-label rather than relying on the <h3> below: a landmark
    // with a stable name is what lets a caller (and the e2e suite) scope to
    // "the comments block" without walking the DOM upwards from a heading,
    // which breaks the moment this section grows a wrapper.
    <section role="region" aria-label="沟通时间线" className="flex flex-col gap-3">
      <div>
        <h3 className="text-sm font-medium text-fg">沟通时间线</h3>
        <p className="mt-0.5 text-xs text-fg-muted">普通评论与 Agent 对话按发生时间共同呈现。</p>
      </div>
      {unfinishedConversation && (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-secondary/25 bg-secondary/10 px-3 py-2.5">
          <div>
            <p className="text-sm font-medium text-fg">
              {unfinishedConversation.claim.status === 'waiting_human'
                ? 'Agent 正在等待回复'
                : 'Agent 正在执行'}
            </p>
            <p className="mt-0.5 text-xs text-fg-muted">
              {unfinishedConversation.claim.token_name || 'External Agent'}
              {' · '}
              {`有效至 ${new Date(unfinishedConversation.claim.expires_at).toLocaleString()}`}
            </p>
          </div>
          <button
            type="button"
            disabled={Boolean(releasingClaimID)}
            onClick={() => void releaseClaim(unfinishedConversation)}
            className="rounded-md border border-border-strong bg-surface px-3 py-1.5 text-xs font-medium text-fg hover:bg-surface-subtle disabled:opacity-50"
          >
            {releasingClaimID === unfinishedConversation.claim.id ? '释放中…' : '释放 Claim'}
          </button>
        </div>
      )}
      {timeline.length === 0 && <p className="text-sm text-fg-muted">还没有评论或 Agent 对话。</p>}
      <ul className="flex flex-col gap-3">
        {timeline.map((entry) => {
          if (entry.type === 'agent') {
            const { message, conversation } = entry
            const waitingForThisQuestion = message.kind === 'question'
              && conversation.claim.status === 'waiting_human'
              && !conversation.messages.some((candidate) => (
                candidate.kind === 'answer' && candidate.reply_to_message_id === message.id
              ))
            const isLatestSubmission = message.id === latestSubmissionID
            const reviewOpen = reviewingSubmissionID === message.id
            return (
              <li
                key={message.id}
                className={`rounded-md px-3 py-2.5 ${
                  message.kind === 'question'
                    ? 'bg-status-in-progress/10'
                    : 'bg-secondary/10'
                }`}
              >
                <div className="mb-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-fg-muted">
                  <span className="font-medium text-fg">
                    {message.author_type === 'agent'
                      ? (message.token_name || conversation.claim.token_name || 'Agent')
                      : (me?.name ?? '人工回复')}
                  </span>
                  <span>{agentMessageLabel(message.kind)}</span>
                  <span>· {new Date(message.created_at).toLocaleString()}</span>
                </div>
                <p className="whitespace-pre-wrap text-sm text-fg">{message.body}</p>
                {isLatestSubmission && (
                  <div className="mt-3">
                    <button
                      type="button"
                      aria-expanded={reviewOpen}
                      onClick={() => setReviewingSubmissionID((current) => (
                        current === message.id ? '' : message.id
                      ))}
                      className="rounded-md bg-secondary px-3 py-1.5 text-xs font-medium text-white"
                    >
                      {reviewOpen ? '收起验收' : '验收本次提交'}
                    </button>
                    {reviewOpen && (
                      <SubmissionReview
                        criteria={acceptanceCriteria}
                        submittedAt={conversation.claim.completed_at ?? message.created_at}
                        onCheck={onReviewCheck}
                        onComplete={onCompleteReview}
                        onReturnForChanges={onReturnForChanges}
                      />
                    )}
                  </div>
                )}
                {waitingForThisQuestion && (
                  <div className="mt-3 flex flex-col gap-2 border-t border-status-in-progress/20 pt-3">
                    <label className="text-xs font-medium text-fg" htmlFor={`claim-answer-${conversation.claim.id}`}>
                      回复 Agent 并恢复此任务
                    </label>
                    <textarea
                      id={`claim-answer-${conversation.claim.id}`}
                      rows={2}
                      value={answers[conversation.claim.id] ?? ''}
                      onChange={(event) => setAnswers((current) => ({
                        ...current, [conversation.claim.id]: event.target.value,
                      }))}
                      className="rounded-md border border-border-strong bg-surface px-2 py-1.5 text-sm text-fg"
                    />
                    <button
                      type="button"
                      disabled={
                        !(answers[conversation.claim.id] ?? '').trim()
                        || answeringClaimID === conversation.claim.id
                      }
                      onClick={() => void answerQuestion(conversation)}
                      className="self-end rounded-md bg-accent px-3 py-1.5 text-xs font-medium text-accent-fg disabled:opacity-50"
                    >
                      {answeringClaimID === conversation.claim.id ? '发送中…' : '发送回复'}
                    </button>
                  </div>
                )}
              </li>
            )
          }
          const c = entry.comment
          const mine = me?.id === c.author_id
          const isReply = Boolean(c.reply_to_comment_id)
          const authorName = memberByID.get(c.author_id)?.user.name
            ?? (mine ? me?.name : undefined)
            ?? '项目成员'
          return (
            <li
              key={c.id}
              className={`rounded-md border border-border p-3 ${isReply ? 'ml-6 bg-accent/5' : ''}`}
            >
              <div className="mb-1 flex items-center gap-2 text-xs text-fg-muted">
                <span className="font-medium text-fg">{authorName}</span>
                {isReply && <span>回复了评论</span>}
              </div>
              {c.deleted ? (
                <p className="mb-2 text-sm italic text-fg-muted">该评论已删除</p>
              ) : mine ? (
                <InlineEditable
                  value={c.body}
                  onCommit={(next) => editComment(c.id, next)}
                  multiline
                  ariaLabel="编辑评论"
                  className="mb-2 w-full text-sm text-fg"
                />
              ) : (
                <CommentBody
                  body={c.body}
                  mentionedUserIDs={c.mentioned_user_ids ?? []}
                  memberByID={memberByID}
                />
              )}
              <div className="flex items-center gap-2">
                <span className="text-xs text-fg-muted">{new Date(c.created_at).toLocaleString()}</span>
                {!c.deleted && (
                  <button
                    type="button"
                    className="text-xs text-fg-muted hover:text-accent"
                    onClick={() => startReply(c)}
                  >
                    回复
                  </button>
                )}
                {mine && !c.deleted && (
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

      <h4 className="text-xs font-medium text-fg-muted">添加普通评论</h4>
      <form className="flex flex-col gap-2 rounded-md border border-border bg-surface-subtle p-3" onSubmit={submit}>
        {replyToCommentID && (
          <div className="flex items-center justify-between gap-3 rounded bg-accent/10 px-2 py-1.5 text-xs text-fg">
            <span>正在回复 {memberByID.get(comments.find(({ id }) => id === replyToCommentID)?.author_id ?? '')?.user.name ?? '项目成员'}</span>
            <button type="button" className="text-fg-muted hover:text-fg" onClick={() => setReplyToCommentID('')}>取消</button>
          </div>
        )}
        <MentionInput
          ref={composerRef}
          value={commentDraft}
          options={mentionOptions}
          onChange={setCommentDraft}
          placeholder="添加评论…"
          ariaLabel="新评论内容"
        />
        <button
          type="submit"
          disabled={!commentDraft.body.trim() || posting}
          className="self-end rounded-md bg-accent px-3 py-1.5 text-xs font-medium text-accent-fg disabled:cursor-not-allowed disabled:opacity-50"
        >
          {posting ? '提交中…' : '评论'}
        </button>
      </form>
      {error && <p className="text-sm text-danger">{error}</p>}
    </section>
  )
}

function CommentBody({
  body,
  mentionedUserIDs,
  memberByID,
}: {
  body: string
  mentionedUserIDs: string[]
  memberByID: Map<string, ProjectMembership>
}) {
  const mentions = mentionedUserIDs.map((id) => ({
    id,
    name: memberByID.get(id)?.user.name ?? '项目成员',
  }))
  const missingMentions = mentions.filter(({ name }) => !body.includes(`@${name}`))
  const names = mentions
    .map(({ name }) => name)
    .filter((name) => name !== '项目成员')
    .sort((left, right) => right.length - left.length)
  const pattern = names.length > 0
    ? new RegExp(`(@(?:${names.map(escapeRegExp).join('|')}))`, 'g')
    : null
  const parts = pattern ? body.split(pattern) : [body]

  return (
    <p className="mb-2 whitespace-pre-wrap text-sm text-fg">
      {missingMentions.map(({ id, name }) => (
        <span key={id} className="mr-1 rounded-sm bg-accent/10 px-1 py-0.5 font-medium text-accent">
          @{name}
        </span>
      ))}
      {parts.map((part, index) => (
        part.startsWith('@') && names.includes(part.slice(1))
          ? <span key={`${part}-${index}`} className="rounded-sm bg-accent/10 px-1 py-0.5 font-medium text-accent">{part}</span>
          : part
      ))}
    </p>
  )
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function agentMessageLabel(kind: TaskClaimMessage['kind']): string {
  switch (kind) {
    case 'progress': return '进展'
    case 'question': return '需要确认'
    case 'answer': return '人工回复'
    case 'handoff': return '释放说明'
    case 'submission': return '提交预验收'
  }
}
