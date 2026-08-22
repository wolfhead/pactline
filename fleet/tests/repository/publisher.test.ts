import { execFile } from 'node:child_process'
import { chmod, mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { promisify } from 'node:util'
import { afterEach, describe, expect, it } from 'vitest'
import { commitDelivery, pushDelivery } from '../../src/repository/publisher.js'
import { prepareWorkspace } from '../../src/repository/workspace.js'

const exec = promisify(execFile)
const directories: string[] = []

afterEach(async () => {
  await Promise.all(directories.splice(0).map(path => rm(path, { recursive: true, force: true })))
})

async function remoteRevision(origin: string, ref: string): Promise<string | undefined> {
  const output = (await exec('git', ['ls-remote', '--refs', origin, ref])).stdout.trim()
  return output === '' ? undefined : output.split(/\s+/)[0]
}

describe('Fleet delivery publisher', () => {
  it('fast-forwards one stable Task ref across corrections and protects base from drift', async () => {
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
    const deliveryRef = `refs/heads/${workspace.branch!}`
    const hookMarker = join(directory, 'hook-ran')
    const hook = join(workspace.repositoryPath, '.git', 'hooks', 'pre-commit')
    await writeFile(hook, `#!/bin/sh\nprintf compromised > ${JSON.stringify(hookMarker)}\n`)
    await chmod(hook, 0o700)

    await writeFile(join(workspace.repositoryPath, 'README.md'), 'first delivery\n')
    const first = await commitDelivery(workspace, ['README.md'], 20)
    await pushDelivery(workspace, first, {
      remote: origin, baseRef: 'refs/heads/main', baseRevision, deliveryRef,
    }, undefined)

    await writeFile(join(workspace.repositoryPath, 'README.md'), 'correction\n')
    const correction = await commitDelivery(workspace, ['README.md'], 20)
    await pushDelivery(workspace, correction, {
      remote: origin, baseRef: 'refs/heads/main', baseRevision, deliveryRef,
      priorDeliveryRevision: first.revision,
    }, undefined)

    expect(workspace.branch).toBe('fleet/project-5/task-20')
    expect(await remoteRevision(origin, 'refs/heads/main')).toBe(baseRevision)
    expect(await remoteRevision(origin, deliveryRef)).toBe(correction.revision)
    await expect(readFile(hookMarker, 'utf8')).rejects.toThrow()
    await expect(exec('git', ['-C', workspace.repositoryPath, 'merge-base', '--is-ancestor', first.revision, correction.revision])).resolves.toBeDefined()

    await writeFile(join(seed, 'base-drift.txt'), 'drift\n')
    await exec('git', ['-C', seed, 'add', 'base-drift.txt'])
    await exec('git', ['-C', seed, '-c', 'user.name=Fleet Test', '-c', 'user.email=fleet@example.invalid', 'commit', '--quiet', '-m', 'base drift'])
    await exec('git', ['-C', seed, 'push', '--quiet', 'origin', 'main'])
    await writeFile(join(workspace.repositoryPath, 'README.md'), 'blocked correction\n')
    const blocked = await commitDelivery(workspace, ['README.md'], 20)

    await expect(pushDelivery(workspace, blocked, {
      remote: origin, baseRef: 'refs/heads/main', baseRevision, deliveryRef,
      priorDeliveryRevision: correction.revision,
    }, undefined)).rejects.toThrow('base ref drifted')
    expect(await remoteRevision(origin, deliveryRef)).toBe(correction.revision)
  })
})
