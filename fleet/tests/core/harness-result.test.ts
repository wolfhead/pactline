import { describe, expect, it } from 'vitest'
import { validateHarnessProposal } from '../../src/core/harness-result.js'
import type { ProposalValidationContext } from '../../src/core/harness-result.js'

const context: ProposalValidationContext = {
  stage: 'execution', runId: 'run-1', claimId: 'claim-1', taskNumber: 12,
  criteria: [{ id: 'criterion-1', revision: 2 }], verificationCommands: ['npm test'],
}

function execution(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    schemaVersion: 1, kind: 'execution', runId: 'run-1', claimId: 'claim-1', taskNumber: 12,
    recommendation: 'complete', summary: 'Implemented.', changedPaths: ['src/a.ts'],
    verification: [{ command: 'npm test', outcome: 'passed', summary: 'All tests passed.' }],
    criteria: [{ criterionId: 'criterion-1', criterionRevision: 2, outcome: 'passed', evidence: 'Covered by test.' }],
    limitations: [], ...overrides,
  }
}

describe('Harness proposal contract', () => {
  it('accepts a complete exact-identity execution proposal', () => {
    expect(validateHarnessProposal(execution(), context)).toMatchObject({ recommendation: 'complete', changedPaths: ['src/a.ts'] })
  })

  it('rejects incomplete, stale, and mismatched Claim proposals', () => {
    expect(() => validateHarnessProposal(execution({ claimId: 'claim-other' }), context)).toThrow('identity')
    expect(() => validateHarnessProposal(execution({ criteria: [] }), context)).toThrow('every current criterion')
    expect(() => validateHarnessProposal(execution({ verification: [] }), context)).toThrow('fixed verification')
  })

  it('permits waived criteria only after explicit typed resolution', () => {
    const waived = execution({
      criteria: [{ criterionId: 'criterion-1', criterionRevision: 2, outcome: 'waived', evidence: 'Superseded by decision.' }],
    })
    expect(() => validateHarnessProposal(waived, context)).toThrow('only after explicit typed resolution')
    expect(validateHarnessProposal(waived, { ...context, waivedCriterionIds: ['criterion-1'] })).toMatchObject({
      criteria: [{ outcome: 'waived' }],
    })
  })

  it('requires evidenced findings for a changes request', () => {
    const reviewContext: ProposalValidationContext = { ...context, stage: 'review' }
    const review = {
      schemaVersion: 1, kind: 'review', runId: 'run-1', claimId: 'claim-1', taskNumber: 12,
      recommendation: 'request_changes', summary: 'Needs correction.', findings: [], verification: [],
      criteria: [{ criterionId: 'criterion-1', criterionRevision: 2, outcome: 'failed', evidence: 'Failure.' }], limitations: [],
    }
    expect(() => validateHarnessProposal(review, reviewContext)).toThrow('at least one finding')
  })

  it('requires a typed request only for request_resolution', () => {
    const request = execution({ recommendation: 'request_resolution', changedPaths: [], verification: [] })
    expect(() => validateHarnessProposal(request, context)).toThrow('resolutionRequest is required')
    expect(validateHarnessProposal({
      ...request,
      resolutionRequest: { issueType: 'decision_required', request: 'Choose the compatibility behavior.' },
    }, context)).toMatchObject({ recommendation: 'request_resolution' })
  })
})
