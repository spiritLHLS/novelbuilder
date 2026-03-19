<template>
  <div class="foreshadowing">
    <div class="page-header">
      <h1>伏笔管理</h1>
      <el-button type="primary" @click="showCreate"><el-icon><Plus /></el-icon>创建伏笔</el-button>
    </div>

    <el-row :gutter="20">
      <!-- Filter -->
      <el-col :span="24" style="margin-bottom: 16px;">
        <el-radio-group v-model="statusFilter" @change="filterForeshadowings">
          <el-radio-button label="">全部</el-radio-button>
          <el-radio-button label="planted">已埋设</el-radio-button>
          <el-radio-button label="triggered">已触发</el-radio-button>
          <el-radio-button label="resolved">已回收</el-radio-button>
        </el-radio-group>
      </el-col>
    </el-row>

    <!-- Foreshadowing Cards -->
    <el-row :gutter="16">
      <el-col :span="8" v-for="f in filtered" :key="f.id">
        <el-card shadow="hover" class="fs-card" :class="f.status">
          <div class="fs-header">
            <h3>{{ f.title }}</h3>
            <el-dropdown @command="(cmd: string) => handleCommand(cmd, f)">
              <el-icon><MoreFilled /></el-icon>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item v-if="f.status === 'planted'" command="triggered">标为触发</el-dropdown-item>
                  <el-dropdown-item v-if="f.status === 'triggered'" command="resolved">标为回收</el-dropdown-item>
                  <el-dropdown-item command="delete" divided>删除</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>

          <el-tag :type="statusTagType(f.status)" size="small">{{ statusLabel(f.status) }}</el-tag>

          <p class="fs-desc">{{ f.description }}</p>

          <div class="fs-meta">
            <div v-if="f.plant_chapter">
              <span class="meta-label">埋设章节:</span> 第{{ f.plant_chapter }}章
            </div>
            <div v-if="f.trigger_chapter">
              <span class="meta-label">触发章节:</span> 第{{ f.trigger_chapter }}章
            </div>
            <div v-if="f.resolve_chapter">
              <span class="meta-label">回收章节:</span> 第{{ f.resolve_chapter }}章
            </div>
          </div>

          <div v-if="f.related_characters?.length" class="fs-chars">
            <el-tag v-for="c in f.related_characters" :key="c" size="small" style="margin: 2px;">{{ c }}</el-tag>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-empty v-if="!filtered.length" description="暂无伏笔" />

    <!-- Timeline View -->
    <el-card shadow="hover" style="margin-top: 24px;" v-if="foreshadowings.length">
      <template #header><span>伏笔时间线</span></template>
      <div class="timeline">
        <el-timeline>
          <el-timeline-item
            v-for="event in timelineEvents" :key="event.key"
            :type="event.type" :timestamp="event.label" placement="top">
            <el-card shadow="never" class="timeline-card">
              <span :class="'event-' + event.eventType">{{ event.eventLabel }}</span>
              {{ event.title }}
            </el-card>
          </el-timeline-item>
        </el-timeline>
      </div>
    </el-card>

    <!-- Create Dialog -->
    <el-dialog v-model="showDialog" title="创建伏笔" width="600px">
      <el-form :model="form" label-position="top">
        <el-form-item label="伏笔标题" required>
          <el-input v-model="form.title" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="4" />
        </el-form-item>
        <el-row :gutter="16">
          <el-col :span="8">
            <el-form-item label="埋设章节">
              <el-input-number v-model="form.plant_chapter" :min="1" style="width: 100%;" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="预计触发章节">
              <el-input-number v-model="form.trigger_chapter" :min="1" style="width: 100%;" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="预计回收章节">
              <el-input-number v-model="form.resolve_chapter" :min="1" style="width: 100%;" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="相关角色（逗号分隔）">
          <el-input v-model="form.related_chars_str" placeholder="角色名1, 角色名2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showDialog = false">取消</el-button>
        <el-button type="primary" @click="createFs" :loading="creating">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { foreshadowingApi } from '@/api'

const route = useRoute()
const projectId = route.params.projectId as string

const foreshadowings = ref<any[]>([])
const statusFilter = ref('')
const showDialog = ref(false)
const creating = ref(false)

const form = ref({
  title: '', description: '', plant_chapter: 1,
  trigger_chapter: 0, resolve_chapter: 0, related_chars_str: '',
})

const filtered = computed(() =>
  statusFilter.value
    ? foreshadowings.value.filter(f => f.status === statusFilter.value)
    : foreshadowings.value
)

const timelineEvents = computed(() => {
  const events: any[] = []
  foreshadowings.value.forEach(f => {
    if (f.plant_chapter) {
      events.push({
        key: f.id + '-plant', chapter: f.plant_chapter, title: f.title,
        eventType: 'plant', eventLabel: '埋设', type: 'primary', label: `第${f.plant_chapter}章`,
      })
    }
    if (f.trigger_chapter) {
      events.push({
        key: f.id + '-trigger', chapter: f.trigger_chapter, title: f.title,
        eventType: 'trigger', eventLabel: '触发', type: 'warning', label: `第${f.trigger_chapter}章`,
      })
    }
    if (f.resolve_chapter) {
      events.push({
        key: f.id + '-resolve', chapter: f.resolve_chapter, title: f.title,
        eventType: 'resolve', eventLabel: '回收', type: 'success', label: `第${f.resolve_chapter}章`,
      })
    }
  })
  return events.sort((a, b) => a.chapter - b.chapter)
})

function statusTagType(s: string) {
  return s === 'planted' ? 'primary' : s === 'triggered' ? 'warning' : 'success'
}

function statusLabel(s: string) {
  return s === 'planted' ? '已埋设' : s === 'triggered' ? '已触发' : '已回收'
}

onMounted(fetchFs)

async function fetchFs() {
  try {
    const res = await foreshadowingApi.list(projectId)
    foreshadowings.value = res.data.data || []
  } catch { /* empty */ }
}

function filterForeshadowings() { /* reactive via computed */ }

function showCreate() {
  form.value = { title: '', description: '', plant_chapter: 1, trigger_chapter: 0, resolve_chapter: 0, related_chars_str: '' }
  showDialog.value = true
}

async function createFs() {
  if (!form.value.title) { ElMessage.warning('请填写标题'); return }
  creating.value = true
  try {
    const payload: any = { ...form.value }
    payload.related_characters = form.value.related_chars_str.split(/[,，]/).map(s => s.trim()).filter(Boolean)
    delete payload.related_chars_str
    await foreshadowingApi.create(projectId, payload)
    ElMessage.success('伏笔已创建')
    showDialog.value = false
    await fetchFs()
  } finally {
    creating.value = false
  }
}

async function handleCommand(cmd: string, f: any) {
  if (cmd === 'delete') {
    await ElMessageBox.confirm('确认删除该伏笔？', '删除', { type: 'warning' })
    try {
      await foreshadowingApi.delete(projectId, f.id)
      ElMessage.success('已删除')
      await fetchFs()
    } catch { ElMessage.error('删除失败') }
  } else {
    try {
      await foreshadowingApi.updateStatus(projectId, f.id, cmd)
      ElMessage.success('状态已更新')
      await fetchFs()
    } catch { ElMessage.error('更新失败') }
  }
}
</script>

<style scoped>
.foreshadowing { max-width: 1400px; margin: 0 auto; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; }
.page-header h1 { font-size: 24px; color: #e0e0e0; }
.fs-card { margin-bottom: 16px; transition: transform 0.2s; }
.fs-card:hover { transform: translateY(-2px); }
.fs-card.planted { border-left: 3px solid #409eff; }
.fs-card.triggered { border-left: 3px solid #e6a23c; }
.fs-card.resolved { border-left: 3px solid #67c23a; }
.fs-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 8px; }
.fs-header h3 { font-size: 16px; color: var(--nb-text-primary); margin: 0; }
.fs-desc { color: var(--nb-text-secondary); margin: 12px 0; line-height: 1.6; font-size: 14px; }
.fs-meta { font-size: 13px; color: #888; }
.meta-label { color: #666; }
.fs-chars { margin-top: 8px; }
.timeline { max-height: 500px; overflow-y: auto; }
.timeline-card { background: transparent; }
.event-plant { color: #409eff; font-weight: 500; margin-right: 8px; }
.event-trigger { color: #e6a23c; font-weight: 500; margin-right: 8px; }
.event-resolve { color: #67c23a; font-weight: 500; margin-right: 8px; }
</style>
