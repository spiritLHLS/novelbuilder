<template>
  <div class="task-queue-page">
    <div class="page-header">
      <h2>{{ projectId ? '项目任务总控' : '全局任务总控' }}</h2>
      <p class="subtitle">查看和管理所有后台异步任务，支持暂停、继续、取消和失败回退</p>
      <div class="header-actions">
        <el-switch v-model="autoRefresh" active-text="自动刷新 (5s)" @change="toggleAutoRefresh" />
        <el-button :icon="Refresh" @click="loadTasks">刷新</el-button>
      </div>
    </div>

    <div v-if="projectId" class="project-controls">
      <el-button @click="changeProjectState('start')">启动项目</el-button>
      <el-button @click="changeProjectState('pause')">暂停项目</el-button>
      <el-button @click="changeProjectState('resume')">继续项目</el-button>
      <el-button type="danger" plain @click="changeProjectState('terminate')">终止项目</el-button>
      <el-button plain @click="changeProjectState('reset')">重置任务状态</el-button>
    </div>

    <!-- Filter bar -->
    <div class="toolbar">
      <el-select v-model="filterStatus" placeholder="全部状态" clearable style="width: 160px" @change="() => { page = 1; loadTasks() }">
        <el-option label="全部" value="" />
        <el-option label="待执行" value="pending" />
        <el-option label="执行中" value="running" />
        <el-option label="已暂停" value="paused" />
        <el-option label="已完成" value="done" />
        <el-option label="失败" value="failed" />
        <el-option label="已取消" value="cancelled" />
      </el-select>
      <el-select v-model="filterType" placeholder="全部类型" clearable style="width: 200px" @change="() => { page = 1; loadTasks() }">
        <el-option label="全部" value="" />
        <el-option label="章节生成" value="chapter_generate" />
        <el-option label="蓝图生成" value="blueprint_generate" />
        <el-option label="章节重写" value="chapter_regenerate" />
        <el-option label="自动续写" value="generate_next_chapter" />
        <el-option label="章节导入处理" value="chapter_import_process" />
        <el-option label="参考书下载" value="reference_fetch_import" />
        <el-option label="参考书分析" value="reference_analyze" />
        <el-option label="参考书深度分析" value="reference_analysis" />
        <el-option label="RAG 重建" value="rag_rebuild" />
        <el-option label="图谱同步" value="graph_sync" />
        <el-option label="向量重建" value="vector_rebuild" />
      </el-select>
    </div>

    <el-table :data="tasks" v-loading="loading" class="task-table" stripe>
      <el-table-column label="任务类型" prop="task_type" min-width="160">
        <template #default="{ row }">
          <code>{{ row.task_type }}</code>
        </template>
      </el-table-column>
      <el-table-column v-if="!projectId" label="项目" prop="project_id" min-width="150" show-overflow-tooltip>
        <template #default="{ row }">
          <span class="muted">{{ row.project_id || '系统任务' }}</span>
        </template>
      </el-table-column>
      <el-table-column label="任务内容" min-width="180" show-overflow-tooltip>
        <template #default="{ row }">
          {{ taskSummary(row) }}
        </template>
      </el-table-column>
      <el-table-column label="状态" prop="status" width="110">
        <template #default="{ row }">
          <el-tag :type="statusTagType(row.status)" size="small">
            {{ statusLabel(row.status) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="重试次数" prop="attempts" width="90" align="center">
        <template #default="{ row }">
          {{ row.attempts ?? 0 }} / {{ row.max_attempts ?? 3 }}
        </template>
      </el-table-column>
      <el-table-column label="耗时" width="150">
        <template #default="{ row }">
          <div class="duration-stack">
            <span>排队 {{ formatDuration(row.queue_wait_ms) }}</span>
            <span>执行 {{ formatDuration(row.runtime_ms) }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="创建时间" prop="created_at" width="180">
        <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="错误信息" prop="error" min-width="200" show-overflow-tooltip>
        <template #default="{ row }">
          <span v-if="row.error_message" class="error-text">{{ row.error_message }}</span>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="220" align="center">
        <template #default="{ row }">
          <el-button
            v-if="row.status === 'pending' || row.status === 'running'"
            type="info"
            text
            size="small"
            @click="pauseTask(row.id)"
          >暂停</el-button>
          <el-button
            v-if="row.status === 'paused'"
            type="success"
            text
            size="small"
            @click="resumeTask(row.id)"
          >继续</el-button>
          <el-button
            v-if="row.status === 'pending' || row.status === 'running' || row.status === 'paused'"
            type="warning"
            text
            size="small"
            @click="cancelTask(row.id)"
          >取消</el-button>
          <el-button
            v-if="row.status === 'failed' || row.status === 'cancelled'"
            type="primary"
            text
            size="small"
            @click="retryTask(row.id)"
          >重试</el-button>
          <span v-if="row.status === 'done'" class="muted">—</span>
        </template>
      </el-table-column>
    </el-table>

    <div v-if="!loading && tasks.length === 0" class="empty-state">
      <el-empty description="暂无任务记录" />
    </div>

    <!-- Pagination -->
    <div v-if="total > pageSize" class="pagination-bar">
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="pageSize"
        :page-sizes="[10, 20, 50, 100]"
        :total="total"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="loadTasks"
        @current-change="loadTasks"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { projectApi, taskApi } from '@/api'

interface Task {
  id: string
  project_id: string | null
  task_type: string
  payload: Record<string, any> | null
  status: string
  attempts: number
  max_attempts: number
  error_message: string | null
  created_at: string
  scheduled_at: string
  started_at: string | null
  completed_at: string | null
  queue_wait_ms?: number
  runtime_ms?: number
}

const route = useRoute()
const projectId = computed(() => (route.params.projectId as string | undefined) || '')

const tasks = ref<Task[]>([])
const loading = ref(false)
const filterStatus = ref('')
const filterType = ref('')
const autoRefresh = ref(false)
const page = ref(1)
const pageSize = ref(10)
const total = ref(0)
let refreshTimer: ReturnType<typeof setInterval> | null = null

const statusLabel = (s: string) => {
  const map: Record<string, string> = {
    pending: '待执行',
    running: '执行中',
    paused: '已暂停',
    done: '已完成',
    failed: '失败',
    cancelled: '已取消',
  }
  return map[s] ?? s
}

const statusTagType = (s: string): '' | 'success' | 'warning' | 'danger' | 'info' => {
  const map: Record<string, '' | 'success' | 'warning' | 'danger' | 'info'> = {
    pending: 'info',
    running: 'warning',
    paused: 'info',
    done: 'success',
    failed: 'danger',
    cancelled: 'info',
  }
  return map[s] ?? 'info'
}

function formatTime(iso: string) {
  if (!iso) return '—'
  return new Date(iso).toLocaleString('zh-CN', { hour12: false })
}

function formatDuration(ms?: number) {
  if (!ms || ms < 0) return '—'
  if (ms < 1000) return `${ms}ms`
  const seconds = Math.round(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  const rest = seconds % 60
  if (minutes < 60) return rest ? `${minutes}m ${rest}s` : `${minutes}m`
  const hours = Math.floor(minutes / 60)
  const minuteRest = minutes % 60
  return minuteRest ? `${hours}h ${minuteRest}m` : `${hours}h`
}

function taskSummary(task: Task) {
  const payload = task.payload || {}
  const request = payload.request || {}

  switch (task.task_type) {
    case 'blueprint_generate':
      return '生成整书蓝图'
    case 'chapter_generate': {
      const chapterNum = Number(request.chapter_num) || 0
      return chapterNum > 0 ? `生成第${chapterNum}章` : '章节生成'
    }
    case 'chapter_regenerate': {
      const chapterNum = Number(request.chapter_num) || 0
      return chapterNum > 0 ? `重生成第${chapterNum}章` : '章节重生成'
    }
    case 'generate_next_chapter':
      return '继续生成下一章'
    case 'reference_fetch_import':
      return payload.title ? `下载参考书《${payload.title}》` : '参考书下载'
    case 'reference_analyze':
      return '参考书分析'
    case 'reference_analysis':
      return payload.ref_id ? `深度分析参考书 ${payload.ref_id}` : '参考书深度分析'
    case 'rag_rebuild':
      return '重建 RAG 知识库'
    case 'graph_sync':
      return '同步图谱记忆'
    case 'vector_rebuild':
      return '重建向量索引'
    case 'generate_chapter_outlines': {
      const volumeNum = Number(payload.volume_num) || 0
      const startChapter = Number(payload.start_chapter) || 0
      if (volumeNum > 0 && startChapter > 0) {
        return `第${volumeNum}卷章节大纲（从第${startChapter}章开始）`
      }
      if (volumeNum > 0) {
        return `第${volumeNum}卷章节大纲`
      }
      return '章节大纲生成'
    }
    default:
      return '—'
  }
}

async function loadTasks() {
  loading.value = true
  try {
    const res = await taskApi.list(projectId.value || undefined, {
      page: page.value,
      page_size: pageSize.value,
      status: filterStatus.value || undefined,
      type: filterType.value || undefined,
    })
    const list = Array.isArray(res.data?.data) ? res.data.data : []
    tasks.value = list
    total.value = res.data?.pagination?.total ?? 0
  } catch {
    ElMessage.error('加载任务列表失败')
  } finally {
    loading.value = false
  }
}

async function cancelTask(id: string) {
  await ElMessageBox.confirm('确定取消该任务？', '取消确认', { type: 'warning' })
  try {
    await taskApi.cancel(id)
    ElMessage.success('任务已取消')
    await loadTasks()
  } catch {
    ElMessage.error('取消失败')
  }
}

async function pauseTask(id: string) {
  try {
    await taskApi.pause(id)
    ElMessage.success('任务已暂停')
    await loadTasks()
  } catch {
    ElMessage.error('暂停失败')
  }
}

async function resumeTask(id: string) {
  try {
    await taskApi.resume(id)
    ElMessage.success('任务已继续')
    await loadTasks()
  } catch {
    ElMessage.error('继续失败')
  }
}

async function retryTask(id: string) {
  try {
    await taskApi.retry(id)
    ElMessage.success('已重新加入队列')
    await loadTasks()
  } catch {
    ElMessage.error('重试失败')
  }
}

async function changeProjectState(action: 'start' | 'pause' | 'resume' | 'terminate' | 'reset') {
  if (!projectId.value) return
  try {
    await projectApi.state(projectId.value, action)
    ElMessage.success('项目任务状态已更新')
    await loadTasks()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '项目状态更新失败')
  }
}

function toggleAutoRefresh(val: boolean) {
  if (val) {
    refreshTimer = setInterval(loadTasks, 5000)
  } else if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
}

onMounted(loadTasks)
onUnmounted(() => {
  if (refreshTimer) clearInterval(refreshTimer)
})
</script>

<style scoped>
.task-queue-page {
  padding: 24px;
  max-width: 1100px;
}

.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 20px;
  flex-wrap: wrap;
  gap: 12px;
}

.page-header h2 {
  font-size: 22px;
  font-weight: 600;
  color: #e0e0e0;
  margin: 0;
}

.subtitle {
  font-size: 13px;
  color: #888;
  margin-top: 4px;
}

.header-actions {
  display: flex;
  gap: 12px;
  align-items: center;
}

.toolbar {
  display: flex;
  gap: 12px;
  align-items: center;
  margin-bottom: 16px;
}

.project-controls {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 14px;
}

.error-text {
  color: #f56c6c;
  font-size: 12px;
}

.duration-stack {
  display: flex;
  flex-direction: column;
  gap: 2px;
  color: var(--nb-text-secondary);
  font-size: 12px;
  line-height: 1.35;
}

.muted {
  color: #666;
}

.empty-state {
  margin-top: 40px;
  text-align: center;
}

.pagination-bar {
  margin-top: 20px;
  display: flex;
  justify-content: center;
}

code {
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  font-size: 12px;
  background: var(--nb-table-header-bg);
  border: 1px solid var(--nb-card-border);
  color: var(--nb-text-secondary);
  padding: 2px 6px;
  border-radius: 4px;
}
</style>
