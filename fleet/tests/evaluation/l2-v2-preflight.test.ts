import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { l2V2EffectInventory, preflightL2V2Repository, type L2V2CommandRunner } from '../../src/evaluation/l2-v2-preflight.js'
import { loadL2V2Spec } from '../../src/evaluation/l2-v2-spec.js'

const fleetRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../..')

describe('Fleet L2 v2 repository preflight', () => {
  it('verifies frozen refs and returns the complete bounded effect inventory', async () => {
    const spec = await loadL2V2Spec(resolve(fleetRoot, 'evaluation/cases/l2-v2.json'))
    const run: L2V2CommandRunner = async (executable, args) => {
      const refs = new Map<string, string>([[spec.repository.baseRef, spec.repository.baseRevision]])
      for (const item of spec.cases) {
        refs.set(item.seedRef, item.baseRevision)
        if (item.candidate !== undefined) refs.set(item.candidate.seedRef, item.candidate.revision)
      }
      if (args[0] === 'repo') return { stdout: '{"nameWithOwner":"wolfhead/pactline","viewerPermission":"ADMIN"}', stderr: '' }
      if (args[1]?.includes('matching-refs')) return { stdout: '[]', stderr: '' }
      const requested = `refs/${args[1]?.split('/git/ref/')[1] ?? ''}`
      return { stdout: JSON.stringify({ ref: requested, object: { sha: refs.get(requested) } }), stderr: '' }
    }
    await expect(preflightL2V2Repository(spec, run)).resolves.toMatchObject({
      viewerPermission: 'ADMIN', verifiedSeedRefs: 9, targetNamespaceEmpty: true,
      inventory: { projectCreates: 1, taskCreates: 6, criterionCreates: 12, maximumDeliveryDraftPullRequests: 5 },
    })
    expect(l2V2EffectInventory(spec).staticRefCreates).toHaveLength(9)
    expect(l2V2EffectInventory(spec).seededDraftPullRequests).toHaveLength(2)
  })

  it('stops on an occupied v2 namespace', async () => {
    const spec = await loadL2V2Spec(resolve(fleetRoot, 'evaluation/cases/l2-v2.json'))
    const run: L2V2CommandRunner = async (executable, args) => {
      const refs = new Map<string, string>([[spec.repository.baseRef, spec.repository.baseRevision]])
      for (const item of spec.cases) {
        refs.set(item.seedRef, item.baseRevision); if (item.candidate !== undefined) refs.set(item.candidate.seedRef, item.candidate.revision)
      }
      if (args[0] === 'repo') return { stdout: '{"nameWithOwner":"wolfhead/pactline","viewerPermission":"ADMIN"}', stderr: '' }
      if (args[1]?.includes('matching-refs')) return { stdout: JSON.stringify([{ ref: 'refs/heads/fleet-eval/l2-v2/source', object: { sha: 'a'.repeat(40) } }]), stderr: '' }
      const requested = `refs/${args[1]?.split('/git/ref/')[1] ?? ''}`
      return { stdout: JSON.stringify({ ref: requested, object: { sha: refs.get(requested) } }), stderr: '' }
    }
    await expect(preflightL2V2Repository(spec, run)).rejects.toThrow('target namespace is not empty')
  })
})
