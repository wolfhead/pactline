import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { parseFleetConfig } from '../../src/config/load.js'
import type { FleetConfigSnapshot } from '../../src/config/types.js'
import type { PactlineTaskSummary } from '../../src/pactline/types.js'
import { FleetRegistry } from '../../src/registry/fleet-registry.js'
import type { ScheduledRunExecutor, WorkDefinitionResolver } from '../../src/scheduler/candidate.js'
import { FairFleetScheduler } from '../../src/scheduler/fair-scheduler.js'
import { NullFleetLogger } from '../../src/service/logger.js'
import { ensurePrivateDirectory } from '../../src/service/state-directory.js'
import { serviceConfigYAML } from '../fixtures/service-config.js'

const directories: string[] = []

afterEach(async () => {
  await Promise.all(directories.splice(0).map(path => rm(path, { recursive: true, force: true })))
})

function task(number: number): PactlineTaskSummary {
  return { id: `task-${String(number)}`, number, title: `Task ${String(number)}`, version: 1, phase: 'ready', activity: 'available' }
}

async function fixture(options: { globalConcurrency?: number } = {}): Promise<{
  snapshot: FleetConfigSnapshot
  registry: FleetRegistry
}> {
  const directory = await mkdtemp(join(tmpdir(), 'fleet-scheduler-test-'))
  directories.push(directory)
  const state = await ensurePrivateDirectory(join(directory, 'state'))
  let source = serviceConfigYAML({
    stateDirectory: state,
    firstWorkspace: join(directory, 'work', 'first'),
    secondWorkspace: join(directory, 'work', 'second'),
  })
  source = source.replace('maxConcurrentRuns: 2', `maxConcurrentRuns: ${String(options.globalConcurrency ?? 1)}`)
  const snapshot = parseFleetConfig(source, join(directory, 'fleet.yml'), { knownAdapterIds: ['codex'] })
  const registry = await FleetRegistry.open(join(state, 'fleet.sqlite3'))
  registry.recordConfiguration(snapshot)
  return { snapshot, registry }
}

const resolver: WorkDefinitionResolver = {
  resolve(candidate) {
    return Promise.resolve({ admission: {
      taskNumber: candidate.task.number,
      taskVersion: candidate.task.version,
      stage: candidate.stage,
      frozenPolicy: { adapter: 'codex', repositoryRevision: 'a'.repeat(40) },
    } })
  },
}

describe('FairFleetScheduler', () => {
  it('round-robins Fleets with deterministic in-Fleet ordering', async () => {
    const { snapshot, registry } = await fixture()
    const queues = new Map<number, PactlineTaskSummary[]>([
      [5, [task(9), task(3)]],
      [12, [task(4)]],
    ])
    const order: string[] = []
    const executor: ScheduledRunExecutor = {
      async execute(run, candidate) {
        order.push(`${candidate.fleetId}:${String(candidate.task.number)}`)
        queues.set(candidate.projectNumber, (queues.get(candidate.projectNumber) ?? []).filter(item => item.number !== candidate.task.number))
        registry.transitionRun(run.runId, 'admitted', 'released', { disposition: 'test' })
        return { kind: 'released', reason: 'test' }
      },
    }
    const scheduler = new FairFleetScheduler({
      snapshot: () => snapshot,
      registry,
      resolver,
      executor,
      logger: new NullFleetLogger(),
      sessionId: 'scheduler-test',
      discovery: {
        listTasks(stage, projectNumber) {
          return Promise.resolve({ data: stage === 'execution' ? [...(queues.get(projectNumber) ?? [])] : [] })
        },
      },
      random: () => 0.5,
    })

    await scheduler.cycle()
    await scheduler.cycle()
    await scheduler.cycle()
    expect(order).toEqual(['first:3', 'second:4', 'first:9'])
    registry.close()
  })

  it('routes available in-progress execution work as a correction stage', async () => {
    const { snapshot, registry } = await fixture()
    let observedStage: string | undefined
    const correction = { ...task(22), phase: 'in_progress' }
    const scheduler = new FairFleetScheduler({
      snapshot: () => snapshot,
      registry,
      resolver,
      logger: new NullFleetLogger(),
      sessionId: 'scheduler-test',
      discovery: { listTasks: async (stage, projectNumber) => ({
        data: stage === 'execution' && projectNumber === 5 ? [correction] : [],
      }) },
      executor: {
        async execute(run, candidate) {
          observedStage = candidate.stage
          registry.transitionRun(run.runId, 'admitted', 'released', { disposition: 'test' })
          return { kind: 'released', reason: 'test' }
        },
      },
    })
    await scheduler.cycle()
    expect(observedStage).toBe('correction')
    registry.close()
  })

  it('enforces the global and per-Fleet active Run budgets', async () => {
    const { snapshot, registry } = await fixture()
    let finish!: () => void
    const gate = new Promise<void>(resolvePromise => { finish = resolvePromise })
    const started: string[] = []
    const scheduler = new FairFleetScheduler({
      snapshot: () => snapshot,
      registry,
      resolver,
      logger: new NullFleetLogger(),
      sessionId: 'scheduler-test',
      discovery: {
        listTasks(stage, projectNumber) {
          if (stage === 'review') return Promise.resolve({ data: [] })
          return Promise.resolve({ data: projectNumber === 5 ? [task(1), task(2)] : [task(3)] })
        },
      },
      executor: {
        async execute(run, candidate) {
          started.push(`${candidate.fleetId}:${String(candidate.task.number)}`)
          await gate
          registry.transitionRun(run.runId, 'admitted', 'released', { disposition: 'test' })
          return { kind: 'released', reason: 'test' }
        },
      },
    })
    const cycle = scheduler.cycle()
    for (let attempt = 0; attempt < 20 && scheduler.activeRunCount === 0; attempt += 1) {
      await new Promise(resolvePromise => setTimeout(resolvePromise, 1))
    }
    expect(scheduler.activeRunCount).toBe(1)
    expect(started).toEqual(['first:1'])
    finish()
    await expect(cycle).resolves.toMatchObject({ admitted: 1 })
    registry.close()
  })

  it('backs off one failing Fleet without blocking another Fleet', async () => {
    const { snapshot, registry } = await fixture()
    let now = new Date('2026-08-16T10:00:00Z')
    let firstCalls = 0
    const order: string[] = []
    const scheduler = new FairFleetScheduler({
      snapshot: () => snapshot,
      registry,
      resolver,
      logger: new NullFleetLogger(),
      sessionId: 'scheduler-test',
      now: () => now,
      random: () => 0.5,
      discovery: {
        listTasks(stage, projectNumber) {
          if (projectNumber === 5) {
            firstCalls += 1
            return Promise.reject(new Error('temporary Pactline failure'))
          }
          return Promise.resolve({ data: stage === 'execution' ? [task(7)] : [] })
        },
      },
      executor: {
        async execute(run, candidate) {
          order.push(candidate.fleetId)
          registry.transitionRun(run.runId, 'admitted', 'released', { disposition: 'test' })
          return { kind: 'released', reason: 'test' }
        },
      },
    })
    await scheduler.cycle()
    await scheduler.cycle()
    expect(firstCalls).toBe(2) // execution + review fail together on the first attempt only
    expect(order).toEqual(['second', 'second'])
    now = new Date(now.getTime() + 1_001)
    await scheduler.cycle()
    expect(firstCalls).toBe(4)
    registry.close()
  })

  it('stops admission during drain and reports a bounded drain deadline', async () => {
    const { snapshot, registry } = await fixture()
    let finish!: () => void
    const gate = new Promise<void>(resolvePromise => { finish = resolvePromise })
    const scheduler = new FairFleetScheduler({
      snapshot: () => snapshot,
      registry,
      resolver,
      logger: new NullFleetLogger(),
      sessionId: 'scheduler-test',
      discovery: {
        listTasks: async stage => ({ data: stage === 'execution' ? [task(11)] : [] }),
      },
      executor: {
        async execute(run) {
          await gate
          registry.transitionRun(run.runId, 'admitted', 'released', { disposition: 'drained' })
          return { kind: 'released', reason: 'drained' }
        },
      },
    })
    const cycle = scheduler.cycle()
    for (let attempt = 0; attempt < 20 && scheduler.activeRunCount === 0; attempt += 1) {
      await new Promise(resolvePromise => setTimeout(resolvePromise, 1))
    }
    scheduler.beginDrain(new Error('shutdown'))
    await expect(scheduler.waitForActive(5)).resolves.toBe(false)
    await expect(scheduler.cycle()).resolves.toMatchObject({ admitted: 0, discovered: 0 })
    finish()
    await cycle
    await expect(scheduler.waitForActive(50)).resolves.toBe(true)
    registry.close()
  })

  it('treats two distributed services competing for one Task as one winner and one normal contention', async () => {
    const first = await fixture()
    const second = await fixture()
    let claimed = false
    const executor = (registry: FleetRegistry): ScheduledRunExecutor => ({
      async execute(run) {
        const won = !claimed
        claimed = true
        registry.transitionRun(run.runId, 'admitted', 'released', { disposition: won ? 'winner_fixture' : 'claim_contention' })
        return won ? { kind: 'released', reason: 'winner_fixture' } : { kind: 'contention', reason: 'ACTIVE_CLAIM' }
      },
    })
    const scheduler = (item: typeof first) => new FairFleetScheduler({
      snapshot: () => item.snapshot,
      registry: item.registry,
      resolver,
      executor: executor(item.registry),
      logger: new NullFleetLogger(),
      sessionId: `service-${item.registry.serviceId}`,
      random: () => 0.5,
      discovery: {
        listTasks: async (stage, projectNumber) => ({
          data: stage === 'execution' && projectNumber === 5 ? [task(41)] : [],
        }),
      },
    })
    const [firstResult, secondResult] = await Promise.all([scheduler(first).cycle(), scheduler(second).cycle()])
    expect(firstResult.contentions + secondResult.contentions).toBe(1)
    expect(firstResult.admitted + secondResult.admitted).toBe(2)
    expect(first.registry.listNonTerminalRuns()).toEqual([])
    expect(second.registry.listNonTerminalRuns()).toEqual([])
    first.registry.close()
    second.registry.close()
  })

  it('replenishes a free slot while another Fleet Run remains active', async () => {
    const { snapshot, registry } = await fixture({ globalConcurrency: 2 })
    const queues = new Map<number, PactlineTaskSummary[]>([[5, [task(1), task(2)]], [12, [task(3)]]])
    let releaseSecond!: () => void
    const secondGate = new Promise<void>(resolvePromise => { releaseSecond = resolvePromise })
    const started: number[] = []
    const scheduler = new FairFleetScheduler({
      snapshot: () => snapshot,
      registry,
      resolver,
      logger: new NullFleetLogger(),
      sessionId: 'scheduler-test',
      discovery: { listTasks: async (stage, projectNumber) => ({
        data: stage === 'execution' ? [...(queues.get(projectNumber) ?? [])] : [],
      }) },
      executor: {
        async execute(run, candidate) {
          started.push(candidate.task.number)
          queues.set(candidate.projectNumber, (queues.get(candidate.projectNumber) ?? []).filter(item => item.number !== candidate.task.number))
          if (candidate.task.number === 3) await secondGate
          registry.transitionRun(run.runId, 'admitted', 'released', { disposition: 'test' })
          return { kind: 'released', reason: 'test' }
        },
      },
    })
    await scheduler.cycle(undefined, false)
    for (let attempt = 0; attempt < 20 && registry.hasNonTerminalRun('first', 1, 'execution'); attempt += 1) {
      await new Promise(resolvePromise => setTimeout(resolvePromise, 1))
    }
    expect(scheduler.activeRunCount).toBe(1)
    await scheduler.cycle(undefined, false)
    expect(started).toEqual([1, 3, 2])
    for (let attempt = 0; attempt < 20 && scheduler.activeRunCount > 1; attempt += 1) {
      await new Promise(resolvePromise => setTimeout(resolvePromise, 1))
    }
    expect(scheduler.activeRunCount).toBe(1)
    releaseSecond()
    await expect(scheduler.waitForActive(100)).resolves.toBe(true)
    registry.close()
  })
})
