import type { HarnessStage } from './harness-adapter.js'

export interface PromptPolicy {
  readonly version: string
  readonly system: string
  readonly stageInstructions: string
}

const OWNERSHIP = [
  'Work only inside the supplied disposable repository workspace.',
  'Do not invoke Pactline, GitHub, GitLab, or any remote-write operation.',
  'Do not read credentials or files outside the supplied workspace.',
  'Return one result matching the supplied schema; the result is a proposal, not a lifecycle decision.',
  'Report only files and verification outcomes you actually observed.',
].join('\n')

const STAGES: Readonly<Record<HarnessStage, string>> = {
  execution: 'Implement the bounded Task. You may edit only allowed paths. Do not publish or settle the work.',
  correction: 'Correct the bounded delivery using the current Task and review evidence. Do not publish or settle the work.',
  review: 'Independently review the frozen delivery. Do not modify any file. Cite concrete file and line evidence.',
  resolution_analysis: 'Analyze the blocking decision or dependency without modifying the repository. Return a typed request when needed.',
}

export function promptPolicy(stage: HarnessStage, version = 'fleet-m1-v1'): PromptPolicy {
  return { version, system: OWNERSHIP, stageInstructions: STAGES[stage] }
}
