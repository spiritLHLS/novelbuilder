/**
 * Download store — tracks background reference-book download tasks across page navigation.
 *
 * Each entry maps ref_id → DownloadTask. The store polls GET /references/:id every few
 * seconds while the task is active, keeping the floating DownloadWidget up to date even
 * when the user navigates away from the References page.
 */
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { referenceApi } from '@/api'

export type DownloadStatus = 'downloading' | 'completed' | 'failed'

export interface DownloadTask {
  refId: string
  projectId: string
  title: string
  fetchStatus: DownloadStatus
  fetchDone: number
  fetchTotal: number
  fetchError?: string
  /** Timestamp when the task was added locally */
  startedAt: number
}

const POLL_INTERVAL_MS = 3000
const STORAGE_KEY = 'nb_download_tasks'

function loadPersisted(): Record<string, DownloadTask> {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) return JSON.parse(raw)
  } catch { /* ignore */ }
  return {}
}

function savePersisted(tasks: Record<string, DownloadTask>) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(tasks))
  } catch { /* ignore */ }
}

export const useDownloadStore = defineStore('download', () => {
  const tasks = ref<Record<string, DownloadTask>>(loadPersisted())
  let _pollTimer: ReturnType<typeof setTimeout> | null = null

  const activeTasks = computed(() =>
    Object.values(tasks.value).filter(t => t.fetchStatus === 'downloading')
  )
  const hasActive = computed(() => activeTasks.value.length > 0)
  const allTasks = computed(() => Object.values(tasks.value)
    .sort((a, b) => b.startedAt - a.startedAt)
  )

  /** Call when a new download is initiated. */
  function addTask(refId: string, projectId: string, title: string, total: number) {
    tasks.value[refId] = {
      refId,
      projectId,
      title,
      fetchStatus: 'downloading',
      fetchDone: 0,
      fetchTotal: total,
      startedAt: Date.now(),
    }
    savePersisted(tasks.value)
    schedulePoll()
  }

  /** Update task from a reference_materials API response. */
  function updateFromRef(ref: any) {
    const id: string = ref.id
    if (!tasks.value[id]) return
    tasks.value[id] = {
      ...tasks.value[id],
      title: ref.title || tasks.value[id].title,
      fetchStatus: (ref.fetch_status as DownloadStatus) ?? tasks.value[id].fetchStatus,
      fetchDone: ref.fetch_done ?? tasks.value[id].fetchDone,
      fetchTotal: ref.fetch_total ?? tasks.value[id].fetchTotal,
      fetchError: ref.fetch_error,
    }
    savePersisted(tasks.value)
  }

  function removeTask(refId: string) {
    delete tasks.value[refId]
    savePersisted(tasks.value)
  }

  function clearCompleted() {
    for (const id of Object.keys(tasks.value)) {
      if (tasks.value[id].fetchStatus !== 'downloading') {
        delete tasks.value[id]
      }
    }
    savePersisted(tasks.value)
  }

  async function pollAll() {
    const active = Object.values(tasks.value).filter(t => t.fetchStatus === 'downloading')
    for (const task of active) {
      try {
        const res = await referenceApi.get(task.refId)
        // GET /references/:id returns { data: ref }, so unwrap one level
        const ref: any = res.data?.data ?? res.data
        updateFromRef(ref)
      } catch { /* silently ignore transient errors */ }
    }
  }

  function schedulePoll() {
    if (_pollTimer !== null) return
    _pollTimer = setInterval(async () => {
      await pollAll()
      if (!hasActive.value) {
        if (_pollTimer !== null) {
          clearInterval(_pollTimer)
          _pollTimer = null
        }
      }
    }, POLL_INTERVAL_MS)
  }

  /** Restore active downloads from localStorage on app start. */
  function restoreAndPoll() {
    const stored = loadPersisted()
    // Merge stored tasks — overwrite only if not already tracked
    for (const [id, task] of Object.entries(stored)) {
      if (!tasks.value[id]) {
        tasks.value[id] = task
      }
    }
    if (hasActive.value) {
      schedulePoll()
    }
  }

  return {
    tasks,
    activeTasks,
    allTasks,
    hasActive,
    addTask,
    updateFromRef,
    removeTask,
    clearCompleted,
    pollAll,
    restoreAndPoll,
    schedulePoll,
  }
})
