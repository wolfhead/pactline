import { BACKEND_URL } from './config'

/**
 * Thin Node-side REST client that talks to the Go backend directly on
 * :8080, bypassing the browser entirely. Used to build fixture state fast
 * (open a bounty, claim it, delivered it, nominate a credit...) so each e2e
 * test's actual browser interaction stays focused on the one rule under
 * test, per the brief: "where setup is faster through the API, do setup via
 * API and the assertion through the browser."
 */

export interface Bounty {
  id: string
  type: 'PLAN' | 'DELIVERY'
  title: string
  status: 'DRAFT' | 'OPEN' | 'CLAIMED' | 'DELIVERED' | 'COMPLETED' | 'ABANDONED'
  sponsor_id: string
  claimed_by?: string
  retrospective?: string
  [key: string]: unknown
}

export interface Credit {
  id: string
  bounty_id: string
  user_id: string
  role: string
  status: 'PENDING' | 'CONFIRMED' | 'DECLINED'
  [key: string]: unknown
}

export class ApiRequestError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message)
    this.name = 'ApiRequestError'
  }
}

async function call<T>(userId: string, method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${BACKEND_URL}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-User-Id': userId },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  const parsed: unknown = text ? JSON.parse(text) : null
  if (!res.ok) {
    const errorField = parsed && typeof parsed === 'object' ? (parsed as { error?: unknown }).error : undefined
    const message = typeof errorField === 'string' ? errorField : res.statusText
    throw new ApiRequestError(message, res.status)
  }
  return parsed as T
}

export interface CreateBountyInput {
  type?: 'PLAN' | 'DELIVERY'
  parent_id?: string
  title: string
  goal?: string
  acceptance_criteria?: string
  visibility?: 'PUBLIC' | 'RESTRICTED' | 'DIRECTED'
  restriction?: string
  directed_to?: string
  business_lines?: { tag: string; weight: number }[]
  commitment?: 'COMMITTED' | 'EXPLORATORY'
}

export function createBounty(sponsorId: string, input: CreateBountyInput): Promise<Bounty> {
  return call<Bounty>(sponsorId, 'POST', '/api/bounties', input)
}

export interface TransitionExtra {
  retrospective?: string
  person_days?: number
}

export function transition(userId: string, bountyId: string, to: Bounty['status'], extra?: TransitionExtra): Promise<Bounty> {
  return call<Bounty>(userId, 'POST', `/api/bounties/${bountyId}/transition`, { to, ...extra })
}

export interface NominateInput {
  user_id: string
  role: string
  evidence?: string
}

export function nominate(userId: string, bountyId: string, input: NominateInput): Promise<Credit> {
  return call<Credit>(userId, 'POST', `/api/bounties/${bountyId}/credits`, input)
}

export function respond(userId: string, creditId: string, status: 'CONFIRMED' | 'DECLINED'): Promise<Credit> {
  return call<Credit>(userId, 'POST', `/api/credits/${creditId}/respond`, { status })
}

export function listCredits(userId: string, bountyId: string): Promise<Credit[]> {
  return call<Credit[]>(userId, 'GET', `/api/bounties/${bountyId}/credits`)
}

export function getBounty(userId: string, bountyId: string): Promise<Bounty> {
  return call<Bounty>(userId, 'GET', `/api/bounties/${bountyId}`)
}
