import { computed, onUnmounted, ref } from 'vue'
import { ElMessage } from 'element-plus'

import { referenceApi } from '@/api'

export function useReferenceDeepAnalysis(fetchRefs: () => Promise<void>) {
  const showDeepAnalysisDialog = ref(false)
  const deepAnalysisRef = ref<any>(null)
  const deepAnalysisJob = ref<any>(null)
  const deepAnalysisDialogLoading = ref(false)
  const deepAnalysisStarting = ref(false)
  const deepAnalysisImporting = ref(false)
  const deepAnalysisResetting = ref(false)
  let deepAnalysisPollTimer: ReturnType<typeof setInterval> | null = null

  const daStatusType = computed(() => {
    const status = deepAnalysisJob.value?.status
    if (status === 'completed') return 'success'
    if (status === 'failed') return 'danger'
    if (status === 'cancelled') return 'info'
    return 'warning'
  })

  const daStatusText = computed(() => {
    const map: Record<string, string> = {
      pending: '等待中',
      running: '分析中',
      completed: '已完成',
      failed: '失败',
      cancelled: '已取消',
    }
    return map[deepAnalysisJob.value?.status] ?? deepAnalysisJob.value?.status ?? '—'
  })

  const daChars = computed<any[]>(() => {
    const raw = deepAnalysisJob.value?.extracted_characters
    if (!raw) return []
    try {
      return Array.isArray(raw) ? raw : JSON.parse(raw)
    } catch {
      return []
    }
  })

  const daWorld = computed<any>(() => {
    const raw = deepAnalysisJob.value?.extracted_world
    if (!raw) return {}
    try {
      return typeof raw === 'object' && !Array.isArray(raw) ? raw : JSON.parse(raw)
    } catch {
      return {}
    }
  })

  const daOutline = computed<any[]>(() => {
    const raw = deepAnalysisJob.value?.extracted_outline
    if (!raw) return []
    try {
      return Array.isArray(raw) ? raw : JSON.parse(raw)
    } catch {
      return []
    }
  })

  const daGlossary = computed<any[]>(() => {
    const raw = deepAnalysisJob.value?.extracted_glossary
    if (!raw) return []
    try {
      return Array.isArray(raw) ? raw : JSON.parse(raw)
    } catch {
      return []
    }
  })

  const daForeshadowings = computed<any[]>(() => {
    const raw = deepAnalysisJob.value?.extracted_foreshadowings
    if (!raw) return []
    try {
      return Array.isArray(raw) ? raw : JSON.parse(raw)
    } catch {
      return []
    }
  })

  function roleTagType(role: string): string {
    const normalized = (role || '').toLowerCase()
    if (normalized.includes('主角') || normalized === 'protagonist') return 'success'
    if (normalized.includes('反派') || normalized === 'antagonist') return 'danger'
    if (normalized.includes('配角') || normalized === 'supporting') return 'warning'
    return 'info'
  }

  function stopDeepAnalysisPoll() {
    if (deepAnalysisPollTimer) {
      clearInterval(deepAnalysisPollTimer)
      deepAnalysisPollTimer = null
    }
  }

  function startDeepAnalysisPoll(referenceId: string) {
    stopDeepAnalysisPoll()
    deepAnalysisPollTimer = setInterval(async () => {
      try {
        const res = await referenceApi.getDeepAnalysisJob(referenceId)
        deepAnalysisJob.value = (res.data as any).data ?? deepAnalysisJob.value
      } catch {
        return
      }

      const status = deepAnalysisJob.value?.status
      if (status !== 'pending' && status !== 'running') {
        stopDeepAnalysisPoll()
      }
    }, 3000)
  }

  async function openDeepAnalysisDialog(referenceRow: any) {
    deepAnalysisRef.value = referenceRow
    deepAnalysisJob.value = null
    deepAnalysisDialogLoading.value = true
    showDeepAnalysisDialog.value = true
    try {
      const res = await referenceApi.getDeepAnalysisJob(referenceRow.id)
      deepAnalysisJob.value = (res.data as any).data ?? null
    } catch {
      deepAnalysisJob.value = null
    } finally {
      deepAnalysisDialogLoading.value = false
    }

    if (deepAnalysisJob.value?.status === 'pending' || deepAnalysisJob.value?.status === 'running') {
      startDeepAnalysisPoll(referenceRow.id)
    }
  }

  async function doStartDeepAnalysis() {
    if (!deepAnalysisRef.value) return
    deepAnalysisStarting.value = true
    try {
      const res = await referenceApi.startDeepAnalysis(deepAnalysisRef.value.id)
      deepAnalysisJob.value = (res.data as any).data
      startDeepAnalysisPoll(deepAnalysisRef.value.id)
    } catch (error: any) {
      ElMessage.error(error?.response?.data?.error || '启动深度分析失败')
    } finally {
      deepAnalysisStarting.value = false
    }
  }

  async function cancelDeepAnalysis() {
    if (!deepAnalysisRef.value) return
    try {
      await referenceApi.cancelDeepAnalysis(deepAnalysisRef.value.id)
      if (deepAnalysisJob.value) {
        deepAnalysisJob.value = { ...deepAnalysisJob.value, status: 'cancelled' }
      }
      stopDeepAnalysisPoll()
      ElMessage.success('已取消')
    } catch {
      ElMessage.error('取消失败')
    }
  }

  async function importDeepAnalysisResult() {
    if (!deepAnalysisRef.value) return
    deepAnalysisImporting.value = true
    try {
      await referenceApi.importDeepAnalysisResult(deepAnalysisRef.value.id)
      ElMessage.success('已成功导入到项目（人物、世界观、大纲、术语表、伏笔），请到对应页面查看')
      showDeepAnalysisDialog.value = false
      await fetchRefs()
    } catch (error: any) {
      ElMessage.error(error?.response?.data?.error || '导入失败')
    } finally {
      deepAnalysisImporting.value = false
    }
  }

  async function doResetDeepAnalysis() {
    if (!deepAnalysisRef.value) return
    deepAnalysisResetting.value = true
    try {
      stopDeepAnalysisPoll()
      await referenceApi.resetDeepAnalysis(deepAnalysisRef.value.id)
      deepAnalysisJob.value = null
      ElMessage.success('已清除历史分析记录，点击「开始深度分析」重新开始')
    } catch (error: any) {
      ElMessage.error(error?.response?.data?.error || '重置失败')
    } finally {
      deepAnalysisResetting.value = false
    }
  }

  onUnmounted(stopDeepAnalysisPoll)

  return {
    showDeepAnalysisDialog,
    deepAnalysisRef,
    deepAnalysisJob,
    deepAnalysisDialogLoading,
    deepAnalysisStarting,
    deepAnalysisImporting,
    deepAnalysisResetting,
    daStatusType,
    daStatusText,
    daChars,
    daWorld,
    daOutline,
    daGlossary,
    daForeshadowings,
    roleTagType,
    openDeepAnalysisDialog,
    doStartDeepAnalysis,
    stopDeepAnalysisPoll,
    cancelDeepAnalysis,
    importDeepAnalysisResult,
    doResetDeepAnalysis,
  }
}