import { etagForVersion, requireVersioned, v1Delete, v1Get, v1Post } from "./v1/client";
import type { Actor, TaskStageClaim, TaskWorkflow } from "@/task-types";

export type RepositoryProvider = "gitlab" | "github";
export type CodeChangeKind = "merge_request" | "pull_request";
export type CodeChangeVerificationStatus = "verified" | "missing" | "unauthorized" | "unreachable" | "disconnected";
export type CodeChangeState = "opened" | "closed" | "merged" | "locked";

export interface CodeChangeVerification {
  status: CodeChangeVerificationStatus;
  attempted_at: string;
}

export interface CodeChangeProviderEvidence {
  connection_id: string;
  provider_repository_id: string;
  provider_change_id: string;
  observed_at: string;
  title: string;
  state: CodeChangeState;
  draft: boolean;
  source_branch: string;
  target_branch: string;
  head_sha: string;
  merge_commit_sha?: string;
  merged_at?: string;
  provider_updated_at: string;
}

export interface TaskCodeChange {
  id: string;
  project_repository_id: string;
  provider: RepositoryProvider;
  repository_url: string;
  kind: CodeChangeKind;
  change_number: number;
  web_url: string;
  linked_by: Actor;
  linked_through_claim_id: string;
  linked_at: string;
  provider_evidence?: CodeChangeProviderEvidence;
  provider_verification?: CodeChangeVerification;
}

export interface CodeChangeSnapshot {
  task_code_change_id: string;
  project_repository_id: string;
  provider: RepositoryProvider;
  kind: CodeChangeKind;
  change_number: number;
  web_url: string;
  provider_evidence?: CodeChangeProviderEvidence;
}

export type DeliveryComparison = "unverified" | "unchanged" | "moved" | "merged" | "missing" | "unauthorized" | "unreachable" | "disconnected";

export interface TaskDeliveryComparison {
  snapshot: CodeChangeSnapshot;
  current?: TaskCodeChange;
  comparison: DeliveryComparison;
}

export interface TaskDelivery {
  active_links: TaskCodeChange[];
  review?: { review_cycle: number; code_changes: TaskDeliveryComparison[] };
}

export interface TaskCodeChangeMutation {
  task: TaskWorkflow;
  code_change: TaskCodeChange;
}

export function getTaskDelivery(taskNumber: number): Promise<TaskDelivery> {
  return v1Get<TaskDelivery>(`/api/v1/tasks/${taskNumber}/code-changes`).then(({ value }) => value);
}

export function linkTaskCodeChange(taskNumber: number, taskVersion: number, claim: TaskStageClaim, codeChangeURL: string): Promise<TaskCodeChangeMutation> {
  return v1Post<TaskCodeChangeMutation>(`/api/v1/claims/${claim.id}/code-changes`, {
    ifMatch: etagForVersion(taskVersion),
    body: { code_change_url: codeChangeURL },
  }).then((response) => requireVersioned(response).value);
}

export function unlinkTaskCodeChange(taskNumber: number, taskVersion: number, claim: TaskStageClaim, linkID: string): Promise<TaskCodeChangeMutation> {
  return v1Delete<TaskCodeChangeMutation>(`/api/v1/claims/${claim.id}/code-changes/${linkID}`, {
    ifMatch: etagForVersion(taskVersion),
  }).then((response) => requireVersioned(response).value);
}
