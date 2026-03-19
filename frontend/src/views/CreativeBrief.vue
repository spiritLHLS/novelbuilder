<template>
  <div class="creative-brief p-6 max-w-4xl mx-auto">
    <div class="flex items-center justify-between mb-6">
      <div>
        <h1 class="text-2xl font-bold">创作简报</h1>
        <p class="text-gray-500 mt-1">输入你的创作构想，AI 为你生成世界观文档与写作规则</p>
      </div>
      <button @click="$router.back()" class="px-4 py-2 border rounded hover:bg-gray-50">返回</button>
    </div>

    <!-- Input form -->
    <div class="bg-white border rounded-lg p-6 mb-6">
      <div class="mb-4">
        <label class="block text-sm font-medium text-gray-700 mb-1">
          创作简报 <span class="text-red-500">*</span>
        </label>
        <textarea v-model="briefText" rows="8"
          class="w-full border rounded p-3 text-sm focus:ring-2 focus:ring-blue-500 focus:border-transparent"
          placeholder="描述你的故事世界、角色设定、核心冲突、写作风格……越详细越好。" />
      </div>
      <div class="flex gap-4 items-end">
        <div class="flex-1">
          <label class="block text-sm font-medium text-gray-700 mb-1">体裁 (可选)</label>
          <input v-model="genre" type="text"
            class="w-full border rounded p-2 text-sm"
            placeholder="e.g. 都市修真、玄幻、历史" />
        </div>
        <button @click="generate" :disabled="running || !briefText.trim()"
          class="px-6 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50">
          {{ running ? '生成中…' : '生成文档' }}
        </button>
      </div>
    </div>

    <!-- Error -->
    <div v-if="error" class="bg-red-50 border border-red-200 rounded p-3 text-red-700 text-sm mb-4">
      {{ error }}
    </div>

    <!-- Loading state -->
    <div v-if="running" class="text-center py-12 text-gray-400">
      <div class="animate-pulse text-4xl mb-3">✍️</div>
      <p>AI 正在构建世界观文档与写作规则…</p>
    </div>

    <!-- Results -->
    <div v-if="result && !running" class="space-y-6">

      <!-- World Bible -->
      <div class="bg-white border rounded-lg overflow-hidden">
        <div class="px-5 py-3 bg-gray-50 border-b flex items-center justify-between">
          <h2 class="font-semibold">世界观文档 (World Bible)</h2>
          <button @click="copyText(worldBibleText)" class="text-sm text-blue-600 hover:underline">
            复制
          </button>
        </div>
        <div class="p-5">
          <div v-if="typeof result.world_bible === 'object'" class="space-y-4">
            <div v-for="(val, key) in result.world_bible" :key="key">
              <h3 class="text-sm font-semibold text-gray-600 uppercase tracking-wide mb-1">{{ key }}</h3>
              <p class="text-sm text-gray-800 whitespace-pre-wrap">{{ val }}</p>
            </div>
          </div>
          <p v-else class="text-sm whitespace-pre-wrap">{{ result.world_bible }}</p>
        </div>
      </div>

      <!-- Writing Rules -->
      <div class="bg-white border rounded-lg overflow-hidden">
        <div class="px-5 py-3 bg-gray-50 border-b flex items-center justify-between">
          <h2 class="font-semibold">写作规则 (Book Rules)</h2>
          <button @click="copyText(result.rules_content)" class="text-sm text-blue-600 hover:underline">
            复制
          </button>
        </div>
        <div class="p-5">
          <textarea :value="result.rules_content" rows="8"
            class="w-full border rounded p-2 text-sm font-mono" readonly />
        </div>
      </div>

      <!-- Style Guide -->
      <div v-if="result.style_guide" class="bg-white border rounded-lg overflow-hidden">
        <div class="px-5 py-3 bg-gray-50 border-b">
          <h2 class="font-semibold">文风指南 (Style Guide)</h2>
        </div>
        <div class="p-5">
          <p class="text-sm whitespace-pre-wrap text-gray-800">{{ result.style_guide }}</p>
        </div>
      </div>

      <!-- Anti-AI wordlist -->
      <div v-if="result.anti_ai_wordlist?.length" class="bg-white border rounded-lg overflow-hidden">
        <div class="px-5 py-3 bg-gray-50 border-b">
          <h2 class="font-semibold">禁用词汇 (Anti-AI Wordlist)</h2>
        </div>
        <div class="p-5 flex flex-wrap gap-2">
          <span v-for="word in result.anti_ai_wordlist" :key="word"
            class="px-2 py-0.5 bg-red-100 text-red-700 rounded text-sm">
            {{ word }}
          </span>
        </div>
      </div>

      <!-- Save confirmation -->
      <div class="bg-green-50 border border-green-200 rounded p-4 text-green-800 text-sm">
        ✅ 以上内容已自动保存为本项目的「写作规则」。
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRoute } from 'vue-router'
import { creativeBriefApi } from '@/api'

const route = useRoute()
const projectId = route.params.projectId as string

const briefText = ref('')
const genre = ref('')
const running = ref(false)
const error = ref('')
const result = ref<any>(null)

const worldBibleText = ref('')

async function generate() {
  running.value = true
  error.value = ''
  result.value = null
  try {
    const res = await creativeBriefApi.generate(projectId, {
      brief_text: briefText.value,
      genre: genre.value || undefined,
    })
    result.value = res.data.data
    worldBibleText.value = typeof result.value.world_bible === 'object'
      ? JSON.stringify(result.value.world_bible, null, 2)
      : String(result.value.world_bible)
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  } finally {
    running.value = false
  }
}

async function copyText(text: string) {
  await navigator.clipboard.writeText(text ?? '')
}
</script>
