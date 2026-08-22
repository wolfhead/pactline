import { execFile } from 'node:child_process'
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { promisify } from 'node:util'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import {
  assertProposalMatchesObservation,
  observeGit,
  runFixedVerification,
} from '../../src/core/verification.js'
import type { ExecutionProposal, ReviewProposal } from '../../src/core/harness-result.js'
import { prepareWorkspace, removeWorkspace } from '../../src/repository/workspace.js'
import type { FleetWorkspace } from '../../src/repository/workspace.js'

const run = promisify(execFile)

describe('Task Workspace and coordinator verification', () => {
  let directory: string
  let origin: string
  let revision: string
  const workspaces: FleetWorkspace[] = []

  beforeEach(async () => {
    directory = await mkdtemp(join(tmpdir(), 'pactline-fleet-repository-test-'))
    origin = join(directory, 'origin.git')
    const seed = join(directory, 'seed')
    await mkdir(seed)
    await run('git', ['init', '--quiet', '--bare', origin])
    await run('git', ['init', '--quiet', seed])
    await writeFile(join(seed, 'README.md'), 'baseline\n')
    await run('git', ['-C', seed, 'add', 'README.md'])
    await run('git', ['-C', seed, '-c', 'user.name=Fleet Test', '-c', 'user.email=fleet@example.invalid', 'commit', '--quiet', '-m', 'baseline'])
    revision = (await run('git', ['-C', seed, 'rev-parse', 'HEAD'])).stdout.trim()
    await run('git', ['-C', seed, 'branch', '-M', 'main'])
    await run('git', ['-C', seed, 'remote', 'add', 'origin', origin])
    await run('git', ['-C', seed, 'push', '--quiet', 'origin', 'main'])
  })

  afterEach(async () => {
    for (const workspace of workspaces) await removeWorkspace(workspace).catch(() => undefined)
    await rm(directory, { recursive: true, force: true })
  })

  async function prepare(mode: 'execution' | 'review'): Promise<FleetWorkspace> {
    const workspace = await prepareWorkspace({
      input: { source: origin, ref: 'refs/heads/main', revision },
      mode, runId: `${mode}-1`, temporaryDirectory: directory,
    })
    workspaces.push(workspace)
    return workspace
  }

  it('uses an isolated write branch and matches diff plus fixed verification to observation', async () => {
    const workspace = await prepare('execution')
    await writeFile(join(workspace.repositoryPath, 'README.md'), 'baseline\nchanged\n')
    const git = await observeGit(workspace.repositoryPath, workspace.baseRevision)
    const commands = await runFixedVerification(workspace.repositoryPath, ['test -f README.md', "grep -q changed README.md"])
    const proposal: ExecutionProposal = {
      schemaVersion: 1, kind: 'execution', runId: 'execution-1', claimId: 'claim-1', taskNumber: 1,
      recommendation: 'complete', summary: 'Changed README.', changedPaths: ['README.md'],
      verification: commands.map(item => ({ command: item.command, outcome: item.outcome, summary: item.summary })),
      criteria: [], limitations: [],
    }
    expect(() => assertProposalMatchesObservation(proposal, { git, commands }, {
      baseHead: revision, allowedPaths: ['README.md'],
    })).not.toThrow()
    expect(() => assertProposalMatchesObservation({ ...proposal, changedPaths: ['other.txt'] }, { git, commands }, {
      baseHead: revision, allowedPaths: ['README.md'],
    })).toThrow('changed paths')
  })

  it('rejects changes outside the path allowlist', async () => {
    const workspace = await prepare('execution')
    await writeFile(join(workspace.repositoryPath, 'outside.txt'), 'unexpected\n')
    const git = await observeGit(workspace.repositoryPath, workspace.baseRevision)
    const commands = await runFixedVerification(workspace.repositoryPath, ['true'])
    const proposal: ExecutionProposal = {
      schemaVersion: 1, kind: 'execution', runId: 'execution-1', claimId: 'claim-1', taskNumber: 1,
      recommendation: 'complete', summary: 'Changed file.', changedPaths: ['outside.txt'],
      verification: [{ command: 'true', outcome: 'passed', summary: 'passed' }], criteria: [], limitations: [],
    }
    expect(() => assertProposalMatchesObservation(proposal, { git, commands }, {
      baseHead: revision, allowedPaths: ['README.md'],
    })).toThrow('outside the allowlist')
  })

  it('uses detached review state and blocks settlement after any mutation', async () => {
    const workspace = await prepare('review')
    const clean = await observeGit(workspace.repositoryPath, workspace.baseRevision)
    const commands = await runFixedVerification(workspace.repositoryPath, ['true'])
    const proposal: ReviewProposal = {
      schemaVersion: 1, kind: 'review', runId: 'review-1', claimId: 'claim-2', taskNumber: 1,
      recommendation: 'accept', summary: 'Accepted.', findings: [],
      verification: [{ command: 'true', outcome: 'passed', summary: 'passed' }], criteria: [], limitations: [],
    }
    expect(() => assertProposalMatchesObservation(proposal, { git: clean, commands }, {
      baseHead: revision, allowedPaths: [],
    })).not.toThrow()
    await writeFile(join(workspace.repositoryPath, 'README.md'), 'mutated\n')
    const mutated = await observeGit(workspace.repositoryPath, workspace.baseRevision)
    expect(() => assertProposalMatchesObservation(proposal, { git: mutated, commands }, {
      baseHead: revision, allowedPaths: [],
    })).toThrow('Review workspace mutation')
  })
})
