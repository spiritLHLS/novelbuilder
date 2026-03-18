<template>
  <div class="rag-page">
    <div class="page-header">
      <h2>知识库管理 (RAG)</h2>
      <p class="subtitle">管理参考书向量索引，为章节生成提供风格与感官参考</p>
    </div>

    <!-- Status Cards -->
    <el-row :gutter="16" class="status-row">
      <el-col :span="8">
        <el-card class="stat-card">
          <div class="stat-value">{{ totalChunks }}</div>
          <div class="stat-label">向量片段总数</div>
        </el-card>
      </el-col>
      <el-col v-for="col in collections" :key="col.collection" :span="8">
        <el-card class="stat-card">
          <div class="stat-value">{{ col.count }}</div>
          <div class="stat-label">{{ collectionLabel(col.collection) }}</div>
        </el-card>
      </el-col>
    </el-row>

    <!-- Actions -->
    <el-card class="action-card">
      <template #header>
        <div class="card-header">
          <span>索引操作</span>
          <el-tag :type="embeddingConfigured ? 'success' : 'warning'" size="small">
            {{ embeddingConfigured ? 'Embedding 已配置' : '需配置 EMBEDDING_API_KEY' }}
          </el-tag>
        </div>
      </template>

      <div class="action-section">
        <div class="action-desc">
          <h4>重建知识库索引</h4>
          <p>
            删除当前项目所有向量，并根据已分析参考书中缓存的文本样本重新建立索引。<br/>
            每次新增或重新分析参考书后，可点此手动同步。
          </p>
        </div>
        <el-button
          type="primary"
          :loading="rebuilding"
          :icon="Refresh"
          @click="handleRebuild"
        >
          重建索引
        </el-button>
      </div>

      <el-divider />

      <div class="action-section">
        <div class="action-desc">
          <h4>参考书列表</h4>
          <p>以下参考书已完成分析，其文本样本会用于向量检索。</p>
        </div>
      </div>

      <el-table :data="references" style="width: 100%" v-loading="loadingRefs">
        <el-table-column prop="title" label="书名" />
        <el-table-column prop="author" label="作者" width="120" />
        <el-table-column prop="genre" label="类型" width="100" />
        <el-table-column prop="status" label="分析状态" width="120">
          <template #default="{ row }">
            <el-tag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="样本缓存" width="100">
          <template #default="{ row }">
            <el-icon v-if="row.sample_texts" color="#67c23a"><CircleCheck /></el-icon>
            <el-icon v-else color="#909399"><CircleClose /></el-icon>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="120">
          <template #default="{ row }">
            <el-button
              v-if="row.status !== 'completed'"
              size="small"
              type="primary"
              :loading="analyzingId === row.id"
              @click="handleAnalyze(row)"
            >
              分析
            </el-button>
            <el-button
              v-else
              size="small"
              :loading="analyzingId === row.id"
              @click="handleAnalyze(row)"
            >
              重析
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- Embedding Config Tips -->
    <el-card class="tips-card">
      <template #header><span>配置说明</span></template>
      <el-descriptions :column="1" border size="small">
        <el-descriptions-item label="EMBEDDING_API_KEY">
          OpenAI 或兼容 API 的密钥（也读取 OPENAI_API_KEY）
        </el-descriptions-item>
        <el-descriptions-item label="EMBEDDING_BASE_URL">
          嵌入 API 基础地址（默认 <code>https://api.openai.com/v1</code>）
        </el-descriptions-item>
        <el-descriptions-item label="EMBEDDING_MODEL">
          嵌入模型名（默认 <code>text-embedding-3-small</code>，支持 dimensions=1024）
        </el-descriptions-item>
        <el-descriptions-item label="向量维度">
          默认 1024 维（与 <code>vector_store</code> 表 VECTOR(1024) 匹配）
        </el-descriptions-item>
      </el-descriptions>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Refresh, CircleCheck, CircleClose } from '@element-plus/icons-vue'
import { ragApi, referenceApi } from '@/api/index'

const route = useRoute()
const projectId = computed(() => route.params.projectId as string)

const totalChunks = ref(0)
const collections = ref<{ collection: string; count: number }[]>([])
const references = ref<any[]>([])
const loadingRefs = ref(false)
const rebuilding = ref(false)
const analyzingId = ref<string | null>(null)
const embeddingConfigured = ref(true) // optimistic; sidecar /embed returns 503 if not

function collectionLabel(col: string): string {
  const map: Record<string, string> = {
    style_samples: '风格样本',
    sensory_samples: '感官片段',
  }
  return map[col] ?? col
}

function statusLabel(status: string): string {
  const map: Record<string, string> = {
    processing: '处理中',
    completed: '已完成',
    failed: '失败',
  }
  return map[status] ?? status
}

function statusType(status: string): 'success' | 'warning' | 'danger' | 'info' {
  return status === 'completed' ? 'success' : status === 'failed' ? 'danger' : 'warning'
}

async function loadStatus() {
  try {
    const res = await ragApi.getStatus(projectId.value)
    totalChunks.value = res.data.total_chunks ?? 0
    collections.value = res.data.collections ?? []
  } catch {
    // RAG not yet indexed — not an error
  }
}

async function loadReferences() {
  loadingRefs.value = true
  try {
    const res = await referenceApi.list(projectId.value)
    references.value = res.data.data ?? []
  } finally {
    loadingRefs.value = false
  }
}

async function handleRebuild() {
  rebuilding.value = true
  try {
    const res = await ragApi.rebuild(projectId.value)
    const count = res.data.rebuilt_sources ?? 0
    ElMessage.success(`索引重建完成，已同步 ${count} 条参考书`)
    await loadStatus()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error ?? '重建失败，请检查 Embedding API 配置')
    embeddingConfigured.value = false
  } finally {
    rebuilding.value = false
  }
}

async function handleAnalyze(ref: any) {
  analyzingId.value = ref.id
  try {
    const res = await referenceApi.analyze(ref.id)
    const d = res.data
    ElMessage.success(
      `分析完成：风格样本 ${d.style_samples ?? 0} 条，感官片段 ${d.sensory_samples ?? 0} 条，已自动入库`,
    )
    await Promise.all([loadReferences(), loadStatus()])
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error ?? '分析失败')
  } finally {
    analyzingId.value = null
  }
}

onMounted(async () => {
  await Promise.all([loadStatus(), loadReferences()])
})
</script>

<style scoped>
.rag-page {
  padding: 24px;
  max-width: 1000px;
}

.page-header {
  margin-bottom: 24px;
}

.page-header h2 {
  font-size: 22px;
  font-weight: 600;
  color: #e0e0e0;
}

.subtitle {
  color: #888;
  margin-top: 4px;
  font-size: 13px;
}

.status-row {
  margin-bottom: 20px;
}

.stat-card {
  text-align: center;
  background: #1e1e30;
  border-color: #2a2a3e;
}

.stat-value {
  font-size: 32px;
  font-weight: 700;
  color: #409eff;
  line-height: 1.2;
}

.stat-label {
  color: #888;
  font-size: 13px;
  margin-top: 4px;
}

.action-card {
  margin-bottom: 20px;
  background: #1a1a2e;
  border-color: #2a2a3e;
}

.card-header {
  display: flex;
  align-items: center;
  gap: 12px;
}

.action-section {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  padding: 8px 0;
}

.action-desc h4 {
  color: #e0e0e0;
  margin-bottom: 6px;
}

.action-desc p {
  color: #888;
  font-size: 13px;
  line-height: 1.6;
}

.tips-card {
  background: #1a1a2e;
  border-color: #2a2a3e;
}

code {
  background: #0f0f1a;
  padding: 1px 5px;
  border-radius: 3px;
  font-size: 12px;
  color: #67c23a;
}
</style>
