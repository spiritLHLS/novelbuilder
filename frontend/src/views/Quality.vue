<template>
  <div class="quality">
    <div class="page-header">
      <h1>质量监控中心</h1>
      <el-button type="primary" @click="runFullCheck" :loading="checking">
        <el-icon><DataAnalysis /></el-icon>全量质检
      </el-button>
    </div>

    <!-- Overview Stats -->
    <el-row :gutter="20">
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card">
          <el-statistic title="平均质量分" :value="avgScore" :precision="1">
            <template #suffix>/ 10</template>
          </el-statistic>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card">
          <el-statistic title="已检查章节" :value="checkedCount" />
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card">
          <el-statistic title="严重问题" :value="criticalCount">
            <template #prefix><el-icon style="color: #f56c6c;"><Warning /></el-icon></template>
          </el-statistic>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover" class="stat-card">
          <el-statistic title="AI概率均值" :value="avgAiProb" :precision="1">
            <template #suffix>%</template>
          </el-statistic>
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="20" style="margin-top: 20px;">
      <!-- Score Trend Chart -->
      <el-col :span="12">
        <el-card shadow="hover">
          <template #header><span>质量趋势</span></template>
          <v-chart :option="trendChartOption" style="height: 350px;" autoresize />
        </el-card>
      </el-col>

      <!-- 4-Role Radar -->
      <el-col :span="12">
        <el-card shadow="hover">
          <template #header><span>四角色评分雷达图</span></template>
          <v-chart :option="radarChartOption" style="height: 350px;" autoresize />
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="20" style="margin-top: 20px;">
      <!-- AI Detection -->
      <el-col :span="12">
        <el-card shadow="hover">
          <template #header><span>AI检测指标</span></template>
          <v-chart :option="aiDetectionChart" style="height: 300px;" autoresize />
        </el-card>
      </el-col>

      <!-- Issues List -->
      <el-col :span="12">
        <el-card shadow="hover">
          <template #header>
            <div class="card-header">
              <span>质量问题</span>
              <el-badge :value="allIssues.length" type="danger" />
            </div>
          </template>
          <div class="issues-list">
            <div v-for="(issue, i) in allIssues" :key="i" class="issue-row">
              <el-tag :type="issue.severity === 'critical' ? 'danger' : 'warning'" size="small">
                {{ issue.severity === 'critical' ? '严重' : '警告' }}
              </el-tag>
              <span class="issue-chapter">第{{ issue.chapter }}章</span>
              <span class="issue-desc">{{ issue.description }}</span>
              <el-tag size="small" type="info">{{ issue.role }}</el-tag>
            </div>
            <el-empty v-if="!allIssues.length" description="暂无问题" :image-size="60" />
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- Chapter-level Details -->
    <el-card shadow="hover" style="margin-top: 20px;">
      <template #header><span>各章质量详情</span></template>
      <el-table :data="chapterReports" style="width: 100%;">
        <el-table-column prop="chapter_number" label="章节" width="80" />
        <el-table-column prop="title" label="标题" />
        <el-table-column label="总分" width="100">
          <template #default="{ row }">
            <el-tag :type="row.overall_score >= 7 ? 'success' : row.overall_score >= 5 ? 'warning' : 'danger'">
              {{ row.overall_score?.toFixed(1) || '-' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="编辑" width="80">
          <template #default="{ row }">{{ row.scores?.editor?.toFixed(1) || '-' }}</template>
        </el-table-column>
        <el-table-column label="读者" width="80">
          <template #default="{ row }">{{ row.scores?.reader?.toFixed(1) || '-' }}</template>
        </el-table-column>
        <el-table-column label="逻辑" width="80">
          <template #default="{ row }">{{ row.scores?.logic_reviewer?.toFixed(1) || '-' }}</template>
        </el-table-column>
        <el-table-column label="反AI" width="80">
          <template #default="{ row }">{{ row.scores?.anti_ai?.toFixed(1) || '-' }}</template>
        </el-table-column>
        <el-table-column label="AI概率" width="100">
          <template #default="{ row }">
            <el-progress :percentage="(row.ai_probability || 0) * 100" :stroke-width="6"
              :color="aiProbColor(row.ai_probability)" :format="(p: number) => p.toFixed(0) + '%'" />
          </template>
        </el-table-column>
        <el-table-column label="问题数" width="80">
          <template #default="{ row }">
            <el-badge :value="row.issue_count || 0" :type="row.issue_count > 0 ? 'danger' : 'info'" />
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { chapterApi, qualityApi } from '@/api'
import VChart from 'vue-echarts'

const route = useRoute()
const projectId = route.params.projectId as string

const checking = ref(false)
const chapterReports = ref<any[]>([])

const avgScore = computed(() => {
  const scores = chapterReports.value.filter(r => r.overall_score).map(r => r.overall_score)
  return scores.length ? scores.reduce((a, b) => a + b, 0) / scores.length : 0
})

const checkedCount = computed(() => chapterReports.value.filter(r => r.overall_score).length)

const criticalCount = computed(() =>
  allIssues.value.filter(i => i.severity === 'critical').length
)

const avgAiProb = computed(() => {
  const probs = chapterReports.value.filter(r => r.ai_probability != null).map(r => r.ai_probability)
  return probs.length ? (probs.reduce((a, b) => a + b, 0) / probs.length) * 100 : 0
})

const allIssues = computed(() => {
  const issues: any[] = []
  chapterReports.value.forEach(r => {
    (r.issues || []).forEach((issue: any) => {
      issues.push({ ...issue, chapter: r.chapter_number })
    })
  })
  return issues
})

const trendChartOption = computed(() => ({
  backgroundColor: 'transparent',
  xAxis: {
    type: 'category',
    data: chapterReports.value.map(r => `第${r.chapter_number}章`),
    axisLabel: { color: '#888' },
  },
  yAxis: { type: 'value', max: 10, axisLabel: { color: '#888' } },
  series: [
    {
      name: '总分', type: 'line', smooth: true,
      data: chapterReports.value.map(r => r.overall_score || 0),
      lineStyle: { color: '#409eff' }, itemStyle: { color: '#409eff' },
    },
    {
      name: '编辑', type: 'line', smooth: true,
      data: chapterReports.value.map(r => r.scores?.editor || 0),
      lineStyle: { color: '#67c23a' }, itemStyle: { color: '#67c23a' },
    },
    {
      name: '读者', type: 'line', smooth: true,
      data: chapterReports.value.map(r => r.scores?.reader || 0),
      lineStyle: { color: '#e6a23c' }, itemStyle: { color: '#e6a23c' },
    },
    {
      name: '反AI', type: 'line', smooth: true,
      data: chapterReports.value.map(r => r.scores?.anti_ai || 0),
      lineStyle: { color: '#f56c6c' }, itemStyle: { color: '#f56c6c' },
    },
  ],
  tooltip: { trigger: 'axis' },
  legend: { data: ['总分', '编辑', '读者', '反AI'], textStyle: { color: '#888' }, bottom: 0 },
  grid: { top: 20, right: 20, bottom: 40, left: 40 },
}))

const radarChartOption = computed(() => {
  const avgScores = { editor: 0, reader: 0, logic_reviewer: 0, anti_ai: 0 }
  let count = 0
  chapterReports.value.forEach(r => {
    if (r.scores) {
      count++
      Object.keys(avgScores).forEach(k => {
        (avgScores as any)[k] += r.scores[k] || 0
      })
    }
  })
  if (count > 0) {
    Object.keys(avgScores).forEach(k => { (avgScores as any)[k] /= count })
  }
  return {
    backgroundColor: 'transparent',
    radar: {
      indicator: [
        { name: '编辑审核', max: 10 },
        { name: '读者体验', max: 10 },
        { name: '逻辑审核', max: 10 },
        { name: '反AI检测', max: 10 },
      ],
      shape: 'circle',
    },
    series: [{
      type: 'radar',
      data: [{
        value: [avgScores.editor, avgScores.reader, avgScores.logic_reviewer, avgScores.anti_ai],
        name: '平均得分',
        areaStyle: { opacity: 0.3 },
      }],
    }],
    tooltip: {},
  }
})

const aiDetectionChart = computed(() => ({
  backgroundColor: 'transparent',
  xAxis: {
    type: 'category',
    data: chapterReports.value.map(r => `第${r.chapter_number}章`),
    axisLabel: { color: '#888' },
  },
  yAxis: [
    { type: 'value', max: 1, name: 'AI概率', axisLabel: { color: '#888' }, nameTextStyle: { color: '#888' } },
    { type: 'value', name: '突发度', axisLabel: { color: '#888' }, nameTextStyle: { color: '#888' } },
  ],
  series: [
    {
      name: 'AI概率', type: 'bar',
      data: chapterReports.value.map(r => r.ai_probability || 0),
      itemStyle: {
        color: (params: any) => {
          const v = params.value
          return v > 0.7 ? '#f56c6c' : v > 0.4 ? '#e6a23c' : '#67c23a'
        },
      },
    },
    {
      name: '突发度', type: 'line', yAxisIndex: 1, smooth: true,
      data: chapterReports.value.map(r => r.burstiness || 0),
      lineStyle: { color: '#909399' }, itemStyle: { color: '#909399' },
    },
  ],
  tooltip: { trigger: 'axis' },
  legend: { data: ['AI概率', '突发度'], textStyle: { color: '#888' }, bottom: 0 },
  grid: { top: 30, right: 60, bottom: 40, left: 50 },
}))

function aiProbColor(prob: number) {
  if (prob > 0.7) return '#f56c6c'
  if (prob > 0.4) return '#e6a23c'
  return '#67c23a'
}

onMounted(fetchReports)

async function fetchReports() {
  try {
    const chapRes = await chapterApi.list(projectId)
    const chapters = (chapRes.data.data || []).sort((a: any, b: any) => a.chapter_number - b.chapter_number)
    chapterReports.value = chapters.map((ch: any) => ({
      chapter_number: ch.chapter_number,
      title: ch.title,
      overall_score: ch.quality_report?.overall_score,
      scores: ch.quality_report?.scores,
      issues: ch.quality_report?.issues,
      issue_count: ch.quality_report?.issues?.length || 0,
      ai_probability: ch.quality_report?.ai_probability,
      burstiness: ch.quality_report?.burstiness,
    }))
  } catch { /* empty */ }
}

async function runFullCheck() {
  checking.value = true
  try {
    const chapRes = await chapterApi.list(projectId)
    const chapters = chapRes.data.data || []
    for (const ch of chapters) {
      try {
        await qualityApi.runCheck(projectId, ch.id)
      } catch { /* continue */ }
    }
    ElMessage.success('全量质检完成')
    await fetchReports()
  } catch {
    ElMessage.error('质检失败')
  } finally {
    checking.value = false
  }
}
</script>

<style scoped>
.quality { max-width: 1400px; margin: 0 auto; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; }
.page-header h1 { font-size: 24px; color: #e0e0e0; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.stat-card { text-align: center; }
.issues-list { max-height: 300px; overflow-y: auto; }
.issue-row { display: flex; align-items: center; gap: 8px; padding: 8px 0; border-bottom: 1px solid var(--nb-divider); }
.issue-chapter { color: #409eff; font-size: 13px; white-space: nowrap; }
.issue-desc { color: var(--nb-text-secondary); font-size: 13px; flex: 1; }
</style>
