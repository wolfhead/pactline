import type { CriterionIdentity } from './harness-result.js'
import type { RepositoryDelivery, RepositoryIdentity } from '../repository/delivery.js'
import type { RepositoryRevision } from '../repository/workspace.js'

/** Harness-neutral admitted work and repository policy for one finite Run. */
export interface FleetWorkDefinition {
  readonly caseId: string
  readonly taskNumber: number
  readonly taskVersion: number
  readonly base: RepositoryRevision
  readonly repository: RepositoryIdentity
  readonly allowedPaths: readonly string[]
  readonly verificationCommands: readonly string[]
  readonly criteria: readonly CriterionIdentity[]
  readonly candidate?: RepositoryDelivery & { readonly ref: string }
}
