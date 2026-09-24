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

export type ClosureCategoryKey = 'open_deviation' | 'expired_safeguard' | 'pending_coverage'

export interface ClosureItem {
  id: number
  reference: string
  reason: string
  state: string
  risk_score: number
  risk_rank: string
  owner_team: string
  scenario_id?: number
}

export interface ClosureCategory {
  key: ClosureCategoryKey
  label: string
  blocked: boolean
  count: number
  highest_risk: number
  risk_rank: string
  owner_team: string
  items: ClosureItem[]
}

export interface DeactivationCheck {
  node_id: number
  node_code: string
  node_status: NodeStatus
  checked_at: string
  can_deactivate: boolean
  blocking_item_count: number
  categories: ClosureCategory[]
}
