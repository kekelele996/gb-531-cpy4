<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { AlertTriangle, CheckCircle2, RefreshCw, ShieldOff } from 'lucide-vue-next'
import RiskBadge from './RiskBadge.vue'
import { ApiError, errorMessage } from '../../api/client'
import * as nodeApi from '../../api/process-node'
import type { DeactivationBlockerGroup, DeactivationCheck, DeactivationGateKey, ProcessNode } from '../../types/process-node'
import { coverageStateLabel, deactivationGateMeta, gateOrder, safeguardLifecycleLabel, scenarioStateLabel } from '../../utils/deactivation-gate'

const props = defineProps<{ visible: boolean; node: ProcessNode | null }>()
const emit = defineEmits<{
  (event: 'update:visible', value: boolean): void
  (event: 'deactivated', node: ProcessNode): void
}>()

const loading = ref(false)
const confirming = ref(false)
const loadError = ref('')
const check = ref<DeactivationCheck>()

const groups = computed<DeactivationBlockerGroup[]>(() => {
  if (!check.value) return []
  const byKey = new Map(check.value.groups.map((group) => [group.key, group]))
  return gateOrder.map((key) => byKey.get(key) ?? emptyGroup(key))
})

function emptyGroup(key: DeactivationGateKey): DeactivationBlockerGroup {
  return { key, count: 0, highest_risk: 0, highest_rank: 'low', owner_teams: [], items: [] }
}

function close() { emit('update:visible', false) }
function itemStateLabel(groupKey: DeactivationGateKey, state?: string) {
  if (!state) return ''
  if (groupKey === 'open_deviations') return scenarioStateLabel(state)
  if (groupKey === 'unconfirmed_coverage') return coverageStateLabel(state)
  return safeguardLifecycleLabel(state)
}
function expiryLabel(value?: string) { return value ? new Date(value).toLocaleDateString('zh-CN') : '未验证' }

async function runCheck() {
  if (!props.node) return
  loading.value = true
  loadError.value = ''
  try {
    check.value = await nodeApi.getDeactivationCheck(props.node.id)
  } catch (error) {
    loadError.value = errorMessage(error)
  } finally {
    loading.value = false
  }
}

async function confirmDeactivate() {
  if (!props.node || !check.value) return
  confirming.value = true
  try {
    // Re-run the gate server-side via the deactivate call itself; concurrent
    // edits can invalidate the check the user is currently looking at.
    const updated = await nodeApi.deactivateProcessNode(props.node.id)
    emit('deactivated', updated)
    close()
  } catch (error) {
    if (error instanceof ApiError && error.code === 'DEACTIVATION_BLOCKED' && isCheck(error.details)) {
      check.value = error.details
      loadError.value = ''
    } else {
      loadError.value = errorMessage(error)
    }
  } finally {
    confirming.value = false
  }
}

function isCheck(value: unknown): value is DeactivationCheck {
  return typeof value === 'object' && value !== null && 'blocked' in value && 'groups' in value
}

watch(
  () => [props.visible, props.node?.id] as const,
  ([visible]) => {
    if (visible && props.node) void runCheck()
  },
)
</script>

<template>
  <el-dialog :model-value="visible" title="停用前收口检查" width="min(760px, 94vw)" @update:model-value="emit('update:visible', $event)">
    <div v-if="node" class="gate-intro">
      <div>
        <strong>{{ node.node_code }} · {{ node.name }}</strong>
        <span>节点当前为<strong>在用</strong>状态；下列收口项全部清零后才能停用，被挡住时节点保持原状态。</span>
      </div>
      <el-button :loading="loading" :disabled="confirming" @click="runCheck"><RefreshCw :size="14" />重新检查</el-button>
    </div>

    <el-alert v-if="loadError" :title="loadError" type="error" :closable="false" show-icon class="gate-alert" />

    <div v-loading="loading" class="gate-body">
      <template v-if="check">
        <el-alert
          :closable="false"
          show-icon
          :type="check.blocked ? 'error' : 'success'"
          class="gate-verdict"
          :title="check.blocked ? `检查未通过：仍有 ${check.total_items} 项收口阻塞，节点不能停用` : '检查通过：未接受偏差、过期保护层与待确认覆盖均已清零'"
        >
          <template #default>
            <span v-if="check.blocked" class="verdict-copy">
              <AlertTriangle :size="13" /> 请联系对应责任团队完成收口后重新检查；审计记录将保留本次拦截证据。
            </span>
            <span v-else class="verdict-copy"><CheckCircle2 :size="13" /> 可以继续停用，停用动作与原有流程一致。</span>
          </template>
        </el-alert>

        <section v-for="group in groups" :key="group.key" class="gate-group" :class="{ blocked: group.count > 0 }">
          <header>
            <div>
              <ShieldOff v-if="group.count > 0" :size="15" class="gate-danger-icon" />
              <CheckCircle2 v-else :size="15" class="gate-ok-icon" />
              <strong>{{ deactivationGateMeta[group.key].label }}</strong>
              <span class="gate-count">{{ group.count }} 项</span>
            </div>
            <div v-if="group.count > 0" class="gate-summary">
              <span>最高风险 <RiskBadge :rank="group.highest_rank" :score="group.highest_risk" /></span>
              <span>责任团队：{{ group.owner_teams.join('、') || '未分配' }}</span>
            </div>
          </header>
          <p class="gate-description">{{ deactivationGateMeta[group.key].description }}</p>
          <ul v-if="group.count > 0" class="gate-items">
            <li v-for="item in group.items" :key="`${group.key}-${item.id}`">
              <div class="gate-item-main">
                <strong>{{ item.title }}</strong>
                <small v-if="item.detail">{{ item.detail }}</small>
              </div>
              <div class="gate-item-meta">
                <span v-if="item.scenario_id" class="gate-scenario-id">场景 #{{ item.scenario_id }}</span>
                <span v-if="item.state" class="state-label" :class="item.state">{{ itemStateLabel(group.key, item.state) }}</span>
                <RiskBadge :rank="item.risk_rank" :score="item.risk_score" />
                <span v-if="group.key === 'expired_safeguards'" class="date-cell" :class="{ expired: true }">有效期至 {{ expiryLabel(item.verification_expires_at) }}</span>
              </div>
            </li>
          </ul>
          <p v-else class="gate-empty">{{ deactivationGateMeta[group.key].emptyHint }}</p>
        </section>
      </template>
    </div>

    <template #footer>
      <el-button :disabled="confirming" @click="close">关闭</el-button>
      <el-tooltip v-if="check?.blocked" content="请先清零全部收口阻塞项" placement="top">
        <span><el-button type="danger" disabled>确认停用</el-button></span>
      </el-tooltip>
      <el-button v-else type="danger" :loading="confirming" :disabled="loading || !check" @click="confirmDeactivate">
        检查通过，确认停用
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.gate-intro { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; margin-bottom: 14px; }
.gate-intro > div:first-child { display: grid; gap: 4px; }
.gate-intro strong { font-size: .92rem; }
.gate-intro span { color: var(--muted); font-size: .74rem; line-height: 1.5; }
.gate-alert { margin-bottom: 12px; }
.gate-verdict :deep(.el-alert__content) { display: grid; gap: 3px; }
.verdict-copy { display: inline-flex; align-items: center; gap: 5px; font-size: .72rem; }
.gate-body { display: grid; gap: 12px; min-height: 120px; }
.gate-group { padding: 12px 14px; background: #f2f6f4; border: 1px solid var(--line); border-left: 3px solid #9bb3aa; }
.gate-group.blocked { background: #fdf7f5; border-color: #ddb5b1; border-left-color: var(--red); }
.gate-group header { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
.gate-group header > div { display: flex; align-items: center; gap: 8px; }
.gate-danger-icon { color: var(--red); }
.gate-ok-icon { color: var(--green); }
.gate-count { padding: 2px 7px; border: 1px solid var(--line-strong); border-radius: 3px; font-size: .65rem; font-weight: 800; }
.gate-group.blocked .gate-count { color: var(--red); border-color: #d29a97; background: var(--red-soft); }
.gate-summary { display: flex; align-items: center; gap: 12px; color: var(--muted); font-size: .68rem; }
.gate-description { margin: 7px 0 0; color: var(--muted); font-size: .7rem; line-height: 1.5; }
.gate-items { margin: 10px 0 0; padding: 0; list-style: none; display: grid; gap: 6px; }
.gate-items li { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 8px 10px; background: var(--paper); border: 1px solid var(--line); }
.gate-item-main { min-width: 0; display: grid; gap: 2px; }
.gate-item-main strong { font-size: .75rem; }
.gate-item-main small { overflow: hidden; color: var(--muted); font-size: .66rem; text-overflow: ellipsis; white-space: nowrap; }
.gate-item-meta { display: flex; align-items: center; gap: 7px; flex-shrink: 0; flex-wrap: wrap; justify-content: flex-end; }
.gate-scenario-id { color: var(--blue); font-size: .65rem; font-weight: 750; }
.gate-empty { margin: 9px 0 0; color: var(--green); font-size: .7rem; }
</style>
