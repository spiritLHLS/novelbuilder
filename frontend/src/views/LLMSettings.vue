<template>
  <div class="llm-settings">
    <div class="page-header">
      <h1>AI 模型配置</h1>
      <el-button type="primary" @click="showCreateDialog = true">
        <el-icon><Plus /></el-icon>
        添加模型配置
      </el-button>
    </div>

    <el-alert
      type="info"
      :closable="false"
      class="info-alert"
    >
      <template #title>
        在此配置您的 AI 模型。设置为默认的模型将用于所有 AI 生成任务（支持只使用一个 API Key 完成全部功能）。
        API Key 加密存储，列表中仅展示脱敏信息。
      </template>
    </el-alert>

    <el-table
      :data="profiles"
      v-loading="loading"
      class="profiles-table"
    >
      <el-table-column label="名称" prop="name" width="160" />
      <el-table-column label="提供商" prop="provider" width="130">
        <template #default="{ row }">
          <el-tag size="small" :type="providerTagType(row.provider)">{{ row.provider }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="API Base URL" prop="base_url" show-overflow-tooltip />
      <el-table-column label="模型名" prop="model_name" width="200" show-overflow-tooltip />
      <el-table-column label="API Key" width="160">
        <template #default="{ row }">
          <span v-if="row.has_api_key" class="masked-key">{{ row.masked_api_key }}</span>
          <el-tag v-else size="small" type="danger">未配置</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="Max Tokens" prop="max_tokens" width="110" align="right" />
      <el-table-column label="默认" width="80" align="center">
        <template #default="{ row }">
          <el-icon v-if="row.is_default" class="default-icon"><StarFilled /></el-icon>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="200" fixed="right">
        <template #default="{ row }">
          <el-button size="small" @click="editProfile(row)">编辑</el-button>
          <el-button
            size="small"
            type="warning"
            v-if="!row.is_default"
            @click="setDefault(row)"
          >设为默认</el-button>
          <el-button
            size="small"
            type="danger"
            @click="deleteProfile(row)"
          >删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- Create / Edit Dialog -->
    <el-dialog
      v-model="showCreateDialog"
      :title="editingProfile ? '编辑模型配置' : '添加模型配置'"
      width="560px"
      destroy-on-close
    >
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-width="110px"
        label-position="left"
      >
        <el-form-item label="配置名称" prop="name">
          <el-input v-model="form.name" placeholder="例如：我的 DeepSeek" />
        </el-form-item>
        <el-form-item label="提供商" prop="provider">
          <el-select v-model="form.provider" placeholder="选择提供商" style="width:100%">
            <el-option label="OpenAI" value="openai" />
            <el-option label="OpenAI 兼容（DeepSeek / 本地等）" value="openai_compatible" />
            <el-option label="Anthropic Claude" value="anthropic" />
          </el-select>
        </el-form-item>
        <el-form-item label="API Base URL" prop="base_url">
          <el-input v-model="form.base_url" placeholder="例如：https://api.deepseek.com/v1" />
        </el-form-item>
        <el-form-item label="API Key" :prop="editingProfile ? '' : 'api_key'">
          <el-input
            v-model="form.api_key"
            type="password"
            show-password
            :placeholder="editingProfile ? '留空保持不变' : '请输入 API Key'"
          />
        </el-form-item>
        <el-form-item label="模型名称" prop="model_name">
          <el-input v-model="form.model_name" placeholder="例如：deepseek-chat" />
        </el-form-item>
        <el-form-item label="Max Tokens">
          <el-input-number v-model="form.max_tokens" :min="512" :max="131072" :step="1024" style="width:100%" />
        </el-form-item>
        <el-form-item label="Temperature">
          <el-slider v-model="form.temperature" :min="0" :max="2" :step="0.05" show-input />
        </el-form-item>
        <el-form-item label="设为默认">
          <el-switch v-model="form.is_default" />
          <span class="hint">默认模型用于所有 AI 任务</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="cancelDialog">取消</el-button>
        <el-button type="primary" @click="submitForm" :loading="submitting">
          {{ editingProfile ? '保存' : '创建' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, StarFilled } from '@element-plus/icons-vue'
import { llmProfileApi } from '@/api'
import type { FormInstance, FormRules } from 'element-plus'

interface LLMProfile {
  id: string
  name: string
  provider: string
  base_url: string
  model_name: string
  max_tokens: number
  temperature: number
  is_default: boolean
  has_api_key: boolean
  masked_api_key: string
}

const profiles = ref<LLMProfile[]>([])
const loading = ref(false)
const showCreateDialog = ref(false)
const submitting = ref(false)
const editingProfile = ref<LLMProfile | null>(null)
const formRef = ref<FormInstance>()

const defaultForm = () => ({
  name: '',
  provider: 'openai_compatible',
  base_url: 'https://api.openai.com/v1',
  api_key: '',
  model_name: '',
  max_tokens: 8192,
  temperature: 0.7,
  is_default: false,
})

const form = reactive(defaultForm())

const rules: FormRules = {
  name: [{ required: true, message: '请输入配置名称', trigger: 'blur' }],
  provider: [{ required: true, message: '请选择提供商', trigger: 'change' }],
  base_url: [{ required: true, message: '请输入 API Base URL', trigger: 'blur' }],
  api_key: [{ required: true, message: '请输入 API Key', trigger: 'blur' }],
  model_name: [{ required: true, message: '请输入模型名称', trigger: 'blur' }],
}

onMounted(fetchProfiles)

async function fetchProfiles() {
  loading.value = true
  try {
    const res = await llmProfileApi.list()
    profiles.value = res.data.data || []
  } catch {
    ElMessage.error('获取模型配置失败')
  } finally {
    loading.value = false
  }
}

function editProfile(profile: LLMProfile) {
  editingProfile.value = profile
  Object.assign(form, {
    name: profile.name,
    provider: profile.provider,
    base_url: profile.base_url,
    api_key: '',
    model_name: profile.model_name,
    max_tokens: profile.max_tokens,
    temperature: profile.temperature,
    is_default: profile.is_default,
  })
  showCreateDialog.value = true
}

function cancelDialog() {
  showCreateDialog.value = false
  editingProfile.value = null
  Object.assign(form, defaultForm())
}

async function submitForm() {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  submitting.value = true
  try {
    if (editingProfile.value) {
      const payload: any = { ...form }
      if (!payload.api_key) delete payload.api_key
      await llmProfileApi.update(editingProfile.value.id, payload)
      ElMessage.success('配置已更新')
    } else {
      await llmProfileApi.create(form)
      ElMessage.success('配置已创建')
    }
    cancelDialog()
    await fetchProfiles()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '操作失败')
  } finally {
    submitting.value = false
  }
}

async function setDefault(profile: LLMProfile) {
  try {
    await llmProfileApi.setDefault(profile.id)
    ElMessage.success(`已将"${profile.name}"设为默认模型`)
    await fetchProfiles()
  } catch {
    ElMessage.error('设置默认失败')
  }
}

async function deleteProfile(profile: LLMProfile) {
  await ElMessageBox.confirm(
    `确认删除配置"${profile.name}"？`,
    '删除确认',
    { type: 'warning' }
  )
  try {
    await llmProfileApi.delete(profile.id)
    ElMessage.success('已删除')
    await fetchProfiles()
  } catch {
    ElMessage.error('删除失败')
  }
}

function providerTagType(provider: string) {
  const map: Record<string, string> = {
    openai: 'success',
    openai_compatible: '',
    anthropic: 'warning',
  }
  return (map[provider] || 'info') as any
}
</script>

<style scoped>
.llm-settings { max-width: 1100px; margin: 0 auto; }
.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
}
.page-header h1 { font-size: 24px; color: var(--nb-text-primary); }
.info-alert { margin-bottom: 20px; }
.profiles-table { background: transparent; }
.masked-key { font-family: monospace; color: #8a8a9a; font-size: 12px; }
.default-icon { color: #f5a623; font-size: 18px; }
.hint { margin-left: 10px; color: #888; font-size: 12px; }
</style>
