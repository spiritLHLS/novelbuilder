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
      <el-table-column label="限速 (RPM)" prop="rpm_limit" width="100" align="right">
        <template #default="{ row }">
          {{ row.rpm_limit > 0 ? row.rpm_limit : '不限制' }}
        </template>
      </el-table-column>
      <el-table-column label="默认" width="80" align="center">
        <template #default="{ row }">
          <el-icon v-if="row.is_default" class="default-icon"><StarFilled /></el-icon>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="240" fixed="right">
        <template #default="{ row }">
          <el-button size="small" @click="editProfile(row)">编辑</el-button>
          <el-button size="small" type="success" @click="testSavedProfile(row)" :loading="testingId === row.id">测试</el-button>
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
      width="600px"
      destroy-on-close
      @closed="onDialogClosed"
    >
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-width="120px"
        label-position="left"
      >
        <el-form-item label="配置名称" prop="name">
          <el-input v-model="form.name" placeholder="例如：我的 DeepSeek" />
        </el-form-item>
        <el-form-item label="提供商" prop="provider">
          <el-select v-model="form.provider" placeholder="选择提供商" style="width:100%" @change="onProviderChange">
            <el-option label="OpenAI" value="openai" />
            <el-option label="OpenAI 兼容（DeepSeek / 本地等）" value="openai_compatible" />
            <el-option label="Anthropic Claude" value="anthropic" />
            <el-option label="Google Gemini" value="gemini" />
          </el-select>
        </el-form-item>
        <el-form-item label="API Base URL" prop="base_url">
          <el-input
            v-model="form.base_url"
            placeholder="例如：https://api.deepseek.com/v1"
            @blur="autoDetect"
          />
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
          <el-input
            v-model="form.model_name"
            placeholder="例如：deepseek-chat"
            @blur="autoDetect"
          />
        </el-form-item>
        <el-form-item label="Max Tokens">
          <el-input-number v-model="form.max_tokens" :min="512" :max="131072" :step="1024" style="width:100%" />
        </el-form-item>
        <el-form-item label="Temperature">
          <el-slider v-model="form.temperature" :min="0" :max="2" :step="0.05" show-input />
        </el-form-item>
        <el-form-item label="限速 (RPM)">
          <el-input-number v-model="form.rpm_limit" :min="0" :max="1000" :step="5" style="width:100%" />
          <div class="hint" style="margin-top:4px">每分钟最大请求数，0 表示不限制（适用于受并发上限的中转站）</div>
        </el-form-item>
        <el-form-item label="API 调用路径">
          <el-select v-model="form.api_style" style="width:100%">
            <el-option label="/chat/completions" value="/chat/completions" />
            <el-option label="/v1/chat/completions" value="/v1/chat/completions" />
            <el-option label="/messages" value="/messages" />
            <el-option label="/responses" value="/responses" />
            <el-option label="/v1/responses" value="/v1/responses" />
            <el-option label="gemini（Google Gemini REST）" value="gemini" />
          </el-select>
          <div class="hint" style="margin-top:4px">路径直接拼接到 Base URL 后面；填写 Base URL 或选择提供商可自动识别</div>
        </el-form-item>
        <el-form-item label="省略参数">
          <el-checkbox v-model="form.omit_max_tokens">不传入 max_tokens</el-checkbox>
          <el-checkbox v-model="form.omit_temperature" style="margin-left:16px">不传入 temperature</el-checkbox>
          <div class="hint" style="margin-top:4px">部分提供商不接受这些参数，勾选后跳过</div>
        </el-form-item>
        <el-form-item label="设为默认">
          <el-switch v-model="form.is_default" />
          <span class="hint">默认模型用于所有 AI 任务</span>
        </el-form-item>
      </el-form>

      <!-- In-dialog test result -->
      <div v-if="dialogTestResult" style="margin-top:12px">
        <el-alert
          :type="dialogTestResult.ok ? 'success' : 'error'"
          :title="dialogTestResult.ok
            ? `连接成功 · 模型: ${dialogTestResult.model} · 耗时 ${dialogTestResult.duration_ms} ms`
            : `连接失败: ${dialogTestResult.error}`"
          :closable="false"
          show-icon
        />
        <el-collapse v-if="dialogTestResult.raw_body" style="margin-top:8px">
          <el-collapse-item title="查看原始响应">
            <pre class="raw-body-pre">{{ dialogTestResult.raw_body }}</pre>
          </el-collapse-item>
        </el-collapse>
      </div>

      <template #footer>
        <el-button @click="cancelDialog">取消</el-button>
        <el-button
          type="info"
          :loading="dialogTesting"
          @click="testDialogForm"
        >测试连接</el-button>
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
  rpm_limit: number
  omit_max_tokens: boolean
  omit_temperature: boolean
  api_style: string
  is_default: boolean
  has_api_key: boolean
  masked_api_key: string
}

interface TestResult {
  ok: boolean
  model?: string
  duration_ms?: number
  error?: string
  raw_body?: string
}

const profiles = ref<LLMProfile[]>([])
const loading = ref(false)
const showCreateDialog = ref(false)
const submitting = ref(false)
const editingProfile = ref<LLMProfile | null>(null)
const formRef = ref<FormInstance>()

// Test state
const testingId = ref<string | null>(null)
const dialogTesting = ref(false)
const dialogTestResult = ref<TestResult | null>(null)

const defaultForm = () => ({
  name: '',
  provider: 'openai_compatible',
  base_url: 'https://api.openai.com/v1',
  api_key: '',
  model_name: '',
  max_tokens: 8192,
  temperature: 0.7,
  rpm_limit: 0,
  omit_max_tokens: false,
  omit_temperature: false,
  api_style: '/chat/completions',
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

// ── Auto-detection ────────────────────────────────────────────────────────────
// Infer provider, api_style, and omit flags from well-known URL/model patterns.
function autoDetect() {
  const url = form.base_url.toLowerCase()
  const model = form.model_name.toLowerCase()

  // Provider detection from URL
  if (url.includes('generativelanguage.googleapis.com') || url.includes('googleapis.com')) {
    form.provider = 'gemini'
  } else if (url.includes('openai.com')) {
    form.provider = 'openai'
  } else if (url.includes('anthropic.com')) {
    form.provider = 'anthropic'
  } else if (url.includes('deepseek.com')) {
    form.provider = 'openai_compatible'
  } else if (url.includes('localhost') || url.includes('127.0.0.1') || url.includes('ollama')) {
    form.provider = 'openai_compatible'
    form.omit_max_tokens = false
    form.omit_temperature = false
  }

  // Derive api_style from resolved provider + model name
  applyProviderDefaults(form.provider, model)
}

// Apply canonical defaults for a given provider (also called when provider dropdown changes).
function applyProviderDefaults(provider: string, modelHint = '') {
  switch (provider) {
    case 'anthropic':
      form.api_style = '/messages'
      form.omit_max_tokens = false
      form.omit_temperature = false
      if (!form.base_url || form.base_url === 'https://api.openai.com/v1') {
        form.base_url = 'https://api.anthropic.com/v1'
      }
      break
    case 'gemini':
      form.api_style = 'gemini'
      form.omit_max_tokens = true  // Gemini uses generationConfig.maxOutputTokens, not max_tokens
      form.omit_temperature = false
      if (!form.base_url || form.base_url === 'https://api.openai.com/v1') {
        form.base_url = 'https://generativelanguage.googleapis.com/v1beta'
      }
      break
    case 'openai': {
      // OpenAI o-series / codex → Responses API; others → Chat Completions
      const responsesPatterns = [/^o\d/, /^codex/, /gpt-4o-realtime/, /gpt-4o-audio/]
      if (responsesPatterns.some(p => p.test(modelHint))) {
        form.api_style = '/responses'
        form.omit_temperature = true
      } else {
        form.api_style = '/chat/completions'
        form.omit_temperature = false
      }
      form.omit_max_tokens = false
      break
    }
    default: // openai_compatible and fallback
      form.api_style = '/chat/completions'
      form.omit_max_tokens = false
      form.omit_temperature = false
  }
}

function onProviderChange(provider: string) {
  applyProviderDefaults(provider, form.model_name.toLowerCase())
}

// ── Test helpers ──────────────────────────────────────────────────────────────
async function testSavedProfile(profile: LLMProfile) {
  testingId.value = profile.id
  try {
    const res = await llmProfileApi.test({ profile_id: profile.id })
    const result: TestResult = res.data
    if (result.ok) {
      ElMessage({
        type: 'success',
        message: `连接成功 · 模型: ${result.model} · 耗时 ${result.duration_ms} ms`,
        duration: 5000,
      })
      if (result.raw_body) {
        console.info('[LLM Test] raw response:', result.raw_body)
      }
    } else {
      ElMessageBox.alert(
        (result.raw_body
          ? `<p>错误: ${result.error}</p><pre style="margin-top:8px;white-space:pre-wrap;word-break:break-all;font-size:12px;background:#f5f5f5;padding:8px;border-radius:4px">${result.raw_body}</pre>`
          : result.error) ?? '未知错误',
        '连接失败',
        { dangerouslyUseHTMLString: true, type: 'error', confirmButtonText: '关闭' },
      )
    }
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '测试请求失败')
  } finally {
    testingId.value = null
  }
}

async function testDialogForm() {
  // Base URL and model name are needed at minimum
  if (!form.base_url || !form.model_name) {
    ElMessage.warning('请先填写 API Base URL 和模型名称')
    return
  }

  const apiKey = form.api_key || (editingProfile.value ? undefined : '')
  if (!apiKey && !editingProfile.value) {
    ElMessage.warning('请先填写 API Key')
    return
  }

  dialogTesting.value = true
  dialogTestResult.value = null
  try {
    const payload: any = {
      base_url: form.base_url,
      model_name: form.model_name,
      api_style: form.api_style,
      provider: form.provider,
    }
    if (form.api_key) {
      payload.api_key = form.api_key
    } else if (editingProfile.value) {
      // Re-test the saved profile's key (backend loads it from DB)
      payload.profile_id = editingProfile.value.id
    }
    const res = await llmProfileApi.test(payload)
    dialogTestResult.value = res.data
  } catch (e: any) {
    dialogTestResult.value = { ok: false, error: e.response?.data?.error || '请求失败' }
  } finally {
    dialogTesting.value = false
  }
}

// ── CRUD ──────────────────────────────────────────────────────────────────────
function editProfile(profile: LLMProfile) {
  editingProfile.value = profile
  dialogTestResult.value = null
  Object.assign(form, {
    name: profile.name,
    provider: profile.provider,
    base_url: profile.base_url,
    api_key: '',
    model_name: profile.model_name,
    max_tokens: profile.max_tokens,
    temperature: profile.temperature,
    rpm_limit: profile.rpm_limit,
    omit_max_tokens: profile.omit_max_tokens,
    omit_temperature: profile.omit_temperature,
    api_style: normalizeAPIStyle(profile.api_style),
    is_default: profile.is_default,
  })
  showCreateDialog.value = true
}

// Reset all dialog state — called by @closed event (fires after animation on any close path:
// X button, Escape, backdrop click, or explicit cancelDialog). This ensures editingProfile
// is always null before the next open, preventing a create form from submitting as an update.
function onDialogClosed() {
  editingProfile.value = null
  dialogTestResult.value = null
  Object.assign(form, defaultForm())
}

function cancelDialog() {
  showCreateDialog.value = false
  // State reset is handled by @closed handler above
}

// Map legacy api_style codes (stored pre-refactor) to path-based values used by the dropdown.
function normalizeAPIStyle(s: string): string {
  const legacy: Record<string, string> = {
    chat_completions: '/chat/completions',
    messages: '/messages',
    responses: '/responses',
  }
  return legacy[s] ?? s ?? '/chat/completions'
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
    gemini: 'primary',
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
.raw-body-pre {
  margin: 0;
  padding: 8px;
  background: var(--el-fill-color-light, #f5f5f5);
  border-radius: 4px;
  font-size: 12px;
  font-family: monospace;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 200px;
  overflow-y: auto;
}
</style>
