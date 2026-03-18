<template>
  <div class="system-settings">
    <div class="page-header">
      <h1>系统设置</h1>
      <el-button type="primary" @click="openAddDialog">
        <el-icon><Plus /></el-icon>
        添加设置项
      </el-button>
    </div>

    <el-alert type="info" :closable="false" class="info-alert">
      <template #title>
        在此管理应用级别的设置。所有配置均存储在数据库中，无需修改配置文件或重启服务。
        加密密钥由系统自动生成，不在此处显示。
      </template>
    </el-alert>

    <!-- Predefined settings cards -->
    <el-row :gutter="16" class="preset-cards">
      <el-col :span="8">
        <el-card class="preset-card">
          <template #header><span>质量检测阈值</span></template>
          <el-form label-width="160px" label-position="left" size="small">
            <el-form-item label="AI 检测分数上限 (%)">
              <el-input-number
                v-model="presets.ai_score_threshold"
                :min="0" :max="100" :precision="1"
                @change="v => setPreset('ai_score_threshold', String(v))"
                style="width:100%"
              />
            </el-form-item>
            <el-form-item label="原创度下限">
              <el-input-number
                v-model="presets.originality_threshold"
                :min="0" :max="1" :step="0.05" :precision="2"
                @change="v => setPreset('originality_threshold', String(v))"
                style="width:100%"
              />
            </el-form-item>
            <el-form-item label="最小奖励密度">
              <el-input-number
                v-model="presets.min_reward_density"
                :min="0" :max="10" :step="0.1" :precision="1"
                @change="v => setPreset('min_reward_density', String(v))"
                style="width:100%"
              />
            </el-form-item>
            <el-form-item label="节奏目标 CV">
              <el-input-number
                v-model="presets.burstiness_target_cv"
                :min="0" :max="5" :step="0.1" :precision="2"
                @change="v => setPreset('burstiness_target_cv', String(v))"
                style="width:100%"
              />
            </el-form-item>
          </el-form>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card class="preset-card">
          <template #header><span>工作流设置</span></template>
          <el-form label-width="160px" label-position="left" size="small">
            <el-form-item label="严格审核模式">
              <el-switch
                v-model="presets.strict_review"
                active-value="true"
                inactive-value="false"
                @change="v => setPreset('strict_review', String(v))"
              />
              <span class="hint">开启后章节生成后需要人工审核</span>
            </el-form-item>
          </el-form>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card class="preset-card">
          <template #header><span>Python Sidecar</span></template>
          <el-form label-width="160px" label-position="left" size="small">
            <el-form-item label="Sidecar URL">
              <el-input
                v-model="presets.sidecar_url"
                placeholder="http://127.0.0.1:8081"
                @blur="setPreset('sidecar_url', presets.sidecar_url)"
              />
            </el-form-item>
          </el-form>
          <p class="hint-block">
            修改后需要重启服务才能生效（仅此项需要重启）。
          </p>
        </el-card>
      </el-col>
    </el-row>

    <!-- Raw settings table for custom / advanced keys -->
    <div class="section-title">自定义设置</div>
    <el-table :data="customSettings" v-loading="loading" class="settings-table">
      <el-table-column label="Key" prop="key" width="240" />
      <el-table-column label="Value" prop="value" show-overflow-tooltip />
      <el-table-column label="操作" width="140" fixed="right">
        <template #default="{ row }">
          <el-button size="small" @click="openEditDialog(row)">编辑</el-button>
          <el-button size="small" type="danger" @click="deleteSetting(row.key)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- Add / Edit dialog -->
    <el-dialog
      v-model="showDialog"
      :title="editingKey ? '编辑设置' : '添加设置'"
      width="480px"
      destroy-on-close
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-width="80px">
        <el-form-item label="Key" prop="key">
          <el-input v-model="form.key" :disabled="!!editingKey" placeholder="settings_key" />
        </el-form-item>
        <el-form-item label="Value" prop="value">
          <el-input v-model="form.value" type="textarea" :rows="3" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showDialog = false">取消</el-button>
        <el-button type="primary" @click="submitDialog" :loading="submitting">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { systemSettingsApi } from '@/api'
import type { FormInstance, FormRules } from 'element-plus'

// Keys managed by the preset cards above; excluded from the raw table
const PRESET_KEYS = new Set([
  'ai_score_threshold',
  'originality_threshold',
  'min_reward_density',
  'burstiness_target_cv',
  'strict_review',
  'sidecar_url',
])

const allSettings = ref<Record<string, string>>({})
const loading = ref(false)
const showDialog = ref(false)
const submitting = ref(false)
const editingKey = ref<string | null>(null)
const formRef = ref<FormInstance>()

const presets = reactive({
  ai_score_threshold: 35,
  originality_threshold: 0.7,
  min_reward_density: 1.5,
  burstiness_target_cv: 0.8,
  strict_review: 'true',
  sidecar_url: 'http://127.0.0.1:8081',
})

const customSettings = computed(() =>
  Object.entries(allSettings.value)
    .filter(([k]) => !PRESET_KEYS.has(k))
    .map(([key, value]) => ({ key, value }))
)

const form = reactive({ key: '', value: '' })
const rules: FormRules = {
  key: [{ required: true, message: '请输入 Key', trigger: 'blur' }],
  value: [{ required: true, message: '请输入 Value', trigger: 'blur' }],
}

onMounted(fetchSettings)

async function fetchSettings() {
  loading.value = true
  try {
    const res = await systemSettingsApi.getAll()
    const data: Record<string, string> = res.data.data || {}
    allSettings.value = data

    // Populate preset values from DB, falling back to defaults
    if (data.ai_score_threshold !== undefined) presets.ai_score_threshold = Number(data.ai_score_threshold)
    if (data.originality_threshold !== undefined) presets.originality_threshold = Number(data.originality_threshold)
    if (data.min_reward_density !== undefined) presets.min_reward_density = Number(data.min_reward_density)
    if (data.burstiness_target_cv !== undefined) presets.burstiness_target_cv = Number(data.burstiness_target_cv)
    if (data.strict_review !== undefined) presets.strict_review = data.strict_review
    if (data.sidecar_url !== undefined) presets.sidecar_url = data.sidecar_url
  } catch {
    ElMessage.error('获取系统设置失败')
  } finally {
    loading.value = false
  }
}

async function setPreset(key: string, value: string) {
  try {
    await systemSettingsApi.set(key, value)
    allSettings.value[key] = value
    ElMessage.success(`已保存 ${key}`)
  } catch {
    ElMessage.error(`保存 ${key} 失败`)
  }
}

function openAddDialog() {
  editingKey.value = null
  form.key = ''
  form.value = ''
  showDialog.value = true
}

function openEditDialog(row: { key: string; value: string }) {
  editingKey.value = row.key
  form.key = row.key
  form.value = row.value
  showDialog.value = true
}

async function submitDialog() {
  await formRef.value?.validate()
  submitting.value = true
  try {
    await systemSettingsApi.set(form.key, form.value)
    allSettings.value[form.key] = form.value
    ElMessage.success('保存成功')
    showDialog.value = false
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '保存失败')
  } finally {
    submitting.value = false
  }
}

async function deleteSetting(key: string) {
  await ElMessageBox.confirm(`确定删除设置项 "${key}"？`, '确认删除', { type: 'warning' })
  try {
    await systemSettingsApi.delete(key)
    delete allSettings.value[key]
    ElMessage.success('已删除')
  } catch {
    ElMessage.error('删除失败')
  }
}
</script>

<style scoped>
.system-settings {
  max-width: 1200px;
}
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}
.page-header h1 {
  font-size: 24px;
  color: #e0e0e0;
}
.info-alert {
  margin-bottom: 24px;
}
.preset-cards {
  margin-bottom: 32px;
}
.preset-card {
  background-color: #1a1a2e;
  border: 1px solid #2a2a3e;
}
.section-title {
  font-size: 16px;
  color: #a0a0b0;
  margin-bottom: 12px;
  font-weight: 600;
}
.settings-table {
  background-color: #1a1a2e;
}
.hint {
  font-size: 12px;
  color: #888;
  margin-left: 8px;
}
.hint-block {
  font-size: 12px;
  color: #888;
  margin-top: 8px;
}
</style>
