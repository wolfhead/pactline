import { expect, test } from '@playwright/test'

const ADMIN = {
  id: '00000000-0000-0000-0000-000000000001',
  name: 'Admin User',
  email: 'admin@example.test',
  avatar_url: null,
  platform_role: 'ADMIN',
  access_status: 'APPROVED',
  roles: ['SPONSOR'],
  active: true,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

test('Administrator creates a GitHub repository Connection', async ({ page }) => {
  let submitted: Record<string, unknown> | undefined
  const connection = {
    id: '10000000-0000-0000-0000-000000000001',
    version: 1,
    label: 'GitHub test repository',
    provider: 'github',
    origin: 'https://github.com',
    provider_repository_id: '52001',
    path_with_namespace: 'example/repository',
    canonical_web_url: 'https://github.com/example/repository',
    default_branch: 'main',
    credential_expires_at: null,
    status: 'active',
    last_validated_at: '2026-08-14T08:00:00Z',
    created_at: '2026-08-14T08:00:00Z',
    updated_at: '2026-08-14T08:00:00Z',
  }

  await page.route((url) => url.pathname.startsWith('/api/'), async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.pathname === '/api/me') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ actor: ADMIN, subject: ADMIN, impersonation: null }),
      })
      return
    }
    if (url.pathname === '/api/admin/repository-connections' && request.method() === 'GET') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' })
      return
    }
    if (url.pathname === '/api/admin/repository-connections' && request.method() === 'POST') {
      submitted = request.postDataJSON() as Record<string, unknown>
      await route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify(connection) })
      return
    }
    await route.fulfill({ status: 404, contentType: 'application/json', body: '{"error":"not mocked"}' })
  })

  await page.goto('/admin/connections')
  await expect(page.getByRole('heading', { name: '代码仓库 Connection' })).toBeVisible()
  await page.getByLabel('Provider').selectOption('github')
  await expect(page.getByLabel('仓库地址')).toHaveAttribute('placeholder', 'https://github.com/owner/repository')
  await page.getByLabel('显示名称').fill(connection.label)
  await page.getByLabel('仓库地址').fill(connection.canonical_web_url)
  await page.getByLabel(/只读 Access Token/).fill('synthetic-github-token')
  await page.getByRole('button', { name: '创建并鉴权' }).click()

  await expect.poll(() => submitted).toEqual({
    label: connection.label,
    provider: 'github',
    repository_url: connection.canonical_web_url,
    credential: 'synthetic-github-token',
    credential_expires_at: null,
  })
  await expect(page.getByText('已创建 example/repository 的 Connection。')).toBeVisible()
  await expect(page.getByLabel(/只读 Access Token/)).toHaveValue('')
})
