import { describe, expect, it } from 'vitest'
import {
  coverageStateLabel,
  deactivationGateMeta,
  gateOrder,
  safeguardLifecycleLabel,
  scenarioStateLabel,
} from './deactivation-gate'

describe('deactivation gate meta', () => {
  it('covers every gate category in stable display order', () => {
    expect(gateOrder).toEqual(['open_deviations', 'expired_safeguards', 'unconfirmed_coverage'])
    for (const key of gateOrder) {
      const meta = deactivationGateMeta[key]
      expect(meta.label).toBeTruthy()
      expect(meta.description).toBeTruthy()
      expect(meta.emptyHint).toBeTruthy()
    }
  })

  it('maps machine states to reviewer-facing Chinese labels', () => {
    expect(scenarioStateLabel('draft')).toBe('草稿')
    expect(scenarioStateLabel('accepted')).toBe('已接受')
    expect(coverageStateLabel('completed')).toBe('待确认')
    expect(coverageStateLabel('failed')).toBe('运行失败')
    expect(safeguardLifecycleLabel('expired')).toBe('已过期')
    expect(safeguardLifecycleLabel('unknown-state')).toBe('unknown-state')
  })
})
