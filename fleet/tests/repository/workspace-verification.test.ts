import { execFile } from 'node:child_process'
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { promisify } from 'node:util'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import {
  assertProposalMatchesObservation,
  observeGit,
  runFixedVerification,
  VerificationMismatchError,
} from '../../src/core/verification.js'
import type { ExecutionProposal, ReviewProposal } from '../../src/core/harness-result.js'
import { prepareWorkspace, removeWorkspace } from '../../src/repository/workspace.js'
import type { FleetWorkspace } from '../../src/repository/workspace.js'

const run = promisify(execFile)

describe('disposable workspace and coordinator verification', () => {
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

  it('reopens the deterministic Workspace for one Task without sharing it with another Task', async () => {
    const first = await prepareWorkspace({
      input: { source: origin, ref: 'refs/heads/main', revision },
      mode: 'execution', runId: 'task-19-first', temporaryDirectory: directory,
      taskIdentity: { projectNumber: 5, taskNumber: 19 },
    })
    workspaces.push(first)
    await writeFile(join(first.repositoryPath, 'continuity.txt'), 'retained\n')

    const resumed = await prepareWorkspace({
      input: { source: origin, ref: 'refs/heads/main', revision },
      mode: 'execution', runId: 'task-19-second', temporaryDirectory: directory,
      taskIdentity: { projectNumber: 5, taskNumber: 19 },
    })
    const other = await prepareWorkspace({
      input: { source: origin, ref: 'refs/heads/main', revision },
      mode: 'execution', runId: 'task-20-first', temporaryDirectory: directory,
      taskIdentity: { projectNumber: 5, taskNumber: 20 },
    })
    workspaces.push(other)

    expect(resumed.root).toBe(first.root)
    await expect(readFile(join(resumed.repositoryPath, 'continuity.txt'), 'utf8')).resolves.toBe('retained\n')
    expect(other.root).not.toBe(first.root)
  })

  it('prepares a review-first Task Workspace with both base and candidate revisions', async () => {
    const candidateSeed = join(directory, 'candidate-seed')
    await run('git', ['clone', '--quiet', origin, candidateSeed])
    await writeFile(join(candidateSeed, 'README.md'), 'candidate\n')
    await run('git', ['-C', candidateSeed, 'add', 'README.md'])
    await run('git', ['-C', candidateSeed, '-c', 'user.name=Fleet Test', '-c', 'user.email=fleet@example.invalid', 'commit', '--quiet', '-m', 'candidate'])
    const candidateRevision = (await run('git', ['-C', candidateSeed, 'rev-parse', 'HEAD'])).stdout.trim()
    await run('git', ['-C', candidateSeed, 'push', '--quiet', 'origin', 'HEAD:refs/heads/candidate'])

    const review = await prepareWorkspace({
      input: { source: origin, ref: 'refs/heads/main', revision },
      candidate: { source: origin, ref: 'refs/heads/candidate', revision: candidateRevision },
      mode: 'execution', runId: 'task-21-review-first', temporaryDirectory: directory,
      taskIdentity: { projectNumber: 5, taskNumber: 21 },
    })
    workspaces.push(review)

    expect((await run('git', ['-C', review.repositoryPath, 'rev-parse', 'HEAD'])).stdout.trim()).toBe(candidateRevision)
    await expect(run('git', ['-C', review.repositoryPath, 'cat-file', '-e', `${revision}^{commit}`])).resolves.toBeDefined()
    expect((await run('git', ['-C', review.repositoryPath, 'diff', '--name-only', `${revision}..${candidateRevision}`])).stdout.trim()).toBe('README.md')
    expect(review.baseRevision).toBe(revision)
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

  it('reports structured Harness and Fleet command differences', async () => {
    const workspace = await prepare('execution')
    const git = await observeGit(workspace.repositoryPath, workspace.baseRevision)
    const commands = await runFixedVerification(workspace.repositoryPath, ['false'])
    const proposal: ExecutionProposal = {
      schemaVersion: 1, kind: 'execution', runId: 'execution-mismatch', claimId: 'claim-mismatch', taskNumber: 1,
      recommendation: 'complete', summary: 'Reported success.', changedPaths: [],
      verification: [{ command: 'false', outcome: 'passed', summary: 'passed' }], criteria: [], limitations: [],
    }

    let mismatch: VerificationMismatchError | undefined
    try {
      assertProposalMatchesObservation(proposal, { git, commands }, {
        baseHead: revision, allowedPaths: ['README.md'],
      })
    } catch (error) {
      if (error instanceof VerificationMismatchError) mismatch = error
      else throw error
    }
    expect(mismatch?.details).toEqual([expect.objectContaining({
      category: 'test_failure', command: 'false',
      harness: expect.objectContaining({ outcome: 'passed' }),
      fleet: expect.objectContaining({ outcome: 'failed', exitCode: 1 }),
    })])

    expect(() => assertProposalMatchesObservation({
      ...proposal, verification: [],
    }, { git, commands }, { baseHead: revision, allowedPaths: ['README.md'] })).toThrowError(expect.objectContaining({
      details: [expect.objectContaining({ category: 'parse_mismatch', command: 'false' })],
    }))

    expect(() => assertProposalMatchesObservation({
      ...proposal, changedPaths: ['reported-only.txt'],
      verification: [{ command: 'false', outcome: 'failed', summary: 'failed' }],
    }, { git, commands }, { baseHead: revision, allowedPaths: ['README.md'] })).toThrowError(expect.objectContaining({
      details: [expect.objectContaining({
        category: 'changed_paths_mismatch', harnessChangedPaths: ['reported-only.txt'], fleetChangedPaths: [],
      })],
    }))

    const manyPaths = Array.from({ length: 70 }, (_, index) => `generated/${String(index).padStart(2, '0')}.txt`)
    expect(() => assertProposalMatchesObservation({
      ...proposal, changedPaths: manyPaths,
      verification: [{ command: 'false', outcome: 'failed', summary: 'failed' }],
    }, { git, commands }, { baseHead: revision, allowedPaths: ['README.md'] })).toThrowError(expect.objectContaining({
      details: [expect.objectContaining({
        category: 'changed_paths_mismatch',
        harnessChangedPaths: manyPaths.slice(0, 64), harnessChangedPathsOmitted: 6,
      })],
    }))
  })

  it('classifies unavailable commands, timeouts, and missing prerequisites', async () => {
    await writeFile(join(directory, 'not-executable'), '#!/bin/sh\nexit 0\n', { mode: 0o644 })
    const unavailable = await runFixedVerification(directory, ['fleet-command-that-does-not-exist'])
    const notExecutable = await runFixedVerification(directory, ['./not-executable'])
    const timedOut = await runFixedVerification(directory, ['sleep 1'], { timeoutMs: 10 })
    const missing = await runFixedVerification(directory, ['test -f required.fixture'])

    expect(unavailable[0]).toMatchObject({ failureKind: 'command_unavailable', exitCode: 127 })
    expect(notExecutable[0]).toMatchObject({ failureKind: 'command_unavailable', exitCode: 126 })
    expect(timedOut[0]).toMatchObject({ failureKind: 'timeout', exitCode: null })
    expect(missing[0]).toMatchObject({ failureKind: 'missing_prerequisite', exitCode: 1 })
  })

  it('bounds and redacts verification output before retaining it', async () => {
    const [observation] = await runFixedVerification(directory, [
      `printf 'token=visible-secret sk-1234567890 '; yes x | head -c 10000; exit 1`,
    ], { maxOutputBytes: 32 * 1024 })

    expect(Buffer.byteLength(observation!.summary)).toBeLessThanOrEqual(2_048)
    expect(observation!.summary).toContain('[REDACTED]')
    expect(observation!.summary).not.toContain('visible-secret')
    expect(observation!.summary).not.toContain('sk-1234567890')

    const [boundary] = await runFixedVerification(directory, [
      `printf '%s' '${'x'.repeat(2_040)} token=boundary-secret'; exit 1`,
    ], { maxOutputBytes: 32 * 1024 })
    expect(Buffer.byteLength(boundary!.summary)).toBeLessThanOrEqual(2_048)
    expect(boundary!.summary).not.toContain('boundary-secret')
    expect(boundary!.summary).not.toContain('token=boundary')
  })

  it('rejects execution Harness commits so only Fleet owns delivery history', async () => {
    const workspace = await prepare('execution')
    await writeFile(join(workspace.repositoryPath, 'README.md'), 'committed by harness\n')
    await run('git', ['-C', workspace.repositoryPath, 'add', 'README.md'])
    await run('git', ['-C', workspace.repositoryPath, '-c', 'user.name=Harness', '-c', 'user.email=harness@example.invalid', 'commit', '--quiet', '-m', 'unauthorized'])
    const git = await observeGit(workspace.repositoryPath, workspace.baseRevision)
    const commands = await runFixedVerification(workspace.repositoryPath, ['true'])
    const proposal: ExecutionProposal = {
      schemaVersion: 1, kind: 'execution', runId: 'execution-commit', claimId: 'claim-commit', taskNumber: 1,
      recommendation: 'complete', summary: 'Committed directly.', changedPaths: ['README.md'],
      verification: [{ command: 'true', outcome: 'passed', summary: 'passed' }], criteria: [], limitations: [],
    }

    expect(() => assertProposalMatchesObservation(proposal, { git, commands }, {
      baseHead: revision, allowedPaths: ['README.md'],
    })).toThrow('leave commits to Fleet delivery authority')
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
