<template>
  <div class="project-list">
    <div class="page-header">
      <h1>项目管理</h1>
      <el-button type="primary" @click="showCreateDialog = true">
        <el-icon><Plus /></el-icon>
        新建项目
      </el-button>
    </div>

    <el-row :gutter="20">
      <el-col :span="8" v-for="project in projectStore.projects" :key="project.id">
        <el-card class="project-card" shadow="hover" @click="enterProject(project)">
          <template #header>
            <div class="card-header">
              <span class="title">{{ project.title }}</span>
              <el-dropdown @click.stop trigger="click">
                <el-icon class="more-btn"><MoreFilled /></el-icon>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item @click="editProject(project)">编辑</el-dropdown-item>
                    <el-dropdown-item @click="confirmDelete(project)" divided>
                      <span style="color: #f56c6c">删除</span>
                    </el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </div>
          </template>
          <div class="card-body">
            <el-tag :type="genreTagType(project.genre)" size="small">{{ project.genre }}</el-tag>
            <p class="target-words">目标字数: {{ formatNumber(project.target_words) }}</p>
            <p class="style-desc" v-if="project.style_description">
              {{ project.style_description }}
            </p>
            <div class="card-footer">
              <el-tag :type="statusTagType(project.status)" size="small">
                {{ statusLabel(project.status) }}
              </el-tag>
              <span class="date">{{ formatDate(project.created_at) }}</span>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-empty v-if="!projectStore.loading && projectStore.projects.length === 0" description="还没有项目，点击上方按钮创建" />

    <!-- Create/Edit Dialog -->
    <el-dialog
      v-model="showCreateDialog"
      :title="editingProject ? '编辑项目' : '新建项目'"
      width="600px"
      @close="resetForm"
    >
      <el-form :model="form" label-width="100px">
        <el-form-item label="项目名称" required>
          <el-input v-model="form.title" placeholder="请输入项目名称" />
        </el-form-item>
        <el-form-item label="类型/流派">
          <el-select v-model="form.genre" placeholder="选择类型">
            <el-option label="玄幻" value="玄幻" />
            <el-option label="仙侠" value="仙侠" />
            <el-option label="都市" value="都市" />
            <el-option label="科幻" value="科幻" />
            <el-option label="历史" value="历史" />
            <el-option label="悬疑" value="悬疑" />
            <el-option label="言情" value="言情" />
            <el-option label="武侠" value="武侠" />
            <el-option label="其他" value="其他" />
          </el-select>
        </el-form-item>
        <el-form-item label="目标字数">
          <el-input-number v-model="form.target_words" :min="10000" :max="10000000" :step="10000" />
        </el-form-item>
        <el-form-item label="风格描述">
          <el-input
            v-model="form.style_description"
            type="textarea"
            :rows="3"
            placeholder="描述期望的写作风格，如：类似天蚕土豆的热血玄幻风格"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">取消</el-button>
        <el-button type="primary" @click="handleSave" :loading="saving">
          {{ editingProject ? '保存' : '创建' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useProjectStore, type Project } from '@/stores/project'

const router = useRouter()
const projectStore = useProjectStore()

const showCreateDialog = ref(false)
const editingProject = ref<Project | null>(null)
const saving = ref(false)

const form = ref({
  title: '',
  genre: '玄幻',
  target_words: 500000,
  style_description: '',
})

onMounted(() => {
  projectStore.fetchProjects()
})

function enterProject(project: Project) {
  projectStore.setCurrentProject(project.id)
  router.push(`/projects/${project.id}/studio`)
}

function editProject(project: Project) {
  editingProject.value = project
  form.value = {
    title: project.title,
    genre: project.genre,
    target_words: project.target_words,
    style_description: project.style_description,
  }
  showCreateDialog.value = true
}

async function handleSave() {
  if (!form.value.title) {
    ElMessage.warning('请输入项目名称')
    return
  }
  saving.value = true
  try {
    if (editingProject.value) {
      await projectStore.updateProject(editingProject.value.id, form.value)
      ElMessage.success('项目已更新')
    } else {
      const project = await projectStore.createProject(form.value)
      ElMessage.success('项目创建成功')
      enterProject(project)
    }
    showCreateDialog.value = false
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '操作失败')
  } finally {
    saving.value = false
  }
}

async function confirmDelete(project: Project) {
  try {
    await ElMessageBox.confirm(`确认删除项目 "${project.title}" 吗？此操作不可恢复。`, '删除确认', {
      type: 'warning',
    })
    await projectStore.deleteProject(project.id)
    ElMessage.success('项目已删除')
  } catch {
    // cancelled
  }
}

function resetForm() {
  editingProject.value = null
  form.value = { title: '', genre: '玄幻', target_words: 500000, style_description: '' }
}

function formatNumber(n: number) {
  return n ? n.toLocaleString() : '0'
}

function formatDate(d: string) {
  return d ? new Date(d).toLocaleDateString('zh-CN') : ''
}

function genreTagType(genre: string) {
  const map: Record<string, string> = {
    '玄幻': '', '仙侠': 'success', '都市': 'info',
    '科幻': 'warning', '悬疑': 'danger',
  }
  return (map[genre] || '') as any
}

function statusTagType(status: string) {
  const map: Record<string, string> = {
    draft: 'info', in_progress: '', completed: 'success',
  }
  return (map[status] || 'info') as any
}

function statusLabel(status: string) {
  const map: Record<string, string> = {
    draft: '草稿', in_progress: '进行中', completed: '已完成',
  }
  return map[status] || status
}
</script>

<style scoped>
.project-list {
  max-width: 1200px;
  margin: 0 auto;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
}

.page-header h1 {
  font-size: 24px;
  color: var(--nb-text-primary);
}

.project-card {
  margin-bottom: 20px;
  cursor: pointer;
  background: var(--nb-card-bg);
  border: 1px solid var(--nb-card-border);
  transition: transform 0.2s, box-shadow 0.2s;
}

.project-card:hover {
  transform: translateY(-4px);
  box-shadow: 0 4px 12px rgba(64, 158, 255, 0.15);
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-header .title {
  font-size: 16px;
  font-weight: 600;
  color: var(--nb-text-primary);
}

.more-btn {
  cursor: pointer;
  color: #999;
}

.target-words {
  margin: 8px 0;
  color: #888;
  font-size: 13px;
}

.style-desc {
  color: #999;
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.card-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 12px;
}

.card-footer .date {
  color: #666;
  font-size: 12px;
}
</style>
