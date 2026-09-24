import type { DeactivationGateKey } from '../types/process-node'

export interface GateMeta {
  key: DeactivationGateKey
  label: string
  description: string
  emptyHint: string
}

export const deactivationGateMeta: Record<DeactivationGateKey, GateMeta> = {
  open_deviations: {
    key: 'open_deviations',
    label: '未接受偏差',
    description: '偏差场景尚未走完“已分析 → 已复核 → 已接受”的收口流程。',
    emptyHint: '所有偏差均已接受。',
  },
  expired_safeguards: {
    key: 'expired_safeguards',
    label: '过期保护层',
    description: '保护层未验证或验证有效期已过，不能再计入独立保护层。',
    emptyHint: '保护层验证均在有效期内。',
  },
  unconfirmed_coverage: {
    key: 'unconfirmed_coverage',
    description: '最近一次覆盖推演还停留在运行中或待确认状态，需安全复核员确认或作废。',
    label: '待确认覆盖',
    emptyHint: '各场景最新覆盖评估均已确认或作废。',
  },
}

export const gateOrder: DeactivationGateKey[] = ['open_deviations', 'expired_safeguards', 'unconfirmed_coverage']

export function scenarioStateLabel(state: string): string {
  return ({ draft: '草稿', analyzed: '已分析', verified: '已复核', accepted: '已接受', rework: '退回修订' })[state] ?? state
}

export function coverageStateLabel(state: string): string {
  return ({
    queued: '排队中',
    running: '运行中',
    completed: '待确认',
    failed: '运行失败',
    confirmed: '已确认',
    voided: '已作废',
  })[state] ?? state
}

export function safeguardLifecycleLabel(state: string): string {
  return ({ pending: '待验证', active: '在用', expired: '已过期', invalid: '已失效' })[state] ?? state
}
