import { execFile } from 'node:child_process'
import { chmod, mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { promisify } from 'node:util'
import { afterEach, describe, expect, it } from 'vitest'
import { commitDelivery, pushDelivery } from '../../src/repository/publisher.js'
import { prepareWorkspace } from '../../src/repository/workspace.js'
import type { FleetWorkspace } from '../../src/repository/workspace.js'

const exec = promisify(execFile)
const directories: string[] = []

afterEach(async () => {
  await Promise.all(directories.splice(0).map(path => rm(path, { recursive: true, force: true })))
})

async function remoteRevision(origin: string, ref: string): Promise<string | undefined> {
  const output = (await exec('git', ['ls-remote', '--refs', origin, ref])).stdout.trim()
  return output === '' ? undefined : output.split(/\s+/)[0]
}

async function setup(): Promise<{
  readonly origin: string
  readonly seed: string
  readonly baseRevision: string
  readonly workspace: FleetWorkspace
  readonly deliveryRef: string
}> {
  const directory = await mkdtemp(join(tmpdir(), 'fleet-publisher-'))
  directories.push(directory)
  const origin = join(directory, 'origin.git')
  const seed = join(directory, 'seed')
  await mkdir(seed)
  await exec('git', ['init', '--quiet', '--bare', origin])
  await exec('git', ['init', '--quiet', seed])
  await writeFile(join(seed, 'README.md'), 'base\n')
  await exec('git', ['-C', seed, 'add', 'README.md'])
  await exec('git', ['-C', seed, '-c', 'user.name=Fleet Test', '-c', 'user.email=fleet@example.invalid', 'commit', '--quiet', '-m', 'base'])
  const baseRevision = (await exec('git', ['-C', seed, 'rev-parse', 'HEAD'])).stdout.trim()
  await exec('git', ['-C', seed, 'branch', '-M', 'main'])
  await exec('git', ['-C', seed, 'remote', 'add', 'origin', origin])
  await exec('git', ['-C', seed, 'push', '--quiet', 'origin', 'main'])
  const workspace = await prepareWorkspace({
    input: { source: origin, ref: 'refs/heads/main', revision: baseRevision },
    mode: 'execution', runId: 'first', temporaryDirectory: directory,
    taskIdentity: { projectNumber: 5, taskNumber: 20 },
  })
  return { origin, seed, baseRevision, workspace, deliveryRef: `refs/heads/${workspace.branch!}` }
}

async function commit(workspace: FleetWorkspace, contents: string) {
  await writeFile(join(workspace.repositoryPath, 'README.md'), contents)
  return await commitDelivery(workspace, ['README.md'], 20)
}

describe('Fleet delivery publisher', () => {
  it('fast-forwards the stable Task ref after the base ref advances without changing the immutable Workspace base', async () => {
    const { origin, seed, baseRevision, workspace, deliveryRef } = await setup()
    const hookMarker = join(workspace.root, 'hook-ran')
    const hook = join(workspace.repositoryPath, '.git', 'hooks', 'pre-commit')
    await writeFile(hook, `#!/bin/sh\nprintf compromised > ${JSON.stringify(hookMarker)}\n`)
    await chmod(hook, 0o700)
    const first = await commit(workspace, 'first delivery\n')
    await pushDelivery(workspace, first, {
      remote: origin, baseRef: 'refs/heads/main', baseRevision, deliveryRef,
    }, undefined)

    await writeFile(join(seed, 'base-progress.txt'), 'upstream progress\n')
    await exec('git', ['-C', seed, 'add', 'base-progress.txt'])
    await exec('git', ['-C', seed, '-c', 'user.name=Fleet Test', '-c', 'user.email=fleet@example.invalid', 'commit', '--quiet', '-m', 'base progress'])
    const advancedBaseRevision = (await exec('git', ['-C', seed, 'rev-parse', 'HEAD'])).stdout.trim()
    await exec('git', ['-C', seed, 'push', '--quiet', 'origin', 'main'])

    const correction = await commit(workspace, 'correction\n')
    await pushDelivery(workspace, correction, {
      remote: origin, baseRef: 'refs/heads/main', baseRevision, deliveryRef,
      priorDeliveryRevision: first.revision,
    }, undefined)

    expect(workspace.baseRevision).toBe(baseRevision)
    expect(advancedBaseRevision).not.toBe(baseRevision)
    expect(await remoteRevision(origin, 'refs/heads/main')).toBe(advancedBaseRevision)
    expect(await remoteRevision(origin, deliveryRef)).toBe(correction.revision)
    await expect(readFile(hookMarker, 'utf8')).rejects.toThrow()
    await expect(exec('git', ['-C', workspace.repositoryPath, 'merge-base', '--is-ancestor', first.revision, correction.revision])).resolves.toBeDefined()
  })

  it('rejects a missing base ref without creating the base or delivery ref', async () => {
    const { origin, seed, baseRevision, workspace, deliveryRef } = await setup()
    const delivery = await commit(workspace, 'delivery\n')
    await exec('git', ['-C', seed, 'push', '--quiet', 'origin', ':refs/heads/main'])

    await expect(pushDelivery(workspace, delivery, {
      remote: origin, baseRef: 'refs/heads/main', baseRevision, deliveryRef,
    }, undefined)).rejects.toThrow('base ref is missing before delivery push')
    expect(await remoteRevision(origin, 'refs/heads/main')).toBeUndefined()
    expect(await remoteRevision(origin, deliveryRef)).toBeUndefined()
  })

  it('rejects repository and stable Task branch identity mismatches', async () => {
    const { origin, baseRevision, workspace, deliveryRef } = await setup()
    const delivery = await commit(workspace, 'delivery\n')

    await expect(pushDelivery(workspace, delivery, {
      remote: `${origin}-other`, baseRef: 'refs/heads/main', baseRevision, deliveryRef,
    }, undefined)).rejects.toThrow('authority does not match the Task Workspace repository revision')
    await expect(pushDelivery(workspace, delivery, {
      remote: origin, baseRef: 'refs/heads/main', baseRevision,
      deliveryRef: 'refs/heads/fleet/project-5/task-999',
    }, undefined)).rejects.toThrow('delivery ref must be the stable non-base Task branch')
    expect(await remoteRevision(origin, deliveryRef)).toBeUndefined()
  })

  it('rejects a rewritten remote delivery ref before push', async () => {
    const { origin, seed, baseRevision, workspace, deliveryRef } = await setup()
    const first = await commit(workspace, 'first delivery\n')
    await pushDelivery(workspace, first, {
      remote: origin, baseRef: 'refs/heads/main', baseRevision, deliveryRef,
    }, undefined)
    await exec('git', ['-C', seed, 'push', '--quiet', '--force', 'origin', `${baseRevision}:${deliveryRef}`])
    const correction = await commit(workspace, 'correction\n')

    await expect(pushDelivery(workspace, correction, {
      remote: origin, baseRef: 'refs/heads/main', baseRevision, deliveryRef,
      priorDeliveryRevision: first.revision,
    }, undefined)).rejects.toThrow('delivery ref drifted before delivery push')
    expect(await remoteRevision(origin, deliveryRef)).toBe(baseRevision)
  })

  it('preserves Git non-fast-forward rejection for a divergent local delivery', async () => {
    const { origin, baseRevision, workspace, deliveryRef } = await setup()
    const first = await commit(workspace, 'first delivery\n')
    await pushDelivery(workspace, first, {
      remote: origin, baseRef: 'refs/heads/main', baseRevision, deliveryRef,
    }, undefined)
    await exec('git', ['-C', workspace.repositoryPath, 'reset', '--hard', '--quiet', baseRevision])
    const divergent = await commit(workspace, 'divergent delivery\n')

    await expect(pushDelivery(workspace, divergent, {
      remote: origin, baseRef: 'refs/heads/main', baseRevision, deliveryRef,
      priorDeliveryRevision: first.revision,
    }, undefined)).rejects.toThrow('Fleet Git command failed')
    expect(await remoteRevision(origin, deliveryRef)).toBe(first.revision)
  })
})
