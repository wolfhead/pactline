export type Status = 'DRAFT' | 'OPEN' | 'CLAIMED' | 'DELIVERED' | 'COMPLETED' | 'ABANDONED'
export type BountyType = 'PLAN' | 'DELIVERY'
export type Visibility = 'PUBLIC' | 'RESTRICTED' | 'DIRECTED'
export type Commitment = 'COMMITTED' | 'EXPLORATORY'
export type CreditRole = 'DEFINE' | 'LEAD' | 'CO_DELIVER' | 'REVIEW' | 'SUPPORT' | 'BASELINE'
export type CreditStatus = 'PENDING' | 'CONFIRMED' | 'DECLINED'
export type UserRole = 'SPONSOR' | 'ENGINEER' | 'TECH_LEAD' | 'STEWARD'

// The three graded levels from internal/domain/bounty.go. Deliberately coarse
// (spec §7.1): a continuous score would invite arguing over decimals.
export type ValueLevel = 'S' | 'A' | 'B' | 'C'
export type Difficulty = 'XS' | 'S' | 'M' | 'L' | 'XL'
export type Completion = 'EXCEEDED' | 'MET' | 'PARTIAL' | 'MISSED'

// From internal/domain/anchor.go's AnchorDimension.
export type AnchorDimension = 'VALUE' | 'DIFFICULTY'

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
  // value_level: sponsor-set (or steward-corrected), only while DRAFT/OPEN
  // through the ordinary channel — see domain.CanSetValueLevel.
  value_level?: ValueLevel
  // difficulty: TECH_LEAD/STEWARD-set only, never the sponsor's call — see
  // domain.CanSetDifficulty. Locked once the bounty is settled.
  difficulty?: Difficulty
  commitment: Commitment
  // completion: sponsor-set, sent with the DELIVERED -> COMPLETED transition.
  // ABANDONED bounties never carry one (spec §7.1.1).
  completion?: Completion
  status: Status
  sponsor_id: string
  claimed_by?: string
  claimed_at?: string
  person_days?: number
  retrospective?: string
  // settled_score / settled_at: present only on GET /api/bounties/{id}.
  // decorate() in internal/api/feed_handler.go deliberately strips both from
  // every WorkView (the feed and every portfolio) — a score is a fact about a
  // work, never about a person, and this is the one place it may be shown.
  // Do not add these to WorkCard/Portfolio/WorkFeed rendering.
  settled_score?: number
  settled_at?: string
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

// Calibration — from internal/domain/calibration.go's Calibration struct
// (spec §4.6). Both the original and calibrated value/score are stored on
// the row itself, so a calibration is a self-contained before/after and
// never needs to be reconciled against the bounty's own (mutable) fields.
export interface Calibration {
  id: string
  bounty_id: string
  quarter: string
  original_value: ValueLevel
  original_score: number
  calibrated_value: ValueLevel
  calibrated_score: number
  note?: string
  created_by: string
  created_at: string
}

// AnchorExample — from internal/domain/anchor.go's AnchorExample struct
// (spec §4.7). Level is a plain string because it holds either a ValueLevel
// or a Difficulty code depending on Dimension.
export interface AnchorExample {
  id: string
  dimension: AnchorDimension
  level: string
  bounty_id: string
  note?: string
  created_at: string
}

// The four outcome buckets of a settlement run — from the settledItem /
// unscorableItem / failedItem / settlementResponse types in
// internal/api/scoring_handler.go.
export interface SettledItem {
  bounty_id: string
  title: string
  score: number
}

export interface UnscorableItem {
  bounty_id: string
  title: string
  reason: string
}

export interface FailedItem {
  bounty_id: string
  title: string
  reason: string
}

export interface SettlementResponse {
  settled: SettledItem[]
  settled_count: number
  already_settled_count: number
  unscorable: UnscorableItem[]
  unscorable_count: number
  failed: FailedItem[]
  failed_count: number
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

// The three graded-level label maps below deliberately keep the level code
// inside the label text itself (e.g. "S · 价值最高", not just "价值最高").
// The codes (S/A/B/C, XS…XL) are the vocabulary the pricing group and tech
// leads actually speak when arguing a case against the anchor list (spec
// §4.7) — hiding them behind prose-only labels, the way CREDIT_ROLE_LABELS
// fully replaces DEFINE with "定义方案", would defeat that. The ordering
// implied by each phrase ("最高" down to "最低") mirrors the fixed weight
// ordering in internal/scoring/constants.go (S=8 > A=5 > B=3 > C=1, and
// XS=0.5 < S=1 < M=2 < L=3.5 < XL=6); it names no business meaning beyond
// that ordering, since the spec deliberately does not anchor these levels to
// anything more concrete than order (§13: "价值分锚定金额" was rejected).
export const VALUE_LEVEL_LABELS: Record<ValueLevel, string> = {
  S: 'S · 价值最高',
  A: 'A · 价值较高',
  B: 'B · 价值较低',
  C: 'C · 价值最低',
}

export const DIFFICULTY_LABELS: Record<Difficulty, string> = {
  XS: 'XS · 难度最低',
  S: 'S · 难度较低',
  M: 'M · 难度中等',
  L: 'L · 难度较高',
  XL: 'XL · 难度最高',
}

// Verbatim from mechanism-design.md §4.3's 完成度档 column.
export const COMPLETION_LABELS: Record<Completion, string> = {
  EXCEEDED: '超出预期',
  MET: '达成',
  PARTIAL: '部分达成',
  MISSED: '未达成',
}

export const ANCHOR_DIMENSION_LABELS: Record<AnchorDimension, string> = {
  VALUE: '价值档',
  DIFFICULTY: '难度档',
}

export const VALUE_LEVELS: ValueLevel[] = ['S', 'A', 'B', 'C']
export const DIFFICULTIES: Difficulty[] = ['XS', 'S', 'M', 'L', 'XL']
export const COMPLETIONS: Completion[] = ['EXCEEDED', 'MET', 'PARTIAL', 'MISSED']
