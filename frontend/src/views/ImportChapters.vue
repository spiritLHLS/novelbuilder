<template>
  <div class="import-chapters p-6 max-w-4xl mx-auto">
    <div class="flex items-center justify-between mb-6">
      <div>
        <h1 class="text-2xl font-bold">导入章节</h1>
        <p class="text-gray-500 mt-1">粘贴已有作品，自动分章并逆向还原世界设定</p>
      </div>
      <button @click="$router.back()" class="px-4 py-2 border rounded hover:bg-gray-50">返回</button>
    </div>

    <!-- Past imports list -->
    <div v-if="imports.length" class="bg-white border rounded-lg p-4 mb-6">
      <h2 class="font-semibold mb-3">历史导入记录</h2>
      <div class="space-y-2">
        <div v-for="imp in imports" :key="imp.id"
          class="flex items-center justify-between py-2 border-b last:border-0">
          <div class="text-sm">
            <span class="font-mono text-gray-400 mr-2">{{ imp.id.slice(0, 8) }}</span>
            <span class="px-2 py-0.5 rounded text-xs mr-2"
              :class="statusClass(imp.status)">{{ statusLabel(imp.status) }}</span>
            <span v-if="imp.total_chapters">{{ imp.total_chapters }} 章</span>
            <span v-if="imp.fanfic_mode" class="ml-2 text-purple-600 text-xs">[{{ imp.fanfic_mode }}]</span>
          </div>
          <div class="flex gap-2">
            <button v-if="imp.status === 'pending'" @click="startProcess(imp.id as string)"
              class="px-3 py-1 text-sm bg-blue-600 text-white rounded hover:bg-blue-700">
              开始处理
            </button>
            <button v-if="imp.status === 'completed'" @click="viewReverse(imp)"
              class="px-3 py-1 text-sm border rounded hover:bg-gray-50">
              查看逆向结果
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- New import form -->
    <div class="bg-white border rounded-lg p-6">
      <h2 class="font-semibold mb-4">新建导入</h2>
      <div class="space-y-4">
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">
            原始文本 <span class="text-red-500">*</span>
          </label>
          <textarea v-model="sourceText" rows="12"
            class="w-full border rounded p-3 text-sm font-mono focus:ring-2 focus:ring-blue-500"
            placeholder="粘贴小说全文或片段…" />
        </div>
        <div class="grid grid-cols-2 gap-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">
              章节分割正则
              <span class="ml-1 text-xs text-gray-400">(默认: 第.{1,4}[章节回])</span>
            </label>
            <input v-model="splitPattern" type="text"
              class="w-full border rounded p-2 text-sm font-mono"
              placeholder="第.{1,4}[章节回]" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">同人模式 (可选)</label>
            <select v-model="fanficMode" class="w-full border rounded p-2 text-sm">
              <option value="">无 (原创续写)</option>
              <option value="canon">Canon (正典)</option>
              <option value="au">AU (架空)</option>
              <option value="ooc">OOC (性格偏差)</option>
              <option value="cp">CP (角色配对)</option>
            </select>
          </div>
        </div>
        <div class="flex justify-end gap-3">
          <button @click="createImport" :disabled="creating || !sourceText.trim()"
            class="px-5 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50">
            {{ creating ? '创建中…' : '创建导入任务' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Error -->
    <div v-if="error" class="bg-red-50 border border-red-200 rounded p-3 text-red-700 text-sm mt-4">
      {{ error }}
    </div>

    <!-- Reverse-engineered results modal -->
    <div v-if="selectedImport" class="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div class="bg-white rounded-xl shadow-2xl max-w-2xl w-full max-h-[80vh] overflow-y-auto">
        <div class="p-5 border-b flex items-center justify-between">
          <h2 class="font-semibold text-lg">逆向还原结果</h2>
          <button @click="selectedImport = null" class="text-gray-400 hover:text-gray-700 text-xl">×</button>
        </div>
        <div class="p-5 space-y-4">
          <div v-for="(val, key) in selectedImport.reverse_engineered" :key="key">
            <h3 class="font-medium text-sm text-gray-600 uppercase tracking-wide mb-1">
              {{ reverseLabel(String(key)) }}
            </h3>
            <div v-if="typeof val === 'object'" class="bg-gray-50 border rounded p-3 text-sm font-mono">
              <pre class="whitespace-pre-wrap">{{ JSON.stringify(val, null, 2) }}</pre>
            </div>
            <p v-else class="text-sm whitespace-pre-wrap bg-gray-50 border rounded p-3">{{ val }}</p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { importApi } from '@/api'

const route = useRoute()
const projectId = route.params.projectId as string

const imports = ref<any[]>([])
const sourceText = ref('')
const splitPattern = ref('')
const fanficMode = ref('')
const creating = ref(false)
const error = ref('')
const selectedImport = ref<any>(null)

const REVERSE_LABELS: Record<string, string> = {
  world_state: '世界状态',
  character_matrix: '角色矩阵',
  resource_ledger: '资源账本',
  foreshadowing_hooks: '伏笔钩子',
  plot_threads: '情节线索',
  theme_analysis: '主题分析',
  style_fingerprint: '文风指纹',
}

function reverseLabel(key: string) {
  return REVERSE_LABELS[key] ?? key
}

function statusClass(status: string) {
  return {
    pending: 'bg-gray-100 text-gray-600',
    processing: 'bg-blue-100 text-blue-700',
    completed: 'bg-green-100 text-green-700',
    failed: 'bg-red-100 text-red-700',
  }[status] ?? 'bg-gray-100 text-gray-600'
}

function statusLabel(status: string) {
  return { pending: '待处理', processing: '处理中', completed: '已完成', failed: '失败' }[status] ?? status
}

async function loadImports() {
  try {
    const res = await importApi.list(projectId)
    imports.value = res.data.data ?? []
  } catch { /* ignore */ }
}

async function createImport() {
  if (!sourceText.value.trim()) return
  creating.value = true
  error.value = ''
  try {
    const res = await importApi.create(projectId, {
      source_text: sourceText.value,
      split_pattern: splitPattern.value || undefined,
      fanfic_mode: fanficMode.value || null,
    })
    const imp = res.data.data
    imports.value.unshift(imp)
    sourceText.value = ''
    splitPattern.value = ''
    fanficMode.value = ''
    // Auto-start processing
    await startProcess(imp.id)
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  } finally {
    creating.value = false
  }
}

async function startProcess(importId: string) {
  try {
    await importApi.process(importId)
    // Update status in list
    const idx = imports.value.findIndex(i => i.id === importId)
    if (idx >= 0) imports.value[idx].status = 'processing'
    // Poll for completion
    pollStatus(importId)
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  }
}

async function pollStatus(importId: string) {
  const poll = async () => {
    try {
      const res = await importApi.get(importId)
      const imp = res.data.data
      const idx = imports.value.findIndex(i => i.id === importId)
      if (idx >= 0) imports.value[idx] = { ...imports.value[idx], ...imp }
      if (imp.status === 'processing' || imp.status === 'pending') {
        setTimeout(poll, 3000)
      }
    } catch { /* ignore */ }
  }
  setTimeout(poll, 2000)
}

function viewReverse(imp: any) {
  selectedImport.value = {
    ...imp,
    reverse_engineered: typeof imp.reverse_engineered === 'string'
      ? JSON.parse(imp.reverse_engineered)
      : (imp.reverse_engineered ?? {}),
  }
}

onMounted(loadImports)
</script>
