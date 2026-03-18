<template>
  <div class="propagation-page">
    <div class="page-header">
      <h2>变更传播</h2>
      <p class="subtitle">修改角色、世界观、大纲等元素后，AI 自动分析影响范围并提供精准的章节修订计划</p>
    </div>

    <el-row :gutter="20" class="main-layout">
      <!-- LEFT: create change event + event history -->
      <el-col :span="8">
        <el-card class="panel">
          <template #header>
            <span>发起变更</span>
          </template>

          <el-form :model="changeForm" label-position="top" size="small">
            <el-form-item label="实体类型">
              <el-select v-model="changeForm.entity_type" placeholder="选择修改的元素类型" style="width:100%">
                <el-option label="角色" value="character" />
                <el-option label="世界观/世界圣经" value="world_bible" />
                <el-option label="大纲节点" value="outline" />
                <el-option label="伏笔" value="foreshadowing" />
                <el-option label="整书蓝图" value="blueprint" />
              </el-select>
            </el-form-item>

            <el-form-item label="实体 ID">
              <el-input
                v-model="changeForm.entity_id"
                placeholder="粘贴对应实体的 UUID"
              />
              <div class="hint">提示：在对应管理页面点击实体可复制其 ID</div>
            </el-form-item>

            <el-form-item label="变更摘要（必填）">
              <el-input
                v-model="changeForm.change_summary"
                type="textarea"
                :rows="3"
                placeholder="用一两句话描述改了什么，例如：将主角林晨的性格改为更内向、稳重，减少冲动行事的场景"
              />
            </el-form-item>

            <el-form-item label="旧内容（可选 JSON）">
              <el-input
                v-model="changeForm.old_snapshot_str"
                type="textarea"
                :rows="3"
                placeholder='{"name":"林晨","personality":"冲动热血"}'
              />
            </el-form-item>

            <el-form-item label="新内容（可选 JSON）">
              <el-input
                v-model="changeForm.new_snapshot_str"
                type="textarea"
                :rows="3"
                placeholder='{"name":"林晨","personality":"内向稳重"}'
              />
            </el-form-item>

            <el-button
              type="primary"
              :loading="analyzing"
              :disabled="!changeForm.entity_type || !changeForm.entity_id || !changeForm.change_summary"
              style="width:100%"
              @click="submitChange"
            >
              {{ analyzing ? 'AI 正在分析影响…' : '提交变更 & 分析影响' }}
            </el-button>
          </el-form>
        </el-card>

        <!-- Change event history -->
        <el-card class="panel" style="margin-top:16px">
          <template #header>
            <span>变更历史</span>
            <el-button size="small" text @click="loadEvents" style="float:right">刷新</el-button>
          </template>

          <div v-if="events.length === 0" class="empty-state">暂无变更记录</div>
          <div v-else class="event-list">
            <div
              v-for="ev in events"
              :key="ev.id"
              class="event-item"
              :class="{ active: selectedEventId === ev.id }"
              @click="selectEvent(ev)"
            >
              <div class="event-type">
                <el-tag size="small" :type="entityTagType(ev.entity_type)">{{ entityLabel(ev.entity_type) }}</el-tag>
                <el-tag size="small" :type="statusTagType(ev.status)" style="margin-left:4px">{{ statusLabel(ev.status) }}</el-tag>
              </div>
              <div class="event-summary">{{ ev.change_summary }}</div>
              <div class="event-time">{{ formatTime(ev.created_at) }}</div>
            </div>
          </div>
        </el-card>
      </el-col>

      <!-- RIGHT: patch plan detail -->
      <el-col :span="16">
        <el-card v-if="!currentPlan" class="panel placeholder-panel">
          <div class="placeholder-text">
            <el-icon size="48"><Connection /></el-icon>
            <p>提交变更后，AI 将分析受影响的章节并生成修订计划</p>
            <p class="hint">或从左侧选择历史变更事件查看其计划</p>
          </div>
        </el-card>

        <el-card v-else class="panel">
          <template #header>
            <div class="plan-header">
              <span>修订计划</span>
              <div>
                <el-tag :type="planStatusType(currentPlan.status)">{{ planStatusLabel(currentPlan.status) }}</el-tag>
                <span class="plan-progress">{{ currentPlan.done_items }}/{{ currentPlan.total_items }}</span>
              </div>
            </div>
          </template>

          <div class="impact-summary">
            <el-icon><InfoFilled /></el-icon>
            <span>{{ currentPlan.impact_summary }}</span>
          </div>

          <div v-if="currentPlan.items && currentPlan.items.length === 0" class="empty-state">
            本次变更不影响任何已生成章节
          </div>

          <div v-else class="items-list">
            <div
              v-for="item in currentPlan.items"
              :key="item.id"
              class="patch-item"
              :class="item.status"
            >
              <div class="item-header">
                <div class="item-meta">
                  <el-tag size="small">{{ itemTypeLabel(item.item_type) }}</el-tag>
                  <span class="item-order">#{{ item.item_order }}</span>
                  <el-tag size="small" :type="itemStatusType(item.status)">{{ itemStatusLabel(item.status) }}</el-tag>
                </div>
                <div class="item-actions">
                  <template v-if="item.status === 'pending'">
                    <el-button size="small" type="success" @click="approveItem(item)">批准</el-button>
                    <el-button size="small" @click="skipItem(item)">跳过</el-button>
                  </template>
                  <template v-else-if="item.status === 'approved'">
                    <el-button
                      size="small"
                      type="primary"
                      :loading="executingItemId === item.id"
                      @click="executeItem(item)"
                    >
                      {{ executingItemId === item.id ? '修改中…' : '执行修改' }}
                    </el-button>
                    <el-button size="small" @click="skipItem(item)">跳过</el-button>
                  </template>
                  <template v-else-if="item.status === 'skipped' || item.status === 'failed'">
                    <el-button size="small" text @click="resetItem(item)">重置</el-button>
                  </template>
                </div>
              </div>

              <div class="item-body">
                <div class="impact-desc">
                  <strong>影响原因：</strong>{{ item.impact_description }}
                </div>
                <div class="patch-instruction">
                  <strong>修改指令：</strong>{{ item.patch_instruction }}
                </div>
              </div>

              <div v-if="item.status === 'done' && item.result_snapshot" class="item-result">
                <el-icon><Check /></el-icon> 修改完成（预览：{{ resultPreview(item.result_snapshot) }}）
              </div>
            </div>
          </div>

          <div class="plan-actions" v-if="currentPlan.items && currentPlan.items.length > 0">
            <el-button
              type="primary"
              :disabled="!hasPendingItems"
              @click="approveAll"
            >全部批准</el-button>
            <el-button
              type="success"
              :loading="executing"
              :disabled="!hasApprovedItems"
              @click="executeAll"
            >执行所有已批准项</el-button>
            <el-button @click="loadPlan(currentPlan.id)">刷新状态</el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Connection, InfoFilled, Check } from '@element-plus/icons-vue'
import { propagationApi } from '@/api/index'

const route = useRoute()
const projectId = computed(() => route.params.projectId as string)

// ── form state ────────────────────────────────────────────────────────────────
const changeForm = ref({
  entity_type: '',
  entity_id: '',
  change_summary: '',
  old_snapshot_str: '',
  new_snapshot_str: '',
})

const analyzing = ref(false)
const executing = ref(false)
const executingItemId = ref<string | null>(null)

// ── events + plan ─────────────────────────────────────────────────────────────
interface ChangeEvent {
  id: string
  entity_type: string
  entity_id: string
  change_summary: string
  status: string
  created_at: string
}

interface PatchItem {
  id: string
  plan_id: string
  item_type: string
  item_id: string
  item_order: number
  impact_description: string
  patch_instruction: string
  status: string
  result_snapshot: Record<string, string> | null
}

interface PatchPlan {
  id: string
  change_event_id: string
  impact_summary: string
  total_items: number
  done_items: number
  status: string
  items: PatchItem[]
}

const events = ref<ChangeEvent[]>([])
const selectedEventId = ref<string | null>(null)
const currentPlan = ref<PatchPlan | null>(null)

onMounted(() => loadEvents())

async function loadEvents() {
  try {
    const resp = await propagationApi.listChangeEvents(projectId.value)
    events.value = resp.data.data ?? []
  } catch {
    ElMessage.error('加载变更历史失败')
  }
}

async function selectEvent(ev: ChangeEvent) {
  selectedEventId.value = ev.id
  // Load the plan for this event by fetching events first is expensive;
  // re-create change event will just create a new one, so instead we'll trigger
  // a createChangeEvent response which returns the plan. Since we already have
  // existing events, we need to get the plan another way.
  // For existing events, we don't have a direct event→plan endpoint, so we re-run
  // a lightweight "get plan for event" by listing plans.
  // Workaround: store plan_id in the event list by adding it to the backend later,
  // OR for now just display a message asking the user to re-submit if they need to
  // re-view the plan.
  // Better approach: use the CreateChangeEvent response plan_id stored in localStorage.
  const planId = localStorage.getItem(`plan_for_event_${ev.id}`)
  if (planId) {
    await loadPlan(planId)
  } else {
    ElMessage.info('请重新提交变更以查看详细计划，或使用历史记录中的最新计划')
  }
}

async function submitChange() {
  analyzing.value = true
  try {
    let oldSnap: unknown = undefined
    let newSnap: unknown = undefined
    if (changeForm.value.old_snapshot_str.trim()) {
      try { oldSnap = JSON.parse(changeForm.value.old_snapshot_str) } catch {
        ElMessage.warning('旧内容 JSON 格式有误，已忽略')
      }
    }
    if (changeForm.value.new_snapshot_str.trim()) {
      try { newSnap = JSON.parse(changeForm.value.new_snapshot_str) } catch {
        ElMessage.warning('新内容 JSON 格式有误，已忽略')
      }
    }
    const resp = await propagationApi.createChangeEvent(projectId.value, {
      entity_type: changeForm.value.entity_type,
      entity_id: changeForm.value.entity_id,
      change_summary: changeForm.value.change_summary,
      old_snapshot: oldSnap,
      new_snapshot: newSnap,
    })
    const plan: PatchPlan = resp.data.data
    currentPlan.value = plan
    // remember plan_id for this event so the user can click back to it
    localStorage.setItem(`plan_for_event_${plan.change_event_id}`, plan.id)
    ElMessage.success(`分析完成：${plan.impact_summary}`)
    await loadEvents()
    // reset form
    changeForm.value = { entity_type: '', entity_id: '', change_summary: '', old_snapshot_str: '', new_snapshot_str: '' }
  } catch (e: unknown) {
    const msg = (e as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '分析失败'
    ElMessage.error(msg)
  } finally {
    analyzing.value = false
  }
}

async function loadPlan(planId: string) {
  try {
    const resp = await propagationApi.getPatchPlan(planId)
    currentPlan.value = resp.data.data
  } catch {
    ElMessage.error('加载计划失败')
  }
}

// ── item actions ──────────────────────────────────────────────────────────────
async function approveItem(item: PatchItem) {
  await propagationApi.updateItemStatus(item.id, 'approved')
  item.status = 'approved'
}

async function skipItem(item: PatchItem) {
  await propagationApi.updateItemStatus(item.id, 'skipped')
  item.status = 'skipped'
}

async function resetItem(item: PatchItem) {
  await propagationApi.updateItemStatus(item.id, 'pending')
  item.status = 'pending'
}

async function executeItem(item: PatchItem) {
  executingItemId.value = item.id
  item.status = 'executing'
  try {
    await propagationApi.executeItem(item.id)
    ElMessage.success('章节修改完成')
    if (currentPlan.value) await loadPlan(currentPlan.value.id)
  } catch (e: unknown) {
    const msg = (e as { response?: { data?: { error?: string } } })?.response?.data?.error ?? '执行失败'
    ElMessage.error(msg)
    item.status = 'approved'
  } finally {
    executingItemId.value = null
  }
}

const hasPendingItems = computed(() =>
  currentPlan.value?.items?.some(i => i.status === 'pending') ?? false)

const hasApprovedItems = computed(() =>
  currentPlan.value?.items?.some(i => i.status === 'approved') ?? false)

async function approveAll() {
  if (!currentPlan.value) return
  const pending = currentPlan.value.items.filter(i => i.status === 'pending')
  await Promise.all(pending.map(i => propagationApi.updateItemStatus(i.id, 'approved')))
  pending.forEach(i => { i.status = 'approved' })
  ElMessage.success(`已批准 ${pending.length} 项`)
}

async function executeAll() {
  if (!currentPlan.value) return
  executing.value = true
  const approved = currentPlan.value.items.filter(i => i.status === 'approved')
  for (const item of approved) {
    executingItemId.value = item.id
    item.status = 'executing'
    try {
      await propagationApi.executeItem(item.id)
    } catch {
      ElMessage.error(`第 ${item.item_order} 项执行失败，已跳过`)
    }
  }
  executingItemId.value = null
  executing.value = false
  ElMessage.success('批量执行完成')
  if (currentPlan.value) await loadPlan(currentPlan.value.id)
}

// ── helpers ───────────────────────────────────────────────────────────────────
function entityLabel(t: string) {
  return { character: '角色', world_bible: '世界观', outline: '大纲', foreshadowing: '伏笔', blueprint: '蓝图' }[t] ?? t
}
function entityTagType(t: string) {
  return { character: 'success', world_bible: 'info', outline: 'warning', foreshadowing: 'danger', blueprint: '' }[t] ?? ''
}
function statusLabel(s: string) {
  return { pending: '待分析', analyzed: '已分析', patching: '修改中', done: '完成', cancelled: '已取消' }[s] ?? s
}
function statusTagType(s: string) {
  return { pending: 'info', analyzed: 'warning', patching: 'warning', done: 'success', cancelled: 'danger' }[s] ?? ''
}
function planStatusLabel(s: string) {
  return { ready: '待执行', executing: '执行中', done: '已完成', cancelled: '已取消' }[s] ?? s
}
function planStatusType(s: string) {
  return { ready: 'warning', executing: 'info', done: 'success', cancelled: 'danger' }[s] ?? ''
}
function itemTypeLabel(t: string) {
  return { chapter: '章节', outline: '大纲', foreshadowing: '伏笔' }[t] ?? t
}
function itemStatusLabel(s: string) {
  return { pending: '待审', approved: '已批准', executing: '执行中', done: '完成', skipped: '已跳过', failed: '失败' }[s] ?? s
}
function itemStatusType(s: string) {
  return { pending: 'info', approved: 'warning', executing: '', done: 'success', skipped: 'info', failed: 'danger' }[s] ?? ''
}
function formatTime(t: string) {
  return new Date(t).toLocaleString('zh-CN', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}
function resultPreview(snap: Record<string, string> | null) {
  if (!snap) return ''
  return snap.preview ?? ''
}
</script>

<style scoped>
.propagation-page {
  padding: 20px;
  color: #e0e0e0;
}

.page-header {
  margin-bottom: 20px;
}

.page-header h2 {
  font-size: 1.4rem;
  margin-bottom: 4px;
}

.subtitle {
  color: #a0a0b0;
  font-size: 0.85rem;
}

.hint {
  color: #808090;
  font-size: 0.78rem;
  margin-top: 2px;
}

.panel {
  background: #1a1a2e;
  border: 1px solid #2a2a4a;
}

.main-layout {
  align-items: flex-start;
}

.empty-state {
  color: #606070;
  text-align: center;
  padding: 20px;
}

.event-list {
  max-height: 400px;
  overflow-y: auto;
}

.event-item {
  padding: 10px;
  border-radius: 6px;
  cursor: pointer;
  border: 1px solid transparent;
  margin-bottom: 8px;
  transition: background 0.2s;
}

.event-item:hover {
  background: #252540;
}

.event-item.active {
  border-color: #409eff;
  background: #1e2a3a;
}

.event-type {
  margin-bottom: 4px;
}

.event-summary {
  font-size: 0.85rem;
  color: #c0c0d0;
  margin-bottom: 4px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.event-time {
  font-size: 0.75rem;
  color: #707080;
}

.placeholder-panel {
  min-height: 400px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.placeholder-text {
  text-align: center;
  color: #606070;
  padding: 40px;
}

.placeholder-text p {
  margin: 8px 0;
}

.plan-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.plan-progress {
  margin-left: 8px;
  font-size: 0.85rem;
  color: #a0a0b0;
}

.impact-summary {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  background: #1e2a3a;
  border-radius: 6px;
  margin-bottom: 16px;
  font-size: 0.88rem;
  color: #c0d0e0;
}

.items-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  max-height: 520px;
  overflow-y: auto;
}

.patch-item {
  border: 1px solid #2a2a4a;
  border-radius: 8px;
  padding: 12px;
  transition: border-color 0.2s;
}

.patch-item.approved { border-color: #e6a23c44; }
.patch-item.executing { border-color: #409eff66; }
.patch-item.done { border-color: #67c23a44; }
.patch-item.skipped { opacity: 0.5; }
.patch-item.failed { border-color: #f56c6c44; }

.item-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}

.item-meta {
  display: flex;
  align-items: center;
  gap: 6px;
}

.item-order {
  font-size: 0.8rem;
  color: #808090;
}

.item-body {
  font-size: 0.83rem;
  color: #b0b0c0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.impact-desc, .patch-instruction {
  line-height: 1.5;
}

.item-result {
  margin-top: 8px;
  font-size: 0.8rem;
  color: #67c23a;
  display: flex;
  align-items: center;
  gap: 4px;
}

.plan-actions {
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid #2a2a4a;
  display: flex;
  gap: 8px;
}
</style>
