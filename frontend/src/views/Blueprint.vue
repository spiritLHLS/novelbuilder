<template>
  <div class="blueprint">
    <div class="page-header">
      <h1>蓝图管理</h1>
      <el-button type="primary" @click="generateBlueprint" :loading="generating"
        :disabled="generating || currentBlueprint?.status === 'approved'">
        <el-icon><MagicStick /></el-icon>生成蓝图
      </el-button>
    </div>

    <!-- No Blueprint -->
    <el-empty v-if="!currentBlueprint && !generating" description="尚未生成蓝图">
      <el-button type="primary" @click="generateBlueprint" :loading="generating">
        一键生成完整蓝图
      </el-button>
      <p style="color: #888; margin-top: 12px; font-size: 13px;">
        蓝图会根据世界设定自动生成世界圣经、大纲、角色、伏笔和卷册规划（至少4卷）
      </p>
    </el-empty>

    <!-- Generating Progress -->
    <el-card v-if="generating" shadow="hover" style="text-align: center; padding: 40px 20px;">
      <el-icon :size="48" class="is-loading" style="color: #409eff;"><Loading /></el-icon>
      <h3 :style="{ color: 'var(--nb-text-primary)', marginTop: '16px' }">正在生成蓝图...</h3>
      <p style="color: #888; margin-bottom: 24px;">AI正在构建世界设定、角色体系、故事大纲和卷册结构，请稍候</p>
      <el-steps :active="generatingStep" align-center style="max-width: 600px; margin: 0 auto;">
        <el-step title="初始化" description="创建任务" />
        <el-step title="AI创作" description="调用大模型" />
        <el-step title="解析数据" description="处理返回内容" />
        <el-step title="写入数据库" description="保存世界圣经/角色/大纲" />
        <el-step title="完成" description="蓝图就绪" />
      </el-steps>
    </el-card>

    <!-- Generation Failed -->
    <el-alert v-if="generationError" type="error" :title="'蓝图生成失败: ' + generationError"
      show-icon closable style="margin-bottom: 16px;" @close="generationError = ''" />

    <!-- Blueprint Content -->
    <template v-if="currentBlueprint && !generating">
      <!-- Status Bar -->
      <el-card shadow="hover" class="status-bar">
        <el-row :gutter="20" align="middle">
          <el-col :span="6">
            <div class="status-label">蓝图状态</div>
            <el-tag :type="blueprintStatusType" size="large">{{ blueprintStatusLabel }}</el-tag>
          </el-col>
          <el-col :span="6">
            <div class="status-label">创建时间</div>
            <div class="status-value">{{ formatDate(currentBlueprint.created_at) }}</div>
          </el-col>
          <el-col :span="12" style="text-align: right;">
            <el-button v-if="currentBlueprint.status === 'draft'"
              type="success" @click="submitReview">提交审核</el-button>
            <el-button v-if="currentBlueprint.status === 'pending_review'"
              type="success" @click="approveBlueprint">批准</el-button>
            <el-button v-if="currentBlueprint.status === 'pending_review'"
              type="danger" @click="rejectBlueprint">驳回</el-button>
            <el-button v-if="currentBlueprint.status === 'rejected' || currentBlueprint.status === 'failed'"
              type="warning" @click="regenerateBlueprint">重新生成</el-button>
          </el-col>
        </el-row>
      </el-card>

      <!-- Asset Overview -->
      <el-row :gutter="20" style="margin-top: 20px;">
        <el-col :span="6">
          <el-card shadow="hover" class="asset-card">
            <el-statistic title="世界设定" :value="worldBibleCount" suffix="项">
              <template #prefix><el-icon style="color: #409eff;"><Document /></el-icon></template>
            </el-statistic>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="hover" class="asset-card">
            <el-statistic title="角色数量" :value="characterCount">
              <template #prefix><el-icon style="color: #e6a23c;"><User /></el-icon></template>
            </el-statistic>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="hover" class="asset-card">
            <el-statistic title="大纲节点" :value="outlineCount">
              <template #prefix><el-icon style="color: #67c23a;"><List /></el-icon></template>
            </el-statistic>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="hover" class="asset-card">
            <el-statistic title="伏笔数量" :value="foreshadowingCount">
              <template #prefix><el-icon style="color: #f56c6c;"><Connection /></el-icon></template>
            </el-statistic>
          </el-card>
        </el-col>
      </el-row>

      <!-- Volume Plan -->
      <el-card shadow="hover" style="margin-top: 20px;">
        <template #header><span>卷册规划</span></template>
        <el-table :data="volumes" style="width: 100%;">
          <el-table-column prop="volume_num" label="卷号" width="80" />
          <el-table-column prop="title" label="卷名" />
          <el-table-column prop="chapter_start" label="起始章" width="100" />
          <el-table-column prop="chapter_end" label="结束章" width="100" />
          <el-table-column prop="status" label="状态" width="120">
            <template #default="{ row }">
              <el-tag :type="row.status === 'approved' ? 'success' : 'info'" size="small">
                {{ row.status === 'approved' ? '已批准' : row.status }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="200">
            <template #default="{ row }">
              <el-button v-if="row.status === 'pending_review'" size="small" type="success"
                @click="approveVolume(row.id)">批准</el-button>
              <el-button v-if="row.status === 'pending_review'" size="small" type="danger"
                @click="rejectVolume(row.id)">驳回</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-card>

      <!-- Blueprint Raw Content -->
      <el-card shadow="hover" style="margin-top: 20px;">
        <template #header><span>蓝图详情</span></template>
        <el-descriptions :column="1" border style="margin-bottom: 16px;" v-if="currentBlueprint.master_outline">
          <el-descriptions-item label="总体大纲">
            <span style="white-space: pre-wrap; font-size: 13px;">{{ formatBlueprintField(currentBlueprint.master_outline) }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="角色关系图" v-if="currentBlueprint.relation_graph">
            <span style="white-space: pre-wrap; font-size: 13px;">{{ formatBlueprintField(currentBlueprint.relation_graph) }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="全局时间线" v-if="currentBlueprint.global_timeline">
            <span style="white-space: pre-wrap; font-size: 13px;">{{ formatBlueprintField(currentBlueprint.global_timeline) }}</span>
          </el-descriptions-item>
        </el-descriptions>
        <pre class="blueprint-content">{{ JSON.stringify({
          master_outline: currentBlueprint.master_outline,
          relation_graph: currentBlueprint.relation_graph,
          global_timeline: currentBlueprint.global_timeline,
        }, null, 2) }}</pre>
      </el-card>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { blueprintApi, volumeApi, worldBibleApi, characterApi, outlineApi, foreshadowingApi } from '@/api'

const route = useRoute()
const projectId = route.params.projectId as string

const currentBlueprint = ref<any>(null)
const generating = ref(false)
const generatingStep = ref(0)
const generationError = ref('')
const volumes = ref<any[]>([])

const worldBibleCount = ref(0)
const characterCount = ref(0)
const outlineCount = ref(0)
const foreshadowingCount = ref(0)

let pollTimer: ReturnType<typeof setInterval> | null = null

function stopPolling() {
  if (pollTimer !== null) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

async function pollBlueprintStatus() {
  try {
    const res = await blueprintApi.get(projectId)
    const bp = res?.data?.data
    if (!bp) return
    currentBlueprint.value = bp
    // Advance the step indicator while waiting
    if (generatingStep.value < 3) generatingStep.value++
    if (bp.status !== 'generating') {
      stopPolling()
      generating.value = false
      if (bp.status === 'failed') {
        generationError.value = bp.error_message || '未知错误'
        currentBlueprint.value = null
        ElMessage.error('蓝图生成失败')
      } else {
        generatingStep.value = 4
        ElMessage.success('蓝图生成完成')
        await fetchAll()
      }
    }
  } catch {
    // Network error – keep polling
  }
}

const blueprintStatusType = computed(() => {
  const map: Record<string, string> = {
    generating: 'info', draft: 'info', pending_review: 'warning', approved: 'success', rejected: 'danger', failed: 'danger',
  }
  return (map[currentBlueprint.value?.status] || 'info') as any
})

const blueprintStatusLabel = computed(() => {
  const map: Record<string, string> = {
    generating: '生成中', draft: '草稿', pending_review: '待审核', approved: '已批准', rejected: '已驳回', failed: '生成失败',
  }
  return map[currentBlueprint.value?.status] || currentBlueprint.value?.status
})

function formatDate(d: string) {
  return d ? new Date(d).toLocaleString('zh-CN') : '-'
}

function formatBlueprintField(val: any): string {
  if (val == null) return ''
  if (typeof val === 'string') return val
  return JSON.stringify(val, null, 2)
}

onMounted(async () => {
  await fetchAll()
  // If a generation is already in progress (e.g., after page refresh) resume polling.
  if (currentBlueprint.value?.status === 'generating') {
    generating.value = true
    generatingStep.value = 1
    pollTimer = setInterval(pollBlueprintStatus, 3000)
  } else if (currentBlueprint.value?.status === 'failed') {
    // Show the error from the previous attempt and allow the user to retry.
    generationError.value = currentBlueprint.value.error_message || '蓝图生成失败，请重试'
    currentBlueprint.value = null
  }
})

onBeforeUnmount(stopPolling)

async function fetchAll() {
  try {
    const [bpRes, volRes] = await Promise.all([
      blueprintApi.get(projectId).catch(() => null),
      volumeApi.list(projectId).catch(() => ({ data: { data: [] } })),
    ])
    if (bpRes?.data?.data) currentBlueprint.value = bpRes.data.data
    volumes.value = volRes.data.data || []

    // Fetch asset counts
    const [wbRes, charRes, olRes, fsRes] = await Promise.all([
      worldBibleApi.get(projectId).catch(() => ({ data: { data: null } })),
      characterApi.list(projectId).catch(() => ({ data: { data: [] } })),
      outlineApi.list(projectId).catch(() => ({ data: { data: [] } })),
      foreshadowingApi.list(projectId).catch(() => ({ data: { data: [] } })),
    ])
    // Count distinct world bible fields (not just presence)
    const wbContent = wbRes?.data?.data?.content
    worldBibleCount.value = wbContent && typeof wbContent === 'object'
      ? Object.keys(wbContent).filter(k => wbContent[k] != null && wbContent[k] !== '').length
      : (wbContent ? 1 : 0)
    characterCount.value = (charRes.data.data || []).length
    outlineCount.value = (olRes.data.data || []).length
    foreshadowingCount.value = (fsRes.data.data || []).length
  } catch { /* empty */ }
}

async function generateBlueprint() {
  generating.value = true
  generatingStep.value = 0
  generationError.value = ''
  try {
    const res = await blueprintApi.generate(projectId, {})
    // 202: generation is running in background, start polling
    const bp = res.data?.data
    if (bp) {
      currentBlueprint.value = bp
      generatingStep.value = 1
    }
    stopPolling()
    pollTimer = setInterval(pollBlueprintStatus, 3000)
  } catch {
    generating.value = false
    ElMessage.error('蓝图生成请求失败')
  }
}

async function regenerateBlueprint() {
  await ElMessageBox.confirm('重新生成将覆盖当前蓝图内容，确认？', '重新生成', { type: 'warning' })
  stopPolling()
  currentBlueprint.value = null
  await generateBlueprint()
}

async function submitReview() {
  try {
    await blueprintApi.submitReview(projectId, currentBlueprint.value.id)
    currentBlueprint.value.status = 'pending_review'
    ElMessage.success('已提交审核')
  } catch { ElMessage.error('提交失败') }
}

async function approveBlueprint() {
  try {
    await blueprintApi.approve(projectId, currentBlueprint.value.id)
    currentBlueprint.value.status = 'approved'
    ElMessage.success('蓝图已批准')
  } catch { ElMessage.error('操作失败') }
}

async function rejectBlueprint() {
  const { value: reason } = await ElMessageBox.prompt('请输入驳回原因', '驳回蓝图', { type: 'warning' })
  try {
    await blueprintApi.reject(projectId, currentBlueprint.value.id, reason || '')
    currentBlueprint.value.status = 'rejected'
    ElMessage.success('蓝图已驳回')
  } catch { ElMessage.error('操作失败') }
}

async function approveVolume(id: string) {
  try {
    await volumeApi.approve(projectId, id)
    ElMessage.success('卷已批准')
    await fetchAll()
  } catch { ElMessage.error('操作失败') }
}

async function rejectVolume(id: string) {
  const { value: reason } = await ElMessageBox.prompt('驳回原因', '驳回', { type: 'warning' })
  try {
    await volumeApi.reject(projectId, id, reason || '')
    ElMessage.success('卷已驳回')
    await fetchAll()
  } catch { ElMessage.error('操作失败') }
}
</script>

<style scoped>
.blueprint { max-width: 1200px; margin: 0 auto; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; }
.page-header h1 { font-size: 24px; color: #e0e0e0; }
.status-bar { }
.status-label { color: #888; font-size: 13px; margin-bottom: 4px; }
.status-value { color: var(--nb-text-primary); font-size: 14px; }
.asset-card { text-align: center; }
.blueprint-content { background: var(--nb-table-header-bg); border: 1px solid var(--nb-card-border); padding: 16px; border-radius: 8px; font-size: 12px; color: var(--nb-text-secondary); max-height: 500px; overflow: auto; white-space: pre-wrap; }
</style>
