<template>
  <div class="chapters">
    <div class="page-header">
      <h1>章节管理</h1>
      <div class="header-actions">
        <el-button type="primary" @click="showGenerate" :disabled="!canGenerate">
          <el-icon><EditPen /></el-icon>生成新章节
        </el-button>
        <el-button style="margin-left: 8px" @click="showBatchDialog = true">批量生成</el-button>
        <el-dropdown @command="handleExport" style="margin-left: 8px">
          <el-button>
            导出<el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="txt">导出 TXT</el-dropdown-item>
              <el-dropdown-item command="markdown">导出 Markdown</el-dropdown-item>
              <el-dropdown-item command="epub">导出 EPUB</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </div>

    <el-alert v-if="!canGenerate" type="warning" :closable="false" show-icon style="margin-bottom: 16px;">
      请先完成蓝图审批才能生成章节
    </el-alert>

    <!-- Chapter List -->
    <el-table :data="chapters" v-loading="loading" style="width: 100%;">
      <el-table-column prop="chapter_num" label="章号" width="80" sortable />
      <el-table-column prop="title" label="章节标题" />
      <el-table-column label="字数" width="100">
        <template #default="{ row }">{{ row.word_count || 0 }}</template>
      </el-table-column>
      <el-table-column prop="status" label="状态" width="120">
        <template #default="{ row }">
          <el-tag :type="chapterStatusType(row.status)" size="small">{{ chapterStatusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="version" label="版本" width="80" />
      <el-table-column label="创建时间" width="180">
        <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="200">
        <template #default="{ row }">
          <el-button size="small" @click="viewChapter(row)">查看</el-button>
          <el-button size="small" type="primary" @click="regenerateChapter(row)"
            v-if="row.status === 'rejected'">重新生成</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-empty v-if="!loading && !chapters.length" description="暂无章节" />

    <!-- Generate Dialog -->
    <el-dialog v-model="showGenerateDialog" title="生成章节" width="700px" :close-on-click-modal="false">
      <template v-if="!streaming">
        <el-form :model="genForm" label-position="top">
          <el-row :gutter="16">
            <el-col :span="12">
              <el-form-item label="章节号">
                <el-input-number v-model="genForm.chapter_num" :min="1" style="width: 100%;" />
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item label="目标字数">
                <el-input-number v-model="genForm.chapter_words" :min="500" :max="20000" :step="500" style="width: 100%;" />
              </el-form-item>
            </el-col>
          </el-row>
          <el-form-item label="本章方向提示（可选）">
            <el-input v-model="genForm.context_hint" type="textarea" :rows="3"
              placeholder="描述本章的大致方向或特殊要求，如：本章重点写师徒矛盾" />
          </el-form-item>
          <el-form-item label="生成方式">
            <el-radio-group v-model="genForm.stream">
              <el-radio :value="true">流式生成（实时显示）</el-radio>
              <el-radio :value="false">普通生成（完成后显示）</el-radio>
            </el-radio-group>
          </el-form-item>
        </el-form>
      </template>

      <!-- Streaming Output -->
      <template v-if="streaming">
        <div class="stream-header">
          <el-tag type="success" effect="dark">
            <el-icon class="is-loading"><Loading /></el-icon> 正在生成中...
          </el-tag>
          <span class="stream-word-count">已生成: {{ streamContent.length }} 字</span>
        </div>
        <div class="stream-content" ref="streamBox">
          <div class="stream-text" v-html="renderedStream"></div>
        </div>
      </template>

      <template #footer>
        <template v-if="!streaming">
          <el-button @click="showGenerateDialog = false">取消</el-button>
          <el-button type="primary" @click="startGenerate" :loading="generating">开始生成</el-button>
        </template>
        <template v-else>
          <el-button @click="stopStream" type="danger">停止</el-button>
          <el-button v-if="streamDone" type="primary" @click="finishGenerate">完成</el-button>
        </template>
      </template>
    </el-dialog>

    <!-- Batch Generate Dialog -->
    <el-dialog v-model="showBatchDialog" title="批量生成章节" width="400px">
      <div class="space-y-4">
        <p class="text-sm text-gray-500">从当前最大章节号之后，连续生成多个章节并加入任务队列。</p>
        <el-form label-position="top">
          <el-form-item label="生成章节数量">
            <el-input-number v-model="batchCount" :min="1" :max="20" style="width: 100%;" />
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <el-button @click="showBatchDialog = false">取消</el-button>
        <el-button type="primary" @click="startBatchGenerate" :loading="batching">提交生成</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowDown } from '@element-plus/icons-vue'
import { chapterApi, exportApi, exportExtApi, batchWriteApi } from '@/api'

const route = useRoute()
const router = useRouter()
const projectId = route.params.projectId as string

const loading = ref(false)
const chapters = ref<any[]>([])
const canGenerate = ref(true)

const showGenerateDialog = ref(false)
const generating = ref(false)
const streaming = ref(false)
const streamContent = ref('')
const streamDone = ref(false)
const streamBox = ref<HTMLElement | null>(null)
let abortController: AbortController | null = null

const genForm = ref({
  chapter_num: 1, chapter_words: 3000, context_hint: '', stream: true,
})

const showBatchDialog = ref(false)
const batchCount = ref(3)
const batching = ref(false)

const renderedStream = computed(() =>
  streamContent.value.replace(/\n/g, '<br>')
)

function chapterStatusType(s: string) {
  const m: Record<string, string> = {
    draft: 'info', pending_review: 'warning', approved: 'success', rejected: 'danger', needs_recheck: 'warning',
  }
  return (m[s] || 'info') as any
}

function chapterStatusLabel(s: string) {
  const m: Record<string, string> = {
    draft: '草稿', pending_review: '待审核', approved: '已通过', rejected: '已驳回', needs_recheck: '待重检',
  }
  return m[s] || s
}

function formatDate(d: string) {
  return d ? new Date(d).toLocaleString('zh-CN') : '-'
}

onMounted(async () => {
  await fetchChapters()
  const maxNum = chapters.value.reduce((m: number, c: any) => Math.max(m, c.chapter_num || 0), 0)
  genForm.value.chapter_num = maxNum + 1
})

async function fetchChapters() {
  loading.value = true
  try {
    const res = await chapterApi.list(projectId)
    chapters.value = (res.data.data || []).sort((a: any, b: any) => a.chapter_num - b.chapter_num)
  } finally {
    loading.value = false
  }
}

function viewChapter(ch: any) {
  router.push({ name: 'chapter-detail', params: { projectId, chapterId: ch.id } })
}

async function handleExport(format: 'txt' | 'markdown' | 'epub') {
  try {
    let res: any
    let ext: string
    if (format === 'epub') {
      res = await exportExtApi.epub(projectId)
      ext = 'epub'
    } else if (format === 'txt') {
      res = await exportApi.txt(projectId)
      ext = 'txt'
    } else {
      res = await exportApi.markdown(projectId)
      ext = 'md'
    }
    const url = URL.createObjectURL(new Blob([res.data]))
    const a = document.createElement('a')
    a.href = url
    a.download = `novel_${projectId}.${ext}`
    a.click()
    URL.revokeObjectURL(url)
    ElMessage.success('导出成功')
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '导出失败')
  }
}

async function startBatchGenerate() {
  batching.value = true
  try {
    const maxNum = chapters.value.reduce((m: number, c: any) => Math.max(m, c.chapter_num || 0), 0)
    await batchWriteApi.generate(projectId, batchCount.value)
    ElMessage.success(`已将 ${batchCount.value} 个章节加入生成队列`)
    showBatchDialog.value = false
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '批量生成失败')
  } finally {
    batching.value = false
  }
}

function showGenerate() {
  const maxNum = chapters.value.reduce((m: number, c: any) => Math.max(m, c.chapter_num || 0), 0)
  genForm.value.chapter_num = maxNum + 1
  genForm.value.context_hint = ''
  streamContent.value = ''
  streaming.value = false
  streamDone.value = false
  showGenerateDialog.value = true
}

function regenerateChapter(ch: any) {
  genForm.value.chapter_num = ch.chapter_num
  genForm.value.context_hint = ''
  streamContent.value = ''
  streaming.value = false
  streamDone.value = false
  showGenerateDialog.value = true
}

async function startGenerate() {
  generating.value = true

  if (genForm.value.stream) {
    streaming.value = true
    streamContent.value = ''
    streamDone.value = false
    abortController = new AbortController()

    try {
      await chapterApi.streamGenerate(
        projectId,
        {
          chapter_num: genForm.value.chapter_num,
          chapter_words: genForm.value.chapter_words,
          context_hint: genForm.value.context_hint,
        },
        (chunk: string) => {
          streamContent.value += chunk
          nextTick(() => {
            if (streamBox.value) {
              streamBox.value.scrollTop = streamBox.value.scrollHeight
            }
          })
        },
        () => {
          streamDone.value = true
          generating.value = false
          ElMessage.success('章节生成完成')
        },
        abortController.signal,
      )
    } catch (e: any) {
      if (e.name !== 'AbortError') {
        ElMessage.error('生成失败: ' + (e.message || '未知错误'))
      }
      generating.value = false
    }
  } else {
    try {
      await chapterApi.generate(projectId, {
        chapter_num: genForm.value.chapter_num,
        chapter_words: genForm.value.chapter_words,
        context_hint: genForm.value.context_hint,
      })
      ElMessage.success('章节生成完成')
      showGenerateDialog.value = false
      await fetchChapters()
    } catch {
      ElMessage.error('生成失败')
    } finally {
      generating.value = false
    }
  }
}

function stopStream() {
  if (abortController) {
    abortController.abort()
    abortController = null
  }
  streamDone.value = true
  generating.value = false
}

function finishGenerate() {
  showGenerateDialog.value = false
  fetchChapters()
}
</script>

<style scoped>
.chapters { max-width: 1200px; margin: 0 auto; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; }
.page-header h1 { font-size: 24px; color: #e0e0e0; }
.stream-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.stream-word-count { color: #888; font-size: 13px; }
.stream-content { background: var(--nb-table-header-bg); border: 1px solid var(--nb-card-border); border-radius: 8px; padding: 20px; max-height: 500px; overflow-y: auto; }
.stream-text { color: var(--nb-text-primary); line-height: 2; font-size: 15px; }
</style>
