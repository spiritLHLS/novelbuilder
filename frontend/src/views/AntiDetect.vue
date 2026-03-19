<template>
  <div class="anti-detect p-6 max-w-5xl mx-auto">
    <div class="flex items-center justify-between mb-6">
      <div>
        <h1 class="text-2xl font-bold">去 AI 味改写</h1>
        <p class="text-gray-500 mt-1">识别并替换 AI 文风特征，注入人类写作风格</p>
      </div>
      <button @click="$router.back()" class="px-4 py-2 border rounded hover:bg-gray-50">返回</button>
    </div>

    <!-- Controls -->
    <div class="bg-white border rounded-lg p-4 mb-6 flex items-center gap-6">
      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">改写强度</label>
        <div class="flex gap-2">
          <button v-for="opt in intensityOptions" :key="opt.value"
            @click="intensity = opt.value"
            class="px-3 py-1.5 rounded border text-sm transition-colors"
            :class="intensity === opt.value
              ? 'bg-blue-600 text-white border-blue-600'
              : 'border-gray-300 hover:bg-gray-50'">
            {{ opt.label }}
          </button>
        </div>
      </div>
      <div class="flex-1" />
      <button @click="runRewrite" :disabled="running || !originalText"
        class="px-5 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50">
        {{ running ? '改写中…' : '开始改写' }}
      </button>
    </div>

    <!-- Error -->
    <div v-if="error" class="bg-red-50 border border-red-200 rounded p-3 text-red-700 text-sm mb-4">
      {{ error }}
    </div>

    <!-- Content panels -->
    <div class="grid grid-cols-2 gap-4">
      <!-- Original -->
      <div class="bg-white border rounded-lg overflow-hidden flex flex-col">
        <div class="px-4 py-2 border-b bg-gray-50 flex items-center justify-between">
          <span class="font-medium text-sm">原文</span>
          <span v-if="result" class="text-xs text-gray-500">
            AI味概率: <strong :class="probColor(result.ai_prob_before)">
              {{ Math.round(result.ai_prob_before * 100) }}%
            </strong>
          </span>
        </div>
        <textarea v-model="originalText" rows="22"
          class="flex-1 p-4 text-sm font-mono resize-none focus:outline-none"
          placeholder="粘贴章节原文…" />
      </div>

      <!-- Rewritten -->
      <div class="bg-white border rounded-lg overflow-hidden flex flex-col">
        <div class="px-4 py-2 border-b bg-gray-50 flex items-center justify-between">
          <span class="font-medium text-sm">改写结果</span>
          <span v-if="result" class="text-xs text-gray-500">
            AI味概率: <strong :class="probColor(result.ai_prob_after)">
              {{ Math.round(result.ai_prob_after * 100) }}%
            </strong>
          </span>
        </div>
        <div v-if="running" class="flex-1 flex items-center justify-center text-gray-400">
          <div class="text-center">
            <div class="animate-spin text-3xl mb-2">⏳</div>
            <p class="text-sm">改写中，请稍候…</p>
          </div>
        </div>
        <textarea v-else v-model="rewrittenText" rows="22"
          class="flex-1 p-4 text-sm font-mono resize-none focus:outline-none"
          placeholder="改写结果将显示在此处…" />
      </div>
    </div>

    <!-- Changes summary -->
    <div v-if="result?.changes_made?.length" class="bg-white border rounded-lg p-4 mt-4">
      <h3 class="font-medium mb-3">改写说明 ({{ result.changes_made.length }} 处)</h3>
      <ul class="space-y-1.5">
        <li v-for="change in result.changes_made" :key="change"
          class="flex items-start gap-2 text-sm text-gray-700">
          <span class="text-blue-500 mt-0.5 flex-shrink-0">✎</span>
          <span>{{ change }}</span>
        </li>
      </ul>
    </div>

    <!-- Copy button -->
    <div v-if="rewrittenText" class="mt-4 flex justify-end">
      <button @click="copyResult"
        class="px-4 py-2 border rounded hover:bg-gray-50 text-sm">
        {{ copied ? '已复制 ✓' : '复制改写结果' }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { antiDetectApi, chapterApi } from '@/api'

const route = useRoute()
const chapterId = route.params.chapterId as string

const originalText = ref('')
const rewrittenText = ref('')
const intensity = ref<'light' | 'medium' | 'heavy'>('medium')
const running = ref(false)
const copied = ref(false)
const error = ref('')
const result = ref<any>(null)

const intensityOptions = [
  { value: 'light' as const, label: '轻度' },
  { value: 'medium' as const, label: '中度' },
  { value: 'heavy' as const, label: '深度' },
]

function probColor(prob: number): string {
  if (prob < 0.3) return 'text-green-600'
  if (prob < 0.6) return 'text-yellow-600'
  return 'text-red-600'
}

async function runRewrite() {
  if (!originalText.value.trim()) return
  running.value = true
  error.value = ''
  result.value = null
  try {
    const res = await antiDetectApi.rewrite(chapterId, intensity.value)
    result.value = res.data.data
    rewrittenText.value = result.value.rewritten_text
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  } finally {
    running.value = false
  }
}

async function copyResult() {
  await navigator.clipboard.writeText(rewrittenText.value)
  copied.value = true
  setTimeout(() => { copied.value = false }, 2000)
}

onMounted(async () => {
  try {
    const res = await chapterApi.get('', chapterId)
    originalText.value = res.data.data?.content || ''
  } catch { /* ignore */ }
})
</script>
