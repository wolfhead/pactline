import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { runL2V2HiddenVerification } from '../../src/evaluation/l2-v2-hidden.js'

const roots: string[] = []
afterEach(async () => { await Promise.all(roots.splice(0).map(path => rm(path, { recursive: true, force: true }))) })

describe('L2 v2 hidden verification boundary', () => {
  it('rejects unknown profiles before copying the Agent workspace', async () => {
    const workspace = await mkdtemp(join(tmpdir(), 'fleet-hidden-test-')); roots.push(workspace)
    await expect(runL2V2HiddenVerification(workspace, 'answer-key-does-not-exist')).rejects.toThrow('Unknown')
  })

  it('keeps the hidden asset out of the Agent workspace when the evaluator fails', async () => {
    const workspace = await mkdtemp(join(tmpdir(), 'fleet-hidden-test-')); roots.push(workspace)
    await mkdir(join(workspace, 'internal/domain'), { recursive: true })
    await writeFile(join(workspace, 'go.mod'), 'module github.com/wolfhead/pactline\n\ngo 1.24\n')
    await writeFile(join(workspace, 'internal/domain/task.go'), 'package domain\n')
    await expect(runL2V2HiddenVerification(workspace, 'nullable_schedule_patch')).rejects.toThrow('did not match passed')
    await expect(import('node:fs/promises').then(fs => fs.access(join(workspace, 'internal/domain/fleet_hidden_l2_v2_test.go')))).rejects.toThrow()
  })
})
