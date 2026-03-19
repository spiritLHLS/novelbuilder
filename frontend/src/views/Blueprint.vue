<template>
  <div class="blueprint">
    <div class="page-header">
      <h1>蓝图管理</h1>
      <el-button type="primary" @click="generateBlueprint" :loading="generating"
        :disabled="!!currentBlueprint">
        <el-icon><MagicStick /></el-icon>生成蓝图
      </el-button>
    </div>

    <!-- No Blueprint -->
    <el-empty v-if="!currentBlueprint && !generating" description="尚未生成蓝图">
      <el-button type="primary" @click="generateBlueprint" :loading="generating">
        一键生成完整蓝图
      </el-button>
      <p style="color: #888; margin-top: 12px; font-size: 13px;">
        蓝图会根据世界设定自动生成世界圣经、大纲、角色、伏笔和卷册规划
      </p>
    </el-empty>

    <!-- Generating Progress -->
    <el-card v-if="generating" shadow="hover" style="text-align: center; padding: 40px 0;">
      <el-icon :size="48" class="is-loading" style="color: #409eff;"><Loading /></el-icon>
      <h3 :style="{ color: 'var(--nb-text-primary)', marginTop: '16px' }">正在生成蓝图...</h3>
      <p style="color: #888;">AI正在构建世界设定、角色体系、故事大纲和卷册结构</p>
    </el-card>

    <!-- Blueprint Content -->
    <template v-if="currentBlueprint">
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
            <el-button v-if="currentBlueprint.status === 'generated'"
              type="success" @click="submitReview">提交审核</el-button>
            <el-button v-if="currentBlueprint.status === 'pending_review'"
              type="success" @click="approveBlueprint">批准</el-button>
            <el-button v-if="currentBlueprint.status === 'pending_review'"
              type="danger" @click="rejectBlueprint">驳回</el-button>
            <el-button v-if="currentBlueprint.status === 'rejected'"
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
          <el-table-column prop="volume_number" label="卷号" width="80" />
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
        <pre class="blueprint-content">{{ JSON.stringify(currentBlueprint.content, null, 2) }}</pre>
      </el-card>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { blueprintApi, volumeApi, worldBibleApi, characterApi, outlineApi, foreshadowingApi } from '@/api'

const route = useRoute()
const projectId = route.params.projectId as string

const currentBlueprint = ref<any>(null)
const generating = ref(false)
const volumes = ref<any[]>([])

const worldBibleCount = ref(0)
const characterCount = ref(0)
const outlineCount = ref(0)
const foreshadowingCount = ref(0)

const blueprintStatusType = computed(() => {
  const map: Record<string, string> = {
    generated: 'info', pending_review: 'warning', approved: 'success', rejected: 'danger',
  }
  return (map[currentBlueprint.value?.status] || 'info') as any
})

const blueprintStatusLabel = computed(() => {
  const map: Record<string, string> = {
    generated: '已生成', pending_review: '待审核', approved: '已批准', rejected: '已驳回',
  }
  return map[currentBlueprint.value?.status] || currentBlueprint.value?.status
})

function formatDate(d: string) {
  return d ? new Date(d).toLocaleString('zh-CN') : '-'
}

onMounted(fetchAll)

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
    worldBibleCount.value = wbRes.data.data ? 1 : 0
    characterCount.value = (charRes.data.data || []).length
    outlineCount.value = (olRes.data.data || []).length
    foreshadowingCount.value = (fsRes.data.data || []).length
  } catch { /* empty */ }
}

async function generateBlueprint() {
  generating.value = true
  try {
    const res = await blueprintApi.generate(projectId, {})
    currentBlueprint.value = res.data.data
    ElMessage.success('蓝图生成完成')
    await fetchAll()
  } catch {
    ElMessage.error('蓝图生成失败')
  } finally {
    generating.value = false
  }
}

async function regenerateBlueprint() {
  await ElMessageBox.confirm('重新生成将覆盖当前蓝图内容，确认？', '重新生成', { type: 'warning' })
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
