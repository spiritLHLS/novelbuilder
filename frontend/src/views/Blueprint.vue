<template>
  <div class="blueprint">
    <div class="page-header">
      <h1>蓝图管理</h1>
      <div style="display: flex; gap: 8px;">
        <el-button v-if="currentBlueprint" type="success" plain @click="exportBlueprint">
          <el-icon><Download /></el-icon>导出蓝图
        </el-button>
        <el-button type="primary" plain @click="showImportDialog = true">
          <el-icon><Upload /></el-icon>导入蓝图
        </el-button>
      </div>
    </div>

    <!-- Import Blueprint Dialog -->
    <el-dialog v-model="showImportDialog" title="导入蓝图" width="600px">
      <el-alert type="info" :closable="false" style="margin-bottom: 16px;">
        导入蓝图将覆盖当前项目的所有蓝图相关数据（世界观、角色、伏笔、卷册、章节大纲）。已生成的章节正文不受影响。
      </el-alert>
      <el-upload
        drag
        :auto-upload="false"
        :limit="1"
        accept=".json"
        :on-change="handleImportFileChange"
        :file-list="importFileList"
      >
        <el-icon class="el-icon--upload"><upload-filled /></el-icon>
        <div class="el-upload__text">
          将蓝图JSON文件拖到此处，或<em>点击上传</em>
        </div>
      </el-upload>
      <template #footer>
        <el-button @click="showImportDialog = false">取消</el-button>
        <el-button type="primary" :loading="importing" @click="confirmImport">
          确认导入
        </el-button>
      </template>
    </el-dialog>

    <!-- Regenerate Config Dialog (only used for re-generation) -->
    <el-dialog v-model="dialogVisible" title="重新生成蓝图" width="520px" :close-on-click-modal="false">
      <el-form ref="genFormRef" :model="genForm" :rules="genFormRules" label-width="120px">
        <el-form-item label="卷数" prop="volume_count" required>
          <el-input-number v-model="genForm.volume_count" :min="1" :max="30" :step="1" style="width: 160px;" />
          <span class="form-hint">卷（必填，规划整本书的卷册数量）</span>
        </el-form-item>
        <el-form-item label="每章最少字数">
          <el-input-number v-model="genForm.chapter_words_min" :min="500" :max="10000" :step="500" style="width: 160px;" />
          <span class="form-hint">字</span>
        </el-form-item>
        <el-form-item label="每章最多字数">
          <el-input-number v-model="genForm.chapter_words_max" :min="500" :max="20000" :step="500" style="width: 160px;" />
          <span class="form-hint">字</span>
        </el-form-item>
        <el-form-item label="补充创意方向">
          <el-input v-model="genForm.idea" type="textarea" :rows="3"
            placeholder="可选：额外的创作方向或改动要点；留空则完全使用项目描述" />
        </el-form-item>
      </el-form>

      <el-alert type="warning" :closable="false" style="margin-top:4px;">
        重新生成将覆盖当前所有蓝图及卷册规划，已写入的章节正文不受影响。
      </el-alert>

      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="generating" @click="confirmGenerate">
          确认重新生成
        </el-button>
      </template>
    </el-dialog>

    <!-- No Blueprint – inline config panel -->
    <el-card v-if="!currentBlueprint && !generating" shadow="hover" class="generate-panel">
      <div class="generate-panel-header">
        <el-icon :size="40" style="color: #409eff; flex-shrink: 0;"><Document /></el-icon>
        <div>
          <h3 style="margin: 0 0 4px; font-size: 18px; color: var(--nb-text-primary);">尚未创建整书蓝图</h3>
          <p style="margin: 0; color: #888; font-size: 13px;">设置卷数等参数，一键生成世界圣经、大纲、角色、伏笔和卷册规划</p>
        </div>
      </div>

      <el-divider />

      <el-form ref="genFormRef" :model="genForm" :rules="genFormRules" label-width="130px" style="max-width: 560px; margin: 0 auto;">
        <el-form-item label="卷数" prop="volume_count" required>
          <el-input-number v-model="genForm.volume_count" :min="1" :max="30" :step="1" style="width: 160px;" />
          <span class="form-hint">卷（规划整本书的卷册数量）</span>
        </el-form-item>
        <el-form-item label="每章最少字数">
          <el-input-number v-model="genForm.chapter_words_min" :min="500" :max="10000" :step="500" style="width: 160px;" />
          <span class="form-hint">字</span>
        </el-form-item>
        <el-form-item label="每章最多字数">
          <el-input-number v-model="genForm.chapter_words_max" :min="500" :max="20000" :step="500" style="width: 160px;" />
          <span class="form-hint">字</span>
        </el-form-item>
        <el-form-item label="补充创意方向">
          <el-input v-model="genForm.idea" type="textarea" :rows="3"
            placeholder="可选：额外的创作方向或改动要点；留空则完全使用项目描述" style="width: 340px;" />
        </el-form-item>
      </el-form>

      <div style="text-align: center; margin-top: 20px;">
        <el-button type="primary" size="large" :loading="generating" @click="quickGenerate">
          <el-icon><Promotion /></el-icon>
          一键生成整书蓝图
        </el-button>
      </div>
    </el-card>

    <!-- Generating Progress -->
    <el-card v-if="generating" shadow="hover" style="text-align: center; padding: 40px 20px;">
      <el-icon :size="48" class="is-loading" style="color: #409eff;"><Loading /></el-icon>
      <h3 :style="{ color: 'var(--nb-text-primary)', marginTop: '16px' }">正在生成蓝图...</h3>
      <p style="color: #888; margin-bottom: 24px;">AI正在构建世界设定、角色体系、故事大纲和卷册结构，请稍候</p>
      <el-steps :active="generatingStep" align-center style="max-width: 600px; margin: 0 auto;">
        <el-step title="初始化" description="创建任务" />
        <el-step title="AI创作" description="调用大模型" />
        <el-step title="解析数据" description="处理返回内容" />
        <el-step title="写入数据库" description="保存世界圣经/角色/大纲" />
        <el-step title="完成" description="蓝图就绪" />
      </el-steps>
    </el-card>

    <!-- Generation Failed -->
    <el-alert v-if="generationError" type="error" :title="'蓝图生成失败: ' + generationError"
      show-icon closable style="margin-bottom: 16px;" @close="generationError = ''" />

    <!-- Blueprint Content -->
    <template v-if="currentBlueprint && !generating">
      <!-- Status Bar -->
      <el-card shadow="hover" class="status-bar">
        <el-row :gutter="20" align="middle">
          <el-col :span="6">
            <div class="status-label">蓝图状态</div>
            <el-tag :type="blueprintStatusType" size="large">{{ blueprintStatusLabel }}</el-tag>
          </el-col>
          <el-col :span="6">
            <div class="status-label">创建时间</div>
            <div class="status-value">{{ formatDate(currentBlueprint.created_at) }}</div>
          </el-col>
          <el-col :span="12" style="text-align: right;">
            <el-button v-if="currentBlueprint.status === 'draft'"
              type="success" @click="submitReview">提交审核</el-button>
            <el-button v-if="currentBlueprint.status === 'pending_review'"
              type="success" @click="approveBlueprint">批准</el-button>
            <el-button v-if="currentBlueprint.status === 'pending_review'"
              type="danger" @click="rejectBlueprint">驳回</el-button>
            <el-button v-if="currentBlueprint.status === 'draft' || currentBlueprint.status === 'rejected' || currentBlueprint.status === 'failed'"
              type="warning" :loading="generating" @click="openGenerateDialog(true)">
              <el-icon><Refresh /></el-icon>重新生成
            </el-button>
          </el-col>
        </el-row>
      </el-card>

      <!-- Asset Overview -->
      <el-row :gutter="20" style="margin-top: 20px;">
        <el-col :span="6">
          <el-card shadow="hover" class="asset-card">
            <el-statistic title="世界设定" :value="worldBibleCount" suffix="项">
              <template #prefix><el-icon style="color: #409eff;"><Document /></el-icon></template>
            </el-statistic>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="hover" class="asset-card">
            <el-statistic title="角色数量" :value="characterCount">
              <template #prefix><el-icon style="color: #e6a23c;"><User /></el-icon></template>
            </el-statistic>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="hover" class="asset-card">
            <el-statistic title="大纲节点" :value="outlineCount">
              <template #prefix><el-icon style="color: #67c23a;"><List /></el-icon></template>
            </el-statistic>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="hover" class="asset-card">
            <el-statistic title="伏笔数量" :value="foreshadowingCount">
              <template #prefix><el-icon style="color: #f56c6c;"><Connection /></el-icon></template>
            </el-statistic>
          </el-card>
        </el-col>
      </el-row>

      <!-- Volume Plan -->
      <el-card shadow="hover" style="margin-top: 20px;">
        <template #header><span>卷册规划</span></template>
        <el-table :data="volumes" style="width: 100%;">
          <el-table-column prop="volume_num" label="卷号" width="80" />
          <el-table-column prop="title" label="卷名" />
          <el-table-column prop="chapter_start" label="起始章" width="100" />
          <el-table-column prop="chapter_end" label="结束章" width="100" />
          <el-table-column prop="status" label="状态" width="120">
            <template #default="{ row }">
              <el-tag :type="row.status === 'approved' ? 'success' : 'info'" size="small">
                {{ row.status === 'approved' ? '已批准' : row.status }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="380">
            <template #default="{ row }">
              <el-button v-if="row.status === 'pending_review'" size="small" type="success"
                @click="approveVolume(row.id)">批准</el-button>
              <el-button v-if="row.status === 'pending_review'" size="small" type="danger"
                @click="rejectVolume(row.id)">驳回</el-button>
              <el-button size="small" type="primary" 
                :loading="generatingOutlines.has(row.volume_num)"
                @click="generateChapterOutlines(row.volume_num)">
                生成章节大纲
              </el-button>
              <el-button size="small" type="warning" 
                :loading="generatingOutlines.has(row.volume_num)"
                @click="regenerateChapterOutlines(row.volume_num)">
                重新生成
              </el-button>
              <el-button size="small" type="info" 
                @click="viewVolumeOutlines(row.volume_num)">
                查看大纲
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-card>

      <!-- Chapter Outlines View Dialog -->
      <el-dialog v-model="showOutlinesDialog" :title="`第${viewingVolumeNum}卷章节大纲`" width="70%" @open="volumeOutlinePage = 1">
        <div v-if="volumeOutlines.length > 0">
          <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:16px;">
            <el-tag type="info" size="small">共 {{ volumeOutlines.length }} 章</el-tag>
            <el-pagination
              v-model:current-page="volumeOutlinePage"
              :page-size="outlinePageSize"
              :total="volumeOutlines.length"
              layout="prev,pager,next"
              small
              background
            />
          </div>
          <el-collapse accordion>
            <el-collapse-item v-for="outline in pagedVolumeOutlines" :key="outline.id" :name="outline.id">
              <template #title>
                <span style="font-weight: 500; margin-right: 8px;">第{{ outline.order_num }}章</span>
                <span style="color: #606266;">{{ outline.title }}</span>
              </template>
              <div v-if="outline.content && outline.content.events" class="chapter-events">
                <div v-for="(event, idx) in outline.content.events" :key="idx" class="event-item">
                  <el-icon style="color: #409eff; margin-right: 4px;"><Finished /></el-icon>
                  {{ event }}
                </div>
              </div>
              <el-empty v-else description="暂无事件" :image-size="60" style="padding: 20px 0;" />
            </el-collapse-item>
          </el-collapse>
          <el-pagination
            v-if="volumeOutlines.length > outlinePageSize"
            v-model:current-page="volumeOutlinePage"
            :page-size="outlinePageSize"
            :total="volumeOutlines.length"
            layout="prev,pager,next,jumper"
            style="margin-top:16px; justify-content:center; display:flex;"
            background
          />
        </div>
        <el-empty v-else description="该卷暂无章节大纲，请先生成" :image-size="80" />
      </el-dialog>

      <!-- Chapter Outlines -->
      <el-card v-if="chapterOutlines.length > 0" shadow="hover" style="margin-top: 20px;">
        <template #header>
          <div style="display:flex; justify-content:space-between; align-items:center;">
            <div><span>章节大纲</span><el-tag size="small" type="info" style="margin-left: 8px;">{{ chapterOutlines.length }} 章</el-tag></div>
            <el-pagination
              v-model:current-page="outlinePage"
              :page-size="outlinePageSize"
              :total="chapterOutlines.length"
              layout="prev,pager,next"
              small
              background
            />
          </div>
        </template>
        <el-collapse accordion>
          <el-collapse-item v-for="outline in pagedChapterOutlines" :key="outline.id" :name="outline.id">
            <template #title>
              <span style="font-weight: 500; margin-right: 8px;">第{{ outline.order_num }}章</span>
              <span style="color: #606266;">{{ outline.title }}</span>
            </template>
            <div v-if="outline.content && outline.content.events" class="chapter-events">
              <div v-for="(event, idx) in outline.content.events" :key="idx" class="event-item">
                <el-icon style="color: #409eff; margin-right: 4px;"><Finished /></el-icon>
                {{ event }}
              </div>
            </div>
            <el-empty v-else description="暂无事件" :image-size="60" style="padding: 20px 0;" />
          </el-collapse-item>
        </el-collapse>
        <el-pagination
          v-if="chapterOutlines.length > outlinePageSize"
          v-model:current-page="outlinePage"
          :page-size="outlinePageSize"
          :total="chapterOutlines.length"
          layout="prev,pager,next,jumper"
          style="margin-top:16px; justify-content:center; display:flex;"
          background
        />
      </el-card>

      <!-- Blueprint Raw Content -->
      <el-card shadow="hover" style="margin-top: 20px;">
        <template #header><span>蓝图详情</span></template>
        <template v-if="hasData(currentBlueprint.master_outline) || hasData(currentBlueprint.relation_graph) || hasData(currentBlueprint.global_timeline)">
          <div v-if="hasData(currentBlueprint.master_outline)" class="bp-section">
            <div class="bp-section-title">总体大纲</div>
            <div class="bp-outline-list">
              <div v-for="(item, idx) in parseMasterOutline(currentBlueprint.master_outline)" :key="idx" class="bp-outline-item">
                <span v-if="item.vol" class="bp-outline-vol">{{ item.vol }}</span>
                <span class="bp-outline-desc">{{ item.desc }}</span>
              </div>
            </div>
          </div>
          <div v-if="hasData(currentBlueprint.relation_graph)" class="bp-section">
            <div class="bp-section-title">角色关系</div>
            <div class="bp-relation-list">
              <div v-for="(rel, idx) in parseRelationGraph(currentBlueprint.relation_graph)" :key="idx" class="bp-relation-item">
                <el-tag size="small" type="info" style="flex-shrink: 0;">{{ rel.pair }}</el-tag>
                <span class="bp-relation-desc">{{ rel.desc }}</span>
              </div>
            </div>
          </div>
          <div v-if="hasData(currentBlueprint.global_timeline)" class="bp-section">
            <div class="bp-section-title">全局时间线</div>
            <div class="bp-timeline-list">
              <div v-for="(event, idx) in parseGlobalTimeline(currentBlueprint.global_timeline)" :key="idx" class="bp-timeline-item">
                <span class="bp-timeline-point">{{ event.point }}</span>
                <span class="bp-timeline-event">{{ event.event }}</span>
              </div>
            </div>
          </div>
        </template>
        <el-empty v-else description="蓝图内容正在解析或生成失败，请查看错误信息" />
      </el-card>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, type FormInstance, type UploadFile } from 'element-plus'
import { blueprintApi, volumeApi, worldBibleApi, characterApi, outlineApi, foreshadowingApi, projectApi, batchWriteApi } from '@/api'
import { Download, Upload, UploadFilled, Finished } from '@element-plus/icons-vue'

const route = useRoute()
const projectId = route.params.projectId as string

const currentBlueprint = ref<any>(null)
const generating = ref(false)
const generatingStep = ref(0)
const generationError = ref('')
const volumes = ref<any[]>([])
const generatingOutlines = ref<Set<number>>(new Set())

const worldBibleCount = ref(0)
const characterCount = ref(0)
const outlineCount = ref(0)
const foreshadowingCount = ref(0)

// Outline data
const outlines = ref<any[]>([])
const chapterOutlines = computed(() => {
  return outlines.value
    .filter(o => o.level === 'chapter')
    .sort((a, b) => a.order_num - b.order_num)
})

// Pagination for chapter outlines
const outlinePageSize = 10
const outlinePage = ref(1)
const pagedChapterOutlines = computed(() => {
  const start = (outlinePage.value - 1) * outlinePageSize
  return chapterOutlines.value.slice(start, start + outlinePageSize)
})

// Pagination for the volume dialog
const volumeOutlinePage = ref(1)
const pagedVolumeOutlines = computed(() => {
  const start = (volumeOutlinePage.value - 1) * outlinePageSize
  return volumeOutlines.value.slice(start, start + outlinePageSize)
})

// Import/Export state
const showImportDialog = ref(false)
const importing = ref(false)
const importFileList = ref<UploadFile[]>([])
const importFileContent = ref<any>(null)

// Chapter outlines view dialog
const showOutlinesDialog = ref(false)
const viewingVolumeNum = ref<number | null>(null)
const volumeOutlines = computed(() => {
  if (!viewingVolumeNum.value) return []
  const volume = volumes.value.find(v => v.volume_num === viewingVolumeNum.value)
  if (!volume) return []
  return chapterOutlines.value.filter(
    o => o.order_num >= volume.chapter_start && o.order_num <= volume.chapter_end
  )
})

// Generation dialog state
const dialogVisible = ref(false)
const isRegenerate = ref(false)
const genFormRef = ref<FormInstance>()
const genForm = ref({ volume_count: 4, chapter_words_min: 2000, chapter_words_max: 3500, idea: '' })
const genFormRules = {
  volume_count: [
    { required: true, message: '请指定卷数', trigger: 'change' },
    { type: 'number', min: 1, message: '卷数至少1卷', trigger: 'change' },
  ],
}

let pollTimer: ReturnType<typeof setInterval> | null = null

function stopPolling() {
  if (pollTimer !== null) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

/** Load project defaults into the generation form. Called on mount and before opening the dialog. */
async function loadProjectDefaults() {
  try {
    const res = await projectApi.get(projectId)
    const p = res?.data?.data
    if (p) {
      const targetWords = p.target_words || 0
      const chapterWords = p.chapter_words || 3000
      let vc = genForm.value.volume_count
      let wMin = genForm.value.chapter_words_min
      let wMax = genForm.value.chapter_words_max
      if (targetWords > 0) {
        vc = Math.max(4, Math.round(targetWords / 100000))
      }
      if (chapterWords > 0) {
        wMin = Math.round(chapterWords * 2 / 3)
        wMax = Math.round(chapterWords * 4 / 3)
      }
      genForm.value = { volume_count: vc, chapter_words_min: wMin, chapter_words_max: wMax, idea: '' }
    }
  } catch { /* keep defaults */ }
}

/** Direct "one-click" generate from the inline panel (no dialog confirmation needed). */
async function quickGenerate() {
  const valid = await genFormRef.value?.validate().catch(() => false)
  if (!valid) return
  isRegenerate.value = false
  await doGenerate()
}

async function openGenerateDialog(regen: boolean) {
  isRegenerate.value = regen
  // Pre-populate volume count from existing volumes when regenerating.
  if (regen && volumes.value.length > 0) {
    genForm.value = { ...genForm.value, volume_count: volumes.value.length, idea: '' }
  } else {
    await loadProjectDefaults()
  }
  dialogVisible.value = true
}

async function confirmGenerate() {
  const valid = await genFormRef.value?.validate().catch(() => false)
  if (!valid) return
  dialogVisible.value = false
  if (isRegenerate.value) {
    stopPolling()
    currentBlueprint.value = null
  }
  await doGenerate()
}

async function pollBlueprintStatus() {
  try {
    const res = await blueprintApi.get(projectId)
    const bp = res?.data?.data
    if (!bp) return
    currentBlueprint.value = bp
    // Advance the step indicator while waiting
    if (generatingStep.value < 3) generatingStep.value++
    if (bp.status !== 'generating') {
      stopPolling()
      generating.value = false
      if (bp.status === 'failed') {
        generationError.value = bp.error_message || '未知错误'
        currentBlueprint.value = null
        ElMessage.error('蓝图生成失败')
      } else {
        generatingStep.value = 4
        ElMessage.success('蓝图生成完成')
        await fetchAll()
      }
    }
  } catch {
    // Network error – keep polling
  }
}

const blueprintStatusType = computed(() => {
  const map: Record<string, string> = {
    generating: 'info', draft: 'info', pending_review: 'warning', approved: 'success', rejected: 'danger', failed: 'danger',
  }
  return (map[currentBlueprint.value?.status] || 'info') as any
})

const blueprintStatusLabel = computed(() => {
  const map: Record<string, string> = {
    generating: '生成中', draft: '草稿', pending_review: '待审核', approved: '已批准', rejected: '已驳回', failed: '生成失败',
  }
  return map[currentBlueprint.value?.status] || currentBlueprint.value?.status
})

function formatDate(d: string) {
  return d ? new Date(d).toLocaleString('zh-CN') : '-'
}

/** Returns true only when a blueprint JSONB field has meaningful content. */
function hasData(val: any): boolean {
  if (val == null || val === undefined) return false
  if (typeof val === 'string') return val.trim() !== ''
  if (Array.isArray(val)) return val.length > 0
  if (typeof val === 'object') {
    // 如果有raw_content，尝试解析
    if (val.raw_content) {
      try {
        const parsed = JSON.parse(val.raw_content)
        return parsed != null && Object.keys(parsed).length > 0
      } catch {
        return String(val.raw_content).trim() !== ''
      }
    }
    return Object.keys(val).length > 0
  }
  return Boolean(val)
}

function parseMasterOutline(val: any): { vol: string; desc: string }[] {
  if (val == null) return []
  let text = ''
  if (typeof val === 'string') {
    text = val
  } else if (typeof val === 'object' && !Array.isArray(val) && val.raw_content) {
    // raw_content可能是完整的blueprint JSON，尝试解析
    try {
      const parsed = JSON.parse(val.raw_content)
      if (parsed && typeof parsed.master_outline === 'string') {
        text = parsed.master_outline
      } else {
        text = String(val.raw_content)
      }
    } catch {
      text = String(val.raw_content)
    }
  } else {
    return [{ vol: '', desc: JSON.stringify(val, null, 2) }]
  }
  return text.split(/。\s*/).filter((s: string) => s.trim()).map((p: string) => {
    const ci = p.indexOf('：') !== -1 ? p.indexOf('：') : p.indexOf(':')
    if (ci > 0) return { vol: p.slice(0, ci).trim(), desc: p.slice(ci + 1).trim() }
    return { vol: '', desc: p.trim() }
  })
}

function parseRelationGraph(val: any): { pair: string; desc: string }[] {
  if (val == null) return []
  let text = ''
  if (typeof val === 'string') {
    text = val
  } else if (typeof val === 'object' && !Array.isArray(val) && val.raw_content) {
    // raw_content可能是完整的blueprint JSON，尝试解析
    try {
      const parsed = JSON.parse(val.raw_content)
      if (parsed && typeof parsed.relation_graph === 'string') {
        text = parsed.relation_graph
      } else {
        text = String(val.raw_content)
      }
    } catch {
      text = String(val.raw_content)
    }
  } else {
    return [{ pair: '', desc: JSON.stringify(val, null, 2) }]
  }
  return text.split(/[;；]\s*/).filter((s: string) => s.trim()).map((p: string) => {
    const ci = p.indexOf('：') !== -1 ? p.indexOf('：') : p.indexOf(':')
    if (ci > 0) return { pair: p.slice(0, ci).trim(), desc: p.slice(ci + 1).trim() }
    return { pair: p.trim(), desc: '' }
  })
}

function parseGlobalTimeline(val: any): { point: string; event: string }[] {
  if (val == null) return []
  let text = ''
  if (typeof val === 'string') {
    text = val
  } else if (typeof val === 'object' && !Array.isArray(val) && val.raw_content) {
    // raw_content可能是完整的blueprint JSON，尝试解析
    try {
      const parsed = JSON.parse(val.raw_content)
      if (parsed && typeof parsed.global_timeline === 'string') {
        text = parsed.global_timeline
      } else {
        text = String(val.raw_content)
      }
    } catch {
      text = String(val.raw_content)
    }
  } else {
    return [{ point: '', event: JSON.stringify(val, null, 2) }]
  }
  return text.split(/[;；]\s*/).filter((s: string) => s.trim()).map((p: string) => {
    const ci = p.indexOf('：') !== -1 ? p.indexOf('：') : p.indexOf(':')
    if (ci > 0) return { point: p.slice(0, ci).trim(), event: p.slice(ci + 1).trim() }
    return { point: p.trim(), event: '' }
  })
}

onMounted(async () => {
  await fetchAll()
  // Pre-fill the inline generation form with project defaults (used when no blueprint exists yet).
  if (!currentBlueprint.value) {
    await loadProjectDefaults()
  }
  // If a generation is already in progress (e.g., after page refresh) resume polling.
  if (currentBlueprint.value?.status === 'generating') {
    generating.value = true
    generatingStep.value = 1
    pollTimer = setInterval(pollBlueprintStatus, 3000)
  } else if (currentBlueprint.value?.status === 'failed') {
    // Show the error from the previous attempt and allow the user to retry.
    generationError.value = currentBlueprint.value.error_message || '蓝图生成失败，请重试'
    currentBlueprint.value = null
  }
})

// ── Import/Export ─────────────────────────────────────────────────────────────

async function exportBlueprint() {
  try {
    const res = await blueprintApi.export(projectId)
    const data = res?.data?.data
    if (!data) {
      ElMessage.error('导出失败：无数据')
      return
    }
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `blueprint-${projectId}-${Date.now()}.json`
    a.click()
    URL.revokeObjectURL(url)
    ElMessage.success('蓝图已导出')
  } catch {
    ElMessage.error('导出失败')
  }
}

function handleImportFileChange(file: UploadFile) {
  if (!file.raw) return
  const reader = new FileReader()
  reader.onload = (e) => {
    try {
      const content = JSON.parse(e.target?.result as string)
      importFileContent.value = content
      importFileList.value = [file]
    } catch {
      ElMessage.error('JSON文件格式错误')
      importFileList.value = []
    }
  }
  reader.readAsText(file.raw)
}

async function confirmImport() {
  if (!importFileContent.value) {
    ElMessage.warning('请先选择文件')
    return
  }
  importing.value = true
  try {
    await blueprintApi.import(projectId, importFileContent.value)
    ElMessage.success('蓝图已导入')
    showImportDialog.value = false
    importFileList.value = []
    importFileContent.value = null
    // Reload data
    await fetchAll()
  } catch {
    ElMessage.error('导入失败')
  } finally {
    importing.value = false
  }
}

onBeforeUnmount(stopPolling)

async function fetchAll() {
  try {
    const [bpRes, volRes] = await Promise.all([
      blueprintApi.get(projectId).catch(() => null),
      volumeApi.list(projectId).catch(() => ({ data: { data: [] } })),
    ])
    if (bpRes?.data?.data) currentBlueprint.value = bpRes.data.data
    volumes.value = volRes.data.data || []

    // Fetch asset counts
    const [wbRes, charRes, olRes, fsRes] = await Promise.all([
      worldBibleApi.get(projectId).catch(() => ({ data: { data: null } })),
      characterApi.list(projectId).catch(() => ({ data: { data: [] } })),
      outlineApi.list(projectId).catch(() => ({ data: { data: [] } })),
      foreshadowingApi.list(projectId).catch(() => ({ data: { data: [] } })),
    ])
    // Count distinct world bible fields (not just presence)
    const wbContent = wbRes?.data?.data?.content
    worldBibleCount.value = wbContent && typeof wbContent === 'object'
      ? Object.keys(wbContent).filter(k => wbContent[k] != null && wbContent[k] !== '').length
      : (wbContent ? 1 : 0)
    characterCount.value = (charRes.data.data || []).length
    outlines.value = (olRes.data.data || [])
    outlineCount.value = outlines.value.length
    // Reset pagination when outlines change
    outlinePage.value = 1
    foreshadowingCount.value = (fsRes.data.data || []).length
  } catch { /* empty */ }
}

async function doGenerate() {
  generating.value = true
  generatingStep.value = 0
  generationError.value = ''
  const payload: Record<string, any> = {
    volume_count: genForm.value.volume_count,
    chapter_words_min: genForm.value.chapter_words_min,
    chapter_words_max: genForm.value.chapter_words_max,
  }
  if (genForm.value.idea.trim()) payload.idea = genForm.value.idea.trim()
  try {
    const res = await blueprintApi.generate(projectId, payload)
    // 202: generation is running in background, start polling
    const bp = res.data?.data
    if (bp) {
      currentBlueprint.value = bp
      generatingStep.value = 1
    }
    stopPolling()
    pollTimer = setInterval(pollBlueprintStatus, 3000)
  } catch (err: any) {
    generating.value = false
    const msg = err?.response?.data?.error || '蓝图生成请求失败'
    ElMessage.error(msg)
  }
}

async function submitReview() {
  try {
    await blueprintApi.submitReview(projectId, currentBlueprint.value.id)
    currentBlueprint.value.status = 'pending_review'
    ElMessage.success('已提交审核')
  } catch { ElMessage.error('提交失败') }
}

async function approveBlueprint() {
  try {
    await blueprintApi.approve(projectId, currentBlueprint.value.id)
    currentBlueprint.value.status = 'approved'
    ElMessage.success('蓝图已批准')
    
    // After approval, ask if user wants to start building the novel
    if (volumes.value.length > 0) {
      const { ElMessageBox } = await import('element-plus')
      try {
        await ElMessageBox.confirm(
          `蓝图已批准，共${volumes.value.length}卷。是否立即开始自动生成所有章节？\n（系统将按卷依次生成章节、自动审阅并批准）`,
          '开始构建小说',
          { confirmButtonText: '开始构建', cancelButtonText: '稍后手动', type: 'success' }
        )
        // Start batch generation for the first volume
        const firstVolume = volumes.value[0]
        if (firstVolume) {
          const response = await batchWriteApi.generateByVolume(projectId, firstVolume.id)
          if (response.data?.data?.task_ids?.length) {
            ElMessage.success({
              message: `已启动第1卷章节生成任务，系统将自动链式生成所有章节。请在"任务队列"中查看进度`,
              duration: 8000
            })
          }
        }
      } catch {
        // User chose "稍后手动" or closed dialog
      }
    }
  } catch { ElMessage.error('操作失败') }
}

async function rejectBlueprint() {
  try {
    const { value: reason } = await (await import('element-plus')).ElMessageBox.prompt('请输入驳回原因', '驳回蓝图', { type: 'warning' })
    await blueprintApi.reject(projectId, currentBlueprint.value.id, reason || '')
    currentBlueprint.value.status = 'rejected'
    ElMessage.success('蓝图已驳回')
  } catch { ElMessage.error('操作失败') }
}

async function approveVolume(id: string) {
  try {
    await volumeApi.approve(projectId, id)
    ElMessage.success('卷已批准')
    await fetchAll()
  } catch { ElMessage.error('操作失败') }
}

async function rejectVolume(id: string) {
  try {
    const { value: reason } = await (await import('element-plus')).ElMessageBox.prompt('驳回原因', '驳回', { type: 'warning' })
    await volumeApi.reject(projectId, id, reason || '')
    ElMessage.success('卷已驳回')
    await fetchAll()
  } catch { ElMessage.error('操作失败') }
}

function viewVolumeOutlines(volumeNum: number) {
  viewingVolumeNum.value = volumeNum
  showOutlinesDialog.value = true
}

async function generateChapterOutlines(volumeNum: number) {
  const volume = volumes.value.find(v => v.volume_num === volumeNum)
  if (!volume) {
    ElMessage.error('找不到卷信息')
    return
  }
  
  const totalChapters = volume.chapter_end - volume.chapter_start + 1
  
  // Count existing chapter outlines for this volume
  const existingCount = chapterOutlines.value.filter(
    o => o.order_num >= volume.chapter_start && o.order_num <= volume.chapter_end
  ).length
  
  const remaining = totalChapters - existingCount
  
  if (remaining === 0) {
    ElMessage.info(`第${volumeNum}卷所有章节大纲已生成完成（共${totalChapters}章）`)
    return
  }
  
  // For large volumes (>15 chapters remaining), ask user for batch size
  let batchSize = remaining
  if (remaining > 15) {
    try {
      const { value } = await (await import('element-plus')).ElMessageBox.prompt(
        `该卷共${totalChapters}章，已生成${existingCount}章，剩余${remaining}章。\n每批生成章节数（推荐5-15章，避免超时）：\n\n💡 系统将自动循环生成，直到该卷所有章节完成`,
        '设置批次大小',
        {
          inputValue: Math.min(10, remaining).toString(),
          inputPattern: /^[1-9]\d*$/,
          inputErrorMessage: '请输入有效的正整数'
        }
      )
      batchSize = parseInt(value, 10)
      if (batchSize > remaining) {
        batchSize = remaining
      }
    } catch {
      // User cancelled
      return
    }
  }
  
  try {
    generatingOutlines.value.add(volumeNum)
    const response = await blueprintApi.generateChapterOutlines(projectId, volumeNum, batchSize)
    
    // Task created (202), redirect to task queue or show message
    if (response.data?.task_id) {
      ElMessage.success({
        message: `第${volumeNum}卷章节大纲生成任务已创建（首批${batchSize}章），系统将自动循环生成至完成。请在"任务队列"中查看进度`,
        duration: 8000
      })
    } else {
      ElMessage.success('章节大纲生成任务已创建')
    }
    
    // Refresh after a short delay to show any already generated outlines
    setTimeout(() => {
      fetchAll()
    }, 2000)
  } catch (err: any) {
    const msg = err?.response?.data?.error || '生成章节大纲失败'
    ElMessage.error(msg)
  } finally {
    generatingOutlines.value.delete(volumeNum)
  }
}

async function regenerateChapterOutlines(volumeNum: number) {
  const volume = volumes.value.find(v => v.volume_num === volumeNum)
  if (!volume) {
    ElMessage.error('找不到卷信息')
    return
  }
  
  const totalChapters = volume.chapter_end - volume.chapter_start + 1
  
  try {
    // Ask user which chapter to regenerate from
    const { value: startChapterInput } = await (await import('element-plus')).ElMessageBox.prompt(
      `第${volumeNum}卷共${totalChapters}章（第${volume.chapter_start}-${volume.chapter_end}章）。\n请输入要重新生成的起始章节号：`,
      '重新生成章节大纲',
      {
        inputValue: volume.chapter_start.toString(),
        inputPattern: /^[1-9]\d*$/,
        inputErrorMessage: '请输入有效的章节号'
      }
    )
    
    const startChapter = parseInt(startChapterInput, 10)
    if (startChapter < volume.chapter_start || startChapter > volume.chapter_end) {
      ElMessage.error(`章节号必须在${volume.chapter_start}到${volume.chapter_end}之间`)
      return
    }
    
    // Ask for batch size
    const remainingFromStart = volume.chapter_end - startChapter + 1
    let batchSize = remainingFromStart
    
    if (remainingFromStart > 15) {
      const { value: batchSizeInput } = await (await import('element-plus')).ElMessageBox.prompt(
        `从第${startChapter}章开始，剩余${remainingFromStart}章。\n每批生成章节数（推荐5-15章，避免超时）：\n\n💡 系统将自动循环生成，直到该卷所有章节完成`,
        '设置批次大小',
        {
          inputValue: Math.min(10, remainingFromStart).toString(),
          inputPattern: /^[1-9]\d*$/,
          inputErrorMessage: '请输入有效的正整数'
        }
      )
      batchSize = parseInt(batchSizeInput, 10)
      if (batchSize > remainingFromStart) {
        batchSize = remainingFromStart
      }
    }
    
    generatingOutlines.value.add(volumeNum)
    const response = await blueprintApi.generateChapterOutlines(projectId, volumeNum, batchSize, startChapter)
    
    if (response.data?.task_id) {
      ElMessage.success({
        message: `第${volumeNum}卷章节大纲重新生成任务已创建（从第${startChapter}章开始，首批${batchSize}章），系统将自动循环生成至完成。请在"任务队列"中查看进度`,
        duration: 8000
      })
    } else {
      ElMessage.success('章节大纲重新生成任务已创建')
    }
    
    setTimeout(() => {
      fetchAll()
    }, 2000)
  } catch (err: any) {
    if (err === 'cancel') {
      // User cancelled prompt
      return
    }
    const msg = err?.response?.data?.error || '重新生成章节大纲失败'
    ElMessage.error(msg)
  } finally {
    generatingOutlines.value.delete(volumeNum)
  }
}
</script>

<style scoped>
.blueprint { max-width: 1200px; margin: 0 auto; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; }
.page-header h1 { font-size: 24px; color: #e0e0e0; }
.status-bar { padding: 0; }
.status-label { color: #888; font-size: 13px; margin-bottom: 4px; }
.status-value { color: var(--nb-text-primary); font-size: 14px; }
.asset-card { text-align: center; }
.blueprint-content { background: var(--nb-table-header-bg); border: 1px solid var(--nb-card-border); padding: 16px; border-radius: 8px; font-size: 12px; color: var(--nb-text-secondary); max-height: 500px; overflow: auto; white-space: pre-wrap; }

/* Inline generation panel */
.generate-panel { max-width: 700px; margin: 0 auto; }
.generate-panel-header { display: flex; align-items: center; gap: 16px; }
/* Generation dialog */
.form-hint { color: #888; font-size: 12px; margin-left: 8px; }

/* Blueprint detail sections */
.bp-section { margin-bottom: 24px; }
.bp-section:last-child { margin-bottom: 0; }
.bp-section-title { font-size: 14px; font-weight: 600; color: #409eff; margin-bottom: 12px; padding-bottom: 6px; border-bottom: 1px solid var(--nb-card-border, #333); }
.bp-outline-item { display: flex; gap: 12px; padding: 8px 0; border-bottom: 1px solid var(--nb-table-header-bg, #2a2a2a); }
.bp-outline-item:last-child { border-bottom: none; }
.bp-outline-vol { flex-shrink: 0; min-width: 60px; font-weight: 600; color: var(--el-color-primary); font-size: 13px; }
.bp-outline-desc { color: var(--nb-text-primary); font-size: 13px; line-height: 1.6; }
.bp-relation-list { display: flex; flex-direction: column; gap: 8px; }
.bp-relation-item { display: flex; align-items: flex-start; gap: 10px; }
.bp-relation-desc { color: var(--nb-text-secondary); font-size: 13px; line-height: 1.5; }
.bp-timeline-list { display: flex; flex-direction: column; gap: 10px; }
.bp-timeline-item { display: flex; gap: 12px; padding-left: 12px; border-left: 2px solid #409eff; }
.bp-timeline-point { flex-shrink: 0; min-width: 80px; font-weight: 600; color: #e6a23c; font-size: 13px; }
.bp-timeline-event { color: var(--nb-text-primary); font-size: 13px; line-height: 1.6; }

.chapter-events { display: flex; flex-direction: column; gap: 12px; }
.event-item { display: flex; align-items: flex-start; padding: 10px 12px; background: var(--nb-card-bg, #1a1a1a); border-radius: 4px; border-left: 3px solid #409eff; }
.event-item:hover { background: var(--nb-hover-bg, #2a2a2a); }
</style>
