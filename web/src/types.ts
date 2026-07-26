export type Status = 'DRAFT' | 'OPEN' | 'CLAIMED' | 'DELIVERED' | 'COMPLETED' | 'ABANDONED'
export type BountyType = 'PLAN' | 'DELIVERY'
export type Visibility = 'PUBLIC' | 'RESTRICTED' | 'DIRECTED'
export type Commitment = 'COMMITTED' | 'EXPLORATORY'
export type CreditRole = 'DEFINE' | 'LEAD' | 'CO_DELIVER' | 'REVIEW' | 'SUPPORT' | 'BASELINE'
export type CreditStatus = 'PENDING' | 'CONFIRMED' | 'DECLINED'
export type UserRole = 'SPONSOR' | 'ENGINEER' | 'TECH_LEAD' | 'STEWARD'

export interface User {
  id: string
  name: string
  email: string
  roles: UserRole[]
  active: boolean
}

export interface BusinessLine {
  tag: string
  weight: number
}

export interface Bounty {
  id: string
  type: BountyType
  parent_id?: string
  title: string
  goal: string
  acceptance_criteria: string
  visibility: Visibility
  restriction?: string
  directed_to?: string
  business_lines: BusinessLine[]
  commitment: Commitment
  status: Status
  sponsor_id: string
  claimed_by?: string
  claimed_at?: string
  person_days?: number
  retrospective?: string
  completed_at?: string
  created_at: string
  updated_at: string
}

export interface Credit {
  id: string
  bounty_id: string
  user_id: string
  role: CreditRole
  nominated_by?: string
  evidence?: string
  status: CreditStatus
  confirmed_at?: string
  created_at: string
}

export interface CreditView {
  credit: Credit
  user_name: string
}

export interface WorkView {
  bounty: Bounty
  credits: CreditView[]
}

export const CREDIT_ROLE_LABELS: Record<CreditRole, string> = {
  DEFINE: '定义方案',
  LEAD: '主交付',
  CO_DELIVER: '共同交付',
  REVIEW: '深度评审',
  SUPPORT: '上下文支援',
  BASELINE: '基线支撑',
}

export const STATUS_LABELS: Record<Status, string> = {
  DRAFT: '草稿',
  OPEN: '可认领',
  CLAIMED: '已认领',
  DELIVERED: '待验收',
  COMPLETED: '已完成',
  ABANDONED: '已放弃',
}
