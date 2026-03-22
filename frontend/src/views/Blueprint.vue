<template>
  <div class="blueprint">
    <div class="page-header">
      <h1>蓝图管理</h1>
      <el-button type="primary" @click="openGenerateDialog(!!currentBlueprint)" :loading="generating"
        :disabled="generating || currentBlueprint?.status === 'approved' || currentBlueprint?.status === 'pending_review'">
        <el-icon><MagicStick /></el-icon>{{ currentBlueprint ? '重新生成' : '生成蓝图' }}
      </el-button>
    </div>

    <!-- Generation Config Dialog -->
    <el-dialog v-model="dialogVisible" :title="isRegenerate ? '重新生成蓝图' : '生成蓝图'" width="520px" :close-on-click-modal="false">
      <el-form :model="genForm" label-width="120px">
        <el-form-item label="卷数" required>
          <el-input-number v-model="genForm.volume_count" :min="1" :max="30" :step="1" style="width: 180px;" />
          <span style="color:#888; margin-left:10px; font-size:12px;">推荐 4～12 卷</span>
        </el-form-item>
        <el-form-item label="每章最少字数">
          <el-input-number v-model="genForm.chapter_words_min" :min="500" :max="10000" :step="500" style="width: 180px;" />
          <span style="color:#888; margin-left:10px; font-size:12px;">默认 2000 字</span>
        </el-form-item>
        <el-form-item label="每章最多字数">
          <el-input-number v-model="genForm.chapter_words_max" :min="500" :max="20000" :step="500" style="width: 180px;" />
          <span style="color:#888; margin-left:10px; font-size:12px;">默认 3500 字</span>
        </el-form-item>
        <el-form-item label="覆盖核心创意">
          <el-input v-model="genForm.idea" type="textarea" :rows="3" placeholder="可选：留空则使用项目描述" />
        </el-form-item>
        <el-alert v-if="isRegenerate" type="warning" :closable="false" style="margin-top:8px;">
          重新生成将覆盖当前所有卷册规划和蓝图内容，已写入的章节不受影响。
        </el-alert>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="generating" @click="confirmGenerate">
          {{ isRegenerate ? '确认重新生成' : '开始生成' }}
        </el-button>
      </template>
    </el-dialog>

    <!-- No Blueprint -->
    <el-empty v-if="!currentBlueprint && !generating" description="尚未生成蓝图">
      <el-button type="primary" @click="openGenerateDialog(false)" :loading="generating">
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
            <el-button v-if="currentBlueprint.status === 'draft' || currentBlueprint.status === 'rejected' || currentBlueprint.status === 'failed'"
              type="warning" @click="openGenerateDialog(true)">重新生成</el-button>
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
        <el-descriptions :column="1" border style="margin-bottom: 16px;" v-if="hasData(currentBlueprint.master_outline)">
          <el-descriptions-item label="总体大纲">
            <span style="white-space: pre-wrap; font-size: 13px;">{{ formatBlueprintField(currentBlueprint.master_outline) }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="角色关系图" v-if="hasData(currentBlueprint.relation_graph)">
            <span style="white-space: pre-wrap; font-size: 13px;">{{ formatBlueprintField(currentBlueprint.relation_graph) }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="全局时间线" v-if="hasData(currentBlueprint.global_timeline)">
            <span style="white-space: pre-wrap; font-size: 13px;">{{ formatBlueprintField(currentBlueprint.global_timeline) }}</span>
          </el-descriptions-item>
        </el-descriptions>
        <el-empty v-if="!hasData(currentBlueprint.master_outline)" description="蓝图内容正在解析或生成失败，请查看错误信息" />
      </el-card>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { blueprintApi, volumeApi, worldBibleApi, characterApi, outlineApi, foreshadowingApi, projectApi } from '@/api'

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

// Generation dialog state
const dialogVisible = ref(false)
const isRegenerate = ref(false)
const genForm = ref({ volume_count: 4, chapter_words_min: 2000, chapter_words_max: 3500, idea: '' })

let pollTimer: ReturnType<typeof setInterval> | null = null

function stopPolling() {
  if (pollTimer !== null) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

async function openGenerateDialog(regen: boolean) {
  isRegenerate.value = regen
  // Load project defaults to pre-fill the form
  try {
    const res = await projectApi.get(projectId)
    const p = res?.data?.data
    if (p) {
      const targetWords = p.target_words || 0
      const chapterWords = p.chapter_words || 3000
      let vc = genForm.value.volume_count
      let wMin = genForm.value.chapter_words_min
      let wMax = genForm.value.chapter_words_max
      if (targetWords > 0) {
        vc = Math.max(4, Math.round(targetWords / 100000))
      }
      if (chapterWords > 0) {
        wMin = Math.round(chapterWords * 2 / 3)
        wMax = Math.round(chapterWords * 4 / 3)
      }
      genForm.value = { volume_count: vc, chapter_words_min: wMin, chapter_words_max: wMax, idea: '' }
    }
  } catch { /* keep defaults */ }
  dialogVisible.value = true
}

async function confirmGenerate() {
  dialogVisible.value = false
  if (isRegenerate.value) {
    stopPolling()
    currentBlueprint.value = null
  }
  await doGenerate()
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

/** Returns true only when a blueprint JSONB field has meaningful content. */
function hasData(val: any): boolean {
  if (val == null || val === undefined) return false
  if (typeof val === 'string') return val.trim() !== ''
  if (Array.isArray(val)) return val.length > 0
  if (typeof val === 'object') return Object.keys(val).length > 0
  return Boolean(val)
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

async function doGenerate() {
  generating.value = true
  generatingStep.value = 0
  generationError.value = ''
  const payload: Record<string, any> = {
    volume_count: genForm.value.volume_count,
    chapter_words_min: genForm.value.chapter_words_min,
    chapter_words_max: genForm.value.chapter_words_max,
  }
  if (genForm.value.idea.trim()) payload.idea = genForm.value.idea.trim()
  try {
    const res = await blueprintApi.generate(projectId, payload)
    // 202: generation is running in background, start polling
    const bp = res.data?.data
    if (bp) {
      currentBlueprint.value = bp
      generatingStep.value = 1
    }
    stopPolling()
    pollTimer = setInterval(pollBlueprintStatus, 3000)
  } catch (err: any) {
    generating.value = false
    const msg = err?.response?.data?.error || '蓝图生成请求失败'
    ElMessage.error(msg)
  }
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
  try {
    const { value: reason } = await (await import('element-plus')).ElMessageBox.prompt('请输入驳回原因', '驳回蓝图', { type: 'warning' })
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
  try {
    const { value: reason } = await (await import('element-plus')).ElMessageBox.prompt('驳回原因', '驳回', { type: 'warning' })
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
