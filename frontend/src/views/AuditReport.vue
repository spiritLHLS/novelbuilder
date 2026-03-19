<template>
  <div class="audit-report p-6 max-w-5xl mx-auto">
    <div class="flex items-center justify-between mb-6">
      <div>
        <h1 class="text-2xl font-bold">章节审计报告</h1>
        <p class="text-gray-500 mt-1">33维度质量检测 · {{ chapterId?.slice(0, 8) }}</p>
      </div>
      <div class="flex gap-3">
        <button @click="runAudit" :disabled="running"
          class="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50">
          {{ running ? '审计中…' : '重新审计' }}
        </button>
        <button @click="$router.back()" class="px-4 py-2 border rounded hover:bg-gray-50">返回</button>
      </div>
    </div>

    <!-- Score Banner -->
    <div v-if="report" class="grid grid-cols-3 gap-4 mb-6">
      <div class="bg-white border rounded-lg p-4 text-center">
        <div class="text-4xl font-bold" :class="scoreColor(report.overall_score)">
          {{ Math.round(report.overall_score * 100) }}
        </div>
        <div class="text-sm text-gray-500 mt-1">综合得分</div>
      </div>
      <div class="bg-white border rounded-lg p-4 text-center">
        <div class="text-4xl font-bold" :class="report.passed ? 'text-green-600' : 'text-red-600'">
          {{ report.passed ? '通过' : '未通过' }}
        </div>
        <div class="text-sm text-gray-500 mt-1">审计状态</div>
      </div>
      <div class="bg-white border rounded-lg p-4 text-center">
        <div class="text-4xl font-bold" :class="aiProbColor(report.ai_probability)">
          {{ Math.round(report.ai_probability * 100) }}%
        </div>
        <div class="text-sm text-gray-500 mt-1">AI味概率</div>
      </div>
    </div>

    <!-- Dimension Grid -->
    <div v-if="report && report.dimensions" class="bg-white border rounded-lg p-6 mb-6">
      <h2 class="text-lg font-semibold mb-4">33维度明细</h2>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div v-for="(dim, name) in report.dimensions" :key="name"
          class="border rounded p-3" :class="dim.passed ? 'border-green-200 bg-green-50' : 'border-red-200 bg-red-50'">
          <div class="flex items-center justify-between mb-1">
            <span class="font-medium text-sm">{{ formatDimName(String(name)) }}</span>
            <span class="text-sm font-mono" :class="scoreColor(dim.score)">
              {{ Math.round(dim.score * 100) }}
            </span>
          </div>
          <div class="w-full bg-gray-200 rounded-full h-1.5">
            <div class="h-1.5 rounded-full transition-all"
              :class="dim.passed ? 'bg-green-500' : 'bg-red-500'"
              :style="{ width: `${dim.score * 100}%` }" />
          </div>
          <ul v-if="dim.issues?.length" class="mt-2 space-y-0.5">
            <li v-for="issue in dim.issues" :key="issue"
              class="text-xs text-red-700">· {{ issue }}</li>
          </ul>
        </div>
      </div>
    </div>

    <!-- Issues Summary -->
    <div v-if="report?.issues?.length" class="bg-white border border-red-200 rounded-lg p-6 mb-6">
      <h2 class="text-lg font-semibold mb-3 text-red-700">主要问题</h2>
      <ul class="space-y-2">
        <li v-for="issue in report.issues" :key="issue"
          class="flex items-start gap-2 text-sm">
          <span class="text-red-500 mt-0.5">⚠</span>
          <span>{{ issue }}</span>
        </li>
      </ul>
    </div>

    <!-- Empty state -->
    <div v-if="!report && !running" class="text-center py-16 text-gray-400">
      <div class="text-5xl mb-4">📊</div>
      <p>暂无审计报告</p>
      <p class="text-sm mt-1">点击「重新审计」开始分析</p>
    </div>

    <!-- Loading -->
    <div v-if="running" class="text-center py-16 text-gray-400">
      <div class="animate-spin text-4xl mb-4">⏳</div>
      <p>正在进行 33 维度审计，请稍候…</p>
    </div>

    <!-- Error -->
    <div v-if="error" class="bg-red-50 border border-red-200 rounded p-4 text-red-700 text-sm">
      {{ error }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { auditApi } from '@/api'

const route = useRoute()
const chapterId = route.params.chapterId as string

const report = ref<any>(null)
const running = ref(false)
const error = ref('')

const DIM_LABELS: Record<string, string> = {
  chapter_length_adequacy: '篇幅充足性',
  ai_pattern_detection: 'AI文风检测',
  high_freq_ai_words: '高频AI词汇',
  repetitive_sentence_structure: '重复句式',
  excessive_summarization: '过度总结',
  cliche_density: '陈词滥调密度',
  character_consistency: '角色一致性',
  character_voice: '人物声音',
  emotional_arc: '情感弧线',
  narrative_pacing: '叙事节奏',
  plot_continuity: '情节连贯',
  foreshadowing_payoff: '伏笔回收',
  world_consistency: '世界观一致',
  resource_tracking: '资源追踪',
  dialogue_naturalness: '对话自然度',
  show_dont_tell: '具体化程度',
  sensory_detail: '感官细节',
  tension_building: '张力构建',
  theme_coherence: '主题一致',
  scene_transitions: '场景转换',
  point_of_view: '视角一致',
  verb_vigor: '动词活力',
  vocabulary_variety: '词汇多样',
  sentence_variety: '句式多样',
  paragraph_flow: '段落流畅',
  subtext_depth: '潜台词深度',
  conflict_development: '冲突发展',
  character_growth: '人物成长',
  reader_engagement: '读者代入',
  ending_hook: '结尾钩子',
  originality_fingerprint: '原创指纹',
  outline_adherence: '大纲符合',
  chapter_summary_quality: '章节摘要',
}

function formatDimName(name: string): string {
  return DIM_LABELS[name] ?? name.replace(/_/g, ' ')
}

function scoreColor(score: number): string {
  if (score >= 0.8) return 'text-green-600'
  if (score >= 0.6) return 'text-yellow-600'
  return 'text-red-600'
}

function aiProbColor(prob: number): string {
  if (prob < 0.3) return 'text-green-600'
  if (prob < 0.6) return 'text-yellow-600'
  return 'text-red-600'
}

async function loadReport() {
  try {
    const res = await auditApi.getReport(chapterId)
    report.value = res.data.data
  } catch (e: any) {
    if (e.response?.status !== 404) {
      error.value = e.response?.data?.error || e.message
    }
  }
}

async function runAudit() {
  running.value = true
  error.value = ''
  try {
    const res = await auditApi.run(chapterId)
    report.value = res.data.data
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  } finally {
    running.value = false
  }
}

onMounted(loadReport)
</script>
