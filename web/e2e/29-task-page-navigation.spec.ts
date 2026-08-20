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
  context: 'This task loads independently from every collection.',
  expected_result: 'Navigation returns to its exact source.',
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
  labels: [],
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
    eligible_tasks: 1,
  },
  milestones: [MILESTONE],
  tasks: [TASK],
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
      await json({ items: [TASK] })
      return
    }
    if (url.pathname === '/api/v1/labels') {
      await json({ items: [] })
      return
    }
    if (url.pathname === '/api/v1/tasks/142/code-changes') {
      await json({ active_links: [] })
      return
    }
    if (url.pathname.startsWith('/api/v1/tasks/142/')) {
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

async function openTaskAndReturn(page: import('@playwright/test').Page, source: string) {
  await page.goto(source)
  await page.getByRole('link', { name: TASK.title, exact: true }).click()
  await expect(page).toHaveURL('/tasks/142')
  await expect(page.getByRole('heading', { name: TASK.title, exact: true })).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(0)

  await page.goBack()
  await expect(page).toHaveURL(source)
  await page.goForward()
  await expect(page).toHaveURL('/tasks/142')

  await page.getByRole('link', { name: '返回任务集合' }).click()
  await expect(page).toHaveURL(source)
}

test('standalone Task navigation preserves all three collection sources and responsive page flow', async ({ page }, testInfo) => {
  await mockApplication(page)
  await page.setViewportSize({ width: 1440, height: 900 })

  await openTaskAndReturn(page, '/tasks?ownership=created&q=Standalone&sort=number&order=asc')
  await openTaskAndReturn(page, '/projects/12/backlog?priority=high&q=Standalone')
  await openTaskAndReturn(
    page,
    `/projects/12/milestones/${MILESTONE.id}?phase=backlog&view=gantt`,
  )

  await page.goto('/tasks/142')
  await expect(page.getByRole('heading', { name: TASK.title, exact: true })).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await testInfo.attach('task-page-desktop', {
    body: await page.screenshot(),
    contentType: 'image/png',
  })

  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByRole('heading', { name: TASK.title, exact: true })).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await testInfo.attach('task-page-mobile', {
    body: await page.screenshot(),
    contentType: 'image/png',
  })
})
