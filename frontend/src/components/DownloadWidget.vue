<template>
  <!-- Floating download widget anchored to bottom-right corner -->
  <Teleport to="body">
    <div v-if="showWidget" class="download-widget" :class="{ collapsed: isCollapsed }">
      <!-- Header bar -->
      <div class="widget-header" @click="toggle">
        <div class="widget-title">
          <el-icon class="spin-icon" v-if="store.hasActive"><Loading /></el-icon>
          <el-icon v-else><Check /></el-icon>
          <span>下载管理</span>
          <el-badge
            v-if="store.activeTasks.length"
            :value="store.activeTasks.length"
            class="task-badge"
          />
        </div>
        <div class="widget-actions">
          <el-button link size="small" @click.stop="store.clearCompleted()" title="清除已完成">
            <el-icon><Delete /></el-icon>
          </el-button>
          <el-icon class="toggle-icon">
            <component :is="isCollapsed ? 'ArrowUp' : 'ArrowDown'" />
          </el-icon>
        </div>
      </div>

      <!-- Task list (visible when expanded) -->
      <transition name="slide">
        <div v-if="!isCollapsed" class="widget-body">
          <div v-if="store.allTasks.length === 0" class="empty-hint">暂无下载任务</div>
          <div
            v-for="task in store.allTasks"
            :key="task.refId"
            class="task-item"
          >
            <div class="task-info">
              <span class="task-title">{{ task.title || task.refId.slice(0, 8) + '…' }}</span>
              <el-tag
                :type="statusTagType(task.fetchStatus)"
                size="small"
                class="task-status-tag"
              >{{ statusLabel(task.fetchStatus) }}</el-tag>
            </div>
            <el-progress
              v-if="task.fetchTotal > 0"
              :percentage="task.fetchTotal > 0 ? Math.round(task.fetchDone / task.fetchTotal * 100) : 0"
              :status="task.fetchStatus === 'failed' ? 'exception' : task.fetchStatus === 'completed' ? 'success' : undefined"
              :stroke-width="6"
              style="margin: 4px 0"
            />
            <div class="task-meta">
              <span class="task-count">{{ task.fetchDone }} / {{ task.fetchTotal }} 章</span>
              <el-button
                v-if="task.fetchStatus === 'failed'"
                size="small"
                type="warning"
                link
                @click="resumeDownload(task)"
              >继续下载</el-button>
              <el-button
                v-if="task.fetchStatus === 'completed' || task.fetchStatus === 'failed'"
                size="small"
                type="info"
                link
                @click="store.removeTask(task.refId)"
              >移除</el-button>
            </div>
            <div v-if="task.fetchError" class="task-error">{{ task.fetchError }}</div>
          </div>
        </div>
      </transition>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { Loading, Check, Delete, ArrowUp, ArrowDown } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useDownloadStore } from '@/stores/download'
import { referenceApi } from '@/api'
import type { DownloadStatus, DownloadTask } from '@/stores/download'

const store = useDownloadStore()
const isCollapsed = ref(false)

// Show widget whenever there is at least one task (including completed/failed)
const showWidget = computed(() => store.allTasks.length > 0)

let autoCloseTimer: ReturnType<typeof setTimeout> | null = null

// When all active downloads finish, auto-clear completed tasks after 5s
// Failed tasks are left visible for user to retry/dismiss manually
watch(() => store.hasActive, (isActive) => {
  if (!isActive && store.allTasks.length > 0) {
    autoCloseTimer = setTimeout(() => {
      for (const task of [...store.allTasks]) {
        if (task.fetchStatus === 'completed') {
          store.removeTask(task.refId)
        }
      }
      autoCloseTimer = null
    }, 5000)
  } else if (isActive && autoCloseTimer !== null) {
    clearTimeout(autoCloseTimer)
    autoCloseTimer = null
  }
})

onBeforeUnmount(() => {
  if (autoCloseTimer !== null) {
    clearTimeout(autoCloseTimer)
  }
})

function toggle() {
  isCollapsed.value = !isCollapsed.value
}

function statusLabel(status: DownloadStatus): string {
  if (status === 'downloading') return '下载中'
  if (status === 'completed') return '已完成'
  return '失败'
}

function statusTagType(status: DownloadStatus): '' | 'success' | 'danger' | 'info' {
  if (status === 'downloading') return 'info'
  if (status === 'completed') return 'success'
  return 'danger'
}

async function resumeDownload(task: DownloadTask) {
  try {
    const res = await referenceApi.resumeDownload(task.refId)
    const data: any = res.data
    store.updateFromRef({
      id: task.refId,
      fetch_status: 'downloading',
      fetch_done: task.fetchDone,
      fetch_total: data.remaining ?? task.fetchTotal,
    })
    store.schedulePoll()
    ElMessage.success('已重新开始下载，请稍候…')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error || '重新下载失败')
  }
}

onMounted(() => {
  // Restore and start polling any in-progress downloads from a previous session
  store.restoreAndPoll()
  // Auto-expand if there are active tasks
  if (store.hasActive) isCollapsed.value = false
})
</script>

<style scoped>
.download-widget {
  position: fixed;
  bottom: 24px;
  right: 24px;
  width: 340px;
  background: var(--el-bg-color, #fff);
  border: 1px solid var(--el-border-color, #dcdfe6);
  border-radius: 10px;
  box-shadow: 0 4px 24px rgba(0, 0, 0, 0.14);
  z-index: 9000;
  overflow: hidden;
  transition: box-shadow 0.2s;
}
.download-widget:hover {
  box-shadow: 0 6px 30px rgba(0, 0, 0, 0.2);
}

/* Collapsed: only header visible */
.download-widget.collapsed .widget-body {
  display: none;
}

.widget-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  cursor: pointer;
  background: var(--el-fill-color-light, #f5f7fa);
  border-bottom: 1px solid var(--el-border-color-light, #e4e7ed);
  user-select: none;
}
.widget-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
  font-size: 13px;
}
.widget-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}
.toggle-icon {
  font-size: 14px;
  color: var(--el-text-color-secondary);
}
.spin-icon {
  animation: spin 1s linear infinite;
}
@keyframes spin {
  to { transform: rotate(360deg); }
}

.widget-body {
  max-height: 340px;
  overflow-y: auto;
  padding: 8px 0;
}
.empty-hint {
  text-align: center;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  padding: 20px;
}

.task-item {
  padding: 8px 14px;
  border-bottom: 1px solid var(--el-border-color-extra-light, #f2f6fc);
}
.task-item:last-child {
  border-bottom: none;
}
.task-info {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 2px;
}
.task-title {
  font-size: 13px;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 200px;
}
.task-status-tag {
  flex-shrink: 0;
}
.task-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 2px;
}
.task-error {
  color: var(--el-color-danger);
  font-size: 11px;
  margin-top: 2px;
  word-break: break-all;
}
.task-badge {
  margin-left: 4px;
}

.slide-enter-active,
.slide-leave-active {
  transition: max-height 0.2s ease, opacity 0.2s ease;
  overflow: hidden;
}
.slide-enter-from,
.slide-leave-to {
  max-height: 0;
  opacity: 0;
}
.slide-enter-to,
.slide-leave-from {
  max-height: 340px;
  opacity: 1;
}
</style>
