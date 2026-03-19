<template>
  <div class="p-6 max-w-6xl mx-auto space-y-6">
    <div class="flex items-center justify-between">
      <h1 class="text-2xl font-bold text-gray-900">💫 情感弧线</h1>
      <button @click="showAdd = true"
        class="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 text-sm">
        ＋ 记录情感
      </button>
    </div>

    <div v-if="error" class="p-4 bg-red-50 border border-red-200 rounded-lg text-red-700 text-sm">{{ error }}</div>

    <!-- Character filter tabs -->
    <div class="flex flex-wrap gap-2" v-if="characters.length">
      <button @click="filterChar = null"
        :class="['px-3 py-1 rounded-full text-sm border transition-colors',
          filterChar === null ? 'bg-blue-600 text-white border-blue-600' : 'bg-white text-gray-600 border-gray-300 hover:border-blue-400']">
        全部
      </button>
      <button v-for="c in characters" :key="c.id"
        @click="filterChar = c.id"
        :class="['px-3 py-1 rounded-full text-sm border transition-colors',
          filterChar === c.id ? 'bg-blue-600 text-white border-blue-600' : 'bg-white text-gray-600 border-gray-300 hover:border-blue-400']">
        {{ c.name }}
      </button>
    </div>

    <!-- Arc timeline per character -->
    <div v-if="filteredChars.length" class="space-y-6">
      <div v-for="char in filteredChars" :key="char.id"
        class="bg-white border border-gray-200 rounded-xl p-5 space-y-3">
        <div class="flex items-center gap-3">
          <div class="w-8 h-8 rounded-full bg-gradient-to-br from-purple-400 to-blue-500 flex items-center justify-center text-white text-sm font-bold">
            {{ char.name.slice(0,1) }}
          </div>
          <span class="font-semibold text-gray-800">{{ char.name }}</span>
          <span class="text-xs text-gray-400">{{ arcsByChar[char.id]?.length ?? 0 }} 条记录</span>
        </div>

        <!-- Timeline -->
        <div v-if="arcsByChar[char.id]?.length" class="relative">
          <!-- Intensity line chart (SVG) -->
          <svg :viewBox="`0 0 ${svgWidth} 60`" class="w-full h-16 overflow-visible">
            <polyline
              :points="linePoints(arcsByChar[char.id])"
              fill="none" stroke="#6366f1" stroke-width="2"
              stroke-linecap="round" stroke-linejoin="round" />
            <circle v-for="(e, i) in arcsByChar[char.id]" :key="e.id"
              :cx="pointX(i, arcsByChar[char.id].length)"
              :cy="60 - e.intensity * 50"
              r="4" fill="#6366f1" stroke="white" stroke-width="2"
              class="cursor-pointer"
              @mouseenter="tooltip = e"
              @mouseleave="tooltip = null" />
          </svg>

          <!-- X axis chapter labels -->
          <div class="flex justify-between text-xs text-gray-400 mt-1 px-1">
            <span v-for="(e, i) in arcsByChar[char.id]" :key="i">第{{ e.chapter_num }}章</span>
          </div>

          <!-- Emotion chips -->
          <div class="flex flex-wrap gap-1 mt-2">
            <span v-for="e in arcsByChar[char.id]" :key="e.id"
              class="px-2 py-0.5 rounded-full text-xs flex items-center gap-1"
              :style="{ background: emotionBg(e.emotion), color: emotionFg(e.emotion) }">
              {{ emotionEmoji(e.emotion) }} {{ e.emotion }}
              <span class="font-medium">{{ (e.intensity * 100).toFixed(0) }}%</span>
              <button @click="del(e)" class="ml-0.5 opacity-50 hover:opacity-100 text-xs">&times;</button>
            </span>
          </div>
        </div>
        <div v-else class="text-xs text-gray-400 italic">暂无情感记录，点击右上角"记录情感"添加。</div>
      </div>
    </div>

    <!-- Tooltip -->
    <div v-if="tooltip" class="fixed bottom-6 right-6 bg-gray-900 text-white text-xs rounded-lg px-4 py-3 space-y-1 z-50 shadow-xl">
      <div>第{{ tooltip.chapter_num }}章</div>
      <div class="font-semibold">{{ tooltip.emotion }} — 强度 {{ (tooltip.intensity * 100).toFixed(0) }}%</div>
      <div v-if="tooltip.note">{{ tooltip.note }}</div>
    </div>

    <!-- Add Modal -->
    <div v-if="showAdd" class="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div class="bg-white rounded-2xl shadow-xl w-full max-w-md p-6 space-y-4">
        <h2 class="text-lg font-semibold">记录情感状态</h2>
        <div class="space-y-3">
          <div>
            <label class="text-xs text-gray-500 mb-1 block">角色 *</label>
            <select v-model="form.character_id" class="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500">
              <option value="">请选择角色</option>
              <option v-for="c in characters" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <div>
              <label class="text-xs text-gray-500 mb-1 block">章节 *</label>
              <input v-model.number="form.chapter_num" type="number" min="1"
                class="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
            </div>
            <div>
              <label class="text-xs text-gray-500 mb-1 block">强度 (0-1)</label>
              <input v-model.number="form.intensity" type="number" min="0" max="1" step="0.1"
                class="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
            </div>
          </div>
          <div>
            <label class="text-xs text-gray-500 mb-1 block">情感 *</label>
            <div class="flex flex-wrap gap-2">
              <button v-for="e in emotions" :key="e"
                @click="form.emotion = e"
                :class="['px-2 py-1 rounded-full text-xs border transition-colors',
                  form.emotion === e ? 'bg-blue-600 text-white border-blue-600' : 'bg-gray-50 text-gray-600 border-gray-200 hover:border-blue-400']">
                {{ emotionEmoji(e) }} {{ e }}
              </button>
            </div>
          </div>
          <div>
            <label class="text-xs text-gray-500 mb-1 block">备注</label>
            <input v-model="form.note" class="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" placeholder="可选说明" />
          </div>
        </div>
        <div class="flex gap-3 pt-2">
          <button @click="showAdd = false" class="flex-1 border rounded-lg py-2 text-sm text-gray-600 hover:bg-gray-50">取消</button>
          <button @click="save" :disabled="!form.character_id || !form.chapter_num || !form.emotion || saving"
            class="flex-1 bg-blue-600 text-white rounded-lg py-2 text-sm hover:bg-blue-700 disabled:opacity-50">
            {{ saving ? '保存中…' : '保存' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { emotionalArcApi, characterApi } from '@/api'

const route = useRoute()
const projectId = route.params.projectId as string

const arcs = ref<any[]>([])
const characters = ref<any[]>([])
const filterChar = ref<string | null>(null)
const showAdd = ref(false)
const saving = ref(false)
const error = ref('')
const tooltip = ref<any>(null)

const emotions = ['喜悦', '悲伤', '愤怒', '恐惧', '期待', '惊喜', '信任', '厌恶', '平静', '困惑', '坚定', '绝望']

const form = ref({ character_id: '', chapter_num: null as number | null, emotion: '', intensity: 0.5, note: '' })

const svgWidth = 600

const arcsByChar = computed(() => {
  const m: Record<string, any[]> = {}
  for (const a of arcs.value) {
    if (!m[a.character_id]) m[a.character_id] = []
    m[a.character_id].push(a)
  }
  // sort by chapter_num
  for (const k of Object.keys(m)) m[k].sort((a, b) => a.chapter_num - b.chapter_num)
  return m
})

const filteredChars = computed(() =>
  filterChar.value ? characters.value.filter(c => c.id === filterChar.value) : characters.value
)

function pointX(i: number, total: number) {
  if (total === 1) return svgWidth / 2
  return (i / (total - 1)) * svgWidth
}

function linePoints(entries: any[]) {
  return entries.map((e, i) =>
    `${pointX(i, entries.length)},${60 - e.intensity * 50}`
  ).join(' ')
}

const emotionMap: Record<string, { bg: string; fg: string; emoji: string }> = {
  喜悦: { bg: '#fef9c3', fg: '#854d0e', emoji: '😄' },
  悲伤: { bg: '#dbeafe', fg: '#1d4ed8', emoji: '😢' },
  愤怒: { bg: '#fee2e2', fg: '#991b1b', emoji: '😠' },
  恐惧: { bg: '#f5f3ff', fg: '#6d28d9', emoji: '😨' },
  期待: { bg: '#d1fae5', fg: '#065f46', emoji: '🤩' },
  惊喜: { bg: '#fce7f3', fg: '#9d174d', emoji: '😲' },
  信任: { bg: '#e0f2fe', fg: '#0c4a6e', emoji: '🫂' },
  厌恶: { bg: '#fef3c7', fg: '#92400e', emoji: '🤢' },
  平静: { bg: '#f0fdf4', fg: '#166534', emoji: '😌' },
  困惑: { bg: '#f3f4f6', fg: '#374151', emoji: '😕' },
  坚定: { bg: '#ede9fe', fg: '#4c1d95', emoji: '😤' },
  绝望: { bg: '#1f2937', fg: '#f9fafb', emoji: '😞' },
}

function emotionBg(e: string) { return emotionMap[e]?.bg ?? '#f3f4f6' }
function emotionFg(e: string) { return emotionMap[e]?.fg ?? '#374151' }
function emotionEmoji(e: string) { return emotionMap[e]?.emoji ?? '🎭' }

async function load() {
  try {
    const [arcsRes, charRes] = await Promise.all([
      emotionalArcApi.list(projectId),
      characterApi.list(projectId),
    ])
    arcs.value = arcsRes.data.data || []
    characters.value = charRes.data.data || []
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  }
}

async function save() {
  if (!form.value.character_id || !form.value.chapter_num || !form.value.emotion) return
  saving.value = true
  try {
    await emotionalArcApi.upsert(projectId, form.value)
    showAdd.value = false
    form.value = { character_id: '', chapter_num: null, emotion: '', intensity: 0.5, note: '' }
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  } finally {
    saving.value = false
  }
}

async function del(entry: any) {
  if (!confirm(`删除第${entry.chapter_num}章的情感记录？`)) return
  try {
    await emotionalArcApi.delete(entry.id)
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  }
}

onMounted(load)
</script>
