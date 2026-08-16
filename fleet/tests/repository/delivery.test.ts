import { describe, expect, it } from 'vitest'
import { validateRepositoryDelivery } from '../../src/repository/delivery.js'

describe('provider-neutral delivery contract', () => {
  it.each([
    ['github', 'github.com', 'https://github.com/wolfhead/pactline/pull/40'],
    ['gitlab', 'gitlab.example.com', 'https://gitlab.example.com/wolfhead/pactline/-/merge_requests/40'],
  ] as const)('accepts %s code-change evidence', (provider, host, codeChangeUrl) => {
    expect(validateRepositoryDelivery({
      repository: { provider, host, owner: 'wolfhead', name: 'pactline' },
      codeChangeUrl, revision: 'a'.repeat(40), branch: 'fleet/run/test-1',
    })).toMatchObject({ repository: { provider }, codeChangeUrl })
  })

  it('rejects provider and repository mismatches', () => {
    expect(() => validateRepositoryDelivery({
      repository: { provider: 'gitlab', host: 'github.com', owner: 'wolfhead', name: 'pactline' },
      codeChangeUrl: 'https://github.com/wolfhead/pactline/pull/40', revision: 'a'.repeat(40), branch: 'fleet/run/test-1',
    })).toThrow('gitlab')
  })
})
