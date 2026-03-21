<template>
  <div class="rag-page">
    <div class="page-header">
      <h2>知识库管理 (RAG)</h2>
      <p class="subtitle">管理参考书向量索引，为章节生成提供风格与感官参考</p>
    </div>

    <!-- Status Cards -->
    <el-row :gutter="16" class="status-row">
      <el-col :span="8">
        <el-card class="stat-card">
          <div class="stat-value">{{ totalChunks }}</div>
          <div class="stat-label">向量片段总数</div>
        </el-card>
      </el-col>
      <el-col v-for="col in collections" :key="col.collection" :span="8">
        <el-card class="stat-card">
          <div class="stat-value">{{ col.count }}</div>
          <div class="stat-label">{{ collectionLabel(col.collection) }}</div>
        </el-card>
      </el-col>
    </el-row>

    <!-- Actions -->
    <el-card class="action-card">
      <template #header>
        <div class="card-header">
          <span>索引操作</span>
          <el-tag :type="embeddingConfigured ? 'success' : 'warning'" size="small">
            {{ embeddingConfigured ? 'Embedding 已配置' : '需配置 EMBEDDING_API_KEY' }}
          </el-tag>
        </div>
      </template>

      <div class="action-section">
        <div class="action-desc">
          <h4>重建知识库索引</h4>
          <p>
            删除当前项目所有向量，并根据已分析参考书中缓存的文本样本重新建立索引。<br/>
            每次新增或重新分析参考书后，可点此手动同步。
          </p>
        </div>
        <el-button
          type="primary"
          :icon="Refresh"
          @click="handleRebuild"
        >
          重建索引
        </el-button>
      </div>

      <el-divider />

      <div class="action-section">
        <div class="action-desc">
          <h4>参考书列表</h4>
          <p>以下参考书已完成分析，其文本样本会用于向量检索。</p>
        </div>
      </div>

      <el-table :data="references" style="width: 100%" v-loading="loadingRefs">
        <el-table-column prop="title" label="书名" />
        <el-table-column prop="author" label="作者" width="120" />
        <el-table-column prop="genre" label="类型" width="100" />
        <el-table-column prop="status" label="分析状态" width="120">
          <template #default="{ row }">
            <el-tag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>

        <el-table-column label="样本缓存" width="100">
          <template #default="{ row }">
            <el-icon v-if="row.sample_texts" color="#67c23a"><CircleCheck /></el-icon>
            <el-icon v-else color="#909399"><CircleClose /></el-icon>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="120">
          <template #default="{ row }">
            <el-button
              v-if="row.status !== 'completed'"
              size="small"
              type="primary"
              :loading="analyzingId === row.id || row.status === 'analyzing'"
              @click="handleAnalyze(row)"
            >
              分析
            </el-button>
            <el-button
              v-else
              size="small"
              :loading="analyzingId === row.id || row.status === 'analyzing'"
              @click="handleAnalyze(row)"
            >
              重析
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

  </div>

  <!-- Rebuild progress dialog -->
  <el-dialog
    v-model="rebuildDialogVisible"
    title="重建知识库索引"
    width="420px"
    :close-on-click-modal="false"
    :close-on-press-escape="false"
    :show-close="rebuildJobStatus !== 'running'"
    @close="onRebuildDialogClose"
  >
    <div class="rebuild-progress">
      <template v-if="rebuildJobStatus === 'running'">
        <el-icon class="spinning" style="font-size:32px"><Loading /></el-icon>
        <p>正在后台重建索引，刷新页面不影响执行进度…</p>
      </template>
      <template v-else-if="rebuildJobStatus === 'completed'">
        <el-icon color="#67c23a" style="font-size:32px"><CircleCheck /></el-icon>
        <p>索引重建完成，已同步 <strong>{{ rebuildSources }}</strong> 条参考书。</p>
      </template>
      <template v-else-if="rebuildJobStatus === 'failed'">
        <el-icon color="#f56c6c" style="font-size:32px"><CircleClose /></el-icon>
        <p>重建失败：{{ rebuildError || '请检查 Embedding API 配置' }}</p>
      </template>
    </div>
    <template #footer>
      <el-button
        v-if="rebuildJobStatus !== 'running'"
        type="primary"
        @click="onRebuildDialogClose"
      >关闭</el-button>
    </template>
  </el-dialog>

</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Refresh, CircleCheck, CircleClose, Loading } from '@element-plus/icons-vue'
import { ragApi, referenceApi } from '@/api/index'

const route = useRoute()
const projectId = computed(() => route.params.projectId as string)

const totalChunks = ref(0)
const collections = ref<{ collection: string; count: number }[]>([])
const references = ref<any[]>([])
const loadingRefs = ref(false)
const analyzingId = ref<string | null>(null)
const embeddingConfigured = ref(true)

// Rebuild dialog state
const rebuildDialogVisible = ref(false)
const rebuildJobStatus = ref<'running' | 'completed' | 'failed' | 'idle'>('idle')
const rebuildSources = ref(0)
const rebuildError = ref('')

let rebuildPoller: ReturnType<typeof setInterval> | null = null
let analyzePoller: ReturnType<typeof setInterval> | null = null

const REBUILD_STORAGE_KEY = computed(() => `rag_rebuild_${projectId.value}`)

function collectionLabel(col: string): string {
  const map: Record<string, string> = {
    style_samples: '风格样本',
    sensory_samples: '感官片段',
  }
  return map[col] ?? col
}

function statusLabel(status: string): string {
  const map: Record<string, string> = {
    processing: '处理中',
    analyzing: '分析中',
    completed: '已完成',
    failed: '失败',
  }
  return map[status] ?? status
}

function statusType(status: string): 'success' | 'warning' | 'danger' | 'info' {
  if (status === 'completed') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'analyzing') return 'warning'
  return 'warning'
}

async function loadStatus() {
  try {
    const res = await ragApi.getStatus(projectId.value)
    totalChunks.value = res.data.total_chunks ?? 0
    collections.value = res.data.collections ?? []
  } catch {
    // RAG not yet indexed — not an error
  }
}

async function loadReferences() {
  loadingRefs.value = true
  try {
    const res = await referenceApi.list(projectId.value)
    references.value = res.data.data ?? []
  } finally {
    loadingRefs.value = false
  }
}

// ── Rebuild index ─────────────────────────────────────────────────────

async function handleRebuild() {
  try {
    await ragApi.rebuild(projectId.value)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error ?? '启动重建失败，请检查 Embedding API 配置')
    embeddingConfigured.value = false
    return
  }

  // Persist job marker so we survive page refresh
  localStorage.setItem(REBUILD_STORAGE_KEY.value, Date.now().toString())

  rebuildJobStatus.value = 'running'
  rebuildError.value = ''
  rebuildSources.value = 0
  rebuildDialogVisible.value = true

  startRebuildPoller()
}

function startRebuildPoller() {
  stopRebuildPoller()
  rebuildPoller = setInterval(async () => {
    try {
      const res = await ragApi.rebuildStatus(projectId.value)
      const { status, rebuilt_sources, error } = res.data

      rebuildJobStatus.value = status
      if (status === 'completed') {
        rebuildSources.value = rebuilt_sources ?? 0
        stopRebuildPoller()
        localStorage.removeItem(REBUILD_STORAGE_KEY.value)
        await loadStatus()
      } else if (status === 'failed') {
        rebuildError.value = error ?? ''
        stopRebuildPoller()
        localStorage.removeItem(REBUILD_STORAGE_KEY.value)
        embeddingConfigured.value = false
      }
    } catch {
      // transient network error — keep polling
    }
  }, 2000)
}

function stopRebuildPoller() {
  if (rebuildPoller !== null) {
    clearInterval(rebuildPoller)
    rebuildPoller = null
  }
}

function onRebuildDialogClose() {
  stopRebuildPoller()
  rebuildDialogVisible.value = false
  localStorage.removeItem(REBUILD_STORAGE_KEY.value)
}

// On mount: recover rebuild job state from localStorage (page was refreshed mid-job)
async function recoverRebuildJob() {
  const marker = localStorage.getItem(REBUILD_STORAGE_KEY.value)
  if (!marker) return

  try {
    const res = await ragApi.rebuildStatus(projectId.value)
    const { status } = res.data
    if (status === 'running') {
      rebuildJobStatus.value = 'running'
      rebuildDialogVisible.value = true
      startRebuildPoller()
    } else {
      // Job finished (or server restarted) — clear marker silently
      localStorage.removeItem(REBUILD_STORAGE_KEY.value)
    }
  } catch {
    localStorage.removeItem(REBUILD_STORAGE_KEY.value)
  }
}

// ── Analyze reference ─────────────────────────────────────────────────

async function handleAnalyze(ref: any) {
  if (analyzingId.value === ref.id || ref.status === 'analyzing') return

  analyzingId.value = ref.id
  try {
    await referenceApi.analyze(ref.id)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error ?? '分析失败')
    analyzingId.value = null
    return
  }

  // Backend returned 202 — update local row status immediately for visual feedback
  const row = references.value.find(r => r.id === ref.id)
  if (row) row.status = 'analyzing'

  startAnalyzePoller(ref.id)
}

function startAnalyzePoller(targetId: string) {
  stopAnalyzePoller()
  analyzePoller = setInterval(async () => {
    try {
      const res = await referenceApi.list(projectId.value)
      const list: any[] = res.data.data ?? []
      references.value = list

      const target = list.find(r => r.id === targetId)
      if (!target || target.status !== 'analyzing') {
        stopAnalyzePoller()
        analyzingId.value = null
        if (target?.status === 'completed') {
          ElMessage.success('分析完成，样本已入库')
          await loadStatus()
        } else if (target?.status === 'failed') {
          ElMessage.error('分析失败，请重试')
        }
      }
    } catch {
      // transient — keep polling
    }
  }, 2500)
}

function stopAnalyzePoller() {
  if (analyzePoller !== null) {
    clearInterval(analyzePoller)
    analyzePoller = null
  }
}

// On mount: if any reference is still 'analyzing' (e.g. after page refresh), resume polling
function recoverAnalyzeJob() {
  const pending = references.value.find(r => r.status === 'analyzing')
  if (pending) {
    analyzingId.value = pending.id
    startAnalyzePoller(pending.id)
  }
}

onMounted(async () => {
  await Promise.all([loadStatus(), loadReferences()])
  await recoverRebuildJob()
  recoverAnalyzeJob()
})

onUnmounted(() => {
  stopRebuildPoller()
  stopAnalyzePoller()
})
</script>

<style scoped>
.rag-page {
  padding: 24px;
  max-width: 1000px;
}

.page-header {
  margin-bottom: 24px;
}

.page-header h2 {
  font-size: 22px;
  font-weight: 600;
  color: #e0e0e0;
}

.subtitle {
  color: #888;
  margin-top: 4px;
  font-size: 13px;
}

.status-row {
  margin-bottom: 20px;
}

.stat-card {
  text-align: center;
  background: #1e1e30;
  border-color: #2a2a3e;
}

.stat-value {
  font-size: 32px;
  font-weight: 700;
  color: #409eff;
  line-height: 1.2;
}

.stat-label {
  color: #888;
  font-size: 13px;
  margin-top: 4px;
}

.action-card {
  margin-bottom: 20px;
  background: var(--nb-card-bg);
  border-color: var(--nb-card-border);
}

.card-header {
  display: flex;
  align-items: center;
  gap: 12px;
}

.action-section {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  padding: 8px 0;
}

.action-desc h4 {
  color: var(--nb-text-primary);
  margin-bottom: 6px;
}

.action-desc p {
  color: #888;
  font-size: 13px;
  line-height: 1.6;
}

code {
  background: var(--nb-table-header-bg);
  padding: 1px 5px;
  border-radius: 3px;
  font-size: 12px;
  color: var(--nb-success);
}

.rebuild-progress {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  padding: 16px 0;
  text-align: center;
}

.rebuild-progress p {
  color: var(--nb-text-primary, #e0e0e0);
  font-size: 14px;
  margin: 0;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}
.spinning {
  animation: spin 1s linear infinite;
}
</style>
