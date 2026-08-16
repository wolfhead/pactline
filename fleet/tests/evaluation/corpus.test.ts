import { chmod, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { loadFleetEvaluationCorpus, parseFleetEvaluationCorpus } from '../../src/evaluation/corpus.js'

function corpus() {
  return {
    schemaVersion: 1,
    projectNumber: 3,
    cases: [{
      caseId: 'M1-01', taskNumber: 40, taskVersion: 2,
      base: { source: 'https://github.com/wolfhead/pactline', ref: 'refs/heads/main', revision: 'a'.repeat(40) },
      repository: { provider: 'github', host: 'github.com', owner: 'wolfhead', name: 'pactline' },
      allowedPaths: ['fleet/'], verificationCommands: ['npm test'],
      criteria: [{ id: 'criterion-1', revision: 1 }],
      candidate: {
        ref: 'refs/heads/fleet/test', branch: 'fleet/test', revision: 'b'.repeat(40),
        codeChangeUrl: 'https://github.com/wolfhead/pactline/pull/100',
      },
    }],
  }
}

describe('Fleet evaluation corpus', () => {
  const directories: string[] = []
  afterEach(async () => { for (const directory of directories) await rm(directory, { recursive: true, force: true }) })

  it('parses provider-neutral direct and candidate inputs', () => {
    expect(parseFleetEvaluationCorpus(corpus())).toMatchObject({
      schemaVersion: 1, projectNumber: 3,
      cases: [{ candidate: { repository: { provider: 'github' }, revision: 'b'.repeat(40) } }],
    })
  })

  it('loads only an owner-private regular corpus file', async () => {
    const directory = await mkdtemp(join(tmpdir(), 'fleet-corpus-'))
    directories.push(directory)
    const path = join(directory, 'corpus.json')
    await writeFile(path, JSON.stringify(corpus()), { mode: 0o600 })
    await expect(loadFleetEvaluationCorpus(path)).resolves.toMatchObject({ projectNumber: 3 })
    await chmod(path, 0o644)
    await expect(loadFleetEvaluationCorpus(path)).rejects.toThrow('owner-only')
  })
})
