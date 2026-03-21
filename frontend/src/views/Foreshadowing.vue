<template>
  <div class="foreshadowing">
    <div class="page-header">
      <h1>伏笔管理</h1>
      <el-button type="primary" @click="openCreate"><el-icon><Plus /></el-icon>创建伏笔</el-button>
    </div>

    <!-- Status Filter -->
    <el-radio-group v-model="statusFilter" style="margin-bottom: 16px;">
      <el-radio-button value="">全部 ({{ foreshadowings.length }})</el-radio-button>
      <el-radio-button value="planned">计划中</el-radio-button>
      <el-radio-button value="planted">已埋设</el-radio-button>
      <el-radio-button value="triggered">已触发</el-radio-button>
      <el-radio-button value="resolved">已回收</el-radio-button>
    </el-radio-group>

    <!-- Cards -->
    <el-row :gutter="16">
      <el-col :span="8" v-for="f in filtered" :key="f.id">
        <el-card shadow="hover" class="fs-card" :class="f.status">
          <div class="fs-status-row">
            <el-tag :type="statusTagType(f.status)" size="small">{{ statusLabel(f.status) }}</el-tag>
            <span class="fs-priority">优先级: {{ f.priority || 3 }}</span>
          </div>
          <p class="fs-content">{{ f.content }}</p>
          <div v-if="f.embed_method" class="fs-meta">
            <span class="meta-label">埋设方式:</span> {{ f.embed_method }}
          </div>
          <div v-if="f.tags?.length" class="fs-tags">
            <el-tag v-for="t in f.tags" :key="t" size="small" type="info" style="margin: 2px;">{{ t }}</el-tag>
          </div>
          <div class="fs-actions">
            <el-button text size="small" @click="openEdit(f)">编辑</el-button>
            <el-button text size="small" type="success" @click="nextStatus(f)" v-if="f.status !== 'resolved'">
              推进→{{ nextStatusLabel(f.status) }}
            </el-button>
            <el-button text size="small" type="danger" @click="doDelete(f)">删除</el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-empty v-if="!filtered.length" description="暂无伏笔" />

    <!-- Stats Overview -->
    <el-card shadow="hover" style="margin-top: 24px;" v-if="foreshadowings.length">
      <template #header><span>状态概览</span></template>
      <el-row :gutter="16" style="text-align: center;">
        <el-col :span="6" v-for="s in statuses" :key="s.key">
          <div class="stat-block">
            <div class="stat-num" :style="{ color: s.color }">{{ countByStatus(s.key) }}</div>
            <div class="stat-label">{{ s.label }}</div>
          </div>
        </el-col>
      </el-row>
    </el-card>

    <!-- Create Dialog -->
    <el-dialog v-model="showCreateDlg" title="创建伏笔" width="560px" :close-on-click-modal="false">
      <el-form :model="createForm" label-position="top">
        <el-form-item label="伏笔内容" required>
          <el-input v-model="createForm.content" type="textarea" :rows="4" placeholder="描述这个伏笔的内容..." />
        </el-form-item>
        <el-form-item label="埋设方式">
          <el-input v-model="createForm.embed_method" placeholder="如：对话暗示、景物描写、道具铺库..." />
        </el-form-item>
        <el-form-item label="优先级">
          <el-slider v-model="createForm.priority" :min="1" :max="5" :step="1" show-stops />
        </el-form-item>
        <el-form-item label="标签（逗号分隔）">
          <el-input v-model="createForm.tags_str" placeholder="人物名, 情节线, 主题词..." />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDlg = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="doCreate">创建</el-button>
      </template>
    </el-dialog>

    <!-- Edit Dialog -->
    <el-dialog v-model="showEditDlg" title="编辑伏笔" width="560px" :close-on-click-modal="false">
      <el-form :model="editForm" label-position="top">
        <el-form-item label="伏笔内容" required>
          <el-input v-model="editForm.content" type="textarea" :rows="4" />
        </el-form-item>
        <el-form-item label="埋设方式">
          <el-input v-model="editForm.embed_method" />
        </el-form-item>
        <el-form-item label="优先级">
          <el-slider v-model="editForm.priority" :min="1" :max="5" :step="1" show-stops />
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="editForm.status" style="width: 100%;">
            <el-option label="计划中" value="planned" />
            <el-option label="已埋设" value="planted" />
            <el-option label="已触发" value="triggered" />
            <el-option label="已回收" value="resolved" />
          </el-select>
        </el-form-item>
        <el-form-item label="标签（逗号分隔）">
          <el-input v-model="editForm.tags_str" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showEditDlg = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="doUpdate">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { foreshadowingApi } from '@/api'

const route = useRoute()
const projectId = route.params.projectId as string

const foreshadowings = ref<any[]>([])
const statusFilter = ref('')
const saving = ref(false)
const showCreateDlg = ref(false)
const showEditDlg = ref(false)
const editingId = ref('')

const createForm = ref({ content: '', embed_method: '', priority: 3, tags_str: '' })
const editForm = ref({ content: '', embed_method: '', priority: 3, status: 'planned', tags_str: '' })

const statuses = [
  { key: 'planned', label: '计划中', color: '#909399' },
  { key: 'planted', label: '已埋设', color: '#409eff' },
  { key: 'triggered', label: '已触发', color: '#e6a23c' },
  { key: 'resolved', label: '已回收', color: '#67c23a' },
]

const filtered = computed(() =>
  statusFilter.value
    ? foreshadowings.value.filter(f => f.status === statusFilter.value)
    : foreshadowings.value
)

function countByStatus(key: string) {
  return foreshadowings.value.filter(f => f.status === key).length
}

function statusTagType(s: string) {
  const m: Record<string, string> = { planned: 'info', planted: 'primary', triggered: 'warning', resolved: 'success' }
  return m[s] || 'info'
}

function statusLabel(s: string) {
  const m: Record<string, string> = { planned: '计划中', planted: '已埋设', triggered: '已触发', resolved: '已回收' }
  return m[s] || s
}

function nextStatusLabel(s: string) {
  const m: Record<string, string> = { planned: '埋设', planted: '触发', triggered: '回收' }
  return m[s] || ''
}

function nextStatusKey(s: string) {
  const m: Record<string, string> = { planned: 'planted', planted: 'triggered', triggered: 'resolved' }
  return m[s] || s
}

onMounted(fetchFs)

async function fetchFs() {
  try {
    const res = await foreshadowingApi.list(projectId)
    foreshadowings.value = res.data.data || []
  } catch { /* ignore */ }
}

function openCreate() {
  createForm.value = { content: '', embed_method: '', priority: 3, tags_str: '' }
  showCreateDlg.value = true
}

function openEdit(f: any) {
  editingId.value = f.id
  editForm.value = {
    content: f.content || '',
    embed_method: f.embed_method || '',
    priority: f.priority || 3,
    status: f.status || 'planned',
    tags_str: (f.tags || []).join(', '),
  }
  showEditDlg.value = true
}

async function doCreate() {
  if (!createForm.value.content.trim()) { ElMessage.warning('请填写伏笔内容'); return }
  saving.value = true
  try {
    const tags = createForm.value.tags_str.split(/[,，]/).map((s: string) => s.trim()).filter(Boolean)
    await foreshadowingApi.create(projectId, {
      content: createForm.value.content,
      embed_method: createForm.value.embed_method,
      priority: createForm.value.priority,
      tags,
    })
    ElMessage.success('伏笔已创建')
    showCreateDlg.value = false
    await fetchFs()
  } catch { ElMessage.error('创建失败') }
  finally { saving.value = false }
}

async function doUpdate() {
  if (!editForm.value.content.trim()) { ElMessage.warning('请填写伏笔内容'); return }
  saving.value = true
  try {
    const tags = editForm.value.tags_str.split(/[,，]/).map((s: string) => s.trim()).filter(Boolean)
    await foreshadowingApi.update(projectId, editingId.value, {
      content: editForm.value.content,
      embed_method: editForm.value.embed_method,
      priority: editForm.value.priority,
      tags,
      status: editForm.value.status,
    })
    ElMessage.success('已保存')
    showEditDlg.value = false
    await fetchFs()
  } catch { ElMessage.error('保存失败') }
  finally { saving.value = false }
}

async function nextStatus(f: any) {
  const newStatus = nextStatusKey(f.status)
  try {
    await foreshadowingApi.updateStatus(projectId, f.id, newStatus)
    ElMessage.success(`已更新为: ${statusLabel(newStatus)}`)
    await fetchFs()
  } catch { ElMessage.error('更新失败') }
}

async function doDelete(f: any) {
  await ElMessageBox.confirm('确认删除该伏笔？', '删除', { type: 'warning' })
  try {
    await foreshadowingApi.delete(projectId, f.id)
    ElMessage.success('已删除')
    await fetchFs()
  } catch { ElMessage.error('删除失败') }
}
</script>

<style scoped>
.foreshadowing { max-width: 1400px; margin: 0 auto; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; }
.page-header h1 { font-size: 24px; color: #e0e0e0; }
.fs-card { margin-bottom: 16px; transition: transform 0.2s; }
.fs-card:hover { transform: translateY(-2px); }
.fs-card.planned { border-left: 3px solid #909399; }
.fs-card.planted { border-left: 3px solid #409eff; }
.fs-card.triggered { border-left: 3px solid #e6a23c; }
.fs-card.resolved { border-left: 3px solid #67c23a; }
.fs-status-row { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.fs-priority { font-size: 12px; color: #888; }
.fs-content { color: var(--nb-text-primary, #e0e0e0); margin: 8px 0 10px; line-height: 1.6; font-size: 14px; }
.fs-meta { font-size: 13px; color: #888; margin-bottom: 6px; }
.meta-label { color: #666; }
.fs-tags { margin-top: 6px; }
.fs-actions { display: flex; gap: 4px; margin-top: 12px; padding-top: 8px; border-top: 1px solid var(--nb-card-border, #333); flex-wrap: wrap; }
.stat-block { padding: 12px; }
.stat-num { font-size: 28px; font-weight: bold; }
.stat-label { font-size: 13px; color: #888; margin-top: 4px; }
</style>
