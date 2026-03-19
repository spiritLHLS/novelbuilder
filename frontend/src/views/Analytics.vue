<template>
  <div class="p-6 max-w-7xl mx-auto space-y-6">
    <div class="flex items-center justify-between">
      <h1 class="text-2xl font-bold text-gray-900">📊 数据分析</h1>
      <button @click="load" :disabled="loading"
        class="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50 text-sm">
        {{ loading ? '加载中…' : '刷新' }}
      </button>
    </div>

    <div v-if="error" class="p-4 bg-red-50 border border-red-200 rounded-lg text-red-700 text-sm">{{ error }}</div>

    <div v-if="data" class="space-y-6">
      <!-- Overview Cards -->
      <div class="grid grid-cols-2 sm:grid-cols-4 gap-4">
        <div v-for="card in overviewCards1" :key="card.label"
          class="bg-white border border-gray-200 rounded-xl p-4 flex flex-col gap-1">
          <div class="text-2xl">{{ card.icon }}</div>
          <div class="text-2xl font-bold text-gray-900">{{ card.value }}</div>
          <div class="text-sm text-gray-500">{{ card.label }}</div>
        </div>
      </div>

      <div class="grid grid-cols-2 sm:grid-cols-4 gap-4">
        <div v-for="card in overviewCards2" :key="card.label"
          class="bg-white border border-gray-200 rounded-xl p-4 flex flex-col gap-1">
          <div class="text-2xl">{{ card.icon }}</div>
          <div class="text-2xl font-bold text-gray-900">{{ card.value }}</div>
          <div class="text-sm text-gray-500">{{ card.label }}</div>
        </div>
      </div>

      <!-- AIGC Distribution -->
      <div class="bg-white border border-gray-200 rounded-xl p-5">
        <h2 class="font-semibold text-gray-800 mb-4">AIGC 检测分布</h2>
        <div class="flex items-end gap-4 h-16">
          <div v-for="(bucket, key) in aigcBuckets" :key="key" class="flex flex-col items-center gap-1 flex-1">
            <div class="w-full rounded-t-md transition-all"
              :style="{ height: (bucket as any).pct + '%', minHeight: '4px' }"
              :class="(bucket as any).color"></div>
            <span class="text-xs text-gray-600">{{ (bucket as any).label }}<br/>{{ (bucket as any).count }}</span>
          </div>
        </div>
      </div>

      <!-- Top Failing Audit Dimensions -->
      <div v-if="data.top_issues?.length" class="bg-white border border-gray-200 rounded-xl p-5">
        <h2 class="font-semibold text-gray-800 mb-4">高频审计问题（维度）</h2>
        <div class="space-y-2">
          <div v-for="issue in data.top_issues" :key="issue.dimension"
            class="flex items-center gap-3">
            <div class="text-sm text-gray-700 w-40 truncate">{{ issue.dimension }}</div>
            <div class="flex-1 bg-gray-100 rounded-full h-3">
              <div class="bg-red-500 h-3 rounded-full"
                :style="{ width: barPct(issue.count) + '%' }"></div>
            </div>
            <span class="text-sm text-gray-500 w-8 text-right">{{ issue.count }}</span>
          </div>
        </div>
      </div>

      <!-- Per-Chapter Table -->
      <div class="bg-white border border-gray-200 rounded-xl overflow-hidden">
        <div class="px-5 py-4 border-b border-gray-100">
          <h2 class="font-semibold text-gray-800">章节明细</h2>
        </div>
        <div class="overflow-x-auto">
          <table class="w-full text-sm">
            <thead class="bg-gray-50 text-gray-500">
              <tr>
                <th class="px-4 py-2 text-left">#</th>
                <th class="px-4 py-2 text-left">标题</th>
                <th class="px-4 py-2 text-right">字数</th>
                <th class="px-4 py-2 text-center">状态</th>
                <th class="px-4 py-2 text-center">审计</th>
                <th class="px-4 py-2 text-right">审计分</th>
                <th class="px-4 py-2 text-right">AI概率</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="ch in data.chapter_stats" :key="ch.chapter_num"
                class="border-t border-gray-50 hover:bg-gray-50">
                <td class="px-4 py-2 text-gray-500">{{ ch.chapter_num }}</td>
                <td class="px-4 py-2 text-gray-800 max-w-xs truncate">{{ ch.title || '—' }}</td>
                <td class="px-4 py-2 text-right text-gray-600">{{ ch.word_count.toLocaleString() }}</td>
                <td class="px-4 py-2 text-center">
                  <span :class="{
                    'px-2 py-0.5 rounded-full text-xs font-medium': true,
                    'bg-green-100 text-green-700': ch.status === 'approved',
                    'bg-yellow-100 text-yellow-700': ch.status === 'pending_review',
                    'bg-red-100 text-red-700': ch.status === 'rejected',
                    'bg-gray-100 text-gray-600': ch.status === 'draft',
                  }">{{ ch.status }}</span>
                </td>
                <td class="px-4 py-2 text-center">
                  <span v-if="ch.audit_passed === null" class="text-gray-400">—</span>
                  <span v-else-if="ch.audit_passed" class="text-green-600">✅</span>
                  <span v-else class="text-red-500">❌</span>
                </td>
                <td class="px-4 py-2 text-right">
                  <span v-if="ch.audit_score" :class="scoreColor(ch.audit_score)">
                    {{ (ch.audit_score * 100).toFixed(0) }}
                  </span>
                  <span v-else class="text-gray-400">—</span>
                </td>
                <td class="px-4 py-2 text-right">
                  <span v-if="ch.ai_probability" :class="aiColor(ch.ai_probability)">
                    {{ (ch.ai_probability * 100).toFixed(0) }}%
                  </span>
                  <span v-else class="text-gray-400">—</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <div v-else-if="!loading" class="text-center py-16 text-gray-500">
      暂无数据，点击刷新加载。
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { analyticsApi } from '@/api'

const route = useRoute()
const projectId = route.params.projectId as string

const loading = ref(false)
const error = ref('')
const data = ref<any>(null)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const res = await analyticsApi.get(projectId)
    data.value = res.data.data
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  } finally {
    loading.value = false
  }
}

onMounted(load)

const overviewCards1 = computed(() => data.value ? [
  { icon: '📖', label: '总章节',     value: data.value.total_chapters },
  { icon: '✅', label: '已通过',     value: data.value.approved_chapters },
  { icon: '✍️', label: '总字数',     value: data.value.total_words.toLocaleString() },
  { icon: '🎯', label: '审计通过率', value: `${(data.value.audit_pass_rate * 100).toFixed(1)}%` },
] : [])

const overviewCards2 = computed(() => data.value ? [
  { icon: '🔍', label: '已审计',     value: data.value.audited_chapters },
  { icon: '⭐', label: '平均审计分', value: (data.value.avg_audit_score * 100).toFixed(1) },
  { icon: '🤖', label: '平均AI概率', value: `${(data.value.avg_ai_probability * 100).toFixed(1)}%` },
  { icon: '🪝', label: '开放伏笔',   value: data.value.open_foreshadowings },
] : [])

const aigcBuckets = computed(() => {
  if (!data.value) return {}
  const b = data.value.aigc_buckets || {}
  const total = (b.low ?? 0) + (b.medium ?? 0) + (b.high ?? 0) || 1
  return {
    low:    { label: '低风险', count: b.low ?? 0,    pct: ((b.low ?? 0) / total * 100).toFixed(0),    color: 'bg-green-400' },
    medium: { label: '中风险', count: b.medium ?? 0, pct: ((b.medium ?? 0) / total * 100).toFixed(0), color: 'bg-yellow-400' },
    high:   { label: '高风险', count: b.high ?? 0,   pct: ((b.high ?? 0) / total * 100).toFixed(0),   color: 'bg-red-500' },
  }
})

function barPct(count: number) {
  if (!data.value?.top_issues?.length) return 0
  const max = Math.max(...data.value.top_issues.map((i: any) => i.count))
  return max ? (count / max * 100).toFixed(0) : 0
}

function scoreColor(score: number) {
  if (score >= 0.8) return 'text-green-600 font-medium'
  if (score >= 0.6) return 'text-yellow-600'
  return 'text-red-500'
}

function aiColor(prob: number) {
  if (prob < 0.33) return 'text-green-600'
  if (prob < 0.67) return 'text-yellow-600'
  return 'text-red-500'
}
</script>
