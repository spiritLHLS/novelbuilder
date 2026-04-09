<template>
  <div class="fanqie-upload-page">
    <h2>番茄小说网自动上传</h2>
    <p class="page-desc">配置番茄小说作者账号，将生成的章节自动上传到番茄小说网</p>

    <!-- 账号配置 -->
    <el-card class="config-card">
      <template #header>
        <div class="card-header">
          <span>账号配置</span>
          <el-tag v-if="account.configured" :type="account.status === 'active' ? 'success' : 'warning'">
            {{ account.status === 'active' ? '已连接' : '待验证' }}
          </el-tag>
          <el-tag v-else type="info">未配置</el-tag>
        </div>
      </template>

      <el-form label-width="100px">
        <el-form-item label="Cookie">
          <el-input
            v-model="cookieInput"
            type="textarea"
            :rows="3"
            placeholder="在番茄小说网作者后台登录后，按 F12 打开控制台，输入 document.cookie 复制结果粘贴到此处"
          />
        </el-form-item>
        <el-form-item label="作品 ID">
          <div style="display: flex; gap: 8px; width: 100%">
            <el-input v-model="bookIdInput" placeholder="番茄小说上的作品 ID" style="flex: 1" />
            <el-button @click="fetchBooks" :loading="loadingBooks">获取作品列表</el-button>
          </div>
        </el-form-item>
        <el-form-item v-if="books.length > 0" label="选择作品">
          <el-select v-model="selectedBook" placeholder="选择目标作品" style="width: 100%"
                     @change="onBookSelect">
            <el-option
              v-for="b in books"
              :key="b.book_id"
              :label="`${b.title} (${b.book_id})`"
              :value="b.book_id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="作品名称">
          <el-input v-model="bookTitleInput" placeholder="可选，便于识别" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="saveConfig" :loading="saving">保存配置</el-button>
          <el-button @click="validateCookies" :loading="validating">验证 Cookie</el-button>
          <el-button @click="showLoginScreenshot" :loading="loadingScreenshot">查看登录页</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 登录页截图弹窗 -->
    <el-dialog v-model="screenshotVisible" title="番茄小说登录页" width="800px">
      <div v-if="screenshotData" style="text-align: center">
        <img :src="screenshotData" style="max-width: 100%; border: 1px solid #ddd; border-radius: 4px" />
        <p style="margin-top: 12px; color: #666">
          请使用番茄小说 / 今日头条 APP 扫描二维码登录，然后从浏览器复制 Cookie
        </p>
      </div>
      <div v-else style="text-align: center; padding: 40px">
        <el-icon class="is-loading" :size="32"><Loading /></el-icon>
        <p>正在加载登录页...</p>
      </div>
    </el-dialog>

    <!-- 章节上传 -->
    <el-card class="upload-card" style="margin-top: 16px">
      <template #header>
        <div class="card-header">
          <span>章节上传</span>
          <div>
            <el-button
              type="primary"
              size="small"
              :disabled="selectedChapters.length === 0 || !account.configured"
              :loading="uploading"
              @click="batchUpload"
            >
              批量上传 ({{ selectedChapters.length }})
            </el-button>
            <el-button size="small" @click="refreshUploads">刷新状态</el-button>
          </div>
        </div>
      </template>

      <el-table
        :data="chaptersWithStatus"
        @selection-change="onSelectionChange"
        v-loading="loadingChapters"
        style="width: 100%"
      >
        <el-table-column type="selection" width="45" />
        <el-table-column prop="chapter_num" label="章节" width="70" />
        <el-table-column prop="title" label="标题" min-width="200" />
        <el-table-column prop="word_count" label="字数" width="80" />
        <el-table-column label="上传状态" width="120">
          <template #default="{ row }">
            <el-tag v-if="row.upload_status === 'success'" type="success" size="small">已上传</el-tag>
            <el-tag v-else-if="row.upload_status === 'uploading'" type="warning" size="small">上传中</el-tag>
            <el-tag v-else-if="row.upload_status === 'failed'" type="danger" size="small">失败</el-tag>
            <el-tag v-else type="info" size="small">未上传</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="错误信息" min-width="150">
          <template #default="{ row }">
            <span v-if="row.error_message" class="error-text">{{ row.error_message }}</span>
          </template>
        </el-table-column>
        <el-table-column label="上传时间" width="160">
          <template #default="{ row }">
            {{ row.uploaded_at ? new Date(row.uploaded_at).toLocaleString('zh-CN') : '-' }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <el-button
              size="small"
              type="primary"
              link
              :disabled="!account.configured"
              :loading="row._uploading"
              @click="uploadSingle(row)"
            >
              上传
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 上传结果截图弹窗 -->
    <el-dialog v-model="resultVisible" title="上传结果" width="800px">
      <div v-if="resultScreenshot" style="text-align: center">
        <img :src="resultScreenshot" style="max-width: 100%; border: 1px solid #ddd; border-radius: 4px" />
      </div>
      <p v-if="resultMessage" style="margin-top: 12px; text-align: center">{{ resultMessage }}</p>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Loading } from '@element-plus/icons-vue'
import { fanqieApi, chapterApi } from '@/api'

const route = useRoute()
const projectId = computed(() => route.params.projectId as string)

// ── State ──
const account = ref<{
  configured: boolean
  book_id: string
  book_title: string
  status: string
}>({ configured: false, book_id: '', book_title: '', status: 'unconfigured' })

const cookieInput = ref('')
const bookIdInput = ref('')
const bookTitleInput = ref('')
const books = ref<{ title: string; book_id: string }[]>([])
const selectedBook = ref('')

const chapters = ref<any[]>([])
const uploads = ref<any[]>([])
const selectedChapters = ref<any[]>([])

const saving = ref(false)
const validating = ref(false)
const loadingBooks = ref(false)
const loadingChapters = ref(false)
const loadingScreenshot = ref(false)
const uploading = ref(false)

const screenshotVisible = ref(false)
const screenshotData = ref('')

const resultVisible = ref(false)
const resultScreenshot = ref('')
const resultMessage = ref('')

// ── Computed ──
const chaptersWithStatus = computed(() => {
  const uploadMap = new Map<string, any>()
  for (const u of uploads.value) {
    uploadMap.set(u.chapter_id, u)
  }
  return chapters.value.map(ch => {
    const u = uploadMap.get(ch.id)
    return {
      ...ch,
      upload_status: u?.status || 'pending',
      error_message: u?.error_message || '',
      uploaded_at: u?.uploaded_at || null,
      fanqie_chapter_id: u?.fanqie_chapter_id || '',
      _uploading: false,
    }
  })
})

// ── Lifecycle ──
onMounted(async () => {
  await Promise.all([loadAccount(), loadChapters(), loadUploads()])
})

// ── Data Loading ──
async function loadAccount() {
  try {
    const { data } = await fanqieApi.getAccount(projectId.value)
    account.value = data
    if (data.book_id) bookIdInput.value = data.book_id
    if (data.book_title) bookTitleInput.value = data.book_title
  } catch { /* first time, no account */ }
}

async function loadChapters() {
  loadingChapters.value = true
  try {
    const { data } = await chapterApi.list(projectId.value)
    chapters.value = (data.data || []).sort((a: any, b: any) => a.chapter_num - b.chapter_num)
  } catch (e: any) {
    ElMessage.error('加载章节列表失败')
  } finally {
    loadingChapters.value = false
  }
}

async function loadUploads() {
  try {
    const { data } = await fanqieApi.listUploads(projectId.value)
    uploads.value = data.data || []
  } catch { /* no uploads yet */ }
}

// ── Account Config ──
async function saveConfig() {
  if (!cookieInput.value.trim()) {
    ElMessage.warning('请填写 Cookie')
    return
  }
  saving.value = true
  try {
    await fanqieApi.configure(projectId.value, {
      cookies: cookieInput.value.trim(),
      book_id: bookIdInput.value.trim(),
      book_title: bookTitleInput.value.trim(),
    })
    ElMessage.success('配置已保存')
    await loadAccount()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '保存失败')
  } finally {
    saving.value = false
  }
}

async function validateCookies() {
  validating.value = true
  try {
    const { data } = await fanqieApi.validate(projectId.value)
    if (data.valid) {
      ElMessage.success('Cookie 验证通过')
    } else {
      ElMessage.warning(`Cookie 无效: ${data.reason || '未知原因'}`)
    }
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '验证失败')
  } finally {
    validating.value = false
  }
}

async function fetchBooks() {
  loadingBooks.value = true
  try {
    const { data } = await fanqieApi.listBooks(projectId.value)
    books.value = data.books || []
    if (books.value.length === 0) {
      ElMessage.info('未找到作品，请确认 Cookie 是否有效')
    }
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '获取作品列表失败')
  } finally {
    loadingBooks.value = false
  }
}

function onBookSelect(bookId: string) {
  bookIdInput.value = bookId
  const book = books.value.find(b => b.book_id === bookId)
  if (book) bookTitleInput.value = book.title
}

async function showLoginScreenshot() {
  screenshotVisible.value = true
  screenshotData.value = ''
  loadingScreenshot.value = true
  try {
    const { data } = await fanqieApi.loginScreenshot(projectId.value)
    screenshotData.value = data.screenshot || ''
  } catch (e: any) {
    ElMessage.error('获取登录页截图失败')
    screenshotVisible.value = false
  } finally {
    loadingScreenshot.value = false
  }
}

// ── Upload ──
function onSelectionChange(rows: any[]) {
  selectedChapters.value = rows
}

async function uploadSingle(row: any) {
  row._uploading = true
  try {
    const { data } = await fanqieApi.uploadChapter(projectId.value, row.id)
    if (data.screenshot) {
      resultScreenshot.value = data.screenshot
      resultMessage.value = data.message || ''
      resultVisible.value = true
    }
    ElMessage.success(`章节 "${row.title}" 上传${data.status === 'success' ? '成功' : '已执行'}`)
    await loadUploads()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '上传失败')
  } finally {
    row._uploading = false
  }
}

async function batchUpload() {
  uploading.value = true
  const ids = selectedChapters.value.map((c: any) => c.id)
  try {
    const { data } = await fanqieApi.batchUpload(projectId.value, ids)
    ElMessage.success(`批量上传完成: ${data.success}/${data.total} 成功`)
    await loadUploads()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '批量上传失败')
  } finally {
    uploading.value = false
  }
}

async function refreshUploads() {
  await loadUploads()
  ElMessage.success('状态已刷新')
}
</script>

<style scoped>
.fanqie-upload-page {
  padding: 24px;
  max-width: 1200px;
}
.fanqie-upload-page h2 {
  margin-bottom: 4px;
  font-size: 20px;
}
.page-desc {
  color: #666;
  margin-bottom: 20px;
  font-size: 13px;
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.config-card :deep(.el-card__body) {
  padding: 20px;
}
.error-text {
  color: #f56c6c;
  font-size: 12px;
}
</style>
