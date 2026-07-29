import { expect, test } from '@playwright/test'

const ADMIN = {
  id: '00000000-0000-0000-0000-000000000001',
  name: 'Admin User',
  email: 'admin@example.test',
  avatar_url: null,
  platform_role: 'ADMIN',
  roles: ['SPONSOR'],
  active: true,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

const MEMBER = {
  ...ADMIN,
  id: '00000000-0000-0000-0000-000000000002',
  name: 'Member User',
  email: 'member@example.test',
  platform_role: 'MEMBER',
}

test('an invitation fragment is scrubbed before the token is exchanged', async ({ page }) => {
  let submittedToken = ''
  await page.route('**/api/me', (route) => route.fulfill({
    status: 401,
    contentType: 'application/json',
    body: JSON.stringify({ error: 'authentication required' }),
  }))
  await page.route('**/api/invitations/accept', async (route) => {
    submittedToken = (route.request().postDataJSON() as { token: string }).token
    expect(new URL(page.url()).hash).toBe('')
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ authorization_url: '/login' }),
    })
  })

  await page.goto('/invite#one-time-secret')

  await expect(page).toHaveURL(/\/login$/)
  expect(submittedToken).toBe('one-time-secret')
  expect(page.url()).not.toContain('one-time-secret')
})

test('Admin can enter and exit read-only impersonation', async ({ page }) => {
  let impersonating = false
  await page.route((url) => url.pathname.startsWith('/api/'), async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.pathname === '/api/me') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          actor: ADMIN,
          subject: impersonating ? MEMBER : ADMIN,
          impersonation: impersonating ? {
            id: '10000000-0000-0000-0000-000000000001',
            session_id: '10000000-0000-0000-0000-000000000002',
            actor_user_id: ADMIN.id,
            subject_user_id: MEMBER.id,
            started_at: '2026-01-01T00:00:00Z',
          } : null,
        }),
      })
      return
    }
    if (url.pathname === '/api/v1/users') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [ADMIN, MEMBER] }) })
      return
    }
    if (url.pathname === '/api/admin/users') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([ADMIN, MEMBER]) })
      return
    }
    if (url.pathname === '/api/admin/impersonation' && request.method() === 'POST') {
      impersonating = true
      await route.fulfill({ status: 204 })
      return
    }
    if (url.pathname === '/api/admin/impersonation' && request.method() === 'DELETE') {
      impersonating = false
      await route.fulfill({ status: 204 })
      return
    }
    if (url.pathname === '/api/v1/tasks') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: [] }),
      })
      return
    }
    if (url.pathname === '/api/v1/labels' || url.pathname === '/api/v1/projects') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [] }) })
      return
    }
    await route.fulfill({ status: 404, contentType: 'application/json', body: '{"error":"not mocked"}' })
  })

  await page.goto('/admin/users')
  await expect(page.getByRole('heading', { name: '用户管理' })).toBeVisible()
  page.once('dialog', (dialog) => dialog.accept())
  await page.getByRole('button', { name: '只读查看' }).click()

  await expect(page.getByText('管理员 Admin User 正以 Member User 身份只读查看')).toBeVisible()
  await expect(page.getByRole('link', { name: '用户' })).toHaveCount(0)
  await expect(page.getByRole('textbox', { name: '新建任务' })).toBeDisabled()

  await page.getByRole('button', { name: '退出只读查看' }).click()
  await expect(page.getByText(/身份只读查看/)).toHaveCount(0)
  await expect(page.getByRole('link', { name: '用户' })).toBeVisible()
})
