<template>
  <div class="characters">
    <div class="page-header">
      <h1>角色管理</h1>
      <el-button type="primary" @click="showCreate"><el-icon><Plus /></el-icon>创建角色</el-button>
    </div>

    <el-row :gutter="20">
      <!-- Character List -->
      <el-col :span="8">
        <el-card shadow="hover" class="char-list-card">
          <template #header><span>角色列表</span></template>
          <div v-for="c in characters" :key="c.id"
            class="char-item" :class="{ active: selected?.id === c.id }"
            @click="selectChar(c)">
            <div class="char-name">{{ c.name }}</div>
            <el-tag size="small" :type="roleTagType(c.role_type)">{{ c.role_type }}</el-tag>
          </div>
          <el-empty v-if="!characters.length" description="暂无角色" />
        </el-card>
      </el-col>

      <!-- Character Detail -->
      <el-col :span="16">
        <el-card v-if="selected" shadow="hover">
          <template #header>
            <div class="card-header">
              <span>{{ selected.name }} 详情</span>
              <div>
                <el-button text type="primary" @click="editMode = true">编辑</el-button>
                <el-button text type="danger" @click="deleteChar">删除</el-button>
              </div>
            </div>
          </template>

          <template v-if="!editMode">
            <el-descriptions :column="2" border>
              <el-descriptions-item label="名称">{{ selected.name }}</el-descriptions-item>
              <el-descriptions-item label="角色定位">{{ selected.role_type }}</el-descriptions-item>
              <el-descriptions-item label="年龄">{{ selected.profile?.age || '-' }}</el-descriptions-item>
              <el-descriptions-item label="性别">{{ selected.profile?.gender || '-' }}</el-descriptions-item>
            </el-descriptions>
            <h4 style="margin: 16px 0 8px; color: #409eff;">背景故事</h4>
            <p class="text-content">{{ selected.profile?.backstory || '暂无' }}</p>
            <h4 style="margin: 16px 0 8px; color: #409eff;">性格特征</h4>
            <div v-if="selected.profile?.personality_traits?.length">
              <el-tag v-for="t in selected.profile.personality_traits" :key="t" style="margin: 2px;">{{ t }}</el-tag>
            </div>
            <h4 style="margin: 16px 0 8px; color: #409eff;">动机</h4>
            <p class="text-content">{{ selected.profile?.motivation || '暂无' }}</p>
            <h4 style="margin: 16px 0 8px; color: #409eff;">成长弧线</h4>
            <p class="text-content">{{ selected.profile?.growth_arc || '暂无' }}</p>
            <h4 style="margin: 16px 0 8px; color: #409eff;">关系网络</h4>
            <div v-if="selected.profile?.relationships">
              <div v-for="(rel, name) in selected.profile.relationships" :key="name" class="rel-item">
                <strong>{{ name }}</strong>: {{ rel }}
              </div>
            </div>
          </template>

          <template v-else>
            <el-form :model="editForm" label-position="top">
              <el-row :gutter="16">
                <el-col :span="12">
                  <el-form-item label="名称"><el-input v-model="editForm.name" /></el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="角色定位">
                    <el-select v-model="editForm.role_type" style="width: 100%;">
                      <el-option label="主角" value="protagonist" />
                      <el-option label="配角" value="supporting" />
                      <el-option label="反派" value="antagonist" />
                      <el-option label="导师" value="mentor" />
                      <el-option label="龙套" value="minor" />
                    </el-select>
                  </el-form-item>
                </el-col>
              </el-row>
              <el-row :gutter="16">
                <el-col :span="12">
                  <el-form-item label="年龄"><el-input v-model="editForm.age" /></el-form-item>
                </el-col>
                <el-col :span="12">
                  <el-form-item label="性别"><el-input v-model="editForm.gender" /></el-form-item>
                </el-col>
              </el-row>
              <el-form-item label="背景故事">
                <el-input v-model="editForm.backstory" type="textarea" :rows="4" />
              </el-form-item>
              <el-form-item label="性格特征（逗号分隔）">
                <el-input v-model="editForm.personality_str" placeholder="勇敢, 固执, 善良" />
              </el-form-item>
              <el-form-item label="动机">
                <el-input v-model="editForm.motivation" type="textarea" :rows="2" />
              </el-form-item>
              <el-form-item label="成长弧线">
                <el-input v-model="editForm.growth_arc" type="textarea" :rows="2" />
              </el-form-item>
              <el-form-item>
                <el-button type="primary" @click="saveEdit">保存</el-button>
                <el-button @click="editMode = false">取消</el-button>
              </el-form-item>
            </el-form>
          </template>
        </el-card>

        <!-- Relationship Graph -->
        <el-card shadow="hover" style="margin-top: 20px;">
          <template #header><span>角色关系图谱</span></template>
          <div ref="cyContainer" class="cy-container"></div>
        </el-card>
      </el-col>
    </el-row>

    <!-- Create Dialog -->
    <el-dialog v-model="showCreateDialog" title="创建角色" width="600px">
      <el-form :model="createForm" label-position="top">
        <el-row :gutter="16">
          <el-col :span="12">
            <el-form-item label="名称" required><el-input v-model="createForm.name" /></el-form-item>
          </el-col>
          <el-col :span="12">
            <el-form-item label="角色定位">
              <el-select v-model="createForm.role" style="width: 100%;">
                <el-option label="主角" value="protagonist" />
                <el-option label="配角" value="supporting" />
                <el-option label="反派" value="antagonist" />
                <el-option label="导师" value="mentor" />
                <el-option label="龙套" value="minor" />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="背景故事">
          <el-input v-model="createForm.backstory" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item label="性格特征（逗号分隔）">
          <el-input v-model="createForm.personality_str" placeholder="勇敢, 固执, 善良" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">取消</el-button>
        <el-button type="primary" @click="createChar" :loading="creating">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, nextTick, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { characterApi } from '@/api'
import cytoscape from 'cytoscape'

const route = useRoute()
const projectId = route.params.projectId as string
const cyContainer = ref<HTMLElement | null>(null)
let cy: any = null

const characters = ref<any[]>([])
const selected = ref<any>(null)
const editMode = ref(false)
const showCreateDialog = ref(false)
const creating = ref(false)

const createForm = ref({ name: '', role: 'supporting', backstory: '', personality_str: '' })
const editForm = ref<any>({})

function roleTagType(role: string) {
  const map: Record<string, string> = {
    protagonist: 'danger', antagonist: 'warning', supporting: '', mentor: 'success', minor: 'info',
  }
  return map[role] || 'info'
}

onMounted(fetchChars)

async function fetchChars() {
  try {
    const res = await characterApi.list(projectId)
    characters.value = res.data.data || []
    buildGraph()
  } catch { /* empty */ }
}

function selectChar(c: any) {
  selected.value = c
  editMode.value = false
  const p = c.profile || {}
  editForm.value = {
    name: c.name,
    role_type: c.role_type,
    backstory: p.backstory || '',
    age: p.age || '',
    gender: p.gender || '',
    motivation: p.motivation || '',
    growth_arc: p.growth_arc || '',
    personality_str: (p.personality_traits || []).join(', '),
  }
}

function showCreate() {
  createForm.value = { name: '', role: 'supporting', backstory: '', personality_str: '' }
  showCreateDialog.value = true
}

async function createChar() {
  if (!createForm.value.name) { ElMessage.warning('请填写角色名称'); return }
  creating.value = true
  try {
    const personality_traits = createForm.value.personality_str.split(/[,，]/).map((s: string) => s.trim()).filter(Boolean)
    await characterApi.create(projectId, {
      name: createForm.value.name,
      role_type: createForm.value.role,
      profile: {
        backstory: createForm.value.backstory,
        personality_traits,
      },
    })
    ElMessage.success('角色已创建')
    showCreateDialog.value = false
    await fetchChars()
  } finally {
    creating.value = false
  }
}

async function saveEdit() {
  try {
    const personality_traits = editForm.value.personality_str.split(/[,，]/).map((s: string) => s.trim()).filter(Boolean)
    await characterApi.update(projectId, selected.value.id, {
      name: editForm.value.name,
      role_type: editForm.value.role_type,
      profile: {
        backstory: editForm.value.backstory,
        age: editForm.value.age,
        gender: editForm.value.gender,
        motivation: editForm.value.motivation,
        growth_arc: editForm.value.growth_arc,
        personality_traits,
      },
    })
    ElMessage.success('角色已更新')
    editMode.value = false
    await fetchChars()
    const updated = characters.value.find(c => c.id === selected.value.id)
    if (updated) selected.value = updated
  } catch {
    ElMessage.error('保存失败')
  }
}

async function deleteChar() {
  await ElMessageBox.confirm('确认删除该角色？', '删除', { type: 'warning' })
  try {
    await characterApi.delete(projectId, selected.value.id)
    selected.value = null
    ElMessage.success('角色已删除')
    await fetchChars()
  } catch {
    ElMessage.error('删除失败')
  }
}

function buildGraph() {
  nextTick(() => {
    if (!cyContainer.value) return
    const nodes = characters.value.map(c => ({
      data: { id: c.id, label: c.name, role: c.role },
    }))
    const edges: any[] = []
    characters.value.forEach(c => {
      if (c.relationships) {
        Object.entries(c.relationships).forEach(([targetName, rel]) => {
          const target = characters.value.find(t => t.name === targetName)
          if (target) {
            edges.push({
              data: { source: c.id, target: target.id, label: rel as string },
            })
          }
        })
      }
    })

    if (cy) cy.destroy()
    cy = cytoscape({
      container: cyContainer.value,
      elements: { nodes, edges },
      style: [
        {
          selector: 'node',
          style: {
            label: 'data(label)',
            'background-color': '#409eff',
            color: '#e0e0e0',
            'text-valign': 'bottom',
            'text-margin-y': 8,
            'font-size': 12,
            width: 40,
            height: 40,
          },
        },
        {
          selector: 'node[role="protagonist"]',
          style: { 'background-color': '#f56c6c', width: 50, height: 50 },
        },
        {
          selector: 'node[role="antagonist"]',
          style: { 'background-color': '#e6a23c' },
        },
        {
          selector: 'edge',
          style: {
            label: 'data(label)',
            'line-color': '#555',
            'target-arrow-color': '#555',
            'target-arrow-shape': 'triangle',
            'curve-style': 'bezier',
            'font-size': 10,
            color: '#888',
          },
        },
      ],
      layout: { name: 'cose', animate: false },
    })
  })
}

watch(() => characters.value, buildGraph, { deep: true })
</script>

<style scoped>
.characters { max-width: 1400px; margin: 0 auto; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; }
.page-header h1 { font-size: 24px; color: #e0e0e0; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.char-list-card :deep(.el-card__body) { max-height: 60vh; overflow-y: auto; }
.char-item { padding: 12px; cursor: pointer; border-radius: 8px; display: flex; justify-content: space-between; align-items: center; transition: background 0.2s; }
.char-item:hover { background: rgba(64,158,255,0.1); }
.char-item.active { background: rgba(64,158,255,0.2); }
.char-name { font-weight: 500; color: var(--nb-text-primary); }
.text-content { color: var(--nb-text-secondary); line-height: 1.8; white-space: pre-wrap; }
.rel-item { padding: 4px 0; color: var(--nb-text-secondary); }
.cy-container { width: 100%; height: 400px; background: var(--nb-card-bg); border: 1px solid var(--nb-card-border); border-radius: 8px; }
</style>
