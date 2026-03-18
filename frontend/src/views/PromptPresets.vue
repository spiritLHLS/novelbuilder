<template>
  <div class="prompt-presets-page">
    <div class="page-header">
      <h2>提示词预设</h2>
      <p class="subtitle">管理可复用的提示词模板，支持变量插值</p>
      <el-button type="primary" :icon="Plus" @click="openCreateDialog">新建预设</el-button>
    </div>

    <!-- Tabs: Global / Project-specific -->
    <el-tabs v-model="activeTab" @tab-change="handleTabChange">
      <el-tab-pane label="全局预设" name="global" />
      <el-tab-pane v-if="projectId" :label="`项目预设`" name="project" />
    </el-tabs>

    <!-- Search & category filter -->
    <div class="toolbar">
      <el-input v-model="search" placeholder="搜索预设..." :prefix-icon="Search" clearable style="width: 240px" />
      <el-select v-model="filterCategory" placeholder="全部分类" clearable style="width: 160px">
        <el-option label="全部" value="" />
        <el-option label="系统提示" value="system" />
        <el-option label="世界观" value="worldbuilding" />
        <el-option label="角色" value="character" />
        <el-option label="章节生成" value="chapter" />
        <el-option label="其他" value="other" />
      </el-select>
    </div>

    <!-- Card grid -->
    <div v-loading="loading" class="preset-grid">
      <el-card
        v-for="preset in filteredPresets"
        :key="preset.id"
        class="preset-card"
        shadow="hover"
      >
        <div class="card-header">
          <div class="card-title">
            <span class="name">{{ preset.name }}</span>
            <el-tag v-if="preset.is_global" type="success" size="small">全局</el-tag>
            <el-tag v-else type="info" size="small">项目</el-tag>
          </div>
          <el-tag size="small">{{ categoryLabel(preset.category) }}</el-tag>
        </div>
        <div class="card-content">{{ truncate(preset.content, 120) }}</div>
        <div v-if="preset.variables && Object.keys(preset.variables).length > 0" class="card-vars">
          <span class="vars-label">变量：</span>
          <el-tag
            v-for="(_, k) in preset.variables"
            :key="k"
            size="small"
            type="warning"
            style="margin-right: 4px; font-family: monospace"
          >{{ k }}</el-tag>
        </div>
        <div class="card-footer">
          <div class="sort-order">排序：{{ preset.sort_order ?? 0 }}</div>
          <div class="card-actions">
            <el-button text type="primary" size="small" @click="openEditDialog(preset)">编辑</el-button>
            <el-button text type="danger" size="small" @click="deletePreset(preset.id)">删除</el-button>
          </div>
        </div>
      </el-card>
    </div>

    <div v-if="!loading && filteredPresets.length === 0" class="empty-state">
      <el-empty description="暂无提示词预设" />
    </div>

    <!-- Create / Edit Dialog -->
    <el-dialog
      v-model="dialogVisible"
      :title="editingPreset ? '编辑预设' : '新建预设'"
      width="640px"
      :close-on-click-modal="false"
    >
      <el-form :model="form" :rules="rules" ref="formRef" label-width="90px">
        <el-form-item label="名称" prop="name">
          <el-input v-model="form.name" placeholder="预设名称" />
        </el-form-item>
        <el-form-item label="分类" prop="category">
          <el-select v-model="form.category" style="width: 100%">
            <el-option label="系统提示" value="system" />
            <el-option label="世界观" value="worldbuilding" />
            <el-option label="角色" value="character" />
            <el-option label="章节生成" value="chapter" />
            <el-option label="其他" value="other" />
          </el-select>
        </el-form-item>
        <el-form-item label="内容" prop="content">
          <el-input
            v-model="form.content"
            type="textarea"
            :rows="8"
            placeholder="提示词内容，可使用 {{变量名}} 语法插入变量"
          />
        </el-form-item>
        <el-form-item label="变量 (JSON)">
          <el-input
            v-model="variablesJson"
            type="textarea"
            :rows="3"
            placeholder='{"变量名": "默认值"}'
            @blur="validateVariablesJson"
          />
          <div v-if="variablesError" class="json-error">{{ variablesError }}</div>
        </el-form-item>
        <el-row :gutter="12">
          <el-col :span="12">
            <el-form-item label="全局可用">
              <el-switch v-model="form.is_global" />
            </el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="排序权重">
              <el-input-number v-model="form.sort_order" :min="0" :max="999" style="width: 100%" />
            </el-form-item>
          </el-col>
        </el-row>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import type { FormInstance, FormRules } from 'element-plus'
import { promptPresetApi } from '@/api'

interface PromptPreset {
  id: string
  project_id: string | null
  name: string
  content: string
  category: string
  variables: Record<string, string> | null
  is_global: boolean
  sort_order: number
  created_at: string
}

const route = useRoute()
const projectId = computed(() => route.params.projectId as string | undefined)

const presets = ref<PromptPreset[]>([])
const loading = ref(false)
const activeTab = ref('global')
const search = ref('')
const filterCategory = ref('')

const dialogVisible = ref(false)
const editingPreset = ref<PromptPreset | null>(null)
const submitting = ref(false)
const formRef = ref<FormInstance>()
const variablesJson = ref('{}')
const variablesError = ref('')

const form = ref({
  name: '',
  content: '',
  category: 'system',
  is_global: true,
  sort_order: 0,
})

const rules: FormRules = {
  name: [{ required: true, message: '请输入预设名称', trigger: 'blur' }],
  content: [{ required: true, message: '请输入提示词内容', trigger: 'blur' }],
  category: [{ required: true, message: '请选择分类', trigger: 'change' }],
}

const filteredPresets = computed(() =>
  presets.value.filter(p => {
    const matchSearch = !search.value || p.name.includes(search.value) || p.content.includes(search.value)
    const matchCat = !filterCategory.value || p.category === filterCategory.value
    return matchSearch && matchCat
  })
)

const categoryLabel = (c: string) => {
  const map: Record<string, string> = {
    system: '系统提示',
    worldbuilding: '世界观',
    character: '角色',
    chapter: '章节生成',
    other: '其他',
  }
  return map[c] ?? c
}

function truncate(text: string, len: number) {
  return text.length > len ? text.slice(0, len) + '...' : text
}

function validateVariablesJson() {
  try {
    JSON.parse(variablesJson.value || '{}')
    variablesError.value = ''
  } catch {
    variablesError.value = 'JSON 格式错误'
  }
}

async function loadPresets() {
  loading.value = true
  try {
    let res
    if (activeTab.value === 'project' && projectId.value) {
      res = await promptPresetApi.listForProject(projectId.value)
    } else {
      res = await promptPresetApi.listGlobal()
    }
    presets.value = res.data ?? []
  } catch {
    ElMessage.error('加载预设列表失败')
  } finally {
    loading.value = false
  }
}

function handleTabChange() {
  loadPresets()
}

function openCreateDialog() {
  editingPreset.value = null
  form.value = { name: '', content: '', category: 'system', is_global: activeTab.value === 'global', sort_order: 0 }
  variablesJson.value = '{}'
  variablesError.value = ''
  dialogVisible.value = true
}

function openEditDialog(preset: PromptPreset) {
  editingPreset.value = preset
  form.value = {
    name: preset.name,
    content: preset.content,
    category: preset.category,
    is_global: preset.is_global,
    sort_order: preset.sort_order ?? 0,
  }
  variablesJson.value = JSON.stringify(preset.variables ?? {}, null, 2)
  variablesError.value = ''
  dialogVisible.value = true
}

async function submitForm() {
  if (!formRef.value) return
  validateVariablesJson()
  if (variablesError.value) return

  await formRef.value.validate(async valid => {
    if (!valid) return
    submitting.value = true
    try {
      let variables: Record<string, string> = {}
      try { variables = JSON.parse(variablesJson.value || '{}') } catch { /* ignore */ }

      const payload = { ...form.value, variables }

      if (editingPreset.value) {
        await promptPresetApi.update(editingPreset.value.id, payload)
        ElMessage.success('更新成功')
      } else {
        const pid = activeTab.value === 'project' ? projectId.value : undefined
        await promptPresetApi.create(payload, pid)
        ElMessage.success('创建成功')
      }
      dialogVisible.value = false
      await loadPresets()
    } catch {
      ElMessage.error('操作失败')
    } finally {
      submitting.value = false
    }
  })
}

async function deletePreset(id: string) {
  await ElMessageBox.confirm('确定删除该预设？', '删除确认', { type: 'warning' })
  try {
    await promptPresetApi.delete(id)
    ElMessage.success('已删除')
    presets.value = presets.value.filter(p => p.id !== id)
  } catch {
    ElMessage.error('删除失败')
  }
}

watch(() => route.params.projectId, () => {
  if (activeTab.value === 'project' && !projectId.value) {
    activeTab.value = 'global'
  }
  loadPresets()
})

onMounted(loadPresets)
</script>

<style scoped>
.prompt-presets-page {
  padding: 24px;
  max-width: 1200px;
}

.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 8px;
  flex-wrap: wrap;
  gap: 12px;
}

.page-header h2 {
  font-size: 22px;
  font-weight: 600;
  color: #e0e0e0;
  margin: 0;
}

.subtitle {
  font-size: 13px;
  color: #888;
  margin-top: 4px;
}

.toolbar {
  display: flex;
  gap: 12px;
  align-items: center;
  margin-bottom: 20px;
}

.preset-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 16px;
}

.preset-card {
  background: rgba(255, 255, 255, 0.04) !important;
  border: 1px solid rgba(255, 255, 255, 0.08) !important;
  transition: border-color 0.2s;
}

.preset-card:hover {
  border-color: #409eff44 !important;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 10px;
}

.card-title {
  display: flex;
  gap: 6px;
  align-items: center;
}

.name {
  font-weight: 600;
  color: #e0e0e0;
  font-size: 15px;
}

.card-content {
  font-size: 12px;
  color: #999;
  line-height: 1.6;
  margin-bottom: 10px;
  white-space: pre-wrap;
  word-break: break-word;
}

.card-vars {
  margin-bottom: 10px;
  font-size: 12px;
}

.vars-label {
  color: #888;
  margin-right: 4px;
}

.card-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-top: 1px solid rgba(255, 255, 255, 0.06);
  padding-top: 8px;
}

.sort-order {
  font-size: 12px;
  color: #666;
}

.card-actions {
  display: flex;
  gap: 4px;
}

.json-error {
  color: #f56c6c;
  font-size: 12px;
  margin-top: 4px;
}

.empty-state {
  margin-top: 40px;
  text-align: center;
}
</style>
