export type RepositoryProvider = 'github' | 'gitlab'

export interface RepositoryIdentity {
  readonly provider: RepositoryProvider
  readonly host: string
  readonly owner: string
  readonly name: string
}

export interface RepositoryDelivery {
  readonly repository: RepositoryIdentity
  readonly codeChangeUrl: string
  readonly revision: string
  readonly branch: string
}

export function validateRepositoryDelivery(delivery: RepositoryDelivery): RepositoryDelivery {
  if (!/^[a-f0-9]{40}$/.test(delivery.revision)) throw new Error('Delivery revision must be a lowercase 40-character Git SHA')
  if (delivery.branch.trim() === '' || delivery.branch.includes('..') || /[~^:?*[\\\s]/.test(delivery.branch)) throw new Error('Delivery branch is invalid')
  if (delivery.repository.host.trim() === '' || delivery.repository.owner.trim() === '' || delivery.repository.name.trim() === '') {
    throw new Error('Delivery repository identity is incomplete')
  }
  let url: URL
  try { url = new URL(delivery.codeChangeUrl) } catch { throw new Error('Delivery code-change URL is invalid') }
  if (url.protocol !== 'https:' || url.username !== '' || url.password !== '' || url.hostname !== delivery.repository.host) {
    throw new Error('Delivery code-change URL does not match the credential-free repository host')
  }
  const expectedPrefix = `/${delivery.repository.owner}/${delivery.repository.name}/`
  if (!url.pathname.startsWith(expectedPrefix)) throw new Error('Delivery code-change URL does not match the repository identity')
  const providerSegment = delivery.repository.provider === 'github' ? '/pull/' : '/-/merge_requests/'
  if (!url.pathname.includes(providerSegment)) throw new Error(`Delivery URL is not a ${delivery.repository.provider} code change`)
  return delivery
}
