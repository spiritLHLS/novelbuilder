import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import type { UploadFile } from 'element-plus'

import { blueprintApi, projectApi } from '@/api'

export type BlueprintField = 'master_outline' | 'relation_graph' | 'global_timeline'

export interface BlueprintGenerationForm {
  volume_count: number
  chapter_words_min: number
  chapter_words_max: number
  idea: string
}

export function formatBlueprintDate(value: string) {
  return value ? new Date(value).toLocaleString('zh-CN') : '-'
}

export function blueprintHasData(value: unknown): boolean {
  if (value == null) return false
  if (typeof value === 'string') return value.trim() !== ''
  if (Array.isArray(value)) return value.length > 0
  if (typeof value === 'object') {
    const record = value as Record<string, unknown>
    if (record.raw_content) {
      try {
        const parsed = JSON.parse(String(record.raw_content))
        return parsed != null && Object.keys(parsed).length > 0
      } catch {
        return String(record.raw_content).trim() !== ''
      }
    }
    return Object.keys(record).length > 0
  }
  return Boolean(value)
}

export function extractBlueprintFieldText(value: unknown, field: BlueprintField): string {
  if (value == null) return ''
  if (typeof value === 'string') return value
  if (typeof value === 'object' && !Array.isArray(value) && 'raw_content' in value) {
    try {
      const parsed = JSON.parse(String((value as Record<string, unknown>).raw_content))
      if (parsed && typeof parsed[field] === 'string') {
        return parsed[field]
      }
      return String((value as Record<string, unknown>).raw_content)
    } catch {
      return String((value as Record<string, unknown>).raw_content)
    }
  }
  return JSON.stringify(value, null, 2)
}

function parseStructuredText<T>(
  value: unknown,
  field: BlueprintField,
  splitter: RegExp,
  leftKey: keyof T,
  rightKey: keyof T,
): T[] {
  if (value == null) return []
  if (typeof value !== 'string' && !(typeof value === 'object' && !Array.isArray(value) && 'raw_content' in value)) {
    return [{ [leftKey]: '', [rightKey]: JSON.stringify(value, null, 2) } as T]
  }

  const text = extractBlueprintFieldText(value, field)
  return text
    .split(splitter)
    .filter((item) => item.trim())
    .map((item) => {
      const separatorIndex = item.indexOf('：') !== -1 ? item.indexOf('：') : item.indexOf(':')
      if (separatorIndex > 0) {
        return {
          [leftKey]: item.slice(0, separatorIndex).trim(),
          [rightKey]: item.slice(separatorIndex + 1).trim(),
        } as T
      }
      return {
        [leftKey]: '',
        [rightKey]: item.trim(),
      } as T
    })
}

export function parseMasterOutline(value: unknown): Array<{ vol: string; desc: string }> {
  return parseStructuredText(value, 'master_outline', /。\s*/, 'vol', 'desc')
}

export function parseRelationGraph(value: unknown): Array<{ pair: string; desc: string }> {
  return parseStructuredText(value, 'relation_graph', /[;；]\s*/, 'pair', 'desc')
}

export function parseGlobalTimeline(value: unknown): Array<{ point: string; event: string }> {
  return parseStructuredText(value, 'global_timeline', /[;；]\s*/, 'point', 'event')
}

function downloadJsonFile(data: unknown, filename: string) {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  anchor.click()
  URL.revokeObjectURL(url)
}

async function readBlueprintImportFile(file: File) {
  const content = await file.text()
  return JSON.parse(content)
}

export function useBlueprintTransfer(projectId: string, fetchAll: () => Promise<void>) {
  const showImportDialog = ref(false)
  const importing = ref(false)
  const importFileList = ref<UploadFile[]>([])
  const importFileContent = ref<unknown>(null)

  async function exportBlueprint() {
    try {
      const res = await blueprintApi.export(projectId)
      const data = res?.data?.data
      if (!data) {
        ElMessage.error('导出失败：无数据')
        return
      }
      downloadJsonFile(data, `blueprint-${projectId}-${Date.now()}.json`)
      ElMessage.success('蓝图已导出')
    } catch {
      ElMessage.error('导出失败')
    }
  }

  async function handleImportFileChange(file: UploadFile) {
    if (!file.raw) return
    try {
      importFileContent.value = await readBlueprintImportFile(file.raw)
      importFileList.value = [file]
    } catch {
      ElMessage.error('JSON文件格式错误')
      importFileList.value = []
      importFileContent.value = null
    }
  }

  async function confirmImport() {
    if (!importFileContent.value) {
      ElMessage.warning('请先选择文件')
      return
    }
    importing.value = true
    try {
      await blueprintApi.import(projectId, importFileContent.value)
      ElMessage.success('蓝图已导入')
      showImportDialog.value = false
      importFileList.value = []
      importFileContent.value = null
      await fetchAll()
    } catch {
      ElMessage.error('导入失败')
    } finally {
      importing.value = false
    }
  }

  return {
    showImportDialog,
    importing,
    importFileList,
    exportBlueprint,
    handleImportFileChange,
    confirmImport,
  }
}

export async function loadBlueprintGenerationDefaults(
  projectId: string,
  currentForm: BlueprintGenerationForm,
): Promise<BlueprintGenerationForm> {
  try {
    const res = await projectApi.get(projectId)
    const project = res?.data?.data
    if (!project) {
      return currentForm
    }

    const targetWords = project.target_words || 0
    const chapterWords = project.chapter_words || 3000
    let volumeCount = currentForm.volume_count
    let wordsMin = currentForm.chapter_words_min
    let wordsMax = currentForm.chapter_words_max

    if (targetWords > 0) {
      volumeCount = Math.max(4, Math.round(targetWords / 100000))
    }
    if (chapterWords > 0) {
      wordsMin = Math.round(chapterWords * 2 / 3)
      wordsMax = Math.round(chapterWords * 4 / 3)
    }

    return {
      ...currentForm,
      volume_count: volumeCount,
      chapter_words_min: wordsMin,
      chapter_words_max: wordsMax,
      idea: '',
    }
  } catch {
    return currentForm
  }
}