<template>
  <div class="users-page">
    <div class="page-header">
      <div>
        <h1>用户与权限</h1>
        <p>管理员可以创建普通用户、停用账号，并控制后续模型策略扩展的 JSON 配置。</p>
      </div>
      <div class="header-actions">
        <el-button @click="fetchUsers" :loading="loading">
          <el-icon><Refresh /></el-icon>
          刷新
        </el-button>
        <el-button type="primary" @click="openCreate">
          <el-icon><Plus /></el-icon>
          新建用户
        </el-button>
      </div>
    </div>

    <el-alert type="info" :closable="false" class="info-alert">
      <template #title>
        普通用户只能看到自己创建的项目和项目内任务；系统级配置、全局任务、模型路由和日志保留给管理员。
      </template>
    </el-alert>

    <el-table :data="users" v-loading="loading" class="users-table">
      <el-table-column prop="username" label="用户名" min-width="160" />
      <el-table-column prop="display_name" label="显示名" min-width="160" />
      <el-table-column label="角色" width="110">
        <template #default="{ row }">
          <el-tag :type="row.role === 'admin' ? 'danger' : 'info'" size="small">
            {{ row.role === 'admin' ? '管理员' : '普通用户' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="110">
        <template #default="{ row }">
          <el-tag :type="row.status === 'active' ? 'success' : 'warning'" size="small">
            {{ row.status === 'active' ? '启用' : '停用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="模型策略" min-width="220" show-overflow-tooltip>
        <template #default="{ row }">
          <code>{{ row.model_policy || '{}' }}</code>
        </template>
      </el-table-column>
      <el-table-column label="创建时间" width="180">
        <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="170" fixed="right">
        <template #default="{ row }">
          <el-button size="small" @click="openEdit(row)">编辑</el-button>
          <el-button size="small" type="danger" @click="deleteUser(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog
      v-model="dialogVisible"
      :title="editingUser ? '编辑用户' : '新建用户'"
      width="560px"
      destroy-on-close
      @closed="resetForm"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-width="100px" label-position="left">
        <el-form-item label="用户名" prop="username">
          <el-input v-model="form.username" :disabled="Boolean(editingUser)" placeholder="username" />
        </el-form-item>
        <el-form-item :label="editingUser ? '新密码' : '密码'" prop="password">
          <el-input
            v-model="form.password"
            type="password"
            show-password
            :placeholder="editingUser ? '留空保持不变，至少 8 位' : '至少 8 位'"
          />
        </el-form-item>
        <el-form-item label="显示名">
          <el-input v-model="form.display_name" placeholder="可留空，默认使用用户名" />
        </el-form-item>
        <el-form-item label="角色">
          <el-segmented v-model="form.role" :options="roleOptions" />
        </el-form-item>
        <el-form-item label="状态">
          <el-segmented v-model="form.status" :options="statusOptions" />
        </el-form-item>
        <el-form-item label="模型策略" prop="model_policy">
          <el-input v-model="form.model_policy" type="textarea" :rows="5" placeholder='{"scope":"all"}' />
          <div class="hint">当前用于持久化后续模型权限策略，必须是合法 JSON。</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submit" :loading="submitting">
          {{ editingUser ? '保存' : '创建' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { getApiErrorMessage, userApi, type UserRecord } from '@/api'
import type { FormInstance, FormRules } from 'element-plus'

const users = ref<UserRecord[]>([])
const loading = ref(false)
const submitting = ref(false)
const dialogVisible = ref(false)
const editingUser = ref<UserRecord | null>(null)
const formRef = ref<FormInstance>()

const form = reactive({
  username: '',
  password: '',
  display_name: '',
  role: 'user',
  status: 'active',
  model_policy: '{"scope":"all"}',
})

const roleOptions = [
  { label: '普通用户', value: 'user' },
  { label: '管理员', value: 'admin' },
]
const statusOptions = [
  { label: '启用', value: 'active' },
  { label: '停用', value: 'disabled' },
]

const rules = computed<FormRules>(() => ({
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [
    {
      validator: (_rule, value, callback) => {
        if (!editingUser.value && !value) {
          callback(new Error('请输入密码'))
          return
        }
        if (value && String(value).length < 8) {
          callback(new Error('密码至少 8 位'))
          return
        }
        callback()
      },
      trigger: 'blur',
    },
  ],
  model_policy: [
    {
      validator: (_rule, value, callback) => {
        try {
          JSON.parse(String(value || '{}'))
          callback()
        } catch {
          callback(new Error('请输入合法 JSON'))
        }
      },
      trigger: 'blur',
    },
  ],
}))

onMounted(fetchUsers)

async function fetchUsers() {
  loading.value = true
  try {
    const res = await userApi.list()
    users.value = res.data.data ?? []
  } catch (e: any) {
    ElMessage.error(getApiErrorMessage(e, '加载用户失败'))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingUser.value = null
  resetForm()
  dialogVisible.value = true
}

function openEdit(user: UserRecord) {
  editingUser.value = user
  form.username = user.username
  form.password = ''
  form.display_name = user.display_name
  form.role = user.role || 'user'
  form.status = user.status || 'active'
  form.model_policy = user.model_policy || '{}'
  dialogVisible.value = true
}

function resetForm() {
  form.username = ''
  form.password = ''
  form.display_name = ''
  form.role = 'user'
  form.status = 'active'
  form.model_policy = '{"scope":"all"}'
  formRef.value?.clearValidate()
}

async function submit() {
  await formRef.value?.validate()
  submitting.value = true
  try {
    const payload = {
      username: form.username.trim(),
      password: form.password,
      display_name: form.display_name.trim(),
      role: form.role,
      status: form.status,
      model_policy: JSON.stringify(JSON.parse(form.model_policy || '{}')),
    }
    if (editingUser.value) {
      const updatePayload: Record<string, any> = { ...payload }
      delete updatePayload.username
      if (!updatePayload.password) {
        delete updatePayload.password
      }
      await userApi.update(editingUser.value.id, updatePayload)
      ElMessage.success('用户已更新')
    } else {
      await userApi.create(payload)
      ElMessage.success('用户已创建')
    }
    dialogVisible.value = false
    await fetchUsers()
  } catch (e: any) {
    ElMessage.error(getApiErrorMessage(e, '保存用户失败'))
  } finally {
    submitting.value = false
  }
}

async function deleteUser(user: UserRecord) {
  try {
    await ElMessageBox.confirm(`确定删除用户 "${user.username}"？`, '删除用户', { type: 'warning' })
    await userApi.delete(user.id)
    ElMessage.success('用户已删除')
    await fetchUsers()
  } catch (e: any) {
    if (e === 'cancel' || e === 'close') return
    ElMessage.error(e.normalized?.message || e.response?.data?.error || '删除用户失败')
  }
}

function formatTime(value: string) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}
</script>

<style scoped>
.users-page { color: var(--nb-text-primary); }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; margin-bottom: 18px; gap: 16px; }
.page-header h1 { font-size: 24px; margin: 0 0 6px; color: var(--nb-text-primary); }
.page-header p { margin: 0; color: var(--nb-text-secondary); font-size: 13px; }
.header-actions { display: flex; gap: 8px; flex-shrink: 0; }
.info-alert { margin-bottom: 16px; }
.users-table code { font-size: 12px; color: var(--nb-text-secondary); }
.hint { margin-top: 6px; color: var(--nb-text-secondary); font-size: 12px; line-height: 1.4; }

@media (max-width: 760px) {
  .page-header { flex-direction: column; }
  .header-actions { width: 100%; }
  .header-actions .el-button { flex: 1; }
}
</style>
