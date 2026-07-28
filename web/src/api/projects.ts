import { apiGet, apiPatch, apiPost } from './client'
import type { AcceptanceCriterion, CreateCriterionBody } from './acceptance'
import type { Task, UserRef } from '@/task-types'

export type ProjectStatus = 'planned' | 'active' | 'paused' | 'completed' | 'cancelled'
export type MilestoneStatus = 'open' | 'completed' | 'cancelled'

export interface Milestone {
  id: string
  name: string
  outcome: string
  description: string
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
  name: string
  outcome: string
  description: string
  owner: UserRef
  creator: UserRef
  status: ProjectStatus
  target_date: string | null
  completed_at: string | null
  cancelled_at: string | null
  archived_at: string | null
  created_at: string
  updated_at: string
  completed_tasks: number
  eligible_tasks: number
  active_criteria: number
  satisfied_criteria: number
}

export interface ProjectDetail {
  project: Project
  acceptance_criteria: AcceptanceCriterion[]
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
  created_at: string
}

export interface CreateProjectBody {
  name: string
  outcome: string
  description?: string
  owner_id: string
  target_date?: string | null
}

export interface CreateMilestoneBody {
  name: string
  outcome: string
  description?: string
  target_date?: string | null
  position: number
}

export function listProjects(includeArchived = false): Promise<Project[]> {
  return apiGet<Project[]>(`/api/projects${includeArchived ? '?archived=all' : ''}`)
}

export function getProject(number: number): Promise<ProjectDetail> {
  return apiGet<ProjectDetail>(`/api/projects/${number}`)
}

export function createProject(body: CreateProjectBody): Promise<Project> {
  return apiPost<Project>('/api/projects', body)
}

export function updateProject(number: number, body: Partial<CreateProjectBody>): Promise<Project> {
  return apiPatch<Project>(`/api/projects/${number}`, body)
}

export function applyProjectLifecycle(number: number, action: string, reason?: string): Promise<Project> {
  return apiPost<Project>(`/api/projects/${number}/${action}`, reason ? { reason } : undefined)
}

export function createMilestone(number: number, body: CreateMilestoneBody): Promise<Milestone> {
  return apiPost<Milestone>(`/api/projects/${number}/milestones`, body)
}

export function updateMilestone(
  projectNumber: number,
  milestoneID: string,
  body: Partial<CreateMilestoneBody>,
): Promise<Milestone> {
  return apiPatch<Milestone>(`/api/projects/${projectNumber}/milestones/${milestoneID}`, body)
}

export function applyMilestoneLifecycle(
  projectNumber: number,
  milestoneID: string,
  action: string,
  reason?: string,
): Promise<Milestone> {
  return apiPost<Milestone>(
    `/api/projects/${projectNumber}/milestones/${milestoneID}/${action}`,
    reason ? { reason } : undefined,
  )
}

export function createProjectCriterion(number: number, body: CreateCriterionBody): Promise<AcceptanceCriterion> {
  return apiPost<AcceptanceCriterion>(`/api/projects/${number}/acceptance-criteria`, body)
}

export function createMilestoneCriterion(
  projectNumber: number,
  milestoneID: string,
  body: CreateCriterionBody,
): Promise<AcceptanceCriterion> {
  return apiPost<AcceptanceCriterion>(
    `/api/projects/${projectNumber}/milestones/${milestoneID}/acceptance-criteria`,
    body,
  )
}
