<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { AlertTriangle, CheckCircle2, RefreshCw, ShieldOff } from 'lucide-vue-next'
import { ElMessage } from 'element-plus'
import RiskBadge from './RiskBadge.vue'
import { ApiError, errorMessage } from '../../api/client'
import { deactivateProcessNode, getDeactivationCheck } from '../../api/process-node'
import type { ClosureCategory, ClosureItem, DeactivationCheck, ProcessNode } from '../../types/process-node'

const props = defineProps<{ modelValue: boolean; node: ProcessNode | null }>()
const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
  (e: 'deactivated', node: ProcessNode): void
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (value) => emit('update:modelValue', value),
})

const check = ref<DeactivationCheck>()
const loading = ref(false)
const submitting = ref(false)

const scenarioStateLabels: Record<string, string> = {
  draft: '草稿', analyzed: '已分析', verified: '已复核', rework: '退回修订', accepted: '已接受',
}
const coverageStateLabels: Record<string, string> = {
  never_run: '未评估', queued: '排队中', running: '计算中',
  completed: '待确认', failed: '计算失败', confirmed: '已确认', voided: '已作废',
}
const safeguardStateLabels: Record<string, string> = {
  pending: '待验证', active: '在册但验证已过期', expired: '已过期', invalid: '已失效',
}
const reasonLabels: Record<string, string> = {
  deviation_not_accepted: '偏差未收口：状态尚未达到“已接受”',
  coverage_not_evaluated: '覆盖评估从未运行，无结论可确认',
  coverage_not_confirmed: '最新覆盖评估尚未经人工确认',
  never_verified: '从未记录验证证据',
  test_interval_invalid: '测试周期配置无效',
  verification_expired: '验证有效期已超期',
}

watch(() => [props.modelValue, props.node?.id] as const, ([open]) => {
  if (open && props.node) void refresh()
})

async function refresh() {
  if (!props.node) return
  loading.value = true
  try {
    check.value = await getDeactivationCheck(props.node.id)
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    loading.value = false
  }
}

async function confirmDeactivate() {
  if (!props.node || !check.value?.can_deactivate) return
  submitting.value = true
  try {
    const updated = await deactivateProcessNode(props.node.id)
    ElMessage.success('收口检查通过，节点已停用')
    visible.value = false
    emit('deactivated', updated)
  } catch (error) {
    if (error instanceof ApiError && error.code === 'CLOSURE_CHECK_BLOCKED' && error.details) {
      check.value = error.details as DeactivationCheck
      ElMessage.warning('收口检查未通过，节点保持“在用”，请先清理阻塞项')
    } else {
      ElMessage.error(errorMessage(error))
    }
  } finally {
    submitting.value = false
  }
}

function stateLabel(item: ClosureItem) {
  return scenarioStateLabels[item.state] ?? coverageStateLabels[item.state] ??
    safeguardStateLabels[item.state] ?? item.state
}
function categoryIcon(category: ClosureCategory) { return category.key === 'expired_safeguard' ? ShieldOff : AlertTriangle }
</script>

<template>
  <el-dialog v-model="visible" :title="`停用前收口检查 · ${node?.node_code ?? ''}`" width="min(760px, 96vw)">
    <div v-loading="loading" class="closure-gate">
      <p v-if="node" class="gate-intro">
        停用 <strong>{{ node.node_code }} {{ node.name }}</strong> 前必须完成偏差、保护层、覆盖评估三类收口。检查时点之后任何变更都需重新检查。
      </p>
      <el-alert
        v-if="check && check.can_deactivate"
        type="success" :closable="false" show-icon
        title="收口检查通过：未接受偏差、过期保护层、待确认覆盖均已清零，可以停用。"
      />
      <el-alert
        v-else-if="check"
        type="error" :closable="false" show-icon
        :title="`收口检查未通过：共 ${check.blocking_item_count} 项阻塞，节点将保持“在用”。请联系对应责任团队清理后重新检查。`"
      />
      <div v-if="check" class="category-list">
        <section v-for="category in check.categories" :key="category.key" class="closure-category" :class="{ blocked: category.blocked }">
          <header class="category-head">
            <div class="category-title">
              <component :is="categoryIcon(category)" :size="16" />
              <strong>{{ category.label }}</strong>
              <el-tag size="small" :type="category.blocked ? 'danger' : 'success'" effect="plain">
                {{ category.blocked ? `${category.count} 项阻塞` : '已清零' }}
              </el-tag>
            </div>
            <div v-if="category.blocked" class="category-meta">
              <RiskBadge :rank="category.risk_rank" :score="category.highest_risk" />
              <span class="owner-team">责任团队：{{ category.owner_team }}</span>
            </div>
          </header>
          <ul v-if="category.blocked" class="item-list">
            <li v-for="item in category.items" :key="`${category.key}-${item.id}`" class="closure-item">
              <div class="item-main">
                <strong>{{ item.reference }}</strong>
                <small>{{ reasonLabels[item.reason] ?? item.reason }}</small>
              </div>
              <div class="item-side">
                <span class="state-label" :class="item.state">{{ stateLabel(item) }}</span>
                <RiskBadge :rank="item.risk_rank" :score="item.risk_score" />
              </div>
            </li>
          </ul>
          <p v-else class="category-clear"><CheckCircle2 :size="14" />该类项目已全部收口</p>
        </section>
      </div>
    </div>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button :loading="loading" @click="refresh"><RefreshCw :size="15" />重新检查</el-button>
      <el-button
        type="danger" :disabled="!check?.can_deactivate" :loading="submitting"
        @click="confirmDeactivate"
      >确认停用</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.gate-intro { margin: 0 0 12px; color: var(--muted); font-size: .8rem; line-height: 1.6; }
.category-list { display: grid; gap: 12px; margin-top: 12px; }
.closure-category { border: 1px solid var(--line, #d8dedc); border-radius: 6px; padding: 12px 14px; background: #fafbfa; }
.closure-category.blocked { border-color: #e0b4b4; background: #fdf7f7; }
.category-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
.category-title { display: flex; align-items: center; gap: 8px; color: var(--ink, #2a3330); }
.category-meta { display: flex; align-items: center; gap: 10px; }
.owner-team { color: var(--muted); font-size: .68rem; font-weight: 700; }
.item-list { list-style: none; margin: 10px 0 0; padding: 0; display: grid; gap: 8px; }
.closure-item { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 8px 10px; background: #fff; border: 1px solid var(--line, #e3e8e6); border-radius: 4px; }
.item-main { display: grid; gap: 2px; min-width: 0; }
.item-main strong { font-size: .78rem; color: var(--ink, #2a3330); }
.item-main small { color: var(--muted); font-size: .68rem; }
.item-side { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }
.category-clear { display: flex; align-items: center; gap: 6px; margin: 8px 0 0; color: #1e6044; font-size: .7rem; font-weight: 700; }
</style>
