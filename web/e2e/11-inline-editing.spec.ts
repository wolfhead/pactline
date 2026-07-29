import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: editing in place. The title is edited exactly where it is
 * displayed (InlineEditable, no modal, no click-to-edit toggle) — Enter
 * commits and the change survives a reload; Escape discards the draft and
 * nothing reaches the server. A property (status) is then changed from the
 * detail's own permanently-visible control and must likewise survive a
 * reload: title and property are the two halves of "edited where it is
 * read", and only one of them is a text field.
 *
 * Rewritten for the new controls (Task 14). Status/priority/assignee are no
 * longer a QuietSelect that turns into a native <select> on click, so
 * `selectOption()` no longer applies to them: a Radix Select is driven by
 * opening the `combobox` and picking an `option`. The detail pane names its
 * controls bare (状态), the list rows name theirs per-task
 * (任务 #142 状态) — hence `exact: true` here, since at xl both are on
 * screen at once and a substring match would find both.
 */
test('title edits in place: Enter commits and survives reload, Escape discards without saving', async ({
  page,
  uniqueTitle,
  trackTask,
  tasksApi,
}) => {
  const original = uniqueTitle('Inline edit original')
  const committed = uniqueTitle('Inline edit committed')
  const abandoned = uniqueTitle('Inline edit abandoned draft')

  const task = await tasksApi.createTask(USERS.engineerC.id, { title: original, status: 'todo' })
  trackTask(task.id)

  await page.goto(`/tasks/${task.number}`)
  await switchIdentity(page, USERS.engineerC.id)

  const titleField = page.getByLabel('任务标题')
  await expect(titleField).toHaveValue(original)

  const firstPatch = page.waitForResponse(
    (res) => res.url().endsWith(`/api/v1/tasks/${task.number}`) && res.request().method() === 'PATCH',
  )
  await titleField.fill(committed)
  await titleField.press('Enter')
  await expect(titleField).toHaveValue(committed)
  // Wait for the commit's PATCH to actually land before reloading — the
  // reload below otherwise races the optimistic UI (already updated)
  // against the still-in-flight request under real network/CPU contention
  // (a full parallel suite run), reloading before the server had persisted
  // it.
  await firstPatch

  await page.reload()
  await expect(page.getByLabel('任务标题')).toHaveValue(committed)

  // Start an edit and abandon it with Escape: the original (last committed)
  // text must return immediately, in the DOM, not eventually via a round
  // trip.
  await page.getByLabel('任务标题').fill(abandoned)
  await expect(page.getByLabel('任务标题')).toHaveValue(abandoned)
  await page.getByLabel('任务标题').press('Escape')
  await expect(page.getByLabel('任务标题')).toHaveValue(committed)

  // And the abandoned draft must genuinely never have been saved.
  await page.reload()
  await expect(page.getByLabel('任务标题')).toHaveValue(committed)

  // The other half of editing in place: a property, changed on the control
  // that is already visible beside its label — no reveal step first.
  const statusField = page.getByRole('combobox', { name: '状态', exact: true })
  await expect(statusField).toHaveText(/待办/)

  const statusPatch = page.waitForResponse(
    (res) => res.url().endsWith(`/api/v1/tasks/${task.number}`) && res.request().method() === 'PATCH',
  )
  await statusField.click()
  await page.getByRole('option', { name: '进行中', exact: true }).click()
  await expect(page.getByRole('combobox', { name: '状态', exact: true })).toHaveText(/进行中/)
  await statusPatch

  await page.reload()
  await expect(page.getByRole('combobox', { name: '状态', exact: true })).toHaveText(/进行中/)
})
