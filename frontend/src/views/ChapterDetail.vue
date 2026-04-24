<template>
  <div class="chapter-detail">
    <div class="page-header">
      <div>
        <el-button text @click="goBack"><el-icon><ArrowLeft /></el-icon>返回章节列表</el-button>
        <h1 v-if="chapter">第{{ chapter.chapter_num }}章 {{ chapter.title || '' }}</h1>
      </div>
      <div class="header-actions" v-if="chapter">
        <el-tag :type="statusType" size="large">{{ statusLabel }}</el-tag>
        <el-button
          v-if="chapter.status === 'draft' || chapter.status === 'needs_recheck' || chapter.status === 'generated'"
          type="success"
          @click="confirmAs正文"
        >确认为正文</el-button>
        <el-button
          v-if="chapter.status === 'draft' || chapter.status === 'needs_recheck' || chapter.status === 'generated'"
          type="warning"
          @click="submitReview"
        >提交审核</el-button>
        <el-button v-if="chapter.status === 'pending_review'" type="success" @click="approveChapter">通过</el-button>
        <el-button v-if="chapter.status === 'pending_review'" type="danger" @click="rejectChapter">驳回</el-button>
        <el-button type="info" @click="runQualityCheck" :loading="checking">质量检查</el-button>
        <el-button plain @click="copyChapter" :loading="copying">复制章节</el-button>
        <el-button type="danger" plain @click="deleteChapter">删除章节</el-button>
      </div>
    </div>

    <el-row :gutter="20" v-if="chapter">
      <!-- Content -->
      <el-col :span="16">
        <el-card shadow="hover">
          <template #header>
            <div class="card-header">
              <div class="card-title-group">
                <span>章节内容</span>
                <span style="color: #888; font-size: 13px;">{{ chapter.word_count || 0 }} 字 | 版本 {{ chapter.version }}</span>
              </div>
              <div class="card-actions">
                <el-button v-if="!isEditing" size="small" type="primary" plain @click="startEdit">编辑正文</el-button>
                <template v-else>
                  <el-button size="small" @click="cancelEdit">取消</el-button>
                  <el-button size="small" type="primary" :loading="saving" @click="saveEdit">保存修改</el-button>
                </template>
              </div>
            </div>
          </template>
          <div v-if="isEditing" class="chapter-editor">
            <el-input v-model="editTitle" class="editor-title" placeholder="章节标题" />
            <el-input v-model="editContent" type="textarea" :rows="26" resize="vertical" placeholder="输入章节正文" />
          </div>
          <div v-else class="chapter-content rich-text" v-html="renderedContent"></div>
        </el-card>
      </el-col>

      <!-- Side Panel -->
      <el-col :span="8">
        <!-- Chapter Info -->
        <el-card shadow="hover">
          <template #header><span>章节信息</span></template>
          <el-descriptions :column="1" size="small" border>
            <el-descriptions-item label="章节号">{{ chapter.chapter_num }}</el-descriptions-item>
            <el-descriptions-item label="字数">{{ chapter.word_count || 0 }}</el-descriptions-item>
            <el-descriptions-item label="版本">{{ chapter.version }}</el-descriptions-item>
            <el-descriptions-item label="状态">
              <el-tag :type="statusType" size="small">{{ statusLabel }}</el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="创建时间">{{ formatDate(chapter.created_at) }}</el-descriptions-item>
            <el-descriptions-item label="更新时间">{{ formatDate(chapter.updated_at) }}</el-descriptions-item>
          </el-descriptions>
        </el-card>

        <!-- Quality Report -->
        <el-card shadow="hover" style="margin-top: 16px;" v-if="qualityReport">
          <template #header>
            <div class="card-header">
              <span>质量报告</span>
              <el-tag :type="qualityReport.overall_score >= 7 ? 'success' : 'warning'" size="small">
                {{ qualityReport.overall_score.toFixed(1) }} 分
              </el-tag>
            </div>
          </template>

          <div v-if="qualityReport?.scores" class="score-grid">
            <div class="score-item" v-for="(score, role) in qualityReport.scores" :key="role">
              <div class="score-label">{{ roleLabel(role as unknown as string) }}</div>
              <el-progress :percentage="(score as number) * 10" :color="scoreColor(score as number)"
                :stroke-width="8" />
            </div>
          </div>

          <el-divider />

          <div v-if="qualityReport.issues?.length">
            <h4 style="color: #f56c6c; margin-bottom: 8px;">发现问题</h4>
            <div v-for="(issue, i) in qualityReport.issues" :key="i" class="issue-item">
              <el-tag :type="issue.severity === 'critical' ? 'danger' : 'warning'" size="small">
                {{ issue.severity === 'critical' ? '严重' : '警告' }}
              </el-tag>
              <span>{{ issue.description }}</span>
            </div>
          </div>
          <el-result v-else icon="success" title="质量良好" sub-title="未发现明显问题" />
        </el-card>

        <!-- Review History -->
        <el-card shadow="hover" style="margin-top: 16px;">
          <template #header><span>审核记录</span></template>
          <el-timeline v-if="reviews.length">
            <el-timeline-item
              v-for="r in reviews" :key="r.id"
              :type="r.decision === 'approved' ? 'success' : r.decision === 'rejected' ? 'danger' : 'primary'"
              :timestamp="formatDate(r.created_at)" placement="top">
              <div class="review-item">
                <el-tag :type="r.decision === 'approved' ? 'success' : 'danger'" size="small">
                  {{ r.decision === 'approved' ? '通过' : '驳回' }}
                </el-tag>
                <span>{{ r.role_name }}</span>
              </div>
              <p v-if="r.comment" class="review-comment">{{ r.comment }}</p>
            </el-timeline-item>
          </el-timeline>
          <el-empty v-else description="暂无审核记录" :image-size="60" />
        </el-card>
      </el-col>
    </el-row>

    <el-skeleton v-if="!chapter" :rows="10" animated />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { chapterApi, qualityApi } from '@/api'
import { renderRichText } from '@/utils/richText'

const route = useRoute()
const router = useRouter()
const projectId = route.params.projectId as string
const chapterId = route.params.chapterId as string

const chapter = ref<any>(null)
const qualityReport = ref<any>(null)
const reviews = ref<any[]>([])
const checking = ref(false)
const isEditing = ref(false)
const saving = ref(false)
const copying = ref(false)
const editTitle = ref('')
const editContent = ref('')

const statusType = computed(() => {
  const m: Record<string, string> = {
    draft: 'info', generated: 'info', pending_review: 'warning', approved: 'success', rejected: 'danger', needs_recheck: 'warning',
  }
  return (m[chapter.value?.status] || 'info') as any
})

const statusLabel = computed(() => {
  const m: Record<string, string> = {
    draft: '草稿', generated: '草稿', pending_review: '待审核', approved: '正文', rejected: '已驳回', needs_recheck: '待复核',
  }
  return m[chapter.value?.status] || chapter.value?.status
})

const renderedContent = computed(() => {
  return renderRichText(chapter.value?.content || '')
})

function formatDate(d: string) {
  return d ? new Date(d).toLocaleString('zh-CN') : '-'
}

function roleLabel(role: string) {
  const m: Record<string, string> = {
    editor: '编辑', reader: '读者', logic_reviewer: '逻辑审核', anti_ai: '反AI检测',
  }
  return m[role] || role
}

function scoreColor(score: number) {
  if (score >= 8) return '#67c23a'
  if (score >= 6) return '#e6a23c'
  return '#f56c6c'
}

async function loadChapter() {
  try {
    const res = await chapterApi.get(projectId, chapterId)
    chapter.value = res.data.data
    editTitle.value = chapter.value?.title || ''
    editContent.value = chapter.value?.content || ''
    if (chapter.value?.quality_report) {
      qualityReport.value = chapter.value.quality_report
    }
    reviews.value = chapter.value?.reviews || []
  } catch {
    ElMessage.error('加载章节失败')
  }
}

onMounted(loadChapter)

function goBack() {
  router.push({ name: 'chapters', params: { projectId } })
}

async function submitReview() {
  try {
    await chapterApi.submitReview(projectId, chapterId)
    chapter.value.status = 'pending_review'
    ElMessage.success('已提交审核')
  } catch { ElMessage.error('操作失败') }
}

async function confirmAs正文() {
  if (!chapter.value) return
  try {
    await chapterApi.approve(projectId, chapterId, 'confirmed as final text', chapter.value.version)
    chapter.value.status = 'approved'
    chapter.value.version += 1
    ElMessage.success('已确认为正文')
  } catch (e: any) {
    const msg = e.response?.data?.message || e.response?.data?.error || '确认失败'
    ElMessage.error(msg)
  }
}

async function approveChapter() {
  try {
    await chapterApi.approve(projectId, chapterId, '', chapter.value.version)
    chapter.value.status = 'approved'
    chapter.value.version += 1
    ElMessage.success('章节已通过')
  } catch { ElMessage.error('操作失败') }
}

async function rejectChapter() {
  const { value: reason } = await ElMessageBox.prompt('请输入驳回原因', '驳回', { type: 'warning' })
  try {
    await chapterApi.reject(projectId, chapterId, reason, chapter.value.version)
    chapter.value.status = 'rejected'
    chapter.value.version += 1
    reviews.value.unshift({
      id: Date.now().toString(), decision: 'rejected', role_name: '人工审核',
      comment: reason, created_at: new Date().toISOString(),
    })
    ElMessage.success('章节已驳回')
  } catch { ElMessage.error('操作失败') }
}

async function runQualityCheck() {
  checking.value = true
  try {
    const res = await qualityApi.runCheck(projectId, chapterId)
    qualityReport.value = res.data.data
    ElMessage.success('质量检查完成')
  } catch {
    ElMessage.error('质量检查失败')
  } finally {
    checking.value = false
  }
}

function startEdit() {
  if (!chapter.value) return
  editTitle.value = chapter.value.title || ''
  editContent.value = chapter.value.content || ''
  isEditing.value = true
}

function cancelEdit() {
  if (!chapter.value) return
  editTitle.value = chapter.value.title || ''
  editContent.value = chapter.value.content || ''
  isEditing.value = false
}

async function saveEdit() {
  if (!chapter.value) return
  saving.value = true
  try {
    const res = await chapterApi.update(projectId, chapterId, {
      title: editTitle.value,
      content: editContent.value,
      version: chapter.value.version,
    })
    chapter.value = res.data.data
    editTitle.value = chapter.value.title || ''
    editContent.value = chapter.value.content || ''
    isEditing.value = false
    ElMessage.success('章节已保存')
  } catch (e: any) {
    const msg = e.response?.data?.message || e.response?.data?.error || '保存失败'
    ElMessage.error(msg)
  } finally {
    saving.value = false
  }
}

async function copyChapter() {
  if (!chapter.value?.content) return
  copying.value = true
  try {
    const text = `第${chapter.value.chapter_num}章 ${chapter.value.title || ''}\n\n${chapter.value.content}`.trim()
    await navigator.clipboard.writeText(text)
    ElMessage.success('章节已复制')
  } catch {
    ElMessage.error('复制失败')
  } finally {
    copying.value = false
  }
}

async function deleteChapter() {
  if (!chapter.value) return
  await ElMessageBox.confirm(
    `确认删除第 ${chapter.value.chapter_num} 章《${chapter.value.title || '未命名章节'}》？当前仅支持删除最后一章。`,
    '删除章节',
    { type: 'warning' },
  )
  try {
    await chapterApi.delete(projectId, chapterId)
    ElMessage.success('章节已删除')
    goBack()
  } catch (e: any) {
    const msg = e.response?.data?.message || e.response?.data?.error || '删除失败'
    ElMessage.error(msg)
  }
}
</script>

<style scoped>
.chapter-detail { max-width: 1400px; margin: 0 auto; }
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 24px; }
.page-header h1 { font-size: 22px; color: #e0e0e0; margin-top: 8px; }
.header-actions { display: flex; align-items: center; gap: 8px; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.card-title-group { display: flex; align-items: center; gap: 12px; }
.card-actions { display: flex; align-items: center; gap: 8px; }
.chapter-content { color: var(--nb-text-primary); line-height: 2; font-size: 15px; max-height: 70vh; overflow-y: auto; }
.rich-text :deep(p) { margin-bottom: 16px; text-indent: 2em; }
.rich-text :deep(h1),
.rich-text :deep(h2),
.rich-text :deep(h3) { margin: 1em 0 0.6em; }
.rich-text :deep(ul),
.rich-text :deep(ol) { margin: 0.75em 0; padding-left: 1.4em; }
.chapter-editor { display: flex; flex-direction: column; gap: 12px; }
.editor-title { margin-bottom: 4px; }
.score-grid { display: grid; gap: 12px; }
.score-item { }
.score-label { color: var(--nb-text-secondary); font-size: 13px; margin-bottom: 4px; }
.issue-item { display: flex; align-items: flex-start; gap: 8px; margin-bottom: 8px; color: var(--nb-text-secondary); font-size: 13px; }
.review-item { display: flex; align-items: center; gap: 8px; }
.review-comment { color: #888; font-size: 13px; margin-top: 4px; }
</style>
