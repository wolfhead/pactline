import { expect, test, type Page, type Route } from '@playwright/test'

const now = '2026-08-16T10:03:00.000Z'
const service = {
  serviceId: 'service-browser', version: 'test', mode: 'ready', live: true, ready: true,
  startedAt: '2026-08-16T10:00:00.000Z', updatedAt: now,
  config: { revision: 'a'.repeat(64), loadedAt: '2026-08-16T10:00:00.000Z' },
  registry: { status: 'ok', path: '/tmp/fleet.sqlite3', schemaVersion: 3, nonTerminalRuns: 1 },
  pactline: { status: 'ok', server: 'http://localhost:8080', checkedAt: now },
  adapters: [{ id: 'deepseek', status: 'ok', version: '0.1.0-rc.6', checkedAt: now, capabilities: { sessionResume: false } }],
  fleets: [{ id: 'development', projectNumber: 5, enabled: true, status: 'healthy', adapters: ['deepseek'], discovery: { status: 'ok', candidateCount: 1, checkedAt: now } }],
}
const run = {
  runId: 'run-41', serviceId: 'service-browser', fleetId: 'development', projectNumber: 5,
  configRevision: 'b'.repeat(64), taskNumber: 41, taskVersion: 7, claimId: 'claim-41', claimVersion: 1,
  claimTaskVersion: 8, runtimeSessionId: 'session-41', stage: 'execution', state: 'validating',
  adapter: 'deepseek', model: 'deepseek-v4-pro', reasoning: 'max', checkpoint: 'harness_result_observed',
  createdAt: '2026-08-16T10:00:00.000Z', updatedAt: now,
  workspace: { repositoryPath: '/tmp/work/project-5', baseRevision: 'c'.repeat(40) },
  timeline: [
    { sequence: 1, at: '2026-08-16T10:00:00.000Z', kind: 'run.admitted', title: 'Run admitted', state: 'admitted' },
    { sequence: 2, at: now, kind: 'run.transitioned', title: 'Run entered validating', state: 'validating', checkpoint: 'harness_result_observed' },
  ],
  effects: [{ kind: 'harness_result', status: 'observed', title: 'Harness result', detail: { terminalState: 'success' }, updatedAt: now }],
}
const fleet = {
  id: 'development', projectNumber: 5, enabled: true, status: 'healthy', maxConcurrentRuns: 1,
  workPluginConfigured: true, workspaceRoot: '/tmp/work/project-5',
  routing: { execution: { adapter: 'deepseek', model: 'deepseek-v4-pro', reasoning: 'max' } },
  activeRunCount: 1, recentRunCount: 0, discovery: { status: 'ok', candidateCount: 1, checkedAt: now },
}
const overview = { attention: [], fleets: [fleet], activeRuns: [run], recentRuns: [] }

function json(route: Route, data: unknown): Promise<void> {
  return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true, data, meta: { generatedAt: now, revision: 'browser' } }) })
}

async function mockObservation(page: Page): Promise<void> {
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    if (url.pathname === '/api/v1/events') return route.fulfill({ status: 204 })
    if (url.pathname === '/api/v1/service') return json(route, service)
    if (url.pathname === '/api/v1/overview') return json(route, overview)
    if (url.pathname === '/api/v1/fleets/development') return json(route, fleet)
    if (url.pathname === '/api/v1/runs/run-41') return json(route, run)
    if (url.pathname === '/api/v1/runs') return json(route, { items: [run] })
    if (url.pathname === '/api/v1/adapters') return json(route, { items: service.adapters })
    return route.fulfill({ status: 404, contentType: 'application/json', body: '{"ok":false}' })
  })
}

test.beforeEach(async ({ page }) => { await mockObservation(page) })

test('moves from the operating overview into its Project-bound Fleet', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Operations overview' })).toBeVisible()
  await expect(page.getByText('Ready for admission')).toBeVisible()
  await page.locator('a[href="/fleets/development"]').click()
  await expect(page.getByRole('heading', { name: 'Project 5' })).toBeVisible()
  await expect(page.getByText('Configured and eligible for scheduling')).toBeVisible()
})

test('keeps current state and the last safe checkpoint prominent on Run detail', async ({ page }) => {
  await page.goto('/runs/run-41')
  await expect(page.getByRole('heading', { name: 'Task #41' })).toBeVisible()
  await expect(page.getByText('Last safe checkpoint')).toBeVisible()
  await expect(page.getByText('Run entered validating')).toBeVisible()
  await expect(page.getByText('Repository Path')).toBeVisible()
})

test('shows dependency and Adapter facts without operational mutation controls', async ({ page }) => {
  await page.goto('/system')
  await expect(page.getByRole('heading', { name: 'System' })).toBeVisible()
  await expect(page.getByText('Harness Adapters')).toBeVisible()
  await expect(page.getByText('0.1.0-rc.6')).toBeVisible()
  await expect(page.getByRole('button', { name: /claim|retry|release|pause|drain/i })).toHaveCount(0)
})

for (const viewport of [{ name: 'phone', width: 390, height: 844 }, { name: 'medium', width: 900, height: 760 }]) {
  test(`${viewport.name} layout preserves the operating flow without horizontal overflow`, async ({ page }) => {
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.goto('/')
    await expect(page.getByRole('heading', { name: 'Operations overview' })).toBeVisible()
    const widths = await page.evaluate(() => ({ client: document.documentElement.clientWidth, scroll: document.documentElement.scrollWidth }))
    expect(widths.scroll).toBe(widths.client)
    if (viewport.name === 'phone') await expect(page.getByRole('navigation', { name: 'Mobile navigation' })).toBeVisible()
  })
}
