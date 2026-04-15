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
      <el-table-column label="操作" width="280">
        <template #default="{ row }">
          <el-button size="small" @click="viewChapter(row)">查看</el-button>
          <el-button
            v-if="row.status === 'draft' || row.status === 'needs_recheck' || row.status === 'generated'"
            size="small"
            type="success"
            @click="confirmAs正文(row)"
          >确认为正文</el-button>
          <el-button size="small" type="primary" @click="regenerateChapter(row)"
            v-if="row.status === 'rejected'">重新生成</el-button>
          <el-button
            v-if="row.chapter_num === latestChapterNum"
            size="small"
            type="danger"
            plain
            @click="deleteChapter(row)"
          >删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-empty v-if="!loading && !chapters.length" description="暂无章节" />

    <!-- Generate Dialog -->
    <el-dialog v-model="showGenerateDialog" title="生成章节" width="700px" :close-on-click-modal="false">
      <el-form :model="genForm" label-position="top">
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="章节号">
              <el-input-number v-model="genForm.chapter_num" :min="1" style="width: 100%;" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="字数下限">
              <el-input-number v-model="genForm.chapter_words_min" :min="500" :max="10000" :step="500" style="width: 100%;" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="字数上限">
              <el-input-number v-model="genForm.chapter_words_max" :min="500" :max="20000" :step="500" style="width: 100%;" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="本章方向提示（可选）">
          <el-input v-model="genForm.context_hint" type="textarea" :rows="3"
            placeholder="描述本章的大致方向或特殊要求，如：本章重点写师徒矛盾" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="showGenerateDialog = false">取消</el-button>
        <el-button type="primary" @click="startGenerate" :loading="generating">开始生成</el-button>
      </template>
    </el-dialog>

    <!-- Batch Generate Dialog -->
    <el-dialog v-model="showBatchDialog" title="批量生成章节" width="460px">
      <div class="space-y-4">
        <p class="text-sm text-gray-500">可按数量生成（从当前最大章节号之后连续生成），也可选择按卷生成该卷所有章节。</p>
        <el-form label-position="top">
          <el-form-item label="生成模式">
            <el-radio-group v-model="batchMode">
              <el-radio value="count">按数量生成</el-radio>
              <el-radio value="volume">按卷生成</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="生成章节数量" v-if="batchMode === 'count'">
            <el-input-number v-model="batchCount" :min="1" :max="50" style="width: 100%;" />
          </el-form-item>
          <el-form-item label="选择卷" v-if="batchMode === 'volume'">
            <el-select v-model="batchVolumeId" placeholder="选择要生成的卷" style="width: 100%;" :loading="volumesLoading">
              <el-option
                v-for="vol in volumes"
                :key="vol.id"
                :label="`第${vol.volume_num}卷「${vol.title || '未命名'}」（第${vol.chapter_start}–${vol.chapter_end}章，共${vol.chapter_end - vol.chapter_start + 1}章）`"
                :value="vol.id"
              />
            </el-select>
            <div v-if="batchMode === 'volume' && volumes.length === 0 && !volumesLoading" class="text-xs text-gray-400 mt-1">
              暂无卷数据，请先在蓝图中设置章节范围。
            </div>
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
import { ref, onMounted, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown } from '@element-plus/icons-vue'
import { chapterApi, exportApi, exportExtApi, batchWriteApi, volumeApi } from '@/api'

const route = useRoute()
const router = useRouter()
const projectId = route.params.projectId as string

const loading = ref(false)
const chapters = ref<any[]>([])
const canGenerate = ref(true)

const showGenerateDialog = ref(false)
const generating = ref(false)

const genForm = ref({
  chapter_num: 1, chapter_words_min: 2000, chapter_words_max: 3500, context_hint: '',
})

const showBatchDialog = ref(false)
const batchCount = ref(3)
const batching = ref(false)
const batchMode = ref<'count' | 'volume'>('count')
const batchVolumeId = ref('')
const volumes = ref<any[]>([])
const volumesLoading = ref(false)

const latestChapterNum = computed(() => chapters.value.reduce((m: number, c: any) => Math.max(m, c.chapter_num || 0), 0))

function chapterStatusType(s: string) {
  const m: Record<string, string> = {
    draft: 'info', generated: 'info', pending_review: 'warning', approved: 'success', rejected: 'danger', needs_recheck: 'warning',
  }
  return (m[s] || 'info') as any
}

function chapterStatusLabel(s: string) {
  const m: Record<string, string> = {
    draft: '草稿', generated: '草稿', pending_review: '待审核', approved: '正文', rejected: '已驳回', needs_recheck: '待复核',
  }
  return m[s] || s
}

function formatDate(d: string) {
  return d ? new Date(d).toLocaleString('zh-CN') : '-'
}

onMounted(async () => {
  await fetchChapters()
  await fetchVolumes()
  const maxNum = chapters.value.reduce((m: number, c: any) => Math.max(m, c.chapter_num || 0), 0)
  genForm.value.chapter_num = maxNum + 1
})

async function fetchVolumes() {
  volumesLoading.value = true
  try {
    const res = await volumeApi.list(projectId)
    volumes.value = (res.data.data || []).sort((a: any, b: any) => a.volume_num - b.volume_num)
  } catch {
    volumes.value = []
  } finally {
    volumesLoading.value = false
  }
}

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
    if (batchMode.value === 'volume') {
      if (!batchVolumeId.value) {
        ElMessage.warning('请选择要生成的卷')
        return
      }
      const volRes = await batchWriteApi.generateByVolume(projectId, batchVolumeId.value)
      const vol = volumes.value.find((v: any) => v.id === batchVolumeId.value)
      const total = volRes.data?.total ?? (vol ? vol.chapter_end - vol.chapter_start + 1 : '?')
      ElMessage.success(`第${vol?.volume_num}卷共 ${total} 章高质量Agent生成已启动`)
    } else {
      const cntRes = await batchWriteApi.generate(projectId, batchCount.value)
      const started = cntRes.data?.total ?? batchCount.value
      ElMessage.success(`${started} 章高质量Agent生成已启动`)
    }
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
  showGenerateDialog.value = true
}

function regenerateChapter(ch: any) {
  genForm.value.chapter_num = ch.chapter_num
  genForm.value.context_hint = ''
  showGenerateDialog.value = true
}

async function confirmAs正文(ch: any) {
  try {
    await chapterApi.approve(projectId, ch.id, 'confirmed as final text', ch.version)
    ElMessage.success('已确认为正文')
    await fetchChapters()
  } catch (e: any) {
    const msg = e.response?.data?.message || e.response?.data?.error || '确认失败'
    ElMessage.error(msg)
  }
}

async function startGenerate() {
  generating.value = true
  try {
    await chapterApi.generate(projectId, {
      chapter_num: genForm.value.chapter_num,
      chapter_words_min: genForm.value.chapter_words_min,
      chapter_words_max: genForm.value.chapter_words_max,
      context_hint: genForm.value.context_hint,
    })
    showGenerateDialog.value = false
    ElMessage({ message: '章节高质量Agent生成已启动，可在"Agent生成"页面查看进度', type: 'success', duration: 5000 })
    await fetchChapters()
  } catch (e: any) {
    const msg = e.response?.data?.message || e.response?.data?.error || '生成失败'
    ElMessage.error(msg)
  } finally {
    generating.value = false
  }
}

async function deleteChapter(ch: any) {
  await ElMessageBox.confirm(
    `确认删除第 ${ch.chapter_num} 章《${ch.title || '未命名章节'}》？仅允许删除最后一章。`,
    '删除章节',
    { type: 'warning' },
  )
  try {
    await chapterApi.delete(projectId, ch.id)
    ElMessage.success('章节已删除')
    await fetchChapters()
  } catch (e: any) {
    const msg = e.response?.data?.message || e.response?.data?.error || '删除失败'
    ElMessage.error(msg)
  }
}
</script>

<style scoped>
.chapters { max-width: 1200px; margin: 0 auto; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; }
.page-header h1 { font-size: 24px; color: #e0e0e0; }
</style>
