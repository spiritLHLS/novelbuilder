<template>
  <div class="p-6 max-w-5xl mx-auto space-y-6">
    <div class="flex items-center justify-between">
      <h1 class="text-2xl font-bold text-gray-900">📡 市场雷达</h1>
    </div>

    <div v-if="error" class="p-4 bg-red-50 border border-red-200 rounded-lg text-red-700 text-sm">{{ error }}</div>

    <!-- Scan form -->
    <div class="bg-white border border-gray-200 rounded-xl p-5">
      <h2 class="text-base font-semibold text-gray-800 mb-4">发起新扫描</h2>
      <div class="grid grid-cols-1 md:grid-cols-3 gap-4 mb-4">
        <div>
          <label class="text-xs text-gray-500 mb-1 block">小说类型 *</label>
          <input v-model="scanForm.genre" list="genres"
            class="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
            placeholder="如：玄幻、都市、科幻" />
          <datalist id="genres">
            <option v-for="g in genreList" :key="g" :value="g" />
          </datalist>
        </div>
        <div>
          <label class="text-xs text-gray-500 mb-1 block">目标平台</label>
          <select v-model="scanForm.platform"
            class="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500">
            <option value="">不限</option>
            <option v-for="p in platforms" :key="p" :value="p">{{ p }}</option>
          </select>
        </div>
        <div>
          <label class="text-xs text-gray-500 mb-1 block">分析重点</label>
          <select v-model="scanForm.focus"
            class="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500">
            <option value="">综合</option>
            <option v-for="f in focusList" :key="f" :value="f">{{ f }}</option>
          </select>
        </div>
      </div>
      <button @click="scan" :disabled="!scanForm.genre || scanning"
        class="px-6 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50 text-sm flex items-center gap-2">
        <span v-if="scanning" class="inline-block w-4 h-4 border-2 border-white border-t-transparent rounded-full animate-spin" />
        {{ scanning ? 'AI分析中，请稍候…' : '🔍 开始扫描' }}
      </button>
    </div>

    <!-- Latest result -->
    <div v-if="latestResult" class="bg-white border border-blue-200 rounded-xl p-5 space-y-5">
      <div class="flex items-center justify-between">
        <h2 class="text-base font-semibold text-gray-800">最新扫描结果</h2>
        <span class="text-xs text-gray-400">{{ formatTime(latestResult.created_at) }}</span>
      </div>

      <div class="grid grid-cols-1 md:grid-cols-2 gap-5">
        <!-- Trends -->
        <div class="space-y-2">
          <h3 class="text-sm font-semibold text-gray-700 flex items-center gap-1">🔥 当前热趋</h3>
          <div v-if="parsedResult.trends?.length" class="space-y-2">
            <div v-for="t in parsedResult.trends" :key="t.name"
              class="bg-orange-50 border border-orange-100 rounded-lg p-3 space-y-1">
              <div class="flex items-center justify-between">
                <span class="text-sm font-medium text-orange-800">{{ t.name }}</span>
                <span v-if="t.heat" class="text-xs bg-orange-200 text-orange-700 px-2 py-0.5 rounded-full">热度 {{ t.heat }}/10</span>
              </div>
              <p v-if="t.description" class="text-xs text-orange-600">{{ t.description }}</p>
            </div>
          </div>
          <div v-else class="text-xs text-gray-400 italic">暂无趋势数据</div>
        </div>

        <!-- Pain points -->
        <div class="space-y-2">
          <h3 class="text-sm font-semibold text-gray-700 flex items-center gap-1">😤 读者痛点</h3>
          <div v-if="parsedResult.reader_pain_points?.length" class="space-y-1.5">
            <div v-for="p in parsedResult.reader_pain_points" :key="p"
              class="flex items-start gap-2 text-sm text-gray-700 bg-red-50 border border-red-100 rounded-lg px-3 py-2">
              <span class="text-red-400 mt-0.5">•</span>
              {{ p }}
            </div>
          </div>
          <div v-else class="text-xs text-gray-400 italic">暂无数据</div>
        </div>

        <!-- Style guide -->
        <div class="space-y-2">
          <h3 class="text-sm font-semibold text-gray-700 flex items-center gap-1">✍️ 文风建议</h3>
          <div v-if="parsedResult.style_guide?.length" class="space-y-1.5">
            <div v-for="s in parsedResult.style_guide" :key="s"
              class="flex items-start gap-2 text-sm text-gray-700 bg-blue-50 border border-blue-100 rounded-lg px-3 py-2">
              <span class="text-blue-400 mt-0.5">→</span>
              {{ s }}
            </div>
          </div>
          <div v-else class="text-xs text-gray-400 italic">暂无数据</div>
        </div>

        <!-- Avoid patterns -->
        <div class="space-y-2">
          <h3 class="text-sm font-semibold text-gray-700 flex items-center gap-1">🚫 规避套路</h3>
          <div v-if="parsedResult.avoid_patterns?.length" class="space-y-1.5">
            <div v-for="a in parsedResult.avoid_patterns" :key="a"
              class="flex items-start gap-2 text-sm text-gray-700 bg-yellow-50 border border-yellow-100 rounded-lg px-3 py-2">
              <span class="text-yellow-500 mt-0.5">✗</span>
              {{ a }}
            </div>
          </div>
          <div v-else class="text-xs text-gray-400 italic">暂无数据</div>
        </div>
      </div>

      <!-- Opportunity note -->
      <div v-if="parsedResult.opportunity_note"
        class="bg-green-50 border border-green-200 rounded-xl px-5 py-4">
        <div class="text-sm font-semibold text-green-700 mb-1">💡 机会洞察</div>
        <p class="text-sm text-green-600">{{ parsedResult.opportunity_note }}</p>
      </div>
    </div>

    <!-- History -->
    <div v-if="history.length" class="space-y-3">
      <h2 class="text-base font-semibold text-gray-800">历史记录</h2>
      <div class="space-y-2">
        <div v-for="h in history" :key="h.id"
          class="bg-white border border-gray-200 rounded-lg px-4 py-3 flex items-center justify-between cursor-pointer hover:border-blue-300 transition-colors"
          @click="selectHistory(h)">
          <div class="flex items-center gap-3">
            <span class="text-sm font-medium text-gray-800">{{ h.genre }}</span>
            <span v-if="h.platform" class="text-xs text-gray-400 bg-gray-100 px-2 py-0.5 rounded">{{ h.platform }}</span>
            <span v-if="h.focus" class="text-xs text-blue-400 bg-blue-50 px-2 py-0.5 rounded">{{ h.focus }}</span>
          </div>
          <span class="text-xs text-gray-400">{{ formatTime(h.created_at) }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { radarApi } from '@/api'

const route = useRoute()
const projectId = route.params.projectId as string

const history = ref<any[]>([])
const latestResult = ref<any>(null)
const scanning = ref(false)
const error = ref('')

const scanForm = ref({ genre: '', platform: '', focus: '' })

const genreList = ['玄幻', '仙侠', '西幻', '都市', '科幻', '历史', '游戏', '武侠', '军事', '悬疑', '言情', '耽美', '同人']
const platforms = ['起点中文网', '晋江文学城', '番茄小说', '七猫小说', 'Webnovel']
const focusList = ['趋势分析', '读者痛点', '竞品分析', '文风建议', '节奏把控']

const parsedResult = computed(() => {
  if (!latestResult.value?.result) return {}
  try {
    return typeof latestResult.value.result === 'string'
      ? JSON.parse(latestResult.value.result)
      : latestResult.value.result
  } catch {
    return {}
  }
})

function formatTime(t: string) {
  if (!t) return ''
  const d = new Date(t)
  return d.toLocaleDateString('zh-CN') + ' ' + d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
}

function selectHistory(h: any) {
  latestResult.value = h
}

async function load() {
  try {
    const res = await radarApi.history(projectId)
    history.value = res.data.data || []
    if (history.value.length) latestResult.value = history.value[0]
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  }
}

async function scan() {
  if (!scanForm.value.genre || scanning.value) return
  scanning.value = true
  error.value = ''
  try {
    const res = await radarApi.scan(projectId, scanForm.value)
    latestResult.value = res.data.data
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  } finally {
    scanning.value = false
  }
}

onMounted(load)
</script>
