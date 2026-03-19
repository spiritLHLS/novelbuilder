<template>
  <div class="studio">
    <div class="page-header">
      <h1>创作工作台</h1>
      <el-tag>{{ projectStore.currentProject?.title }}</el-tag>
    </div>

    <el-row :gutter="20">
      <!-- 概览卡片 -->
      <el-col :span="6">
        <el-card class="stat-card" shadow="hover">
          <el-statistic title="总章节数" :value="stats.chapters" />
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card" shadow="hover">
          <el-statistic title="已通过章节" :value="stats.approved" />
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card" shadow="hover">
          <el-statistic title="总字数" :value="stats.totalWords" />
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card class="stat-card" shadow="hover">
          <el-statistic title="角色数量" :value="stats.characters" />
        </el-card>
      </el-col>
    </el-row>

    <!-- 快捷操作 -->
    <el-card class="section-card" header="快捷操作" shadow="never">
      <el-row :gutter="16">
        <el-col :span="6">
          <el-button type="primary" size="large" class="quick-btn" @click="goTo('blueprint')">
            <el-icon><Document /></el-icon>
            生成整书蓝图
          </el-button>
        </el-col>
        <el-col :span="6">
          <el-button type="success" size="large" class="quick-btn" @click="continueWrite">
            <el-icon><Edit /></el-icon>
            继续生成章节
          </el-button>
        </el-col>
        <el-col :span="6">
          <el-button type="warning" size="large" class="quick-btn" @click="goTo('workflow')">
            <el-icon><SetUp /></el-icon>
            工作流控制台
          </el-button>
        </el-col>
        <el-col :span="6">
          <el-button type="info" size="large" class="quick-btn" @click="goTo('quality')">
            <el-icon><DataAnalysis /></el-icon>
            质量检测
          </el-button>
        </el-col>
      </el-row>
      <el-divider />
      <el-row :gutter="16">
        <el-col :span="6">
          <el-button size="large" class="quick-btn" @click="goTo('analytics')">
            📊 数据分析
          </el-button>
        </el-col>
        <el-col :span="6">
          <el-button size="large" class="quick-btn" @click="goTo('subplots')">
            🗂️ 副线看板
          </el-button>
        </el-col>
        <el-col :span="6">
          <el-button size="large" class="quick-btn" @click="goTo('emotional-arcs')">
            💫 情感弧线
          </el-button>
        </el-col>
        <el-col :span="6">
          <el-button size="large" class="quick-btn" @click="goTo('character-matrix')">
            🕸️ 关系矩阵
          </el-button>
        </el-col>
      </el-row>
      <el-row :gutter="16" style="margin-top: 16px">
        <el-col :span="6">
          <el-button size="large" class="quick-btn" @click="goTo('radar')">
            📡 市场雷达
          </el-button>
        </el-col>
      </el-row>
    </el-card>

    <!-- 最近章节 -->
    <el-card class="section-card" header="最近章节" shadow="never">
      <el-table :data="recentChapters" style="width: 100%" v-loading="loading">
        <el-table-column prop="chapter_num" label="章节" width="80" />
        <el-table-column prop="title" label="标题" />
        <el-table-column prop="word_count" label="字数" width="100" />
        <el-table-column prop="status" label="状态" width="120">
          <template #default="{ row }">
            <el-tag :type="statusType(row.status)" size="small">{{ statusText(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="200">
          <template #default="{ row }">
            <el-button size="small" @click="viewChapter(row.id)">查看</el-button>
            <el-button size="small" type="primary" @click="runQC(row.id)" v-if="row.status === 'generated'">质检</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 项目进度 -->
    <el-card class="section-card" header="项目进度" shadow="never">
      <div class="progress-info">
        <span>目标: {{ formatNumber(projectStore.currentProject?.target_words || 0) }} 字</span>
        <span>已完成: {{ formatNumber(stats.totalWords) }} 字</span>
      </div>
      <el-progress
        :percentage="progressPercent"
        :stroke-width="20"
        :text-inside="true"
        striped
        striped-flow
      />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useProjectStore } from '@/stores/project'
import { chapterApi, characterApi } from '@/api'

const router = useRouter()
const route = useRoute()
const projectStore = useProjectStore()
const projectId = route.params.projectId as string

const loading = ref(false)
const recentChapters = ref<any[]>([])
const stats = ref({ chapters: 0, approved: 0, totalWords: 0, characters: 0 })

const progressPercent = computed(() => {
  const target = projectStore.currentProject?.target_words || 1
  return Math.min(100, Math.round((stats.value.totalWords / target) * 100))
})

onMounted(async () => {
  loading.value = true
  try {
    const [chaptersRes, charsRes] = await Promise.all([
      chapterApi.list(projectId),
      characterApi.list(projectId),
    ])
    const chapters = chaptersRes.data.data || []
    recentChapters.value = chapters.slice(-10).reverse()
    stats.value.chapters = chapters.length
    stats.value.approved = chapters.filter((c: any) => c.status === 'approved').length
    stats.value.totalWords = chapters.reduce((sum: number, c: any) => sum + (c.word_count || 0), 0)
    stats.value.characters = (charsRes.data.data || []).length
  } catch {
    // may not have data yet
  } finally {
    loading.value = false
  }
})

function goTo(page: string) {
  router.push(`/projects/${projectId}/${page}`)
}

async function continueWrite() {
  try {
    const res = await chapterApi.continueGenerate(projectId)
    ElMessage.success('章节生成完成')
    recentChapters.value.unshift(res.data.data)
    stats.value.chapters++
  } catch (e: any) {
    const code = e.response?.data?.code
    const msg = e.response?.data?.message || e.response?.data?.error || '生成失败'
    ElMessage.warning(msg)
    if (code === 'WF_001') {
      goTo('blueprint')
    }
  }
}

function viewChapter(id: string) {
  router.push(`/projects/${projectId}/chapters/${id}`)
}

async function runQC(id: string) {
  try {
    const res = await chapterApi.qualityCheck(id)
    ElMessage.success('质检完成')
  } catch {
    ElMessage.error('质检失败')
  }
}

function formatNumber(n: number) {
  return n.toLocaleString()
}

function statusType(status: string) {
  const map: Record<string, string> = {
    pending: 'info', generated: '', pending_review: 'warning', approved: 'success', rejected: 'danger',
  }
  return (map[status] || 'info') as any
}

function statusText(status: string) {
  const map: Record<string, string> = {
    pending: '待生成', generated: '已生成', pending_review: '待审核', approved: '已通过', rejected: '已驳回',
  }
  return map[status] || status
}
</script>

<style scoped>
.studio { max-width: 1200px; margin: 0 auto; }
.page-header { display: flex; align-items: center; gap: 16px; margin-bottom: 24px; }
.page-header h1 { font-size: 24px; color: #e0e0e0; }
.stat-card { background: var(--nb-card-bg); border: 1px solid var(--nb-card-border); margin-bottom: 20px; }
.section-card { background: var(--nb-card-bg); border: 1px solid var(--nb-card-border); margin-bottom: 20px; }
.quick-btn { width: 100%; height: 60px; font-size: 15px; }
.progress-info { display: flex; justify-content: space-between; margin-bottom: 12px; color: #999; }
</style>
