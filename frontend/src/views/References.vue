<template>
  <div class="references">
    <div class="page-header">
      <h1>参考书管理</h1>
      <div class="header-actions">
        <!-- Local file: raw text or exported JSON bundle (auto-detected) -->
        <input
          ref="localFileInput"
          type="file"
          accept=".txt,.md,.html,.htm,.json"
          style="display:none"
          @change="handleLocalFile"
        />
        <el-button type="default" @click="localFileInput?.click()">
          <el-icon><Upload /></el-icon>本地添加
        </el-button>

        <!-- Batch export selected -->
        <el-button
          v-if="selectedIds.length > 0"
          type="success"
          @click="exportBatch"
          :loading="exportingBatch"
        >
          <el-icon><DocumentCopy /></el-icon>导出已选 ({{ selectedIds.length }})
        </el-button>

        <!-- Multi-site search & download -->
        <el-button type="primary" @click="openFetchDialog">
          <el-icon><Search /></el-icon>从书库搜索导入
        </el-button>
      </div>
    </div>

    <el-table
      :data="references"
      v-loading="loading"
      style="width: 100%"
      @selection-change="handleSelectionChange"
    >
      <el-table-column type="selection" width="45" />
      <el-table-column prop="title" label="书名" min-width="140" />
      <el-table-column prop="author" label="作者" width="110" />
      <el-table-column prop="genre" label="类型" width="80" />
      <el-table-column label="来源" width="90">
        <template #default="{ row }">
          <el-tag v-if="row.source_url" type="success" size="small">网络</el-tag>
          <el-tag v-else-if="row.fetch_site" type="primary" size="small">{{ row.fetch_site }}</el-tag>
          <el-tag v-else type="info" size="small">本地</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="下载状态" width="140">
        <template #default="{ row }">
          <template v-if="row.fetch_status === 'downloading'">
            <el-progress
              :percentage="row.fetch_total > 0 ? Math.round(row.fetch_done / row.fetch_total * 100) : 0"
              :stroke-width="6"
              style="width:100px"
            />
            <span class="progress-text">{{ row.fetch_done }}/{{ row.fetch_total }}</span>
          </template>
          <el-tag v-else-if="row.fetch_status === 'completed'" type="success" size="small">已下载</el-tag>
          <el-tag v-else-if="row.fetch_status === 'failed'" type="danger" size="small">下载失败</el-tag>
          <el-tag v-else type="info" size="small">—</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="status" label="分析状态" width="90">
        <template #default="{ row }">
          <el-tag :type="row.status === 'completed' ? 'success' : 'info'" size="small">
            {{ row.status === 'completed' ? '已分析' : '未分析' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="280" fixed="right">
        <template #default="{ row }">
          <el-button size="small" @click="openChaptersDialog(row)">章节管理</el-button>
          <el-button size="small" type="primary" plain @click="openDeepAnalysisDialog(row)">深度分析</el-button>
          <el-button size="small" type="warning" plain @click="setAsContinuationSource(row)"
            :loading="settingContinuation === row.id">设为续写底本</el-button>
          <el-button size="small" type="success" @click="exportSingle(row)"
            :loading="exporting === row.id">导出</el-button>
          <el-button size="small" type="danger" @click="deleteReference(row.id)"
            :loading="deleting === row.id">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- ── Chapter Management Dialog ────────────────────────────────────── -->
    <el-dialog
      v-model="showChaptersDialog"
      :title="`章节管理：${chapterRefTitle}`"
      width="780px"
      top="5vh"
    >
      <div class="chapter-toolbar">
        <el-input
          v-model="chapterSearch"
          placeholder="搜索章节标题…"
          clearable
          style="width:240px"
          @input="chapterSearchPage = 1"
        />
        <div class="chapter-toolbar-right">
          <el-checkbox
            v-model="chapterSelectAll"
            :indeterminate="isChapterIndeterminate"
            @change="toggleSelectAllChapters"
          >全选本页</el-checkbox>
          <el-button
            size="small"
            type="danger"
            :disabled="selectedChapterIds.length === 0"
            :loading="deletingChapters"
            @click="batchDeleteChapters"
          >
            <el-icon><Delete /></el-icon>删除选中 ({{ selectedChapterIds.length }})
          </el-button>
        </div>
      </div>

      <div v-if="chaptersLoading" class="loading-block">
        <el-icon class="is-loading"><Loading /></el-icon> 加载章节…
      </div>
      <el-table
        v-else
        :data="pagedChapters"
        size="small"
        style="width:100%"
        @selection-change="handleChapterSelectionChange"
        ref="chapterTableRef"
      >
        <el-table-column type="selection" width="45" />
        <el-table-column prop="chapter_no" label="序号" width="60" />
        <el-table-column prop="title" label="章节标题" min-width="200" />
        <el-table-column prop="word_count" label="字数" width="70" />
        <el-table-column label="操作" width="80">
          <template #default="{ row }">
            <el-button size="small" type="danger" link @click="deleteSingleChapter(row.id)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="chapter-pagination" v-if="filteredChapters.length > CHAPTER_PAGE_SIZE">
        <el-pagination
          v-model:current-page="chapterSearchPage"
          :page-size="CHAPTER_PAGE_SIZE"
          :total="filteredChapters.length"
          layout="prev, pager, next"
          small
        />
      </div>

      <div class="chapter-footer-hint" v-if="chapters.length > 0">
        共 {{ chapters.length }} 章（已删 {{ deletedCount }} 章）
      </div>

      <el-empty v-if="!chaptersLoading && filteredChapters.length === 0" description="暂无章节" />
    </el-dialog>

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

        <div class="site-settings-card">
          <div class="site-settings-header">
            <div>
              <div class="site-settings-title">搜索书源</div>
              <div class="site-settings-meta" v-if="siteCatalog">
                默认已启用全部 {{ siteCatalog.count }} 个可搜索站点
                <span v-if="siteCatalog.legado_source_count">，另有 {{ siteCatalog.legado_source_count }} 个阅读书源支持下方 URL 直导</span>
              </div>
              <div class="site-settings-meta" v-else>
                未加载到书源清单时，将回退到后端默认站点集合
              </div>
            </div>
            <div class="site-settings-actions">
              <el-button link :disabled="siteCatalogLoading || !siteCatalog?.sites?.length" @click="resetSearchSiteSelection">全选</el-button>
              <el-button link :disabled="siteCatalogLoading || !selectedSearchSites.length" @click="selectedSearchSites = []">清空</el-button>
            </div>
          </div>

          <el-skeleton :loading="siteCatalogLoading" animated :rows="2">
            <template #default>
              <el-select
                v-model="selectedSearchSites"
                multiple
                filterable
                clearable
                collapse-tags
                collapse-tags-tooltip
                placeholder="选择搜索站点（默认全选）"
                style="width: 100%"
              >
                <el-option v-for="site in siteCatalog?.sites ?? []" :key="site" :label="site" :value="site" />
              </el-select>
              <div class="site-settings-footer">
                <span v-if="siteCatalog">已选 {{ selectedSearchSites.length }} / {{ siteCatalog.count }} 个搜索站点</span>
                <span v-if="siteCatalog?.legado_source_count">阅读书源不参与聚合搜索，但支持 URL 解析导入</span>
              </div>
            </template>
          </el-skeleton>
        </div>

        <el-divider>或直接粘贴书籍 URL</el-divider>

        <el-input
          v-model="directBookURL"
          placeholder="粘贴书籍详情页 URL，支持 novel-downloader 内置站点与阅读书源"
          clearable
          @keydown.enter="resolveBookURLImport"
        >
          <template #append>
            <el-button :loading="urlResolving" @click="resolveBookURLImport">解析 URL</el-button>
          </template>
        </el-input>
        <div class="step-hint">URL 导入会先解析站点与 book_id，再进入章节选择。</div>
      </div>

      <!-- STEP 2: results -->
      <div v-else-if="fetchStep === 'results'" class="fetch-step">
        <div class="results-header">
          <span class="results-count">
            <template v-if="searchLoading">
              <el-icon class="is-loading"><Loading /></el-icon>
              {{ searchStreamStatus || '搜索中…' }}
            </template>
            <template v-else>
              共 {{ searchResults.length }} 条结果
              <span v-if="totalPages > 1">（第 {{ searchPage + 1 }} / {{ totalPages }} 页）</span>
            </template>
          </span>
          <el-button link @click="fetchStep = 'search'">重新搜索</el-button>
        </div>
        <div class="results-list">
          <div
            v-for="(r, i) in pagedResults"
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
        <!-- Pagination -->
        <div v-if="totalPages > 1" class="results-pagination">
          <el-button size="small" :disabled="searchPage === 0" @click="searchPage--">上一页</el-button>
          <span class="page-indicator">{{ searchPage + 1 }} / {{ totalPages }}</span>
          <el-button size="small" :disabled="searchPage >= totalPages - 1" @click="searchPage++">下一页</el-button>
        </div>
      </div>

      <!-- STEP 3: chapters preview -->
      <div v-else-if="fetchStep === 'chapters'" class="fetch-step">
        <div v-if="bookInfoLoading" class="loading-block">
          <el-icon class="is-loading"><Loading /></el-icon>
          <span>正在获取章节列表…</span>
        </div>
        <template v-else-if="bookInfo">
          <div v-if="resolvedSourceMeta" class="source-url-info">
            <el-tag size="small" :type="resolvedSourceMeta.source_kind === 'legado' ? 'warning' : 'success'">
              {{ resolvedSourceMeta.source_kind === 'legado' ? '阅读书源 URL' : '书籍 URL' }}
            </el-tag>
            <span v-if="resolvedSourceMeta.source_name">{{ resolvedSourceMeta.source_name }}</span>
            <a :href="resolvedSourceMeta.url" target="_blank" rel="noreferrer noopener">{{ resolvedSourceMeta.url }}</a>
          </div>

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

      <!-- STEP 4: importing — now background, show brief confirmation -->
      <div v-else-if="fetchStep === 'importing'" class="fetch-step importing-step">
        <el-result icon="success" title="下载已在后台启动">
          <template #sub-title>
            <p>《{{ importingBookTitle }}》正在后台下载，共 {{ importStartedTotal }} 章。</p>
            <p>您可以切换到其他页面，右下角下载管理器会持续显示进度。</p>
          </template>
          <template #extra>
            <el-button type="primary" @click="showFetchDialog = false">知道了</el-button>
          </template>
        </el-result>
      </div>
    </el-dialog>

    <!-- ── Deep Analysis Dialog ─────────────────────────────────────────── -->
    <el-dialog
      v-model="showDeepAnalysisDialog"
      title="深度分析（提取人物 / 世界观 / 大纲）"
      width="720px"
      @closed="stopDeepAnalysisPoll"
    >
      <div v-if="deepAnalysisDialogLoading" class="loading-block">
        <el-icon class="is-loading"><Loading /></el-icon> 正在查询分析状态…
      </div>
      <template v-else>
        <div v-if="deepAnalysisJob" class="deep-analysis-status">
          <div class="da-header">
            <span class="da-title">{{ deepAnalysisRef?.title }}</span>
            <el-tag :type="daStatusType" size="small">{{ daStatusText }}</el-tag>
          </div>
          <template v-if="deepAnalysisJob.status === 'running' || deepAnalysisJob.status === 'pending'">
            <el-progress
              :percentage="deepAnalysisJob.total_chunks > 0
                ? Math.round(deepAnalysisJob.done_chunks / deepAnalysisJob.total_chunks * 100)
                : 0"
              :stroke-width="10"
              :format="() => `${deepAnalysisJob.done_chunks} / ${deepAnalysisJob.total_chunks} 块`"
              style="margin: 16px 0"
            />
            <p class="da-hint">分析在后台运行中，可关闭此窗口稍后回看进度</p>
          </template>
          <el-alert v-else-if="deepAnalysisJob.status === 'failed'"
            type="error" :title="deepAnalysisJob.error_message || '分析失败'" :closable="false"
            style="margin-top:12px" show-icon />
          <template v-else-if="deepAnalysisJob.status === 'completed'">
            <el-alert
              type="success" title="深度分析完成，以下为提取结果预览，点击「导入到项目」将数据写入人物、世界观和大纲" :closable="false"
              style="margin: 12px 0" show-icon />
            <el-tabs class="da-result-tabs">
              <el-tab-pane :label="`人物设定（${daChars.length} 个）`">
                <div class="da-result-scroll">
                  <div v-if="daChars.length > 0">
                    <div v-for="ch in daChars" :key="ch.name" class="da-char-item">
                      <div class="da-char-header">
                        <strong class="da-char-name">{{ ch.name }}</strong>
                        <el-tag size="small" :type="roleTagType(ch.role)">{{ ch.role || '其他' }}</el-tag>
                      </div>
                      <p class="da-char-desc">{{ ch.description }}</p>
                      <div v-if="ch.traits?.length" class="da-char-traits">
                        <el-tag v-for="t in ch.traits" :key="t" size="small" type="info" style="margin:2px">{{ t }}</el-tag>
                      </div>
                    </div>
                  </div>
                  <el-empty v-else description="未提取到人物信息" />
                </div>
              </el-tab-pane>
              <el-tab-pane label="世界观">
                <div class="da-result-scroll" v-if="daWorld && Object.keys(daWorld).length > 0">
                  <div class="da-world-item" v-if="daWorld.setting">
                    <div class="da-world-label">世界背景</div>
                    <div class="da-world-value">{{ daWorld.setting }}</div>
                  </div>
                  <div class="da-world-item" v-if="daWorld.time_period">
                    <div class="da-world-label">时代背景</div>
                    <div class="da-world-value">{{ daWorld.time_period }}</div>
                  </div>
                  <div class="da-world-item" v-if="daWorld.locations?.length">
                    <div class="da-world-label">主要场景</div>
                    <div class="da-world-tags">
                      <el-tag v-for="loc in daWorld.locations" :key="loc" size="small" style="margin:2px">{{ loc }}</el-tag>
                    </div>
                  </div>
                  <div class="da-world-item" v-if="daWorld.systems?.length">
                    <div class="da-world-label">体系设定</div>
                    <div class="da-world-tags">
                      <el-tag v-for="sys in daWorld.systems" :key="sys" size="small" type="warning" style="margin:2px">{{ sys }}</el-tag>
                    </div>
                  </div>
                </div>
                <el-empty v-else description="未提取到世界观信息" />
              </el-tab-pane>
              <el-tab-pane :label="`大纲（${daOutline.length} 条）`">
                <div class="da-result-scroll">
                  <div v-if="daOutline.length > 0">
                    <div v-for="(node, idx) in daOutline" :key="idx" class="da-outline-item"
                      :class="node.level === 'meso' ? 'da-outline-sub' : node.level === 'micro' ? 'da-outline-sub2' : ''">
                      <span class="da-outline-idx">{{ idx + 1 }}.</span>
                      <span class="da-outline-title">{{ node.title }}</span>
                      <span class="da-outline-summary" v-if="node.summary || node.key_events">— {{ node.summary || node.key_events }}</span>
                    </div>
                  </div>
                  <el-empty v-else description="未提取到大纲信息" />
                </div>
              </el-tab-pane>
              <el-tab-pane :label="`术语（${daGlossary.length} 条）`">
                <div class="da-result-scroll">
                  <div v-if="daGlossary.length > 0">
                    <div v-for="(term, idx) in daGlossary" :key="idx" class="da-outline-item">
                      <el-tag size="small" type="info" style="margin-right:8px">{{ term.category || '概念' }}</el-tag>
                      <strong>{{ term.term }}</strong>
                      <span class="da-outline-summary" v-if="term.definition"> — {{ term.definition }}</span>
                    </div>
                  </div>
                  <el-empty v-else description="未提取到术语信息" />
                </div>
              </el-tab-pane>
              <el-tab-pane :label="`伏笔（${daForeshadowings.length} 条）`">
                <div class="da-result-scroll">
                  <div v-if="daForeshadowings.length > 0">
                    <div v-for="(fs, idx) in daForeshadowings" :key="idx" class="da-outline-item">
                      <span class="da-outline-idx">{{ idx + 1 }}.</span>
                      <span class="da-outline-title">{{ fs.content }}</span>
                      <div v-if="fs.related_characters?.length" style="margin-left:20px;margin-top:4px">
                        <el-tag v-for="c in fs.related_characters" :key="c" size="small" style="margin:2px">{{ c }}</el-tag>
                      </div>
                    </div>
                  </div>
                  <el-empty v-else description="未提取到伏笔信息" />
                </div>
              </el-tab-pane>
            </el-tabs>
          </template>
          <el-alert v-else-if="deepAnalysisJob.status === 'cancelled'"
            type="info"
            :title="deepAnalysisJob.done_chunks > 0
              ? `分析已取消（已完成 ${deepAnalysisJob.done_chunks}/${deepAnalysisJob.total_chunks} 块），点击「继续分析」可从断点续跑`
              : '分析已取消'"
            :closable="false"
            style="margin-top:12px" />
        </div>
        <div v-else class="da-empty">
          <p>点击「开始深度分析」将对整本参考书进行分块分析，自动提取人物设定、世界观和大纲，并可导入当前项目。</p>
          <p class="da-hint">大型小说（≥100万字）分析可能需要较长时间，任务在后台运行不影响其他操作。</p>
        </div>
      </template>
      <template #footer>
        <el-button @click="showDeepAnalysisDialog = false">关闭</el-button>
        <el-button
          v-if="!deepAnalysisJob || ['failed','cancelled'].includes(deepAnalysisJob.status)"
          type="primary"
          :loading="deepAnalysisStarting"
          @click="doStartDeepAnalysis"
        >{{ deepAnalysisJob?.done_chunks > 0 ? '继续分析' : '开始深度分析' }}</el-button>
        <el-button
          v-if="deepAnalysisJob && ['failed','cancelled','completed'].includes(deepAnalysisJob.status)"
          type="danger"
          plain
          :loading="deepAnalysisResetting"
          @click="doResetDeepAnalysis"
        >重新分析</el-button>
        <el-button
          v-if="deepAnalysisJob?.status === 'pending' || deepAnalysisJob?.status === 'running'"
          type="warning"
          @click="cancelDeepAnalysis"
        >取消分析</el-button>
        <el-button
          v-if="deepAnalysisJob?.status === 'completed'"
          type="success"
          :loading="deepAnalysisImporting"
          @click="importDeepAnalysisResult"
        ><el-icon><Download /></el-icon>导入到项目</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Upload, Search, Loading, Lock, Delete, DocumentCopy, Download } from '@element-plus/icons-vue'
import { referenceApi, projectApi } from '@/api'
import { useReferenceDeepAnalysis } from '@/views/references/useReferenceDeepAnalysis'
import { useReferenceChapters } from '@/views/references/useReferenceChapters'
import { useReferenceFetch } from '@/views/references/useReferenceFetch'

const route = useRoute()
const projectId = route.params.projectId as string

const genres = ['玄幻', '修真', '西幻', '都市', '历史', '科幻', '悬疑', '武侠', '其他']

// ─── reference list ───────────────────────────────────────────────────────────
const loading = ref(false)
const references = ref<any[]>([])
const deleting = ref<string | null>(null)
const settingContinuation = ref<string | null>(null)
const exporting = ref<string | null>(null)
const exportingBatch = ref(false)
const selectedIds = ref<string[]>([])
const localFileInput = ref<HTMLInputElement | null>(null)

// ─── composables ──────────────────────────────────────────────────────────────
const {
  showChaptersDialog,
  chapterRefTitle,
  chapters,
  chaptersLoading,
  chapterSearch,
  chapterSearchPage,
  CHAPTER_PAGE_SIZE,
  selectedChapterIds,
  chapterSelectAll,
  deletingChapters,
  chapterTableRef,
  deletedCount,
  filteredChapters,
  pagedChapters,
  isChapterIndeterminate,
  openChaptersDialog,
  handleChapterSelectionChange,
  toggleSelectAllChapters,
  deleteSingleChapter,
  batchDeleteChapters,
} = useReferenceChapters()

const {
  showFetchDialog,
  fetchStep,
  searchKeyword,
  searchLoading,
  searchResults,
  searchPage,
  searchStreamStatus,
  siteCatalogLoading,
  siteCatalog,
  selectedSearchSites,
  directBookURL,
  urlResolving,
  resolvedSourceMeta,
  totalPages,
  pagedResults,
  bookInfo,
  bookInfoLoading,
  fetchGenre,
  selectedChapterRange,
  importingBookTitle,
  importStartedTotal,
  fetchDialogTitle,
  flatChapters,
  chapterRangeMarks,
  openFetchDialog,
  handleFetchDialogClose,
  resetSearchSiteSelection,
  doSearch,
  selectBook,
  resolveBookURLImport,
  startFetchImport,
} = useReferenceFetch(projectId, fetchRefs)

const {
  showDeepAnalysisDialog,
  deepAnalysisRef,
  deepAnalysisJob,
  deepAnalysisDialogLoading,
  deepAnalysisStarting,
  deepAnalysisImporting,
  deepAnalysisResetting,
  daStatusType,
  daStatusText,
  daChars,
  daWorld,
  daOutline,
  daGlossary,
  daForeshadowings,
  roleTagType,
  openDeepAnalysisDialog,
  doStartDeepAnalysis,
  stopDeepAnalysisPoll,
  cancelDeepAnalysis,
  importDeepAnalysisResult,
  doResetDeepAnalysis,
} = useReferenceDeepAnalysis(fetchRefs)

// ─── reference CRUD ───────────────────────────────────────────────────────────
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

function handleSelectionChange(rows: any[]) {
  selectedIds.value = rows.map(r => r.id)
}

async function setAsContinuationSource(row: any) {
  settingContinuation.value = row.id
  try {
    await projectApi.setContinuationMode(projectId, row.id, 0)
    ElMessage.success(`已将《${row.title}》设为本项目的续写底本，开始章节将自动从章节数量推算`)
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error ?? '设置续写底本失败')
  } finally {
    settingContinuation.value = null
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

// ─── export / local file ──────────────────────────────────────────────────────
async function exportSingle(row: any) {
  exporting.value = row.id
  try {
    const res = await referenceApi.exportSingle(row.id)
    downloadBlob(res.data, `ref_${row.title || row.id}.json`)
  } catch {
    ElMessage.error('导出失败')
  } finally {
    exporting.value = null
  }
}

async function exportBatch() {
  if (selectedIds.value.length === 0) return
  exportingBatch.value = true
  try {
    const res = await referenceApi.exportBatch(projectId, selectedIds.value)
    downloadBlob(res.data, 'references_export.json')
  } catch {
    ElMessage.error('批量导出失败')
  } finally {
    exportingBatch.value = false
  }
}

function downloadBlob(data: Blob, filename: string) {
  const url = URL.createObjectURL(data)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

async function handleLocalFile(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]
  if (!file) return
  try {
    if (file.name.toLowerCase().endsWith('.json')) {
      const text = await file.text()
      const bundle = JSON.parse(text)
      const res = await referenceApi.importLocal(projectId, bundle)
      const data: any = res.data
      ElMessage.success(`导入成功，共导入 ${data.count} 本参考书`)
    } else {
      const formData = new FormData()
      formData.append('file', file)
      formData.append('title', file.name.replace(/\.[^.]+$/, ''))
      formData.append('author', '')
      formData.append('genre', '')
      const res = await referenceApi.upload(projectId, formData)
      if ((res.data as any).data) references.value.push((res.data as any).data)
      ElMessage.success('上传成功')
    }
    await fetchRefs()
  } catch (e: any) {
    ElMessage.error('操作失败：' + (e?.response?.data?.error || e?.message || '未知错误'))
  } finally {
    if (localFileInput.value) localFileInput.value.value = ''
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
.site-settings-card {
  margin-top: 14px;
  padding: 14px;
  border-radius: 10px;
  border: 1px solid var(--nb-card-border, #333);
  background: var(--nb-card-bg, #1e1e1e);
}
.site-settings-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}
.site-settings-title { font-size: 14px; font-weight: 600; color: #e0e0e0; }
.site-settings-meta { margin-top: 4px; font-size: 12px; color: var(--nb-text-secondary); line-height: 1.5; }
.site-settings-actions { display: flex; align-items: center; gap: 10px; }
.site-settings-footer {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-top: 8px;
  font-size: 12px;
  color: var(--nb-text-secondary);
}

/* Results */
.results-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.results-count { font-size: 13px; color: var(--nb-text-secondary); }
.results-list { max-height: 380px; overflow-y: auto; display: flex; flex-direction: column; gap: 8px; }
.results-pagination { display: flex; align-items: center; justify-content: center; gap: 12px; margin-top: 12px; padding-top: 12px; border-top: 1px solid var(--nb-card-border, #333); }
.page-indicator { font-size: 13px; color: var(--nb-text-secondary); min-width: 48px; text-align: center; }
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
.deep-analysis-status { padding: 4px 0; }
.da-header { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; }
.da-title { font-size: 15px; font-weight: 500; color: #e0e0e0; }
.da-hint { font-size: 12px; color: var(--nb-text-secondary); margin-top: 6px; }
.da-empty p { margin: 8px 0; color: var(--nb-text-secondary); font-size: 13px; line-height: 1.6; }
.da-result-tabs { margin-top: 4px; }
.da-result-scroll { max-height: 320px; overflow-y: auto; padding: 4px 2px; }
.da-char-item { padding: 10px 12px; border: 1px solid var(--nb-card-border, #333); border-radius: 8px; margin-bottom: 8px; background: var(--nb-card-bg, #1e1e1e); }
.da-char-header { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; }
.da-char-name { font-size: 14px; color: #e0e0e0; }
.da-char-desc { font-size: 13px; color: var(--nb-text-secondary); margin: 4px 0; line-height: 1.5; }
.da-char-traits { margin-top: 4px; }
.da-world-item { margin-bottom: 14px; }
.da-world-label { font-size: 12px; color: #409eff; font-weight: 600; margin-bottom: 4px; }
.da-world-value { font-size: 13px; color: #e0e0e0; line-height: 1.6; }
.da-world-tags { display: flex; flex-wrap: wrap; gap: 4px; }
.da-outline-item { display: flex; gap: 6px; padding: 6px 0; border-bottom: 1px solid var(--nb-card-border, #2a2a2a); font-size: 13px; line-height: 1.5; }
.da-outline-sub { padding-left: 20px; color: var(--nb-text-secondary); }
.da-outline-idx { color: #409eff; flex-shrink: 0; min-width: 28px; }
.da-outline-title { font-weight: 500; color: #e0e0e0; flex-shrink: 0; }
.da-outline-summary { color: var(--nb-text-secondary); flex: 1; }
.source-url-info { display: flex; align-items: center; gap: 6px; margin-bottom: 16px; font-size: 13px; color: var(--nb-text-secondary); }
.source-url-info a { color: #409eff; word-break: break-all; }
</style>
