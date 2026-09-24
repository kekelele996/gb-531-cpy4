export type NodeStatus = 'active' | 'inactive'

export interface ProcessNode {
  id: number
  node_code: string
  name: string
  unit_name: string
  medium: string
  design_pressure: number
  design_temperature: number
  owner_team: string
  status: NodeStatus
  coverage_summary?: {
    scenario_count: number
    active_safeguards: number
    open_high_risk: number
    latest_coverage: number
    uncovered_path_count: number
  }
  scenario_count?: number
  highest_risk?: string
  created_at: string
  updated_at: string
}

export interface ProcessNodeInput {
  node_code: string
  name: string
  unit_name: string
  medium: string
  design_pressure: number
  design_temperature: number
  owner_team: string
  status?: NodeStatus
}

export type DeactivationGateKey = 'open_deviations' | 'expired_safeguards' | 'unconfirmed_coverage'

export interface DeactivationBlockerItem {
  id: number
  scenario_id?: number
  title: string
  detail?: string
  state?: string
  risk_score: number
  risk_rank: string
  owner_team: string
  verification_expires_at?: string
}

export interface DeactivationBlockerGroup {
  key: DeactivationGateKey
  count: number
  highest_risk: number
  highest_rank: string
  owner_teams: string[]
  items: DeactivationBlockerItem[]
}

export interface DeactivationCheck {
  node_id: number
  node_code: string
  node_status: NodeStatus
  blocked: boolean
  total_items: number
  groups: DeactivationBlockerGroup[]
  checked_at: string
}
