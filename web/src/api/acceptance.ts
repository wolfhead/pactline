import {
  etagForVersion,
  requireVersioned,
  v1Delete,
  v1Get,
  v1Patch,
  v1Post,
} from './v1/client'
import type { TaskStageClaim } from '@/task-types'

export type AcceptanceOutcome = 'passed' | 'failed' | 'unable' | 'waived'

export interface AcceptanceCheck {
  id: string
  criterion_id: string
  criterion_revision: number
  outcome: AcceptanceOutcome
  evidence: string
  checker_type: 'user' | 'agent' | 'system'
  checked_by_user_id: string | null
  checker_ref?: string
  purpose?: 'execution_verification' | 'acceptance'
  task_claim_id?: string
  review_cycle?: number
  checked_at: string
}

export interface AcceptanceCriterion {
  id: string
  version: number
  project_id?: string
  milestone_id?: string
  task_id?: string
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
  return v1Get<{ items: AcceptanceCriterion[] }>(`/api/v1/tasks/${number}/criteria`)
    .then(({ value }) => value.items)
}

export function createTaskCriterion(
  number: number,
  taskVersion: number,
  body: CreateCriterionBody,
): Promise<AcceptanceCriterion> {
  return v1Post<AcceptanceCriterion>(`/api/v1/tasks/${number}/criteria`, {
    ifMatch: etagForVersion(taskVersion), body,
  }).then((response) => requireVersioned(response).value)
}

export function checkCriterion(
  criterionID: string,
  criterionVersion: number,
  criterionRevision: number,
  outcome: AcceptanceOutcome,
  evidence: string,
): Promise<AcceptanceCheck> {
  return v1Post<AcceptanceCheck>(`/api/v1/criteria/${criterionID}/checks`, {
    ifMatch: etagForVersion(criterionVersion),
    body: {
      criterion_revision: criterionRevision,
      outcome,
      evidence,
    },
  }).then(({ value }) => value)
}

export function checkTaskCriterionThroughClaim(
  taskNumber: number,
  taskVersion: number,
  claim: TaskStageClaim,
  criterion: AcceptanceCriterion,
  outcome: AcceptanceOutcome,
  evidence: string,
): Promise<AcceptanceCheck> {
  return v1Post<AcceptanceCheck>(
    `/api/v1/tasks/${taskNumber}/claims/${claim.id}/criteria/${criterion.id}/checks`,
    {
      ifMatch: etagForVersion(taskVersion),
      body: {
        claim_version: claim.version,
        criterion_revision: criterion.revision,
        outcome,
        evidence,
      },
    },
  ).then(({ value }) => value)
}

export function updateCriterion(
  criterionID: string,
  criterionVersion: number,
  body: Partial<CreateCriterionBody>,
): Promise<AcceptanceCriterion> {
  return v1Patch<AcceptanceCriterion>(`/api/v1/criteria/${criterionID}`, {
    ifMatch: etagForVersion(criterionVersion), body,
  }).then((response) => requireVersioned(response).value)
}

export function removeCriterion(
  criterionID: string,
  criterionVersion: number,
  reason?: string,
): Promise<void> {
  return v1Delete(`/api/v1/criteria/${criterionID}`, {
    ifMatch: etagForVersion(criterionVersion),
    body: reason ? { reason } : undefined,
  }).then(() => undefined)
}
