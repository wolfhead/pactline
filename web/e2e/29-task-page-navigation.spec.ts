import { expect, test } from '@playwright/test'

const USER = {
  id: '00000000-0000-0000-0000-000000000001',
  name: 'Navigation Tester',
  email: 'navigation@example.test',
  avatar_url: null,
  platform_role: 'ADMIN',
  access_status: 'APPROVED',
  roles: ['SPONSOR'],
  active: true,
  created_at: '2026-08-20T00:00:00Z',
  updated_at: '2026-08-20T00:00:00Z',
}

const TASK = {
  id: '00000000-0000-4000-8000-000000000142',
  number: 142,
  version: 1,
  title: 'Standalone navigation task',
  context: '## Why this matters\n\nThis task loads independently from every collection while preserving a readable line length for a deliberately long explanation.\n\n- Direct links remain stable\n- Collection context remains recoverable',
  expected_result: 'Navigation returns to its exact source, and the complete task remains operable without turning into a wide form.',
  description: '',
  phase: 'backlog',
  activity: null,
  review_cycle: 0,
  main_thread_id: '00000000-0000-4000-8000-000000001142',
  priority: 'high',
  assignee: USER,
  creator: USER,
  start_date: null,
  due_date: null,
  project: { id: '00000000-0000-4000-8000-000000000012', number: 12, name: 'Launch' },
  milestone: { id: '00000000-0000-4000-8000-000000000099', name: 'Release' },
  labels: [
    { id: '00000000-0000-4000-8000-000000000201', name: 'Mobile readiness verification' },
    { id: '00000000-0000-4000-8000-000000000202', name: 'UnbrokenLabelNameThatMustWrapWithinThePropertyColumn' },
  ],
  parent: null,
  children: [],
  dependencies: [],
  dependents: [],
  blocked: false,
  created_at: '2026-08-20T00:00:00Z',
  updated_at: '2026-08-20T00:00:00Z',
  completed_at: null,
  archived_at: null,
}

const BACKLOG_TASK = {
  ...TASK,
  id: '00000000-0000-4000-8000-000000000143',
  number: 143,
  title: 'Standalone backlog navigation task',
  main_thread_id: '00000000-0000-4000-8000-000000001143',
  milestone: null,
}

const MILESTONE = {
  id: TASK.milestone.id,
  project_id: TASK.project.id,
  version: 1,
  name: TASK.milestone.name,
  outcome: 'Verify standalone task navigation.',
  description: '',
  owner_id: USER.id,
  status: 'active',
  target_date: null,
  position: 0,
  completed_at: null,
  cancelled_at: null,
  created_at: TASK.created_at,
  updated_at: TASK.updated_at,
  acceptance_criteria: [],
}

const PROJECT_DETAIL = {
  project: {
    id: TASK.project.id,
    number: TASK.project.number,
    version: 1,
    name: TASK.project.name,
    description: 'Project fixture',
    creator: USER,
    archived_at: null,
    created_at: TASK.created_at,
    updated_at: TASK.updated_at,
    completed_tasks: 0,
    eligible_tasks: 2,
  },
  milestones: [MILESTONE],
  tasks: [TASK, BACKLOG_TASK],
  activity: [],
}

async function mockApplication(page: import('@playwright/test').Page) {
  await page.route((url) => url.pathname.startsWith('/api/'), async (route) => {
    const url = new URL(route.request().url())
    const json = (body: unknown, headers: Record<string, string> = {}) => route.fulfill({
      status: 200,
      contentType: 'application/json',
      headers,
      body: JSON.stringify(body),
    })

    if (url.pathname === '/api/me') {
      await json({ actor: USER, subject: USER, impersonation: null })
      return
    }
    if (url.pathname === '/api/v1/users') {
      await json({ items: [USER] })
      return
    }
    if (url.pathname === '/api/v1/tasks/142') {
      await json(TASK, { ETag: '"1"' })
      return
    }
    if (url.pathname === '/api/v1/tasks/143') {
      await json(BACKLOG_TASK, { ETag: '"1"' })
      return
    }
    if (url.pathname === '/api/v1/projects/12') {
      await json(PROJECT_DETAIL, { ETag: '"1"' })
      return
    }
    if (url.pathname === '/api/v1/projects/12/members') {
      await json({ items: [] })
      return
    }
    if (url.pathname === '/api/v1/projects') {
      await json({ items: [PROJECT_DETAIL.project] })
      return
    }
    if (url.pathname === '/api/v1/tasks') {
      const items = url.searchParams.get('backlog_only') === 'true'
        ? [BACKLOG_TASK]
        : url.searchParams.has('milestone_id')
          ? [TASK]
          : [TASK, BACKLOG_TASK]
      await json({ items })
      return
    }
    if (url.pathname === '/api/v1/labels') {
      await json({ items: [] })
      return
    }
    if (/^\/api\/v1\/tasks\/(142|143)\/code-changes$/.test(url.pathname)) {
      await json({ active_links: [] })
      return
    }
    if (/^\/api\/v1\/tasks\/(142|143)\//.test(url.pathname)) {
      await json({ items: [] })
      return
    }
    await route.fulfill({
      status: 404,
      contentType: 'application/problem+json',
      body: JSON.stringify({ title: 'Not mocked', code: 'NOT_MOCKED', request_id: 'e2e' }),
    })
  })
}

async function openTaskAndReturn(
  page: import('@playwright/test').Page,
  source: string,
  task = TASK,
) {
  await page.goto(source)
  await page.getByRole('link', { name: task.title, exact: true }).click()
  await expect(page).toHaveURL(`/tasks/${task.number}`)
  await expect(page.getByRole('heading', { name: task.title, exact: true, level: 1 })).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(0)

  await page.goBack()
  await expect(page).toHaveURL(source)
  await page.goForward()
  await expect(page).toHaveURL(`/tasks/${task.number}`)

  await page.getByRole('link', { name: '返回任务集合' }).click()
  await expect(page).toHaveURL(source)
}

test('standalone Task navigation preserves all three collection sources and responsive page flow', async ({ page }, testInfo) => {
  await mockApplication(page)
  await page.setViewportSize({ width: 1440, height: 1000 })

  await openTaskAndReturn(page, '/tasks?ownership=created&q=Standalone&sort=number&order=asc')
  await openTaskAndReturn(
    page,
    '/projects/12?priority=high&q=Standalone',
    BACKLOG_TASK,
  )
  await openTaskAndReturn(
    page,
    `/projects/12/milestones/${MILESTONE.id}?phase=backlog&view=gantt`,
  )

  await page.goto('/tasks/142')
  await expect(page.getByRole('heading', { name: TASK.title, exact: true, level: 1 })).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  const pageHeader = page.getByRole('region', { name: '任务页标题' })
  const taskBody = page.getByRole('region', { name: '任务正文' })
  const taskSidebar = page.getByRole('complementary', { name: '任务属性与交付' })
  await expect(pageHeader).toContainText('待规划')
  await expect(pageHeader).toContainText(`负责人：${USER.name}`)
  await expect(taskBody).toBeVisible()
  await expect(taskSidebar).toBeVisible()
  await expect(page.getByRole('region', { name: '当前工作' })).toBeVisible()
  expect(await taskBody.evaluate((element) => element.getBoundingClientRect().width)).toBeLessThanOrEqual(780)
  expect(await taskSidebar.evaluate((element) => getComputedStyle(element).position)).toBe('sticky')
  expect(await page.getByText('任务阶段', { exact: true }).evaluate(
    (element) => element.getBoundingClientRect().bottom <= window.innerHeight,
  )).toBe(true)
  await testInfo.attach('task-page-desktop', {
    body: await page.screenshot(),
    contentType: 'image/png',
  })

  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByRole('heading', { name: TASK.title, exact: true, level: 1 })).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  expect(await page.locator('[data-task-brief]').evaluate((brief) => {
    const acceptance = document.querySelector('[data-task-acceptance]')
    const thread = document.querySelector('[data-task-thread]')
    const sidebar = document.querySelector('[data-task-sidebar]')
    if (!acceptance || !thread || !sidebar) return false
    return Boolean(
      (brief.compareDocumentPosition(acceptance) & Node.DOCUMENT_POSITION_FOLLOWING)
      && (acceptance.compareDocumentPosition(thread) & Node.DOCUMENT_POSITION_FOLLOWING)
      && (thread.compareDocumentPosition(sidebar) & Node.DOCUMENT_POSITION_FOLLOWING)
    )
  })).toBe(true)
  await expect(page.getByRole('button', { name: '编辑背景 / 问题' })).toBeVisible()
  await expect(page.getByRole('button', { name: '编辑期望结果' })).toBeVisible()
  await expect(page.getByRole('button', { name: '归档' })).toBeVisible()
  const properties = page.locator('[data-task-properties]')
  const labels = page.getByRole('button', { name: '标签' })
  await expect(labels).toContainText('UnbrokenLabelNameThatMustWrapWithinThePropertyColumn')
  expect(await labels.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true)
  expect(await properties.evaluate((element) => (
    [...element.querySelectorAll<HTMLElement>('*')].every((child) => (
      child.getBoundingClientRect().right <= element.getBoundingClientRect().right + 1
    ))
  ))).toBe(true)
  expect(await taskSidebar.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true)
  await testInfo.attach('task-page-mobile', {
    body: await page.screenshot(),
    contentType: 'image/png',
  })
})
