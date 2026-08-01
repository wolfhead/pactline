import {
  etagForVersion,
  requireVersioned,
  v1Get,
  v1Delete,
  v1Patch,
  v1Post,
} from './v1/client'
import type { AcceptanceCriterion, CreateCriterionBody } from './acceptance'
import type { Task, UserRef } from '@/task-types'

export type MilestoneStatus = 'planned' | 'active' | 'completed' | 'cancelled'

export interface Milestone {
  id: string
  project_id: string
  version: number
  name: string
  outcome: string
  description: string
  owner_id: string
  status: MilestoneStatus
  target_date: string | null
  position: number
  completed_at: string | null
  cancelled_at: string | null
  created_at: string
  updated_at: string
  acceptance_criteria: AcceptanceCriterion[]
}

export interface Project {
  id: string
  number: number
  version: number
  name: string
  description: string
  creator: UserRef
  archived_at: string | null
  created_at: string
  updated_at: string
  completed_tasks: number
  eligible_tasks: number
}

export interface ProjectDetail {
  project: Project
  milestones: Milestone[]
  tasks: Task[]
  activity: ProjectActivity[]
}

export interface ProjectActivity {
  id: string
  milestone_id: string | null
  actor_id: string
  action: string
  reason: string | null
  old_value: string | null
  new_value: string | null
  authentication_method?: 'session' | 'api_token'
  token_name?: string
  request_id?: string
  created_at: string
}

export interface CreateProjectBody {
  name: string
  description?: string
}

export type ProjectRole = 'admin' | 'member'

export interface ProjectMembership {
  id: string
  project_id: string
  user: UserRef
  role: ProjectRole
  active: boolean
  created_at: string
  updated_at: string
}

export interface ProjectMembershipMutation {
  project_version: number
  membership?: ProjectMembership
}

export interface CreateMilestoneBody {
  name: string
  outcome: string
  description?: string
  owner_id: string
  target_date?: string | null
  position: number
}

export function listProjects(includeArchived = false): Promise<Project[]> {
  return v1Get<{ items: Project[] }>(
    `/api/v1/projects${includeArchived ? '?archived=all' : ''}`,
  ).then(({ value }) => value.items)
}

export function getProject(number: number): Promise<ProjectDetail> {
  return v1Get<ProjectDetail>(`/api/v1/projects/${number}`)
    .then((response) => requireVersioned(response).value)
}

export function createProject(body: CreateProjectBody): Promise<Project> {
  return v1Post<Project>('/api/v1/projects', { body })
    .then((response) => requireVersioned(response).value)
}

export function updateProject(
  number: number,
  version: number,
  body: Partial<CreateProjectBody>,
): Promise<Project> {
  return v1Patch<Project>(`/api/v1/projects/${number}`, {
    ifMatch: etagForVersion(version), body,
  }).then((response) => requireVersioned(response).value)
}

export function listProjectMembers(number: number): Promise<ProjectMembership[]> {
  return v1Get<{ items: ProjectMembership[] }>(`/api/v1/projects/${number}/members`)
    .then(({ value }) => value.items)
}

export function addProjectMember(
  number: number,
  projectVersion: number,
  userID: string,
  role: ProjectRole,
): Promise<ProjectMembershipMutation> {
  return v1Post<ProjectMembershipMutation>(`/api/v1/projects/${number}/members`, {
    ifMatch: etagForVersion(projectVersion), body: { user_id: userID, role },
  }).then((response) => requireVersioned(response).value)
}

export function updateProjectMember(
  number: number,
  projectVersion: number,
  userID: string,
  role: ProjectRole,
): Promise<ProjectMembershipMutation> {
  return v1Patch<ProjectMembershipMutation>(`/api/v1/projects/${number}/members/${userID}`, {
    ifMatch: etagForVersion(projectVersion), body: { role },
  }).then((response) => requireVersioned(response).value)
}

export function removeProjectMember(
  number: number,
  projectVersion: number,
  userID: string,
): Promise<ProjectMembershipMutation> {
  return v1Delete<ProjectMembershipMutation>(`/api/v1/projects/${number}/members/${userID}`, {
    ifMatch: etagForVersion(projectVersion),
  }).then((response) => requireVersioned(response).value)
}

export function applyProjectArchive(
  number: number,
  version: number,
  archived: boolean,
): Promise<Project> {
  return v1Post<Project>(
    `/api/v1/projects/${number}/${archived ? 'archive' : 'restore'}`,
    { ifMatch: etagForVersion(version) },
  ).then((response) => requireVersioned(response).value)
}

export function createMilestone(
  number: number,
  projectVersion: number,
  body: CreateMilestoneBody,
): Promise<Milestone> {
  return v1Post<Milestone>(`/api/v1/projects/${number}/milestones`, {
    ifMatch: etagForVersion(projectVersion), body,
  }).then((response) => requireVersioned(response).value)
}

export function updateMilestone(
  projectNumber: number,
  projectVersion: number,
  milestoneID: string,
  milestoneVersion: number,
  body: Partial<CreateMilestoneBody>,
): Promise<Milestone> {
  return v1Patch<Milestone>(
    `/api/v1/projects/${projectNumber}/milestones/${milestoneID}`,
    {
      ifMatch: etagForVersion(milestoneVersion),
      projectIfMatch: etagForVersion(projectVersion),
      body,
    },
  ).then((response) => requireVersioned(response).value)
}

export function applyMilestoneLifecycle(
  projectNumber: number,
  projectVersion: number,
  milestoneID: string,
  milestoneVersion: number,
  action: 'activate' | 'complete' | 'cancel' | 'reopen',
  reason?: string,
): Promise<Milestone> {
  return v1Post<Milestone>(
    `/api/v1/projects/${projectNumber}/milestones/${milestoneID}/${action}`,
    {
      ifMatch: etagForVersion(milestoneVersion),
      projectIfMatch: etagForVersion(projectVersion),
      body: reason ? { reason } : undefined,
    },
  ).then((response) => requireVersioned(response).value)
}

export function createMilestoneCriterion(
  projectNumber: number,
  projectVersion: number,
  milestoneID: string,
  milestoneVersion: number,
  body: CreateCriterionBody,
): Promise<AcceptanceCriterion> {
  return v1Post<AcceptanceCriterion>(
    `/api/v1/projects/${projectNumber}/milestones/${milestoneID}/criteria`,
    {
      ifMatch: etagForVersion(milestoneVersion),
      projectIfMatch: etagForVersion(projectVersion),
      body,
    },
  ).then((response) => requireVersioned(response).value)
}
