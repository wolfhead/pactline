import { apiDelete, apiGet, apiPatch, apiPost } from './client'

export type AcceptanceOutcome = 'passed' | 'failed' | 'unable' | 'waived'

export interface AcceptanceCheck {
  id: string
  criterion_revision: number
  outcome: AcceptanceOutcome
  evidence: string
  checker_type: 'user' | 'agent' | 'system'
  checked_by_user_id: string | null
  checker_ref?: string
  checked_at: string
}

export interface AcceptanceCriterion {
  id: string
  criterion: string
  verification_instructions: string
  revision: number
  position: number
  current_check: AcceptanceCheck | null
}

export interface CreateCriterionBody {
  criterion: string
  verification_instructions: string
  position: number
}

export function listTaskCriteria(number: number): Promise<AcceptanceCriterion[]> {
  return apiGet<AcceptanceCriterion[]>(`/api/tasks/${number}/acceptance-criteria`)
}

export function createTaskCriterion(
  number: number,
  body: CreateCriterionBody,
): Promise<AcceptanceCriterion> {
  return apiPost<AcceptanceCriterion>(`/api/tasks/${number}/acceptance-criteria`, body)
}

export function checkCriterion(
  criterionID: string,
  criterionRevision: number,
  outcome: AcceptanceOutcome,
  evidence: string,
): Promise<AcceptanceCheck> {
  return apiPost<AcceptanceCheck>(`/api/acceptance-criteria/${criterionID}/checks`, {
    criterion_revision: criterionRevision,
    outcome,
    evidence,
  })
}

export function updateCriterion(
  criterionID: string,
  body: Partial<CreateCriterionBody>,
): Promise<AcceptanceCriterion> {
  return apiPatch<AcceptanceCriterion>(`/api/acceptance-criteria/${criterionID}`, body)
}

export function removeCriterion(criterionID: string, reason?: string): Promise<void> {
  return apiDelete<void>(`/api/acceptance-criteria/${criterionID}`, reason ? { reason } : undefined)
}
