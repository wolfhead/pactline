import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { loadL2V2Spec, parseL2V2Spec } from '../../src/evaluation/l2-v2-spec.js'

const fleetRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../..')

describe('Fleet L2 v2 specification', () => {
  it('loads the frozen six-case Codex cohort', async () => {
    const spec = await loadL2V2Spec(resolve(fleetRoot, 'evaluation/cases/l2-v2.json'))
    expect(spec.repository).toMatchObject({
      baseRevision: 'abc3599c863fbc2041e0cd463776d3d8ca8c7fb1', branchPrefix: 'fleet-eval/l2-v2/',
    })
    expect(spec.cases).toHaveLength(6)
    expect(spec.cases.map(item => item.expectedPath)).toEqual([
      'direct_accept', 'direct_accept', 'direct_accept',
      'changes_correction_accept', 'clean_review_accept', 'resolution_accept',
    ])
  })

  it('rejects cohort size, candidate-path, and resolution inconsistencies', async () => {
    const source = await import('node:fs/promises').then(async fs => JSON.parse(await fs.readFile(resolve(fleetRoot, 'evaluation/cases/l2-v2.json'), 'utf8')) as Record<string, unknown>)
    expect(() => parseL2V2Spec({ ...source, cases: [] })).toThrow('exactly six')
    const cases = structuredClone(source.cases) as Record<string, unknown>[]
    delete cases[3]?.candidate
    expect(() => parseL2V2Spec({ ...source, cases })).toThrow('candidate presence')
    const resolutionCases = structuredClone(source.cases) as Record<string, unknown>[]
    delete resolutionCases[5]?.resolution
    expect(() => parseL2V2Spec({ ...source, cases: resolutionCases })).toThrow('resolution policy')
  })
})
