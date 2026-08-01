import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: comments remain understandable as a one-level thread, mentions
 * are structured project-member references, and deleting a replied-to root
 * preserves both the reply context and the reply itself.
 */
test('comments: mention, reply, edit, and preserve a deleted root placeholder', async ({
  page,
  uniqueTitle,
  trackTask,
  ensureProjectMember,
  tasksApi,
}) => {
  const title = uniqueTitle('Comment lifecycle')
  const task = await tasksApi.createTask(USERS.engineerC.id, { title })
  trackTask(task.id)
  await ensureProjectMember(task.project.id, USERS.engineerC.id)

  await page.goto(`/tasks/${task.number}`)
  await switchIdentity(page, USERS.engineerC.id)

  // Scope every locator to the combined timeline itself: both "评论" (the
  // submit button) and "删除" collide with controls elsewhere on the page
  // (the label manager also has a "删除" button, just hidden inside a
  // collapsed <details> — still present in the DOM and still a strict-mode
  // match). CommentSection carries a named region, so the
  // block is addressed by name rather than by climbing out of its heading
  // with an xpath that any added wrapper would break.
  const commentSection = page.getByRole('region', { name: '沟通时间线' })

  await expect(commentSection.getByText('还没有评论或 Agent 对话。')).toBeVisible()

  const commentEditor = commentSection.getByRole('combobox', { name: '新评论内容' })
  await commentEditor.fill('First comment ')
  await commentEditor.press('@')
  // The reusable picker is portalled so it can escape a scrolling inspector;
  // locate its options from the page rather than the timeline region.
  await page.getByRole('option', { name: /研发 C/ }).click()
  await expect(commentEditor.locator(`[data-mention-id="${USERS.engineerC.id}"]`)).toBeVisible()
  const commentCreate = page.waitForResponse(
    (res) => res.url().endsWith(`/api/v1/tasks/${task.number}/comments`) && res.request().method() === 'POST',
  )
  await commentSection.getByRole('button', { name: '评论', exact: true }).click()
  const createdComment = await (await commentCreate).json() as { mentioned_user_ids: string[] }
  expect(createdComment.mentioned_user_ids).toEqual([USERS.engineerC.id])

  await expect(commentSection.getByText('First comment @研发 C', { exact: true })).toBeVisible()
  await expect(commentSection.getByText('还没有评论或 Agent 对话。')).not.toBeVisible()
  const rootComment = commentSection.locator('li').filter({ hasText: 'First comment' })
  await expect(rootComment).toContainText('@研发 C')

  // Edit: the comment body is itself an InlineEditable, for its own author.
  const editPatch = page.waitForResponse(
    (res) => res.url().includes(`/api/v1/tasks/${task.number}/comments/`) && res.request().method() === 'PATCH',
  )
  const commentField = rootComment.getByLabel('编辑评论')
  await commentField.fill('First comment, edited')
  await commentField.blur()
  await expect(commentSection.getByText('First comment, edited', { exact: true })).toBeVisible()
  // Wait for the edit's PATCH to actually persist before reloading — this
  // is optimistic like every other mutation in the app, so reloading right
  // after the (already-updated) UI assertion can otherwise race ahead of
  // the server actually saving it.
  await editPatch

  await rootComment.getByRole('button', { name: '回复', exact: true }).click()
  await expect(commentSection.getByText(/正在回复/)).toBeVisible()
  await commentSection.getByLabel('新评论内容').fill('Nested reply')
  await commentSection.getByRole('button', { name: '评论', exact: true }).click()
  const replyComment = commentSection.locator('li').filter({ hasText: 'Nested reply' })
  await expect(replyComment.getByText('回复了评论', { exact: true })).toBeVisible()

  await page.reload()
  const commentSectionAfterReload = page.getByRole('region', { name: '沟通时间线' })
  await expect(commentSectionAfterReload.getByText('First comment, edited', { exact: true })).toBeVisible()
  await expect(commentSectionAfterReload.getByText('Nested reply', { exact: true })).toBeVisible()

  // Delete the root. Its placeholder remains because the reply still points
  // to it, while the original body and mention are no longer exposed.
  const rootAfterReload = commentSectionAfterReload.locator('li').filter({ hasText: 'First comment, edited' })
  await rootAfterReload.getByRole('button', { name: '删除', exact: true }).click()
  await expect(commentSectionAfterReload.getByText('First comment, edited', { exact: true })).not.toBeVisible()
  await expect(commentSectionAfterReload.getByText('该评论已删除', { exact: true })).toBeVisible()
  await expect(commentSectionAfterReload.getByText('Nested reply', { exact: true })).toBeVisible()
})
