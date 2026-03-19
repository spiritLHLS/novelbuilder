<template>
  <div class="system-logs">
    <div class="page-header">
      <h1>系统日志</h1>
      <div class="header-actions">
        <el-select
          v-model="selectedService"
          placeholder="选择服务"
          style="width: 180px"
          @change="fetchLogs"
        >
          <el-option
            v-for="s in services"
            :key="s"
            :label="serviceLabel(s)"
            :value="s"
          />
        </el-select>
        <el-select v-model="lineCount" style="width: 120px" @change="fetchLogs">
          <el-option :value="100" label="最近 100 行" />
          <el-option :value="200" label="最近 200 行" />
          <el-option :value="500" label="最近 500 行" />
          <el-option :value="1000" label="最近 1000 行" />
        </el-select>
        <el-button
          type="primary"
          :loading="loading"
          :icon="Refresh"
          @click="fetchLogs"
        >刷新</el-button>
        <el-button :icon="CopyDocument" @click="copyAll">复制全部</el-button>
        <el-switch
          v-model="autoRefresh"
          active-text="自动刷新 (5s)"
          inactive-text=""
          style="margin-left: 8px"
        />
      </div>
    </div>

    <el-alert
      v-if="error"
      type="error"
      :title="error"
      :closable="false"
      style="margin-bottom: 12px"
    />

    <div v-if="!selectedService" class="service-grid">
      <el-card
        v-for="s in services"
        :key="s"
        class="service-card"
        shadow="hover"
        @click="selectService(s)"
      >
        <div class="service-icon">{{ serviceIcon(s) }}</div>
        <div class="service-name">{{ serviceLabel(s) }}</div>
      </el-card>
    </div>

    <div v-else class="log-container">
      <div class="log-toolbar">
        <span class="log-info">
          服务：<strong>{{ serviceLabel(selectedService) }}</strong>
          <template v-if="lines.length > 0">
            · 共 {{ lines.length }} 行
          </template>
        </span>
        <el-input
          v-model="filterText"
          placeholder="过滤日志行…"
          clearable
          style="width: 280px"
          :prefix-icon="Search"
        />
      </div>
      <div class="log-body" ref="logBodyRef">
        <div
          v-for="(line, i) in filteredLines"
          :key="i"
          class="log-line"
          :class="lineClass(line)"
        >
          <span class="line-num">{{ i + 1 }}</span>
          <span class="line-text">{{ line }}</span>
        </div>
        <div v-if="filteredLines.length === 0 && !loading" class="log-empty">
          {{ lines.length === 0 ? '暂无日志' : '无匹配行' }}
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { Refresh, CopyDocument, Search } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { logsApi } from '@/api'

const services = ref<string[]>([])
const selectedService = ref('')
const lineCount = ref(200)
const lines = ref<string[]>([])
const loading = ref(false)
const error = ref('')
const filterText = ref('')
const autoRefresh = ref(false)
const logBodyRef = ref<HTMLElement | null>(null)

let _autoRefreshTimer: ReturnType<typeof setInterval> | null = null

const filteredLines = computed(() => {
  const f = filterText.value.trim().toLowerCase()
  if (!f) return lines.value
  return lines.value.filter(l => l.toLowerCase().includes(f))
})

const serviceLabels: Record<string, string> = {
  'go-backend': 'Go 后端',
  'python-sidecar': 'Python 智能体',
  'postgresql': 'PostgreSQL',
  'redis': 'Redis',
  'neo4j': 'Neo4j 图数据库',
  'qdrant': 'Qdrant 向量库',
  'supervisord': 'Supervisord',
}

function serviceLabel(s: string) {
  return serviceLabels[s] ?? s
}

function serviceIcon(s: string) {
  const icons: Record<string, string> = {
    'go-backend': '⚙️',
    'python-sidecar': '🐍',
    'postgresql': '🐘',
    'redis': '🔴',
    'neo4j': '🕸️',
    'qdrant': '🔍',
    'supervisord': '📋',
  }
  return icons[s] ?? '📄'
}

function lineClass(line: string) {
  const l = line.toLowerCase()
  if (l.includes('error') || l.includes('err]') || l.includes('fatal') || l.includes('panic') || l.includes('critical')) {
    return 'level-error'
  }
  if (l.includes('warn') || l.includes('warning')) {
    return 'level-warn'
  }
  if (l.includes('info')) {
    return 'level-info'
  }
  if (l.includes('debug')) {
    return 'level-debug'
  }
  return ''
}

function selectService(s: string) {
  selectedService.value = s
  fetchLogs()
}

async function fetchLogs() {
  if (!selectedService.value) return
  loading.value = true
  error.value = ''
  try {
    const res = await logsApi.getLines(selectedService.value, lineCount.value)
    lines.value = res.data.lines ?? []
    await nextTick()
    // Scroll to bottom
    if (logBodyRef.value) {
      logBodyRef.value.scrollTop = logBodyRef.value.scrollHeight
    }
  } catch (e: any) {
    error.value = e?.response?.data?.error || e?.message || '获取日志失败'
  } finally {
    loading.value = false
  }
}

async function loadServices() {
  try {
    const res = await logsApi.listServices()
    services.value = res.data.services ?? []
  } catch (e: any) {
    error.value = e?.response?.data?.error || e?.message || '获取服务列表失败'
  }
}

function copyAll() {
  const text = filteredLines.value.join('\n')
  navigator.clipboard.writeText(text).then(() => {
    ElMessage.success('已复制到剪贴板')
  }).catch(() => {
    ElMessage.error('复制失败，请手动选择')
  })
}

watch(autoRefresh, (enabled) => {
  if (_autoRefreshTimer) {
    clearInterval(_autoRefreshTimer)
    _autoRefreshTimer = null
  }
  if (enabled) {
    _autoRefreshTimer = setInterval(fetchLogs, 5000)
  }
})

onMounted(loadServices)
onUnmounted(() => {
  if (_autoRefreshTimer) clearInterval(_autoRefreshTimer)
})
</script>

<style scoped>
.system-logs { height: 100%; display: flex; flex-direction: column; }
.page-header {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: 16px; flex-shrink: 0;
}
.page-header h1 { font-size: 20px; font-weight: 600; margin: 0; }
.header-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }

.service-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 12px;
  margin-top: 8px;
}
.service-card {
  cursor: pointer; text-align: center; padding: 8px;
  transition: transform 0.15s, box-shadow 0.15s;
}
.service-card:hover { transform: translateY(-2px); }
.service-icon { font-size: 28px; margin-bottom: 6px; }
.service-name { font-size: 12px; color: var(--el-text-color-secondary); }

.log-container { flex: 1; display: flex; flex-direction: column; min-height: 0; }
.log-toolbar {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: 8px; flex-shrink: 0;
}
.log-info { font-size: 13px; color: var(--el-text-color-secondary); }

.log-body {
  flex: 1; overflow-y: auto; min-height: 0;
  background: #1a1a1a; border-radius: 6px;
  padding: 8px 4px; font-family: 'Menlo', 'Monaco', 'Courier New', monospace;
  font-size: 12px; line-height: 1.6;
}
.log-line {
  display: flex; align-items: flex-start; gap: 8px;
  padding: 1px 8px; border-radius: 2px;
}
.log-line:hover { background: rgba(255,255,255,0.04); }
.line-num {
  min-width: 40px; color: #555; text-align: right;
  flex-shrink: 0; user-select: none; font-size: 11px;
}
.line-text { color: #ccc; word-break: break-all; white-space: pre-wrap; }

.level-error .line-text { color: #f87171; }
.level-warn .line-text { color: #fbbf24; }
.level-info .line-text { color: #60a5fa; }
.level-debug .line-text { color: #9ca3af; }

.log-empty {
  text-align: center; color: #555; padding: 40px 0; font-size: 14px;
}
</style>
