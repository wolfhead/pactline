import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: comments — add, edit and delete, all as the author.
 */
test('comments: add, edit and delete as the author', async ({ page, uniqueTitle, trackTask, tasksApi }) => {
  const title = uniqueTitle('Comment lifecycle')
  const task = await tasksApi.createTask(USERS.engineerC.id, { title })
  trackTask(task.id)

  await page.goto(`/tasks/${task.number}`)
  await switchIdentity(page, USERS.engineerC.id)

  // Scope every locator to the comments section itself: both "评论" (the
  // submit button) and "删除" collide with controls elsewhere on the page
  // (the label manager also has a "删除" button, just hidden inside a
  // collapsed <details> — still present in the DOM and still a strict-mode
  // match). CommentSection carries role="region" + aria-label="评论", so the
  // block is addressed by name rather than by climbing out of its heading
  // with an xpath that any added wrapper would break.
  const commentSection = page.getByRole('region', { name: '评论' })

  await expect(commentSection.getByText('还没有评论。')).toBeVisible()

  await commentSection.getByLabel('新评论内容').fill('First comment')
  await commentSection.getByRole('button', { name: '评论', exact: true }).click()

  await expect(commentSection.getByText('First comment', { exact: true })).toBeVisible()
  await expect(commentSection.getByText('还没有评论。')).not.toBeVisible()

  // Edit: the comment body is itself an InlineEditable, for its own author.
  const editPatch = page.waitForResponse(
    (res) => res.url().includes(`/api/v1/tasks/${task.number}/comments/`) && res.request().method() === 'PATCH',
  )
  const commentField = commentSection.getByLabel('编辑评论')
  await commentField.fill('First comment, edited')
  await commentField.blur()
  await expect(commentSection.getByText('First comment, edited', { exact: true })).toBeVisible()
  // Wait for the edit's PATCH to actually persist before reloading — this
  // is optimistic like every other mutation in the app, so reloading right
  // after the (already-updated) UI assertion can otherwise race ahead of
  // the server actually saving it.
  await editPatch

  await page.reload()
  const commentSectionAfterReload = page.getByRole('region', { name: '评论' })
  await expect(commentSectionAfterReload.getByText('First comment, edited', { exact: true })).toBeVisible()

  // Delete.
  await commentSectionAfterReload.getByRole('button', { name: '删除', exact: true }).click()
  await expect(commentSectionAfterReload.getByText('First comment, edited', { exact: true })).not.toBeVisible()
  await expect(commentSectionAfterReload.getByText('还没有评论。')).toBeVisible()
})
