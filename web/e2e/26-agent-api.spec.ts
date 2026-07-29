import type { APIRequestContext, APIResponse, Page } from '@playwright/test'
import type { Pool } from 'pg'
import { randomUUID } from 'node:crypto'
import { expect, test } from './support/task-fixtures'

interface IssuedToken {
  id: string
  name: string
  token: string
}

interface VersionedResource {
  id: string
  number: number
  version: number
}

interface Criterion {
  id: string
  version: number
  revision: number
}

interface Milestone {
  id: string
  version: number
  status: string
}

interface ProjectDetail extends VersionedResource {
  status: string
  milestones: Milestone[]
}

interface TaskResource extends VersionedResource {
  status: string
  title: string
}

interface Problem {
  code: string
  current_version?: number
}

test('a human can provision, inspect, and revoke an Agent that completes versioned work', async ({
  page,
  request,
  taskDbPool,
  runTag,
}) => {
  test.setTimeout(90_000)

  const writeTokenName = `Agent write ${runTag}`
  const readTokenName = `Agent read ${runTag}`
  const projectName = `Agent project ${runTag}`
  const taskTitle = `Agent task ${runTag}`
  const auditRequestID = `agent-e2e-${runTag}`
  const tokenNames = [writeTokenName, readTokenName]
  const existingAdmin = (await taskDbPool.query<{ id: string; name: string }>(
    `SELECT id, name FROM users WHERE platform_role='ADMIN' LIMIT 1`,
  )).rows[0]
  const temporaryAdmin = !existingAdmin
  const adminID = existingAdmin?.id ?? randomUUID()
  const adminName = existingAdmin?.name ?? `Agent Admin ${runTag}`

  let projectID: string | undefined
  let taskID: string | undefined
  let sessionID: string | undefined
  if (temporaryAdmin) {
    await taskDbPool.query(
      `INSERT INTO users (
         id, name, email, roles, active, platform_role, created_at, updated_at
       ) VALUES ($1, $2, $3, '{}', true, 'ADMIN', now(), now())`,
      [adminID, adminName, `agent-admin-${runTag}@example.test`],
    )
  }

  try {
    await authenticateDevelopmentUser(page, adminID)
    sessionID = (await taskDbPool.query<{ id: string }>(
      `SELECT id FROM sessions WHERE user_id=$1 ORDER BY created_at DESC LIMIT 1`,
      [adminID],
    )).rows[0]?.id
    const writeToken = await issueToken(page, writeTokenName, 'write')
    const readToken = await issueToken(page, readTokenName, 'read')

    await test.step('read-only authority cannot mutate and bearer auth cannot discover internal routes', async () => {
      const readWriteAttempt = await agentRequest(
        request,
        'POST',
        '/api/v1/tasks',
        readToken.token,
        { data: { title: `Forbidden ${runTag}` }, idempotencyKey: `read-denied-${runTag}` },
      )
      expect(readWriteAttempt.status()).toBe(403)
      expect((await readWriteAttempt.json() as Problem).code).toBe('INSUFFICIENT_SCOPE')

      const internal = await agentRequest(request, 'GET', '/api/account/tokens', writeToken.token)
      expect(internal.status()).toBe(404)
      const removed = await agentRequest(request, 'GET', '/api/tasks', writeToken.token)
      expect(removed.status()).toBe(404)
    })

    const projectKey = `project-${runTag}`
    const projectCreate = await agentRequest(request, 'POST', '/api/v1/projects', writeToken.token, {
      idempotencyKey: projectKey,
      data: {
        name: projectName,
        outcome: 'A human-visible Agent workflow reaches verified completion',
        owner_id: adminID,
      },
    })
    expect(projectCreate.status()).toBe(201)
    expect(projectCreate.headers()['ratelimit-limit']).toBe('120')
    const project = await projectCreate.json() as VersionedResource
    projectID = project.id
    let projectETag = requiredHeader(projectCreate, 'etag')

    const projectReplay = await agentRequest(request, 'POST', '/api/v1/projects', writeToken.token, {
      idempotencyKey: projectKey,
      data: {
        name: projectName,
        outcome: 'A human-visible Agent workflow reaches verified completion',
        owner_id: adminID,
      },
    })
    expect(projectReplay.status()).toBe(201)
    expect(projectReplay.headers()['idempotency-replayed']).toBe('true')
    expect((await projectReplay.json() as VersionedResource).id).toBe(project.id)

    const projectCriterionCreate = await agentRequest(
      request,
      'POST',
      `/api/v1/projects/${project.number}/criteria`,
      writeToken.token,
      {
        ifMatch: projectETag,
        idempotencyKey: `project-criterion-${runTag}`,
        data: {
          criterion: 'The Agent-managed project is complete',
          verification_instructions: 'Inspect the linked task and API audit evidence',
          position: 0,
        },
      },
    )
    expect(projectCriterionCreate.status()).toBe(201)
    const projectCriterion = await projectCriterionCreate.json() as Criterion
    requiredHeader(projectCriterionCreate, 'etag')

    const projectCheck = await agentRequest(
      request,
      'POST',
      `/api/v1/criteria/${projectCriterion.id}/checks`,
      writeToken.token,
      {
        ifMatch: `"${projectCriterion.version}"`,
        idempotencyKey: `project-check-${runTag}`,
        data: {
          criterion_revision: projectCriterion.revision,
          outcome: 'passed',
          evidence: 'The browser-driven Agent API acceptance reached this step.',
        },
      },
    )
    expect(projectCheck.status()).toBe(201)
    const checkedProject = await getProject(request, writeToken.token, project.number)
    projectETag = checkedProject.etag

    const activateProject = await agentRequest(
      request,
      'POST',
      `/api/v1/projects/${project.number}/activate`,
      writeToken.token,
      {
        ifMatch: projectETag,
        idempotencyKey: `activate-project-${runTag}`,
        data: {},
      },
    )
    expect(activateProject.status()).toBe(200)
    projectETag = requiredHeader(activateProject, 'etag')

    const milestoneCreate = await agentRequest(
      request,
      'POST',
      `/api/v1/projects/${project.number}/milestones`,
      writeToken.token,
      {
        ifMatch: projectETag,
        idempotencyKey: `milestone-${runTag}`,
        data: {
          name: 'Agent API accepted',
          outcome: 'The complete API workflow is verified',
          position: 0,
        },
      },
    )
    expect(milestoneCreate.status()).toBe(201)
    const milestone = await milestoneCreate.json() as Milestone
    let milestoneETag = requiredHeader(milestoneCreate, 'etag')

    let projectDetail = await getProject(request, writeToken.token, project.number)
    projectETag = projectDetail.etag

    const milestoneCriterionCreate = await agentRequest(
      request,
      'POST',
      `/api/v1/projects/${project.number}/milestones/${milestone.id}/criteria`,
      writeToken.token,
      {
        ifMatch: milestoneETag,
        projectIfMatch: projectETag,
        idempotencyKey: `milestone-criterion-${runTag}`,
        data: {
          criterion: 'The Agent workflow is auditable',
          verification_instructions: 'Filter the Admin audit by request ID',
          position: 0,
        },
      },
    )
    expect(milestoneCriterionCreate.status()).toBe(201)
    const milestoneCriterion = await milestoneCriterionCreate.json() as Criterion

    const milestoneCheck = await agentRequest(
      request,
      'POST',
      `/api/v1/criteria/${milestoneCriterion.id}/checks`,
      writeToken.token,
      {
        ifMatch: `"${milestoneCriterion.version}"`,
        idempotencyKey: `milestone-check-${runTag}`,
        data: {
          criterion_revision: milestoneCriterion.revision,
          outcome: 'passed',
          evidence: 'The request is correlated through its audited token identity.',
        },
      },
    )
    expect(milestoneCheck.status()).toBe(201)

    const taskCreate = await agentRequest(request, 'POST', '/api/v1/tasks', writeToken.token, {
      idempotencyKey: `task-${runTag}`,
      data: {
        title: taskTitle,
        project_number: project.number,
        milestone_id: milestone.id,
      },
    })
    expect(taskCreate.status()).toBe(201)
    let task = await taskCreate.json() as TaskResource
    taskID = task.id
    let taskETag = requiredHeader(taskCreate, 'etag')

    const taskCriterionCreate = await agentRequest(
      request,
      'POST',
      `/api/v1/tasks/${task.number}/criteria`,
      writeToken.token,
      {
        ifMatch: taskETag,
        idempotencyKey: `task-criterion-${runTag}`,
        data: {
          criterion: 'The Agent can recover from a human write',
          verification_instructions: 'Create a stale ETag conflict and retry with the latest ETag',
          position: 0,
        },
      },
    )
    expect(taskCriterionCreate.status()).toBe(201)
    const taskCriterion = await taskCriterionCreate.json() as Criterion

    const taskCheck = await agentRequest(
      request,
      'POST',
      `/api/v1/criteria/${taskCriterion.id}/checks`,
      writeToken.token,
      {
        ifMatch: `"${taskCriterion.version}"`,
        idempotencyKey: `task-check-${runTag}`,
        data: {
          criterion_revision: taskCriterion.revision,
          outcome: 'passed',
          evidence: 'The Agent observed and recovered from VERSION_CONFLICT.',
        },
      },
    )
    expect(taskCheck.status()).toBe(201)

    const taskBeforeHuman = await getTask(request, writeToken.token, task.number)
    task = taskBeforeHuman.value
    taskETag = taskBeforeHuman.etag

    const humanUpdate = await humanTaskPatch(
      page,
      task.number,
      taskETag,
      { title: `${taskTitle} reviewed by human` },
    )
    expect(humanUpdate.status()).toBe(200)

    const staleAgentUpdate = await agentRequest(
      request,
      'PATCH',
      `/api/v1/tasks/${task.number}`,
      writeToken.token,
      {
        ifMatch: taskETag,
        idempotencyKey: `stale-agent-${runTag}`,
        data: { status: 'done' },
      },
    )
    expect(staleAgentUpdate.status()).toBe(412)
    const staleProblem = await staleAgentUpdate.json() as Problem
    expect(staleProblem.code).toBe('VERSION_CONFLICT')

    const latestTask = await getTask(request, writeToken.token, task.number)
    expect(staleProblem.current_version).toBe(latestTask.value.version)
    const recoveredTask = await agentRequest(
      request,
      'PATCH',
      `/api/v1/tasks/${task.number}`,
      writeToken.token,
      {
        ifMatch: latestTask.etag,
        idempotencyKey: `recover-agent-${runTag}`,
        requestID: auditRequestID,
        data: { status: 'done' },
      },
    )
    expect(recoveredTask.status()).toBe(200)
    expect((await recoveredTask.json() as TaskResource).status).toBe('done')

    projectDetail = await getProject(request, writeToken.token, project.number)
    projectETag = projectDetail.etag
    const currentMilestone = projectDetail.value.milestones.find((item) => item.id === milestone.id)
    expect(currentMilestone).toBeTruthy()
    milestoneETag = `"${currentMilestone!.version}"`

    const completeMilestone = await agentRequest(
      request,
      'POST',
      `/api/v1/projects/${project.number}/milestones/${milestone.id}/complete`,
      writeToken.token,
      {
        ifMatch: milestoneETag,
        projectIfMatch: projectETag,
        idempotencyKey: `complete-milestone-${runTag}`,
        data: {},
      },
    )
    expect(completeMilestone.status()).toBe(200)
    expect((await completeMilestone.json() as Milestone).status).toBe('completed')

    projectDetail = await getProject(request, writeToken.token, project.number)
    const completeProject = await agentRequest(
      request,
      'POST',
      `/api/v1/projects/${project.number}/complete`,
      writeToken.token,
      {
        ifMatch: projectDetail.etag,
        idempotencyKey: `complete-project-${runTag}`,
        data: {},
      },
    )
    expect(completeProject.status()).toBe(200)
    expect((await completeProject.json() as ProjectDetail).status).toBe('completed')

    await test.step('the owner and Administrator can inspect token provenance', async () => {
      await page.goto('/account/api-tokens')
      await expect(page.getByRole('heading', { name: 'API Token' })).toBeVisible()
      await expect(page.getByRole('heading', { name: '最近 API 活动' })).toBeVisible()
      await expect(page.getByText(writeTokenName).first()).toBeVisible()

      await page.goto('/api-docs')
      await expect(page.getByRole('heading', { name: 'API 文档' })).toBeVisible()
      await expect(page.getByRole('heading', { name: /^Bounty Board Work API/ })).toBeVisible()
      await expect(page.getByText(/确认前，文档仅可阅读/)).toBeVisible()

      await page.goto('/admin/api-audit')
      await expect(page.getByRole('heading', { name: 'API 审计' })).toBeVisible()
      await page.getByLabel('用户').selectOption(adminID)
      await page.getByLabel('Token').selectOption({ label: `${adminName} · ${writeTokenName}` })
      await page.getByLabel('方法').selectOption('PATCH')
      await page.getByLabel('路由').fill('/api/v1/tasks/{number}')
      await page.getByLabel('状态码').fill('200')
      await page.getByLabel('Request ID').fill(auditRequestID)
      await page.getByRole('button', { name: '筛选' }).click()
      await expect(page.getByText(auditRequestID, { exact: true })).toBeVisible()
      await expect(page.getByText('PATCH /api/v1/tasks/{number}', { exact: true })).toBeVisible()
    })

    await revokeToken(page, writeTokenName)
    await revokeToken(page, readTokenName)

    const revoked = await agentRequest(request, 'GET', '/api/v1/me', writeToken.token)
    expect(revoked.status()).toBe(401)
    expect((await revoked.json() as Problem).code).toBe('TOKEN_REVOKED')

    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/account/api-tokens')
    await expect(page.getByRole('heading', { name: 'API Token' })).toBeVisible()
    await expect(page.getByRole('row').filter({ hasText: writeTokenName }).getByText('已撤销')).toBeVisible()
    await page.goto('/admin/api-audit')
    await expect(page.getByRole('heading', { name: 'API 审计' })).toBeVisible()
    await expect(page.getByLabel('Request ID')).toBeVisible()
  } finally {
    await cleanupAgentScenario(taskDbPool, {
      projectID,
      projectName,
      taskID,
      taskTitle,
      tokenNames,
      adminID,
      temporaryAdmin,
      sessionID,
    })
  }
})

async function authenticateDevelopmentUser(page: Page, userID: string): Promise<void> {
  const response = await page.request.post('/api/auth/dev/session', {
    data: { user_id: userID },
  })
  expect(response.status()).toBe(204)
}

async function issueToken(
  page: Page,
  name: string,
  access: 'read' | 'write',
): Promise<IssuedToken> {
  await page.goto('/account/api-tokens')
  await expect(page.getByRole('heading', { name: 'API Token' })).toBeVisible()
  await page.getByLabel('名称').fill(name)
  await page.getByLabel('权限').selectOption(access)
  await page.getByLabel('有效期').selectOption('30')

  const responsePromise = page.waitForResponse(
    (response) => response.url().endsWith('/api/account/tokens') &&
      response.request().method() === 'POST',
  )
  await page.getByRole('button', { name: '创建', exact: true }).click()
  const response = await responsePromise
  expect(response.status()).toBe(201)
  const issued = await response.json() as IssuedToken
  await expect(page.getByLabel('新 API Token')).toHaveValue(issued.token)
  await page.getByRole('button', { name: '关闭完整 Token' }).click()
  await expect(page.getByText(issued.token, { exact: false })).toHaveCount(0)
  await page.reload()
  await expect(page.getByText(issued.token, { exact: false })).toHaveCount(0)
  return issued
}

async function revokeToken(page: Page, name: string): Promise<void> {
  await page.goto('/account/api-tokens')
  const row = page.getByRole('row').filter({ hasText: name })
  page.once('dialog', (dialog) => dialog.accept())
  await row.getByRole('button', { name: '撤销' }).click()
  await expect(row.getByText('已撤销')).toBeVisible()
}

async function agentRequest(
  request: APIRequestContext,
  method: string,
  path: string,
  token: string,
  options: {
    data?: unknown
    ifMatch?: string
    projectIfMatch?: string
    idempotencyKey?: string
    requestID?: string
  } = {},
): Promise<APIResponse> {
  const headers: Record<string, string> = {
    Authorization: `Bearer ${token}`,
    Accept: 'application/json',
  }
  if (options.ifMatch) headers['If-Match'] = options.ifMatch
  if (options.projectIfMatch) headers['X-Project-If-Match'] = options.projectIfMatch
  if (options.idempotencyKey) headers['Idempotency-Key'] = options.idempotencyKey
  if (options.requestID) headers['X-Request-ID'] = options.requestID
  return request.fetch(path, { method, headers, data: options.data })
}

async function getTask(
  request: APIRequestContext,
  token: string,
  number: number,
): Promise<{ value: TaskResource; etag: string }> {
  const response = await agentRequest(request, 'GET', `/api/v1/tasks/${number}`, token)
  expect(response.status()).toBe(200)
  return { value: await response.json() as TaskResource, etag: requiredHeader(response, 'etag') }
}

async function getProject(
  request: APIRequestContext,
  token: string,
  number: number,
): Promise<{ value: ProjectDetail; etag: string }> {
  const response = await agentRequest(request, 'GET', `/api/v1/projects/${number}`, token)
  expect(response.status()).toBe(200)
  return { value: await response.json() as ProjectDetail, etag: requiredHeader(response, 'etag') }
}

async function humanTaskPatch(
  page: Page,
  number: number,
  etag: string,
  patch: Record<string, unknown>,
): Promise<APIResponse> {
  const cookies = await page.context().cookies()
  const csrf = cookies.find((cookie) => cookie.name === 'bb_csrf')?.value
  expect(csrf).toBeTruthy()
  return page.request.patch(`/api/v1/tasks/${number}`, {
    headers: {
      'If-Match': etag,
      'X-CSRF-Token': csrf!,
      Origin: new URL(page.url()).origin,
    },
    data: patch,
  })
}

function requiredHeader(response: APIResponse, name: string): string {
  const value = response.headers()[name.toLowerCase()]
  expect(value, `${name} header`).toBeTruthy()
  return value
}

async function cleanupAgentScenario(
  pool: Pool,
  state: {
    projectID?: string
    projectName: string
    taskID?: string
    taskTitle: string
    tokenNames: string[]
    adminID: string
    temporaryAdmin: boolean
    sessionID?: string
  },
): Promise<void> {
  const taskIDs = state.taskID
    ? [state.taskID]
    : (await pool.query<{ id: string }>('SELECT id FROM tasks WHERE title=$1', [state.taskTitle])).rows.map((row) => row.id)
  const projectIDs = state.projectID
    ? [state.projectID]
    : (await pool.query<{ id: string }>('SELECT id FROM projects WHERE name=$1', [state.projectName])).rows.map((row) => row.id)
  const tokenIDs = (await pool.query<{ id: string }>(
    'SELECT id FROM api_tokens WHERE name=ANY($1::text[])',
    [state.tokenNames],
  )).rows.map((row) => row.id)

  if (taskIDs.length > 0) {
    await pool.query(
      `DELETE FROM acceptance_checks WHERE criterion_id IN (
         SELECT id FROM acceptance_criteria WHERE task_id=ANY($1::uuid[])
       )`,
      [taskIDs],
    )
    await pool.query('DELETE FROM acceptance_criteria WHERE task_id=ANY($1::uuid[])', [taskIDs])
    await pool.query('DELETE FROM task_comments WHERE task_id=ANY($1::uuid[])', [taskIDs])
    await pool.query('DELETE FROM task_activity WHERE task_id=ANY($1::uuid[])', [taskIDs])
    await pool.query('DELETE FROM tasks WHERE id=ANY($1::uuid[])', [taskIDs])
  }
  for (const projectID of projectIDs) {
    await pool.query(
      `DELETE FROM acceptance_checks WHERE criterion_id IN (
         SELECT id FROM acceptance_criteria
         WHERE project_id=$1 OR milestone_id IN (SELECT id FROM milestones WHERE project_id=$1)
       )`,
      [projectID],
    )
    await pool.query(
      `DELETE FROM acceptance_criteria
       WHERE project_id=$1 OR milestone_id IN (SELECT id FROM milestones WHERE project_id=$1)`,
      [projectID],
    )
    await pool.query('DELETE FROM project_activity WHERE project_id=$1', [projectID])
    await pool.query('DELETE FROM milestones WHERE project_id=$1', [projectID])
    await pool.query('DELETE FROM projects WHERE id=$1', [projectID])
  }
  if (tokenIDs.length > 0) {
    await pool.query('DELETE FROM task_activity WHERE api_token_id=ANY($1::uuid[])', [tokenIDs])
    await pool.query('DELETE FROM project_activity WHERE api_token_id=ANY($1::uuid[])', [tokenIDs])
    await pool.query('DELETE FROM business_audit_events WHERE token_id=ANY($1::uuid[])', [tokenIDs])
    await pool.query('DELETE FROM api_request_audit_events WHERE token_id=ANY($1::uuid[])', [tokenIDs])
    await pool.query('DELETE FROM idempotency_records WHERE token_id=ANY($1::uuid[])', [tokenIDs])
    await pool.query('DELETE FROM api_tokens WHERE id=ANY($1::uuid[])', [tokenIDs])
  }
  if (state.sessionID) {
    await pool.query('DELETE FROM identity_audit_events WHERE session_id=$1', [state.sessionID])
    await pool.query('DELETE FROM impersonations WHERE session_id=$1', [state.sessionID])
    await pool.query('DELETE FROM sessions WHERE id=$1', [state.sessionID])
  }
  if (state.temporaryAdmin) {
    await pool.query('DELETE FROM idempotency_records WHERE user_id=$1', [state.adminID])
    await pool.query('DELETE FROM api_request_audit_events WHERE user_id=$1', [state.adminID])
    await pool.query('DELETE FROM business_audit_events WHERE actor_user_id=$1', [state.adminID])
    await pool.query(
      `DELETE FROM identity_audit_events
       WHERE actor_user_id=$1 OR subject_user_id=$1`,
      [state.adminID],
    )
    await pool.query(
      `DELETE FROM impersonations
       WHERE actor_user_id=$1 OR subject_user_id=$1`,
      [state.adminID],
    )
    await pool.query('DELETE FROM sessions WHERE user_id=$1', [state.adminID])
    await pool.query('DELETE FROM users WHERE id=$1', [state.adminID])
  }
}
