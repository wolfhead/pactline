import { chmod, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { parseFleetConfig } from '../../src/config/load.js'
import { ExecutableWorkDefinitionResolver } from '../../src/work-plugin/executable-plugin.js'
import { serviceConfigYAML } from '../fixtures/service-config.js'

const directories: string[] = []

afterEach(async () => {
  await Promise.all(directories.splice(0).map(path => rm(path, { recursive: true, force: true })))
})

describe('executable Fleet work plugin', () => {
  it('resolves and freezes explicit work authority without forwarding model or Pactline credentials', async () => {
    const directory = await mkdtemp(join(tmpdir(), 'fleet-work-plugin-test-'))
    directories.push(directory)
    const pluginPath = join(directory, 'plugin.mjs')
    await writeFile(pluginPath, `#!/usr/bin/env node
let input = ''
for await (const chunk of process.stdin) input += chunk
const request = JSON.parse(input)
if (process.env.TEST_PACTLINE_TOKEN || process.env.PACTLINE_TOKEN || process.env.DEEPSEEK_API_KEY || process.env.OPENAI_API_KEY) process.exit(9)
const criterion = request.taskPacket.criteria[0]
process.stdout.write(JSON.stringify({ ok: true, data: {
  caseId: 'plugin-test', taskNumber: request.candidate.task.number, taskVersion: request.candidate.task.version,
  base: { source: '${directory}/origin.git', ref: 'refs/heads/main', revision: '${'a'.repeat(40)}' },
  repository: { provider: 'github', host: 'github.com', owner: 'wolfhead', name: 'pactline' },
  allowedPaths: ['fleet/'], verificationCommands: ['npm test'],
  criteria: [{ id: criterion.id, revision: criterion.revision }]
} }))
`)
    await chmod(pluginPath, 0o700)
    const source = `${serviceConfigYAML({
      stateDirectory: join(directory, 'state'), firstWorkspace: join(directory, 'work'),
    }).replace('    credentials:\n', `    workPlugin:\n      executable: ${pluginPath}\n      timeout: 5s\n    credentials:\n`)}`
    const snapshot = parseFleetConfig(source, join(directory, 'fleet.yml'), { knownAdapterIds: ['codex'] })
    const resolver = new ExecutableWorkDefinitionResolver(
      { showTask: async () => ({ data: { task: { number: 21, version: 1 }, criteria: [{ id: 'criterion-1', revision: 2 }] } }) } as never,
      () => snapshot,
      {
        PATH: process.env.PATH,
        TEST_PACTLINE_TOKEN: 'not-real', PACTLINE_TOKEN: 'not-real', DEEPSEEK_API_KEY: 'not-real', OPENAI_API_KEY: 'not-real',
      },
      'plugin-test',
    )
    const fleet = snapshot.config.fleets.first!
    const resolved = await resolver.resolve({
      fleetId: fleet.id, projectNumber: fleet.projectNumber, stage: 'execution',
      task: { id: 'task-21', number: 21, title: 'Test', version: 1, phase: 'ready', activity: 'available' },
    }, fleet, new AbortController().signal)

    expect(resolved?.admission).toMatchObject({
      taskNumber: 21, taskVersion: 1, stage: 'execution',
      frozenPolicy: {
        definition: { allowedPaths: ['fleet/'], verificationCommands: ['npm test'], criteria: [{ id: 'criterion-1', revision: 2 }] },
        route: { adapter: 'codex', model: 'gpt-5.6-sol' },
        plugin: { executable: pluginPath, timeoutMs: 5_000 },
      },
    })
  })
})
