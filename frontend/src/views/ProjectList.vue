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
      <el-col :xs="24" :sm="12" :md="8" v-for="project in projectStore.projects" :key="project.id">
        <el-card class="project-card" shadow="hover" @click="enterProject(project)">
          <template #header>
            <div class="card-header">
              <span class="title">{{ project.title }}</span>
              <div class="card-actions">
                <el-button size="small" @click.stop="editProject(project)">编辑</el-button>
                <el-button size="small" type="danger" plain @click.stop="confirmDelete(project)">删除</el-button>
              </div>
            </div>
          </template>
          <div class="card-body">
            <el-tag :type="genreTagType(project.genre)" size="small">{{ project.genre }}</el-tag>
            <el-tag size="small" style="margin-left: 6px">{{ project.language === 'en-US' ? 'English' : '中文' }}</el-tag>
            <el-tag size="small" type="info" style="margin-left: 6px">{{ creationModeLabel(project.creation_mode) }}</el-tag>
            <p class="target-words">目标字数: {{ formatNumber(project.target_words) }}</p>
            <p class="target-words">单章字数: {{ formatNumber(project.chapter_words || 3000) }}</p>
            <p class="style-desc" v-if="project.description">
              {{ project.description }}
            </p>
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
            <el-option label="西幻" value="西幻" />
            <el-option label="都市" value="都市" />
            <el-option label="科幻" value="科幻" />
            <el-option label="历史" value="历史" />
            <el-option label="悬疑" value="悬疑" />
            <el-option label="言情" value="言情" />
            <el-option label="武侠" value="武侠" />
            <el-option label="其他" value="其他" />
          </el-select>
        </el-form-item>
        <el-form-item label="写作语言">
          <el-segmented v-model="form.language" :options="languageOptions" />
        </el-form-item>
        <el-form-item label="创建方式">
          <el-select v-model="form.creation_mode" placeholder="选择创建方式" style="width: 100%">
            <el-option v-for="option in creationModeOptions" :key="option.value" :label="option.label" :value="option.value" />
          </el-select>
          <p class="form-hint">{{ creationModeHelp }}</p>
        </el-form-item>
        <el-form-item label="目标字数">
          <el-input-number v-model="form.target_words" :min="10000" :max="10000000" :step="10000" />
        </el-form-item>
        <el-form-item label="单章字数">
          <el-input-number v-model="form.chapter_words" :min="500" :max="20000" :step="500" />
        </el-form-item>
        <el-form-item label="项目简介">
          <el-input
            v-model="form.description"
            type="textarea"
            :rows="5"
            :placeholder="descriptionPlaceholder"
          />
        </el-form-item>
        <el-form-item label="风格描述">
          <el-input
            v-model="form.style_description"
            type="textarea"
            :rows="3"
            placeholder="描述期望的写作风格，如：类似天蚕土豆的热血玄幻风格"
          />
        </el-form-item>
        <el-form-item v-if="!editingProject" label="创建后">
          <el-checkbox v-model="autoGenerateBlueprint">立即生成整书蓝图</el-checkbox>
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
import { computed, ref, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { blueprintApi } from '@/api'
import { useProjectStore, type Project } from '@/stores/project'

const router = useRouter()
const projectStore = useProjectStore()

const showCreateDialog = ref(false)
const editingProject = ref<Project | null>(null)
const saving = ref(false)
const autoGenerateBlueprint = ref(true)
const languageOptions = [
  { label: '中文', value: 'zh-CN' },
  { label: 'English', value: 'en-US' },
]
const creationModeOptions = [
  { label: '仅用 prompt 开书', value: 'prompt_only', help: '适合一句话创意、核心卖点和读者体验驱动的项目。' },
  { label: '从零开始共创', value: 'scratch', help: '让系统从题材、主角、世界和冲突开始完整规划。' },
  { label: '我已有大纲', value: 'own_outline', help: '把你的卷纲、章纲、时间线或关键场景放到项目简介里。' },
  { label: '参考书仿写/拆解', value: 'reference_style', help: '先在参考书页导入作品，项目简介里说明要学习的结构、节奏和风格边界。' },
  { label: '基于原文改写', value: 'rewrite_original', help: '把原文或改写目标放到项目简介，后续可在章节导入页继续拆章处理。' },
  { label: '续写已有作品', value: 'continuation', help: '创建后到参考书页选择续写底本，系统会从底本尾章接续。' },
  { label: '同风格异世界', value: 'same_style_new_world', help: '学习参考作品的文风和节奏，但要求人物、世界和主线完全新建。' },
] as const

type CreationMode = typeof creationModeOptions[number]['value']

const creationModeHelp = computed(() =>
  creationModeOptions.find((option) => option.value === form.value.creation_mode)?.help || ''
)

const descriptionPlaceholder = computed(() => {
  const map: Record<CreationMode, string> = {
    prompt_only: '直接写开书 prompt：题材、主角、核心冲突、卖点、期望读者体验',
    scratch: '写你确定的少量约束即可：题材、禁忌、目标读者、希望避免的套路',
    own_outline: '粘贴你的大纲、时间线、卷结构、关键场景或人物关系',
    reference_style: '说明参考对象、要学习的节奏/结构/文风，以及不能照搬的边界',
    rewrite_original: '粘贴原文片段或说明改写目标：保留什么、替换什么、规避什么',
    continuation: '说明续写方向、起始状态、必须继承的角色关系和不可推翻设定',
    same_style_new_world: '说明要学习的风格特征，同时列出新世界观、新主角和新冲突要求',
  }
  return map[form.value.creation_mode] || map.prompt_only
})

const form = ref({
  title: '',
  genre: '玄幻',
  language: 'zh-CN' as 'zh-CN' | 'en-US',
  creation_mode: 'prompt_only' as CreationMode,
  description: '',
  target_words: 500000,
  chapter_words: 3000,
  style_description: '',
})

function onVisibilityChange() {
  if (document.visibilityState === 'visible') {
    projectStore.fetchProjects()
  }
}

onMounted(() => {
  projectStore.fetchProjects()
  document.addEventListener('visibilitychange', onVisibilityChange)
})

onUnmounted(() => {
  document.removeEventListener('visibilitychange', onVisibilityChange)
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
    language: project.language || 'zh-CN',
    creation_mode: (project.creation_mode || 'prompt_only') as CreationMode,
    description: project.description || '',
    target_words: project.target_words,
    chapter_words: project.chapter_words || 3000,
    style_description: project.style_description,
  }
  showCreateDialog.value = true
}

async function handleSave() {
  form.value.title = form.value.title.trim()
  if (!form.value.title) {
    ElMessage.warning('请输入项目名称')
    return
  }
  if (form.value.chapter_words < 500 || form.value.chapter_words > 20000) {
    ElMessage.warning('单章字数应在 500 到 20000 之间')
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
      if (autoGenerateBlueprint.value && form.value.description.trim()) {
        try {
          await blueprintApi.generate(project.id, { idea: buildBlueprintIdea(), genre: form.value.genre })
          ElMessage.success('整书蓝图任务已创建')
        } catch (err: any) {
          ElMessage.warning(err.response?.data?.error || '项目已创建，但蓝图任务启动失败')
        }
      }
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
  form.value = {
    title: '',
    genre: '玄幻',
    language: 'zh-CN',
    creation_mode: 'prompt_only',
    description: '',
    target_words: 500000,
    chapter_words: 3000,
    style_description: '',
  }
  autoGenerateBlueprint.value = true
}

function creationModeLabel(mode: string) {
  return creationModeOptions.find((option) => option.value === mode)?.label || '仅用 prompt 开书'
}

function buildBlueprintIdea() {
  const blocks = [
    `创建方式：${creationModeLabel(form.value.creation_mode)}`,
    form.value.description.trim(),
  ]
  if (form.value.style_description.trim()) {
    blocks.push(`风格要求：${form.value.style_description.trim()}`)
  }
  return blocks.filter(Boolean).join('\n\n')
}

function formatNumber(n: number) {
  return n ? n.toLocaleString() : '0'
}

function formatDate(d: string) {
  return d ? new Date(d).toLocaleDateString('zh-CN') : ''
}

function genreTagType(genre: string) {
  const map: Record<string, string> = {
    '玄幻': 'primary', '仙侠': 'success', '西幻': 'warning', '都市': 'info',
    '科幻': 'warning', '悬疑': 'danger',
  }
  return (map[genre] || 'info') as any
}

function statusTagType(status: string) {
  const map: Record<string, string> = {
    draft: 'info', active: 'success', in_progress: 'success', paused: 'warning', terminated: 'danger', completed: 'success',
  }
  return (map[status] || 'info') as any
}

function statusLabel(status: string) {
  const map: Record<string, string> = {
    draft: '草稿', active: '创作中', in_progress: '进行中', paused: '已暂停', terminated: '已终止', completed: '已完成',
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

.card-actions {
  display: flex;
  gap: 6px;
}

.target-words {
  margin: 8px 0;
  color: #888;
  font-size: 13px;
}

.form-hint {
  margin-top: 6px;
  color: var(--nb-text-secondary);
  font-size: 12px;
  line-height: 1.4;
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
