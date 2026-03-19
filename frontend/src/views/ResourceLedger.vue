<template>
  <div class="resource-ledger-page">
    <div class="page-header">
      <h2>资源账本</h2>
      <p class="subtitle">追踪小说中道具、货币、技能等资源的流转</p>
      <el-button type="primary" :icon="Plus" @click="openCreateDialog">新增资源</el-button>
    </div>

    <div class="content-layout">
      <!-- Left: resource list -->
      <div class="resource-list-panel">
        <el-input v-model="resourceSearch" placeholder="搜索资源..." :prefix-icon="Search" clearable style="margin-bottom: 12px" />
        <el-scrollbar height="calc(100vh - 280px)">
          <div
            v-for="item in filteredResources"
            :key="item.id"
            :class="['resource-card', { active: selectedId === item.id }]"
            @click="selectResource(item)"
          >
            <div class="card-top">
              <span class="resource-name">{{ item.name }}</span>
              <el-tag :type="categoryTagType(item.category)" size="small">{{ categoryLabel(item.category) }}</el-tag>
            </div>
            <div class="card-meta">
              <span class="quantity">{{ item.quantity }} {{ item.unit }}</span>
              <span class="holder">持有：{{ item.holder || '—' }}</span>
            </div>
            <div v-if="item.description" class="card-desc">{{ item.description }}</div>
            <div class="card-actions">
              <el-button text type="primary" size="small" @click.stop="openEditDialog(item)">编辑</el-button>
              <el-button text type="primary" size="small" @click.stop="openRecordDialog(item)">记录变动</el-button>
              <el-button text type="danger" size="small" @click.stop="deleteResource(item.id)">删除</el-button>
            </div>
          </div>
          <div v-if="!loading && filteredResources.length === 0" style="padding: 40px; text-align: center; color: #666">
            暂无资源
          </div>
        </el-scrollbar>
      </div>

      <!-- Right: change history -->
      <div class="history-panel">
        <template v-if="selectedResource">
          <h3 class="history-title">「{{ selectedResource.name }}」变动历史</h3>
          <div v-if="historyLoading" v-loading="true" style="height: 200px" />
          <el-timeline v-else-if="changes.length > 0" class="change-timeline">
            <el-timeline-item
              v-for="c in changes"
              :key="c.id"
              :timestamp="formatTime(c.created_at)"
              placement="top"
              :type="c.delta > 0 ? 'success' : 'danger'"
            >
              <div class="change-item">
                <span :class="['delta', c.delta > 0 ? 'positive' : 'negative']">
                  {{ c.delta > 0 ? '+' : '' }}{{ c.delta }}
                </span>
                <span class="change-note">{{ c.note || '无备注' }}</span>
              </div>
            </el-timeline-item>
          </el-timeline>
          <el-empty v-else description="暂无变动记录" />
        </template>
        <div v-else class="no-selection">
          <el-empty description="点击左侧资源查看变动历史" />
        </div>
      </div>
    </div>

    <!-- Create / Edit Dialog -->
    <el-dialog
      v-model="formDialogVisible"
      :title="editingResource ? '编辑资源' : '新增资源'"
      width="500px"
      :close-on-click-modal="false"
    >
      <el-form :model="form" :rules="formRules" ref="formRef" label-width="80px">
        <el-form-item label="名称" prop="name">
          <el-input v-model="form.name" placeholder="如：魔法石、金币、剑术等级" />
        </el-form-item>
        <el-form-item label="分类" prop="category">
          <el-select v-model="form.category" style="width: 100%">
            <el-option label="物品" value="item" />
            <el-option label="货币" value="currency" />
            <el-option label="技能" value="skill" />
            <el-option label="武器" value="weapon" />
            <el-option label="其他" value="misc" />
          </el-select>
        </el-form-item>
        <el-row :gutter="12">
          <el-col :span="12">
            <el-form-item label="数量" prop="quantity">
              <el-input-number v-model="form.quantity" :min="0" style="width: 100%" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="单位">
              <el-input v-model="form.unit" placeholder="个/枚/级/点" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="持有者">
          <el-input v-model="form.holder" placeholder="角色名称" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">确定</el-button>
      </template>
    </el-dialog>

    <!-- Record Change Dialog -->
    <el-dialog v-model="recordDialogVisible" title="记录资源变动" width="400px" :close-on-click-modal="false">
      <el-form :model="recordForm" :rules="recordRules" ref="recordFormRef" label-width="80px">
        <el-form-item label="变动量" prop="delta">
          <el-input-number v-model="recordForm.delta" style="width: 100%" placeholder="正数增加，负数减少" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="recordForm.note" type="textarea" :rows="2" placeholder="说明变动原因" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="recordDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="recordSubmitting" @click="submitRecord">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import type { FormInstance, FormRules } from 'element-plus'
import { resourceApi } from '@/api'

interface StoryResource {
  id: string
  project_id: string
  name: string
  category: string
  quantity: number
  unit: string
  holder: string
  description: string
  created_at: string
}

interface ResourceChange {
  id: string
  resource_id: string
  delta: number
  note: string
  chapter_id: string | null
  created_at: string
}

const route = useRoute()
const projectId = computed(() => route.params.projectId as string)

const resources = ref<StoryResource[]>([])
const changes = ref<ResourceChange[]>([])
const loading = ref(false)
const historyLoading = ref(false)
const selectedId = ref<string | null>(null)
const selectedResource = ref<StoryResource | null>(null)
const resourceSearch = ref('')

const formDialogVisible = ref(false)
const editingResource = ref<StoryResource | null>(null)
const submitting = ref(false)
const formRef = ref<FormInstance>()

const recordDialogVisible = ref(false)
const recordingResource = ref<StoryResource | null>(null)
const recordSubmitting = ref(false)
const recordFormRef = ref<FormInstance>()

const form = ref({ name: '', category: 'item', quantity: 0, unit: '个', holder: '', description: '' })
const formRules: FormRules = {
  name: [{ required: true, message: '请输入资源名称', trigger: 'blur' }],
  category: [{ required: true, message: '请选择分类', trigger: 'change' }],
}

const recordForm = ref({ delta: 0, note: '' })
const recordRules: FormRules = {
  delta: [{ required: true, message: '请输入变动量', trigger: 'blur' }],
}

const filteredResources = computed(() =>
  resources.value.filter(r =>
    !resourceSearch.value || r.name.includes(resourceSearch.value) || r.holder?.includes(resourceSearch.value)
  )
)

const categoryLabel = (c: string) => {
  const map: Record<string, string> = { item: '物品', currency: '货币', skill: '技能', weapon: '武器', misc: '其他' }
  return map[c] ?? c
}

const categoryTagType = (c: string): '' | 'success' | 'warning' | 'danger' | 'info' => {
  const map: Record<string, '' | 'success' | 'warning' | 'danger' | 'info'> = {
    item: '',
    currency: 'success',
    skill: 'warning',
    weapon: 'danger',
    misc: 'info',
  }
  return map[c] ?? 'info'
}

function formatTime(iso: string) {
  if (!iso) return ''
  return new Date(iso).toLocaleString('zh-CN', { hour12: false })
}

async function loadResources() {
  loading.value = true
  try {
    const res = await resourceApi.list(projectId.value)
    resources.value = res.data ?? []
  } catch {
    ElMessage.error('加载资源列表失败')
  } finally {
    loading.value = false
  }
}

async function selectResource(item: StoryResource) {
  selectedId.value = item.id
  selectedResource.value = item
  historyLoading.value = true
  try {
    const res = await resourceApi.listChanges(item.id)
    changes.value = (res.data ?? []).sort(
      (a: ResourceChange, b: ResourceChange) =>
        new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
    )
  } catch {
    ElMessage.error('加载变动历史失败')
  } finally {
    historyLoading.value = false
  }
}

function openCreateDialog() {
  editingResource.value = null
  form.value = { name: '', category: 'item', quantity: 0, unit: '个', holder: '', description: '' }
  formDialogVisible.value = true
}

function openEditDialog(item: StoryResource) {
  editingResource.value = item
  form.value = { name: item.name, category: item.category, quantity: item.quantity, unit: item.unit, holder: item.holder, description: item.description }
  formDialogVisible.value = true
}

async function submitForm() {
  if (!formRef.value) return
  await formRef.value.validate(async valid => {
    if (!valid) return
    submitting.value = true
    try {
      if (editingResource.value) {
        await resourceApi.update(editingResource.value.id, form.value)
        ElMessage.success('更新成功')
      } else {
        await resourceApi.create(projectId.value, form.value)
        ElMessage.success('创建成功')
      }
      formDialogVisible.value = false
      await loadResources()
    } catch {
      ElMessage.error('操作失败')
    } finally {
      submitting.value = false
    }
  })
}

function openRecordDialog(item: StoryResource) {
  recordingResource.value = item
  recordForm.value = { delta: 0, note: '' }
  recordDialogVisible.value = true
}

async function submitRecord() {
  if (!recordFormRef.value) return
  await recordFormRef.value.validate(async valid => {
    if (!valid) return
    if (!recordingResource.value) return
    recordSubmitting.value = true
    try {
      await resourceApi.recordChange(recordingResource.value.id, {
        delta: recordForm.value.delta,
        note: recordForm.value.note,
        chapter_id: '',
      })
      ElMessage.success('变动已记录')
      recordDialogVisible.value = false
      await loadResources()
      if (selectedId.value === recordingResource.value.id) {
        await selectResource(recordingResource.value)
      }
    } catch {
      ElMessage.error('记录失败')
    } finally {
      recordSubmitting.value = false
    }
  })
}

async function deleteResource(id: string) {
  await ElMessageBox.confirm('确定删除该资源？此操作不可恢复。', '删除确认', { type: 'warning' })
  try {
    await resourceApi.delete(id)
    ElMessage.success('已删除')
    if (selectedId.value === id) {
      selectedId.value = null
      selectedResource.value = null
      changes.value = []
    }
    resources.value = resources.value.filter(r => r.id !== id)
  } catch {
    ElMessage.error('删除失败')
  }
}

onMounted(loadResources)
</script>

<style scoped>
.resource-ledger-page {
  padding: 24px;
  height: 100%;
  display: flex;
  flex-direction: column;
}

.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 20px;
  flex-wrap: wrap;
  gap: 12px;
}

.page-header h2 {
  font-size: 22px;
  font-weight: 600;
  color: var(--nb-text-primary);
  margin: 0;
}

.subtitle {
  font-size: 13px;
  color: #888;
  margin-top: 4px;
}

.content-layout {
  display: flex;
  gap: 20px;
  flex: 1;
  min-height: 0;
}

.resource-list-panel {
  width: 360px;
  flex-shrink: 0;
}

.history-panel {
  flex: 1;
  background: var(--nb-card-bg);
  border-radius: 8px;
  padding: 20px;
}

.resource-card {
  background: var(--nb-card-bg);
  border: 1px solid var(--nb-card-border);
  border-radius: 8px;
  padding: 12px 14px;
  margin-bottom: 10px;
  cursor: pointer;
  transition: all 0.2s;
}

.resource-card:hover {
  border-color: #409eff44;
  background: rgba(64, 158, 255, 0.06);
}

.resource-card.active {
  border-color: #409eff;
  background: rgba(64, 158, 255, 0.1);
}

.card-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 6px;
}

.resource-name {
  font-weight: 600;
  color: var(--nb-text-primary);
}

.card-meta {
  display: flex;
  gap: 16px;
  font-size: 13px;
  color: #aaa;
  margin-bottom: 4px;
}

.quantity {
  font-weight: 600;
  color: #67c23a;
}

.card-desc {
  font-size: 12px;
  color: #888;
  margin-bottom: 6px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.card-actions {
  display: flex;
  gap: 4px;
}

.history-title {
  font-size: 16px;
  color: var(--nb-text-secondary);
  margin-bottom: 16px;
}

.change-timeline {
  padding-top: 8px;
}

.change-item {
  display: flex;
  gap: 12px;
  align-items: center;
}

.delta {
  font-size: 16px;
  font-weight: 700;
  min-width: 50px;
}

.delta.positive { color: #67c23a; }
.delta.negative { color: #f56c6c; }

.change-note {
  color: #ccc;
  font-size: 13px;
}

.no-selection {
  display: flex;
  justify-content: center;
  align-items: center;
  height: 100%;
}
</style>
