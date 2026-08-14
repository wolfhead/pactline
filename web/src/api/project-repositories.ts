import { etagForVersion, requireVersioned, v1Delete, v1Get, v1Post } from './v1/client'

export interface ProjectRepository {
  id: string
  canonical_web_url: string
	label: string
	provider: 'gitlab' | 'github'
	origin: string
	provider_repository_id: string
  path_with_namespace: string
  default_branch: string
  bound_at: string
}

export interface ProjectRepositoryMutation {
  project_version: number
  repository: ProjectRepository
}

export function listProjectRepositories(projectNumber: number): Promise<ProjectRepository[]> {
  return v1Get<{ items: ProjectRepository[] }>(
    `/api/v1/projects/${projectNumber}/repositories`,
  ).then(({ value }) => value.items)
}

export function bindProjectRepository(
  projectNumber: number,
  projectVersion: number,
  repositoryURL: string,
): Promise<ProjectRepositoryMutation> {
  return v1Post<ProjectRepositoryMutation>(`/api/v1/projects/${projectNumber}/repositories`, {
    ifMatch: etagForVersion(projectVersion),
    body: { repository_url: repositoryURL },
  }).then((response) => requireVersioned(response).value)
}

export function unbindProjectRepository(
  projectNumber: number,
  projectVersion: number,
  repositoryID: string,
): Promise<ProjectRepositoryMutation> {
  return v1Delete<ProjectRepositoryMutation>(
    `/api/v1/projects/${projectNumber}/repositories/${repositoryID}`,
    { ifMatch: etagForVersion(projectVersion) },
  ).then((response) => requireVersioned(response).value)
}
