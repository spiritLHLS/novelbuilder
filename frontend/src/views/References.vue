<template>
  <div class="references">
    <div class="page-header">
      <h1>参考书管理</h1>
      <div class="header-actions">
        <!-- Upload from local file -->
        <el-upload
          :action="`/api/projects/${projectId}/references/upload`"
          :on-success="handleUploadSuccess"
          :on-error="handleUploadError"
          :show-file-list="false"
          :data="{ title: '', author: '', genre: '' }"
          accept=".txt,.md,.html,.htm"
        >
          <el-button type="default"><el-icon><Upload /></el-icon>本地上传</el-button>
        </el-upload>

        <!-- Multi-site search & download -->
        <el-button type="primary" @click="openFetchDialog">
          <el-icon><Search /></el-icon>从书库搜索导入
        </el-button>

      </div>
    </div>

    <el-table :data="references" v-loading="loading" style="width: 100%">
      <el-table-column prop="title" label="书名" />
      <el-table-column prop="author" label="作者" width="120" />
      <el-table-column prop="genre" label="类型" width="100" />
      <el-table-column label="来源" width="100">
        <template #default="{ row }">
          <el-tag v-if="row.source_url" type="success" size="small" title="从网络导入">网络</el-tag>
          <el-tag v-else-if="row.fetch_site" type="primary" size="small">{{ row.fetch_site }}</el-tag>
          <el-tag v-else type="info" size="small">本地</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="status" label="分析状态" width="110">
        <template #default="{ row }">
          <el-tag :type="row.status === 'completed' ? 'success' : 'info'" size="small">
            {{ row.status === 'completed' ? '已分析' : '待分析' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="280">
        <template #default="{ row }">
          <el-button size="small" @click="viewAnalysis(row)">查看分析</el-button>
          <el-button size="small" type="primary" @click="startAnalysis(row.id)"
            :loading="analyzing === row.id">分析</el-button>
          <el-button size="small" type="warning" @click="showMigration(row)">迁移配置</el-button>
          <el-button size="small" type="danger" @click="deleteReference(row.id)"
            :loading="deleting === row.id">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- ── Multi-step Fetch Dialog ─────────────────────────────────────── -->
    <el-dialog
      v-model="showFetchDialog"
      :title="fetchDialogTitle"
      width="660px"
      :close-on-click-modal="fetchStep !== 'importing'"
      :before-close="handleFetchDialogClose"
      top="6vh"
    >
      <!-- STEP 1: search -->
      <div v-if="fetchStep === 'search'" class="fetch-step">
        <el-input
          v-model="searchKeyword"
          placeholder="输入书名或作者名搜索…"
          size="large"
          clearable
          @keydown.enter="doSearch"
        >
          <template #append>
            <el-button :loading="searchLoading" @click="doSearch">搜索</el-button>
          </template>
        </el-input>
        <div class="step-hint">支持笔趣阁、起点、晋江等多站点聚合搜索</div>
      </div>

      <!-- STEP 2: results -->
      <div v-else-if="fetchStep === 'results'" class="fetch-step">
        <div class="results-header">
          <span class="results-count">共找到 {{ searchResults.length }} 条结果</span>
          <el-button link @click="fetchStep = 'search'">重新搜索</el-button>
        </div>
        <div class="results-list">
          <div
            v-for="(r, i) in searchResults"
            :key="`${r.site}-${r.book_id}-${i}`"
            class="result-item"
            @click="selectBook(r)"
          >
            <div class="result-main">
              <span class="result-title">{{ r.title }}</span>
              <el-tag size="small" type="primary" class="site-tag">{{ r.site }}</el-tag>
            </div>
            <div class="result-meta">
              <span v-if="r.author">作者：{{ r.author }}</span>
              <span v-if="r.word_count">字数：{{ r.word_count }}</span>
              <span v-if="r.latest_chapter" class="latest-chap">最新：{{ r.latest_chapter }}</span>
            </div>
          </div>
          <el-empty v-if="searchResults.length === 0" description="未找到相关结果，请换个关键字" />
        </div>
      </div>

      <!-- STEP 3: chapters preview -->
      <div v-else-if="fetchStep === 'chapters'" class="fetch-step">
        <div v-if="bookInfoLoading" class="loading-block">
          <el-icon class="is-loading"><Loading /></el-icon>
          <span>正在获取章节列表…</span>
        </div>
        <template v-else-if="bookInfo">
          <!-- Book header -->
          <div class="book-header">
            <img v-if="bookInfo.cover_url" :src="bookInfo.cover_url" class="book-cover" alt="封面" />
            <div class="book-meta">
              <div class="book-title">{{ bookInfo.title }}</div>
              <div class="book-author" v-if="bookInfo.author">作者：{{ bookInfo.author }}</div>
              <div class="book-chapters">共 {{ bookInfo.total_chapters }} 章</div>
              <div class="book-summary" v-if="bookInfo.summary">{{ bookInfo.summary.slice(0, 120) }}{{ bookInfo.summary.length > 120 ? '…' : '' }}</div>
            </div>
          </div>

          <!-- Genre -->
          <el-form label-width="80px" style="margin-top:16px">
            <el-form-item label="分类">
              <el-select v-model="fetchGenre" placeholder="选择类型" style="width:180px" clearable>
                <el-option v-for="g in genres" :key="g" :label="g" :value="g" />
              </el-select>
            </el-form-item>
            <el-form-item label="章节范围">
              <el-slider
                v-model="selectedChapterRange"
                range
                :min="0"
                :max="Math.max(flatChapters.length - 1, 0)"
                :marks="chapterRangeMarks"
                style="width: 100%; margin: 0 8px;"
              />
              <div class="range-label">
                第 {{ selectedChapterRange[0] + 1 }} 章 ～ 第 {{ selectedChapterRange[1] + 1 }} 章
                （共 {{ selectedChapterRange[1] - selectedChapterRange[0] + 1 }} 章）
              </div>
            </el-form-item>
          </el-form>

          <!-- Volume / chapter tree (collapsed) -->
          <el-collapse class="volume-collapse">
            <el-collapse-item
              v-for="(vol, vi) in bookInfo.volumes"
              :key="vi"
              :title="`${vol.volume_name}（${vol.chapters.length} 章）`"
            >
              <div class="chapter-list">
                <div
                  v-for="(ch, ci) in vol.chapters"
                  :key="ch.chapter_id"
                  class="chapter-item"
                  :class="{ inaccessible: !ch.accessible }"
                >
                  <el-icon v-if="!ch.accessible" title="VIP/付费章节"><Lock /></el-icon>
                  <span>{{ ci + 1 }}. {{ ch.title }}</span>
                </div>
              </div>
            </el-collapse-item>
          </el-collapse>
        </template>
        <el-alert v-else type="error" :closable="false" title="获取章节列表失败，请返回重试" />

        <div class="step-actions">
          <el-button @click="fetchStep = 'results'">返回</el-button>
          <el-button
            type="primary"
            :disabled="!bookInfo || bookInfoLoading"
            @click="startFetchImport"
          >
            开始导入
          </el-button>
        </div>
      </div>

      <!-- STEP 4: importing (streaming progress) -->
      <div v-else-if="fetchStep === 'importing'" class="fetch-step importing-step">
        <div class="importing-title">{{ importingBookTitle }} 导入中…</div>
        <el-progress
          :percentage="importPercent"
          :stroke-width="14"
          status="active"
          style="margin: 16px 0"
        />
        <div class="importing-status">
          {{ importProgress.done }} / {{ importProgress.total }} 章
          <span v-if="importProgress.chapterTitle" class="chapter-title-hint">
            — {{ importProgress.chapterTitle }}
          </span>
        </div>
        <el-alert
          v-if="importError"
          type="error"
          :title="importError"
          :closable="false"
          style="margin-top: 12px"
        />
      </div>
    </el-dialog>

    <!-- ── Analysis Result Dialog ─────────────────────────────────────────── -->
    <el-dialog v-model="showAnalysisDialog" title="四层分析报告" width="800px" top="5vh">
      <template v-if="selectedRef">
        <div v-if="selectedRef.source_url" class="source-url-info">
          <el-icon><Link /></el-icon>
          <a :href="selectedRef.source_url" target="_blank" rel="noopener noreferrer">{{ selectedRef.source_url }}</a>
        </div>
        <el-tabs>
          <el-tab-pane label="风格指纹层">
            <div class="analysis-section">
              <h3>Layer 1: 风格指纹</h3>
              <div v-if="selectedRef.style_layer && Object.keys(selectedRef.style_layer).length > 0" class="chart-container">
                <v-chart :option="styleChartOption" style="height: 300px" autoresize />
                <pre class="json-view">{{ JSON.stringify(selectedRef.style_layer, null, 2) }}</pre>
              </div>
              <el-empty v-else description="尚未分析" />
            </div>
          </el-tab-pane>
          <el-tab-pane label="叙事结构层">
            <div class="analysis-section">
              <h3>Layer 2: 叙事结构</h3>
              <pre class="json-view" v-if="selectedRef.narrative_layer && Object.keys(selectedRef.narrative_layer).length > 0">{{ JSON.stringify(selectedRef.narrative_layer, null, 2) }}</pre>
              <el-empty v-else description="尚未分析" />
            </div>
          </el-tab-pane>
          <el-tab-pane label="氛围萃取层">
            <div class="analysis-section">
              <h3>Layer 3: 氛围萃取</h3>
              <div v-if="selectedRef.atmosphere_layer && Object.keys(selectedRef.atmosphere_layer).length > 0" class="chart-container">
                <v-chart :option="atmosphereChartOption" style="height: 300px" autoresize />
                <pre class="json-view">{{ JSON.stringify(selectedRef.atmosphere_layer, null, 2) }}</pre>
              </div>
              <el-empty v-else description="尚未分析" />
            </div>
          </el-tab-pane>
          <el-tab-pane label="隔离区">
            <div class="analysis-section">
              <h3>Layer 4: 情节元素隔离区</h3>
              <el-alert type="warning" :closable="false" show-icon>
                隔离区中的情节元素被锁定，不会直接用于生成，仅提供结构参考。
              </el-alert>
            </div>
          </el-tab-pane>
        </el-tabs>
      </template>
    </el-dialog>

    <!-- ── Migration Config Dialog ────────────────────────────────────────── -->
    <el-dialog v-model="showMigrationDialog" title="氛围迁移配置" width="600px">
      <el-form :model="migrationForm" label-width="120px">
        <el-form-item label="迁移强度">
          <el-slider v-model="migrationForm.intensity" :min="0" :max="100" :step="5" show-stops />
        </el-form-item>
        <el-form-item label="允许迁移层">
          <el-checkbox-group v-model="migrationForm.layers">
            <el-checkbox label="style">风格层</el-checkbox>
            <el-checkbox label="narrative">叙事层</el-checkbox>
            <el-checkbox label="atmosphere">氛围层</el-checkbox>
          </el-checkbox-group>
        </el-form-item>
        <el-form-item label="禁止迁移项">
          <el-input v-model="migrationForm.forbidden" type="textarea" :rows="3"
            placeholder="列出不希望迁移的特定元素，每行一个" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showMigrationDialog = false">取消</el-button>
        <el-button type="primary" @click="saveMigration">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox, type FormInstance } from 'element-plus'
import { Upload, Link, Search, Loading, Lock } from '@element-plus/icons-vue'
import { referenceApi, streamFetchImport } from '@/api'
import type { NovelSearchResult, FetchBookInfo, FetchChapterInfo } from '@/api'
import VChart from 'vue-echarts'

const route = useRoute()
const projectId = route.params.projectId as string

const genres = ['玄幻', '修真', '都市', '历史', '科幻', '悬疑', '武侠', '其他']

// ─── reference list ───────────────────────────────────────────────────────────
const loading = ref(false)
const references = ref<any[]>([])
const analyzing = ref<string | null>(null)
const deleting = ref<string | null>(null)
const showAnalysisDialog = ref(false)
const showMigrationDialog = ref(false)
const selectedRef = ref<any>(null)
const selectedRefId = ref('')

const migrationForm = ref({
  intensity: 50,
  layers: ['style', 'atmosphere'],
  forbidden: '',
})

// ─── multi-step fetch dialog ──────────────────────────────────────────────────
const showFetchDialog = ref(false)
const fetchStep = ref<'search' | 'results' | 'chapters' | 'importing'>('search')

const searchKeyword = ref('')
const searchLoading = ref(false)
const searchResults = ref<NovelSearchResult[]>([])

const selectedBook = ref<NovelSearchResult | null>(null)
const bookInfo = ref<FetchBookInfo | null>(null)
const bookInfoLoading = ref(false)
const fetchGenre = ref('')
const selectedChapterRange = ref<[number, number]>([0, 0])

const importProgress = ref({ done: 0, total: 0, chapterTitle: '' })
const importingBookTitle = ref('')
const importError = ref('')

const fetchDialogTitle = computed(() => {
  if (fetchStep.value === 'search') return '搜索参考书'
  if (fetchStep.value === 'results') return `搜索结果：${searchKeyword.value}`
  if (fetchStep.value === 'chapters') return bookInfo.value?.title ?? '选择章节'
  return '正在导入…'
})

const flatChapters = computed((): FetchChapterInfo[] => {
  if (!bookInfo.value) return []
  return bookInfo.value.volumes.flatMap(v => v.chapters)
})

const importPercent = computed(() => {
  const { done, total } = importProgress.value
  if (total === 0) return 0
  return Math.round((done / total) * 100)
})

const chapterRangeMarks = computed(() => {
  const total = flatChapters.value.length
  if (total === 0) return {}
  return {
    0: '1',
    [total - 1]: String(total),
  }
})

function openFetchDialog() {
  fetchStep.value = 'search'
  searchKeyword.value = ''
  searchResults.value = []
  bookInfo.value = null
  selectedBook.value = null
  fetchGenre.value = ''
  importError.value = ''
  showFetchDialog.value = true
}

function handleFetchDialogClose(done: () => void) {
  if (fetchStep.value === 'importing') {
    ElMessageBox.confirm('导入正在进行中，确定要关闭吗？', '提示', {
      confirmButtonText: '关闭',
      cancelButtonText: '继续等待',
      type: 'warning',
    }).then(done).catch(() => {})
  } else {
    done()
  }
}

async function doSearch() {
  const kw = searchKeyword.value.trim()
  if (!kw) return
  searchLoading.value = true
  try {
    const res = await referenceApi.searchNovels(projectId, kw)
    searchResults.value = (res.data as any).results ?? []
    fetchStep.value = 'results'
  } catch (e: any) {
    const msg = e?.response?.data?.error || e?.response?.data?.detail || '搜索失败，请稍后重试'
    ElMessage.error(msg)
  } finally {
    searchLoading.value = false
  }
}

async function selectBook(book: NovelSearchResult) {
  selectedBook.value = book
  bookInfo.value = null
  bookInfoLoading.value = true
  fetchStep.value = 'chapters'
  try {
    const res = await referenceApi.getBookInfo(projectId, book.site, book.book_id)
    bookInfo.value = res.data as FetchBookInfo
    const total = bookInfo.value.total_chapters
    selectedChapterRange.value = [0, Math.max(total - 1, 0)]
  } catch (e: any) {
    const msg = e?.response?.data?.error || e?.response?.data?.detail || '获取章节列表失败'
    ElMessage.error(msg)
  } finally {
    bookInfoLoading.value = false
  }
}

async function startFetchImport() {
  if (!selectedBook.value || !bookInfo.value) return
  const flat = flatChapters.value
  const [startIdx, endIdx] = selectedChapterRange.value
  const chapterIds = flat.slice(startIdx, endIdx + 1).map(c => c.chapter_id)
  if (chapterIds.length === 0) {
    ElMessage.warning('请至少选择一章')
    return
  }

  importingBookTitle.value = bookInfo.value.title
  importProgress.value = { done: 0, total: chapterIds.length, chapterTitle: '' }
  importError.value = ''
  fetchStep.value = 'importing'

  try {
    for await (const event of streamFetchImport(projectId, {
      site: selectedBook.value.site,
      book_id: selectedBook.value.book_id,
      title: bookInfo.value.title,
      author: bookInfo.value.author,
      genre: fetchGenre.value,
      chapter_ids: chapterIds,
    })) {
      if (event.type === 'progress') {
        importProgress.value = {
          done: event.done,
          total: event.total,
          chapterTitle: event.chapter_title,
        }
      } else if (event.type === 'done') {
        ElMessage.success(`《${importingBookTitle.value}》导入完成，共 ${event.total_chapters} 章`)
        showFetchDialog.value = false
        await fetchRefs()
      } else if (event.type === 'error') {
        importError.value = event.message
        fetchStep.value = 'chapters'
      }
    }
  } catch (e: any) {
    importError.value = e?.message || '导入失败，请重试'
    fetchStep.value = 'chapters'
  }
}

// ─── analysis & migration ─────────────────────────────────────────────────────
const styleChartOption = computed(() => {
  const fp = selectedRef.value?.style_layer
  if (!fp || Object.keys(fp).length === 0) return {}
  const indicators = [
    { name: '词汇丰富度', max: 1 },
    { name: '平均句长', max: 50 },
    { name: '突发度', max: 1 },
    { name: '比喻密度', max: 0.1 },
    { name: '对话占比', max: 1 },
  ]
  return {
    backgroundColor: 'transparent',
    radar: { indicator: indicators, shape: 'circle' },
    series: [{
      type: 'radar',
      data: [{
        value: [
          fp.vocabulary_richness?.ttr || 0,
          fp.sentence?.avg_length || 0,
          fp.sentence?.burstiness_index || 0,
          fp.rhetoric?.metaphor_density || 0,
          fp.punctuation?.dialogue_quote_count ? 0.5 : 0,
        ],
        name: '风格指纹',
      }],
    }],
  }
})

const atmosphereChartOption = computed(() => {
  const ap = selectedRef.value?.atmosphere_layer
  if (!ap || Object.keys(ap).length === 0) return {}
  const sensory = ap.sensory_profile || {}
  return {
    backgroundColor: 'transparent',
    radar: {
      indicator: [
        { name: '视觉', max: 1 },
        { name: '听觉', max: 1 },
        { name: '嗅觉', max: 1 },
        { name: '触觉', max: 1 },
        { name: '味觉', max: 1 },
      ],
      shape: 'circle',
    },
    series: [{
      type: 'radar',
      data: [{
        value: [
          sensory.visual?.ratio || 0,
          sensory.auditory?.ratio || 0,
          sensory.olfactory?.ratio || 0,
          sensory.tactile?.ratio || 0,
          sensory.gustatory?.ratio || 0,
        ],
        name: '感官分布',
      }],
    }],
  }
})

onMounted(fetchRefs)

async function fetchRefs() {
  loading.value = true
  try {
    const res = await referenceApi.list(projectId)
    references.value = (res.data as any).data || []
  } finally {
    loading.value = false
  }
}

function handleUploadSuccess(response: any) {
  ElMessage.success('上传成功')
  if (response.data) {
    references.value.push(response.data)
  }
}

function handleUploadError() {
  ElMessage.error('上传失败，请重试')
}

async function startAnalysis(id: string) {
  analyzing.value = id
  try {
    await referenceApi.analyze(id)
    ElMessage.success('分析完成')
    await fetchRefs()
  } catch {
    ElMessage.error('分析失败')
  } finally {
    analyzing.value = null
  }
}

async function viewAnalysis(ref: any) {
  try {
    const res = await referenceApi.get(ref.id)
    selectedRef.value = (res.data as any).data
    showAnalysisDialog.value = true
  } catch {
    ElMessage.error('获取分析结果失败')
  }
}

function showMigration(ref: any) {
  selectedRefId.value = ref.id
  if (ref.migration_config) {
    migrationForm.value = { ...ref.migration_config }
  }
  showMigrationDialog.value = true
}

async function saveMigration() {
  try {
    await referenceApi.updateMigrationConfig(selectedRefId.value, migrationForm.value)
    ElMessage.success('迁移配置已保存')
    showMigrationDialog.value = false
  } catch {
    ElMessage.error('保存失败')
  }
}

async function deleteReference(id: string) {
  try {
    await ElMessageBox.confirm('确定要删除该参考书吗？此操作不可撤销。', '删除确认', {
      confirmButtonText: '删除',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return
  }
  deleting.value = id
  try {
    await referenceApi.delete(id)
    references.value = references.value.filter(r => r.id !== id)
    ElMessage.success('已删除')
  } catch {
    ElMessage.error('删除失败')
  } finally {
    deleting.value = null
  }
}
</script>

<style scoped>
.references { max-width: 1200px; margin: 0 auto; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; }
.page-header h1 { font-size: 24px; color: #e0e0e0; }
.header-actions { display: flex; gap: 12px; align-items: center; }

/* Fetch dialog steps */
.fetch-step { min-height: 120px; }
.step-hint { margin-top: 8px; font-size: 12px; color: var(--nb-text-secondary); }

/* Results */
.results-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.results-count { font-size: 13px; color: var(--nb-text-secondary); }
.results-list { max-height: 420px; overflow-y: auto; display: flex; flex-direction: column; gap: 8px; }
.result-item {
  padding: 12px 16px; border-radius: 8px;
  border: 1px solid var(--nb-card-border, #333);
  background: var(--nb-card-bg, #1e1e1e);
  cursor: pointer; transition: border-color .2s, background .2s;
}
.result-item:hover { border-color: #409eff; background: var(--nb-glass-bg-hover, #252525); }
.result-main { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; }
.result-title { font-weight: 500; color: #e0e0e0; }
.site-tag { flex-shrink: 0; }
.result-meta { display: flex; flex-wrap: wrap; gap: 12px; font-size: 12px; color: var(--nb-text-secondary); }
.latest-chap { max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

/* Book header */
.book-header { display: flex; gap: 16px; margin-bottom: 4px; }
.book-cover { width: 72px; height: 96px; object-fit: cover; border-radius: 6px; flex-shrink: 0; }
.book-meta { flex: 1; }
.book-title { font-size: 16px; font-weight: 600; color: #e0e0e0; margin-bottom: 6px; }
.book-author { font-size: 13px; color: var(--nb-text-secondary); margin-bottom: 4px; }
.book-chapters { font-size: 13px; color: #409eff; margin-bottom: 6px; }
.book-summary { font-size: 12px; color: var(--nb-text-secondary); line-height: 1.5; }

/* Range */
.range-label { font-size: 12px; color: var(--nb-text-secondary); margin-top: 8px; }

/* Volume collapse */
.volume-collapse { margin-top: 12px; max-height: 240px; overflow-y: auto; }
.chapter-list { display: flex; flex-direction: column; gap: 4px; max-height: 200px; overflow-y: auto; }
.chapter-item { font-size: 12px; color: var(--nb-text-secondary); display: flex; align-items: center; gap: 4px; }
.chapter-item.inaccessible { opacity: 0.5; }

/* Step actions */
.step-actions { display: flex; justify-content: flex-end; gap: 12px; margin-top: 20px; }

/* Importing */
.importing-step { text-align: center; padding: 12px 0; }
.importing-title { font-size: 16px; color: #e0e0e0; margin-bottom: 8px; }
.importing-status { font-size: 13px; color: var(--nb-text-secondary); }
.chapter-title-hint { font-style: italic; }

/* Loading block */
.loading-block { display: flex; align-items: center; gap: 8px; color: var(--nb-text-secondary); justify-content: center; padding: 40px 0; }

/* Analysis */
.analysis-section { padding: 16px 0; }
.analysis-section h3 { margin-bottom: 16px; color: #409eff; }
.json-view { background: var(--nb-table-header-bg); border: 1px solid var(--nb-card-border); padding: 16px; border-radius: 8px; font-size: 12px; color: var(--nb-text-secondary); max-height: 400px; overflow: auto; margin-top: 16px; }
.chart-container { background: var(--nb-card-bg); border: 1px solid var(--nb-card-border); border-radius: 8px; padding: 16px; }
.form-hint { font-size: 12px; color: var(--nb-text-secondary); margin-top: 4px; }
.source-url-info { display: flex; align-items: center; gap: 6px; margin-bottom: 16px; font-size: 13px; color: var(--nb-text-secondary); }
.source-url-info a { color: #409eff; word-break: break-all; }
</style>
