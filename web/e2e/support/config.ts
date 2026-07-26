/**
 * Shared constants for the e2e suite: where the backend lives when hit
 * directly (bypassing the Vite proxy, for fast API-based fixture setup), the
 * database connection used only to delete rows a test created, and the six
 * seeded Phase 1 identities (see migrations/0002_seed_users.sql).
 */

export const BACKEND_URL = process.env.E2E_BACKEND_URL ?? 'http://localhost:8080'

export const DATABASE_URL =
  process.env.E2E_DATABASE_URL ?? 'postgres://bounty:bounty@localhost:5433/bountyboard?sslmode=disable'

export interface SeedUser {
  id: string
  name: string
}

export const USERS = {
  sponsorA: { id: '00000000-0000-0000-0000-000000000001', name: '产品 A' },
  leadB: { id: '00000000-0000-0000-0000-000000000002', name: '技术 Leader B' },
  engineerC: { id: '00000000-0000-0000-0000-000000000003', name: '研发 C' },
  engineerD: { id: '00000000-0000-0000-0000-000000000004', name: '研发 D' },
  engineerE: { id: '00000000-0000-0000-0000-000000000005', name: '研发 E' },
  stewardF: { id: '00000000-0000-0000-0000-000000000006', name: 'Steward F' },
} as const satisfies Record<string, SeedUser>
