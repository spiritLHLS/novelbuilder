<template>
  <div class="outline">
    <div class="page-header">
      <h1>大纲管理</h1>
      <el-button type="primary" @click="showCreate"><el-icon><Plus /></el-icon>添加大纲节点</el-button>
    </div>

    <el-row :gutter="20">
      <!-- Outline Tree -->
      <el-col :span="10">
        <el-card shadow="hover">
          <template #header>
            <div class="card-header">
              <span>DOC三层大纲</span>
              <el-radio-group v-model="viewLevel" size="small">
                <el-radio-button label="all">全部</el-radio-button>
                <el-radio-button label="macro">宏观</el-radio-button>
                <el-radio-button label="meso">中观</el-radio-button>
                <el-radio-button label="micro">微观</el-radio-button>
              </el-radio-group>
            </div>
          </template>
          <el-tree
            :data="treeData"
            :props="{ label: 'title', children: 'children' }"
            :default-expand-all="true"
            highlight-current
            @node-click="onNodeClick"
          >
            <template #default="{ data }">
              <div class="tree-node">
                <el-tag :type="levelTagType(data.level)" size="small">{{ levelLabel(data.level) }}</el-tag>
                <span class="node-title">{{ data.title }}</span>
                <span v-if="data.tension_target" class="tension">⚡{{ data.tension_target }}</span>
              </div>
            </template>
          </el-tree>
          <el-empty v-if="!treeData.length" description="暂无大纲" />
        </el-card>
      </el-col>

      <!-- Detail Panel -->
      <el-col :span="14">
        <el-card v-if="selectedNode" shadow="hover">
          <template #header>
            <div class="card-header">
              <span>{{ selectedNode.title }}</span>
              <div>
                <el-button text type="primary" @click="editNode">编辑</el-button>
                <el-button text type="danger" @click="deleteNode">删除</el-button>
              </div>
            </div>
          </template>

          <el-descriptions :column="2" border>
            <el-descriptions-item label="层级">
              <el-tag :type="levelTagType(selectedNode.level)">{{ levelLabel(selectedNode.level) }}</el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="排序">{{ selectedNode.sort_order }}</el-descriptions-item>
            <el-descriptions-item label="张力目标">{{ selectedNode.tension_target || '-' }}</el-descriptions-item>
            <el-descriptions-item label="预估字数">{{ selectedNode.estimated_words || '-' }}</el-descriptions-item>
          </el-descriptions>

          <h4 style="margin: 16px 0 8px; color: #409eff;">内容概要</h4>
          <p class="text-content">{{ selectedNode.content || '暂无' }}</p>

          <h4 style="margin: 16px 0 8px; color: #409eff;">关键事件</h4>
          <p class="text-content">{{ selectedNode.key_events || '暂无' }}</p>

          <h4 style="margin: 16px 0 8px; color: #409eff;">涉及角色</h4>
          <div v-if="selectedNode.involved_characters?.length">
            <el-tag v-for="c in selectedNode.involved_characters" :key="c" style="margin: 2px;">{{ c }}</el-tag>
          </div>
          <span v-else class="text-content">暂无</span>
        </el-card>

        <!-- Tension Curve -->
        <el-card shadow="hover" style="margin-top: 20px;">
          <template #header><span>张力曲线</span></template>
          <v-chart :option="tensionChartOption" style="height: 300px;" autoresize />
        </el-card>
      </el-col>
    </el-row>

    <!-- Create/Edit Dialog -->
    <el-dialog v-model="showDialog" :title="isEdit ? '编辑大纲' : '创建大纲'" width="600px">
      <el-form :model="form" label-position="top">
        <el-form-item label="标题" required>
          <el-input v-model="form.title" />
        </el-form-item>
        <el-row :gutter="16">
          <el-col :span="8">
            <el-form-item label="层级">
              <el-select v-model="form.level" style="width: 100%;">
                <el-option label="宏观" value="macro" />
                <el-option label="中观" value="meso" />
                <el-option label="微观" value="micro" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="排序">
              <el-input-number v-model="form.sort_order" :min="0" style="width: 100%;" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="张力目标 (0-10)">
              <el-input-number v-model="form.tension_target" :min="0" :max="10" :step="0.5" style="width: 100%;" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="父节点">
          <el-select v-model="form.parent_id" clearable style="width: 100%;" placeholder="无（顶级）">
            <el-option v-for="o in flatOutlines" :key="o.id" :label="o.title" :value="o.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="内容概要">
          <el-input v-model="form.content" type="textarea" :rows="4" />
        </el-form-item>
        <el-form-item label="关键事件">
          <el-input v-model="form.key_events" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item label="预估字数">
          <el-input-number v-model="form.estimated_words" :min="0" :step="500" style="width: 100%;" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showDialog = false">取消</el-button>
        <el-button type="primary" @click="saveOutline" :loading="saving">{{ isEdit ? '保存' : '创建' }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { outlineApi } from '@/api'
import VChart from 'vue-echarts'

const route = useRoute()
const projectId = route.params.projectId as string

const outlines = ref<any[]>([])
const selectedNode = ref<any>(null)
const showDialog = ref(false)
const isEdit = ref(false)
const saving = ref(false)
const viewLevel = ref('all')

const form = ref({
  title: '', level: 'macro', sort_order: 0, parent_id: '',
  content: '', key_events: '', tension_target: 5, estimated_words: 0,
  involved_characters: [] as string[],
})

const flatOutlines = computed(() => outlines.value)

const treeData = computed(() => {
  const items = viewLevel.value === 'all'
    ? outlines.value
    : outlines.value.filter(o => o.level === viewLevel.value)

  const map = new Map<string, any>()
  const roots: any[] = []
  items.forEach(o => map.set(o.id, { ...o, children: [] }))
  items.forEach(o => {
    const node = map.get(o.id)!
    if (o.parent_id && map.has(o.parent_id)) {
      map.get(o.parent_id)!.children.push(node)
    } else {
      roots.push(node)
    }
  })
  return roots.sort((a, b) => a.sort_order - b.sort_order)
})

const tensionChartOption = computed(() => {
  const sorted = [...outlines.value]
    .filter(o => o.level === 'meso' || o.level === 'micro')
    .sort((a, b) => a.sort_order - b.sort_order)
  return {
    backgroundColor: 'transparent',
    xAxis: {
      type: 'category',
      data: sorted.map(o => o.title?.substring(0, 8) || ''),
      axisLabel: { color: '#888', rotate: 30 },
    },
    yAxis: {
      type: 'value',
      max: 10,
      axisLabel: { color: '#888' },
    },
    series: [{
      type: 'line',
      data: sorted.map(o => o.tension_target || 0),
      smooth: true,
      areaStyle: { opacity: 0.3, color: 'rgba(64,158,255,0.3)' },
      lineStyle: { color: '#409eff' },
      itemStyle: { color: '#409eff' },
    }],
    tooltip: { trigger: 'axis' },
    grid: { top: 20, right: 20, bottom: 50, left: 40 },
  }
})

function levelTagType(level: string) {
  return level === 'macro' ? 'danger' : level === 'meso' ? 'warning' : 'success'
}

function levelLabel(level: string) {
  return level === 'macro' ? '宏观' : level === 'meso' ? '中观' : '微观'
}

onMounted(fetchOutlines)

async function fetchOutlines() {
  try {
    const res = await outlineApi.list(projectId)
    outlines.value = res.data.data || []
  } catch { /* empty */ }
}

function onNodeClick(data: any) {
  selectedNode.value = data
}

function showCreate() {
  isEdit.value = false
  form.value = {
    title: '', level: 'macro', sort_order: outlines.value.length,
    parent_id: '', content: '', key_events: '', tension_target: 5,
    estimated_words: 0, involved_characters: [],
  }
  showDialog.value = true
}

function editNode() {
  if (!selectedNode.value) return
  isEdit.value = true
  form.value = { ...selectedNode.value }
  showDialog.value = true
}

async function saveOutline() {
  if (!form.value.title) { ElMessage.warning('请填写标题'); return }
  saving.value = true
  try {
    if (isEdit.value) {
      await outlineApi.update(projectId, selectedNode.value.id, form.value)
      ElMessage.success('大纲已更新')
    } else {
      await outlineApi.create(projectId, form.value)
      ElMessage.success('大纲已创建')
    }
    showDialog.value = false
    await fetchOutlines()
  } finally {
    saving.value = false
  }
}

async function deleteNode() {
  await ElMessageBox.confirm('确认删除该大纲节点？', '删除', { type: 'warning' })
  try {
    await outlineApi.delete(projectId, selectedNode.value.id)
    selectedNode.value = null
    ElMessage.success('已删除')
    await fetchOutlines()
  } catch {
    ElMessage.error('删除失败')
  }
}
</script>

<style scoped>
.outline { max-width: 1400px; margin: 0 auto; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; }
.page-header h1 { font-size: 24px; color: #e0e0e0; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.tree-node { display: flex; align-items: center; gap: 8px; }
.node-title { color: #e0e0e0; }
.tension { color: #e6a23c; font-size: 12px; }
.text-content { color: #b0b0c0; line-height: 1.8; white-space: pre-wrap; }
</style>
