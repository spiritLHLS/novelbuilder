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
                <el-button text type="primary" @click="openEditDialog">编辑</el-button>
                <el-button text type="danger" @click="deleteChar">删除</el-button>
              </div>
            </div>
          </template>

          <template v-if="true">
            <el-descriptions :column="2" border>
              <el-descriptions-item label="名称">{{ selected.name }}</el-descriptions-item>
              <el-descriptions-item label="角色定位">{{ selected.role_type }}</el-descriptions-item>
              <el-descriptions-item label="年龄">{{ selected.profile?.age || '-' }}</el-descriptions-item>
              <el-descriptions-item label="性别">{{ selected.profile?.gender || '-' }}</el-descriptions-item>
            </el-descriptions>
            <h4 style="margin: 16px 0 8px; color: #409eff;">背景故事</h4>
            <div class="text-content rich-text" v-html="renderRichText(selected.profile?.backstory || '暂无')"></div>
            <h4 style="margin: 16px 0 8px; color: #409eff;">性格特征</h4>
            <div v-if="selected.profile?.personality_traits?.length">
              <el-tag v-for="t in selected.profile.personality_traits" :key="t" style="margin: 2px;">{{ t }}</el-tag>
            </div>
            <h4 style="margin: 16px 0 8px; color: #409eff;">动机</h4>
            <div class="text-content rich-text" v-html="renderRichText(selected.profile?.motivation || '暂无')"></div>
            <h4 style="margin: 16px 0 8px; color: #409eff;">成长弧线</h4>
            <div class="text-content rich-text" v-html="renderRichText(selected.profile?.growth_arc || '暂无')"></div>
            <h4 style="margin: 16px 0 8px; color: #409eff;">关系网络</h4>
            <div v-if="selected.profile?.relationships">
              <div v-for="(rel, name) in selected.profile.relationships" :key="name" class="rel-item">
                <strong>{{ name }}</strong>
                <div class="rich-text" v-html="renderRichText(rel)"></div>
              </div>
            </div>
          </template>
        </el-card>

        <!-- Relationship Graph -->
        <el-card shadow="hover" style="margin-top: 20px;">
          <template #header>
            <div class="card-header graph-header">
              <div class="graph-title-group">
                <span>角色关系图谱</span>
                <span v-if="maxTimelineChapter > 1" class="graph-subtitle">当前显示到第 {{ timelineChapter }} 章</span>
              </div>
              <div class="graph-controls">
                <el-button size="small" @click="graphFit">适应窗口</el-button>
                <el-button size="small" @click="graphZoomIn">放大</el-button>
                <el-button size="small" @click="graphZoomOut">缩小</el-button>
                <el-button size="small" @click="graphRelayout">重新布局</el-button>
              </div>
            </div>
          </template>
          <div v-if="maxTimelineChapter > 1" class="graph-timeline">
            <div class="graph-timeline-labels">
              <span>开篇</span>
              <span>第 {{ timelineChapter }} 章</span>
              <span>第 {{ maxTimelineChapter }} 章</span>
            </div>
            <el-slider v-model="timelineChapter" :min="1" :max="maxTimelineChapter" :step="1" show-stops />
            <div class="graph-timeline-note">关系会按章节首次出现顺序逐步展开，避免整书静态关系一次性挤在一起。</div>
          </div>
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

    <!-- Edit Dialog -->
    <el-dialog v-model="showEditDlg" title="编辑角色" width="600px" :close-on-click-modal="false">
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
        <el-form-item label="关系网络">
          <div class="rel-editor">
            <div v-for="(rel, idx) in editForm.relationship_list" :key="idx" class="rel-editor-row">
              <el-input v-model="rel.name" placeholder="角色名" style="width:140px;margin-right:8px" />
              <el-input v-model="rel.desc" placeholder="关系描述" style="flex:1;margin-right:8px" />
              <el-button type="danger" text @click="editForm.relationship_list.splice(idx,1)">删除</el-button>
            </div>
            <el-button size="small" @click="editForm.relationship_list.push({name:'',desc:''})">+ 添加关系</el-button>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showEditDlg = false">取消</el-button>
        <el-button type="primary" @click="saveEdit">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, nextTick, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { characterApi, outlineApi, charInteractionApi, chapterApi } from '@/api'
import cytoscape from 'cytoscape'
import { renderRichText } from '@/utils/richText'

const route = useRoute()
const projectId = route.params.projectId as string
const cyContainer = ref<HTMLElement | null>(null)
let cy: any = null

const characters = ref<any[]>([])
const outlines = ref<any[]>([])
const interactions = ref<any[]>([])
const chapters = ref<any[]>([])
const selected = ref<any>(null)
const editMode = ref(false)
const showCreateDialog = ref(false)
const showEditDlg = ref(false)
const creating = ref(false)
const timelineChapter = ref(1)
const maxTimelineChapter = ref(1)

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
    const [charRes, outlineRes, interactionRes, chapterRes] = await Promise.all([
      characterApi.list(projectId),
      outlineApi.list(projectId).catch(() => ({ data: { data: [] } })),
      charInteractionApi.list(projectId).catch(() => ({ data: { data: [] } })),
      chapterApi.list(projectId).catch(() => ({ data: { data: [] } })),
    ])
    characters.value = charRes.data.data || []
    outlines.value = outlineRes.data.data || []
    interactions.value = interactionRes.data.data || []
    chapters.value = chapterRes.data.data || []
    maxTimelineChapter.value = computeTimelineMax()
    timelineChapter.value = Math.max(1, maxTimelineChapter.value)
    buildGraph()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.error ?? '加载角色列表失败')
  }
}

function extractOutlineCharacters(outline: any): string[] {
  const content = outline?.content && typeof outline.content === 'object' ? outline.content : {}
  if (Array.isArray(content.involved_characters)) {
    return content.involved_characters.map((item: unknown) => String(item).trim()).filter(Boolean)
  }
  if (Array.isArray(content.characters)) {
    return content.characters.map((item: unknown) => String(item).trim()).filter(Boolean)
  }
  if (typeof content.characters === 'string') {
    return content.characters.split(/[，,、]/).map((item: string) => item.trim()).filter(Boolean)
  }
  return []
}

function computeTimelineMax(): number {
  let maxChapter = 1
  // Use actual chapter numbers from generated chapters (most authoritative source)
  chapters.value.forEach((ch: any) => {
    const num = Number(ch?.chapter_num) || 0
    if (num > maxChapter) maxChapter = num
  })
  outlines.value.forEach((outline: any, index: number) => {
    const chapterNum = Number(outline?.order_num) || index + 1
    if (chapterNum > maxChapter) maxChapter = chapterNum
  })
  interactions.value.forEach((interaction: any) => {
    const firstMeet = Number(interaction?.first_meet_chapter) || 0
    const lastInteract = Number(interaction?.last_interact_chapter) || 0
    if (firstMeet > maxChapter) maxChapter = firstMeet
    if (lastInteract > maxChapter) maxChapter = lastInteract
  })
  return Math.max(1, maxChapter)
}

function computeFirstAppearanceMap(): Map<string, number> {
  const result = new Map<string, number>()

  characters.value.forEach((character: any) => {
    const firstAppearance = Number(character?.profile?.first_appearance_chapter)
    if (Number.isFinite(firstAppearance) && firstAppearance > 0) {
      result.set(character.name, firstAppearance)
    }
  })

  outlines.value
    .slice()
    .sort((a: any, b: any) => (Number(a?.order_num) || 0) - (Number(b?.order_num) || 0))
    .forEach((outline: any, index: number) => {
      const chapterNum = Number(outline?.order_num) || index + 1
      extractOutlineCharacters(outline).forEach((name) => {
        const current = result.get(name)
        if (!current || chapterNum < current) {
          result.set(name, chapterNum)
        }
      })
    })

  const idToName = new Map(characters.value.map((character: any) => [character.id, character.name]))
  interactions.value.forEach((interaction: any) => {
    const firstMeet = Number(interaction?.first_meet_chapter) || 0
    if (!firstMeet) return
    const names = [interaction?.char_a_name, interaction?.char_b_name]
      .map((name: unknown, idx: number) => String(name || idToName.get(idx === 0 ? interaction?.char_a_id : interaction?.char_b_id) || '').trim())
      .filter(Boolean)
    names.forEach((name) => {
      const current = result.get(name)
      if (!current || firstMeet < current) {
        result.set(name, firstMeet)
      }
    })
  })

  return result
}

function selectChar(c: any) {
  selected.value = c
  editMode.value = false
}

function openEditDialog() {
  const p = selected.value?.profile || {}
  // Convert relationships object {name: desc} → [{name, desc}] for the editor
  const relObj = p.relationships || {}
  const relationship_list = Object.entries(relObj).map(([name, desc]) => ({ name, desc: String(desc) }))
  editForm.value = {
    name: selected.value.name,
    role_type: selected.value.role_type,
    backstory: p.backstory || '',
    age: p.age || '',
    gender: p.gender || '',
    motivation: p.motivation || '',
    growth_arc: p.growth_arc || '',
    personality_str: (p.personality_traits || []).join(', '),
    relationship_list,
  }
  showEditDlg.value = true
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
    // Convert [{name, desc}] → {name: desc} object, skipping empty entries
    const relationships: Record<string, string> = {}
    for (const rel of (editForm.value.relationship_list || [])) {
      if (rel.name.trim()) relationships[rel.name.trim()] = rel.desc.trim()
    }
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
        relationships,
      },
    })
    ElMessage.success('角色已更新')
    showEditDlg.value = false
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
    const visibleChapter = Math.max(1, timelineChapter.value || maxTimelineChapter.value || 1)
    const firstAppearanceMap = computeFirstAppearanceMap()
    const charById = new Map(characters.value.map((character: any) => [character.id, character]))
    const charByName = new Map(characters.value.map((character: any) => [character.name, character]))

    const visibleCharacters = characters.value.filter((character: any) => {
      if (maxTimelineChapter.value <= 1) return true
      const firstAppearance = firstAppearanceMap.get(character.name) || Number(character?.profile?.first_appearance_chapter) || 1
      return firstAppearance <= visibleChapter
    })
    const visibleIds = new Set(visibleCharacters.map((character: any) => character.id))

    const relationMap = new Map<string, { source: string; target: string; labels: Set<string> }>()

    interactions.value.forEach((interaction: any) => {
      const firstMeet = Number(interaction?.first_meet_chapter) || 1
      if (maxTimelineChapter.value > 1 && firstMeet > visibleChapter) return
      const source = String(interaction?.char_a_id || '')
      const target = String(interaction?.char_b_id || '')
      if (!source || !target || !visibleIds.has(source) || !visibleIds.has(target)) return
      const key = `${source}:${target}`
      const entry = relationMap.get(key) || { source, target, labels: new Set<string>() }
      const relationText = String(interaction?.relationship || '').trim()
      if (relationText) {
        entry.labels.add(relationText)
      }
      relationMap.set(key, entry)
    })

    visibleCharacters.forEach((character: any) => {
      if (!character.profile?.relationships) return
      Object.entries(character.profile.relationships).forEach(([targetName, rel]) => {
        const target = charByName.get(targetName)
        if (!target || !visibleIds.has(target.id)) return
        const relationText = String(rel || '').trim()
        const firstMeet = Math.min(
          firstAppearanceMap.get(character.name) || Number(character?.profile?.first_appearance_chapter) || 1,
          firstAppearanceMap.get(target.name) || Number(target?.profile?.first_appearance_chapter) || 1,
        )
        if (maxTimelineChapter.value > 1 && firstMeet > visibleChapter) return
        const [source, destination] = [character.id, target.id].sort()
        const key = `${source}:${destination}`
        const entry = relationMap.get(key) || { source, target: destination, labels: new Set<string>() }
        if (relationText) {
          entry.labels.add(relationText)
        }
        relationMap.set(key, entry)
      })
    })

    const relationCountById = new Map<string, number>()
    relationMap.forEach((edge) => {
      relationCountById.set(edge.source, (relationCountById.get(edge.source) || 0) + 1)
      relationCountById.set(edge.target, (relationCountById.get(edge.target) || 0) + 1)
    })

    const nodes = visibleCharacters.map((character: any) => ({
      data: {
        id: character.id,
        label: character.name,
        role: character.role_type,
        weight: Math.max(1, relationCountById.get(character.id) || 0),
      },
    }))

    const edges = Array.from(relationMap.values()).map((edge, index) => ({
      data: {
        id: `edge-${index}`,
        source: edge.source,
        target: edge.target,
        label: Array.from(edge.labels).join(' / '),
      },
    }))

    const layoutOptions = {
      name: 'cose',
      animate: false,
      fit: true,
      padding: nodes.length > 12 ? 120 : 80,
      nodeDimensionsIncludeLabels: true,
      componentSpacing: nodes.length > 15 ? 250 : nodes.length > 8 ? 180 : 130,
      nodeRepulsion: (node: any) => {
        const degree = node.connectedEdges().length
        // Much higher repulsion to guarantee non-overlap
        if (degree > 5) return 80000
        if (degree > 2) return 55000
        return 38000
      },
      idealEdgeLength: () => {
        if (edges.length > 20) return 320
        if (edges.length > 10) return 260
        return 200
      },
      edgeElasticity: () => 100,
      gravity: nodes.length > 15 ? 0.15 : 0.1,
      numIter: nodes.length > 15 ? 3000 : 2000,
      nodeOverlap: 999999, // Maximum overlap penalty — forces strict non-overlap
      randomize: true,
      initialTemp: 500,
      coolingFactor: 0.97,
      minTemp: 0.5,
    }

    if (cy) cy.destroy()
    cy = cytoscape({
      container: cyContainer.value,
      elements: { nodes, edges },
      style: [
        {
          selector: 'node',
          style: {
            label: 'data(label)',
            shape: 'roundrectangle',
            'background-color': '#315b7a',
            'background-opacity': 0.96,
            color: '#f5f8ff',
            'text-valign': 'center',
            'text-halign': 'center',
            'font-size': 12,
            'font-weight': '500',
            'text-wrap': 'wrap',
            'text-max-width': 96,
            padding: '14px',
            width: 'label',
            height: 'label',
            'border-width': 1.5,
            'border-color': 'rgba(255,255,255,0.18)',
            'shadow-blur': 18,
            'shadow-color': 'rgba(0, 0, 0, 0.28)',
            'shadow-opacity': 0.35,
            'shadow-offset-x': 0,
            'shadow-offset-y': 8,
          },
        },
        {
          selector: 'node[role="protagonist"]',
          style: {
            'background-color': '#9d3d52',
            'border-color': 'rgba(255,210,218,0.45)',
            'border-width': 2.5,
          },
        },
        {
          selector: 'node[role="antagonist"]',
          style: {
            'background-color': '#8f5a24',
            'border-color': 'rgba(255,223,168,0.4)',
          },
        },
        {
          selector: 'node[role="mentor"]',
          style: { 'background-color': '#2f7a62' },
        },
        {
          selector: 'node[role="minor"]',
          style: { 'background-color': '#5e6478', color: '#eef3ff' },
        },
        {
          selector: 'node:selected',
          style: {
            'border-color': '#ffffff',
            'border-width': 3,
            'background-opacity': 1,
            'shadow-opacity': 0.5,
          },
        },
        {
          selector: 'edge',
          style: {
            label: 'data(label)',
            'line-color': 'rgba(132, 170, 214, 0.46)',
            'line-style': 'solid',
            'curve-style': 'bezier',
            'target-arrow-shape': 'none',
            'source-arrow-shape': 'none',
            'font-size': 9,
            color: 'rgba(222,232,245,0.9)',
            'text-rotation': 'autorotate',
            'text-wrap': 'wrap',
            'text-max-width': 100,
            'text-background-color': 'rgba(14,20,32,0.85)',
            'text-background-opacity': 1,
            'text-background-padding': '3px',
            'text-background-shape': 'roundrectangle',
            'text-border-opacity': 0,
            'text-margin-y': -6,
            width: 1.5,
          },
        },
        {
          selector: 'edge:selected',
          style: { 'line-color': 'rgba(223,236,255,0.95)', width: 3 },
        },
      ],
      layout: layoutOptions,
      minZoom: 0.2,
      maxZoom: 3,
      wheelSensitivity: 0.3,
    })

    cy.ready(() => {
      cy.fit(undefined, nodes.length > 12 ? 100 : 80)
    })

    // Highlight neighbors on tap
    cy.on('tap', 'node', (evt: any) => {
      cy.elements().style({ opacity: 0.2 })
      const node = evt.target
      const neighborhood = node.neighborhood().add(node)
      neighborhood.style({ opacity: 1 })
      const matched = characters.value.find((item: any) => item.id === node.id())
      if (matched) {
        selected.value = matched
      }
    })
    cy.on('tap', (evt: any) => {
      if (evt.target === cy) {
        cy.elements().style({ opacity: 1 })
      }
    })
  })
}

function graphFit() {
  if (cy) cy.fit(undefined, 50)
}
function graphZoomIn() {
  if (cy) cy.zoom({ level: cy.zoom() * 1.3, renderedPosition: { x: cy.width() / 2, y: cy.height() / 2 } })
}
function graphZoomOut() {
  if (cy) cy.zoom({ level: cy.zoom() / 1.3, renderedPosition: { x: cy.width() / 2, y: cy.height() / 2 } })
}
function graphRelayout() {
  if (cy) {
    const visibleNodeCount = cy.nodes().length
    const visibleEdgeCount = cy.edges().length
    cy.layout({
      name: 'cose',
      animate: true,
      animationDuration: 600,
      fit: true,
      padding: visibleNodeCount > 12 ? 120 : 80,
      nodeDimensionsIncludeLabels: true,
      componentSpacing: visibleNodeCount > 15 ? 250 : visibleNodeCount > 8 ? 180 : 130,
      nodeRepulsion: (node: any) => {
        const degree = node.connectedEdges().length
        if (degree > 5) return 80000
        if (degree > 2) return 55000
        return 38000
      },
      idealEdgeLength: () => {
        if (visibleEdgeCount > 20) return 320
        if (visibleEdgeCount > 10) return 260
        return 200
      },
      edgeElasticity: () => 100,
      gravity: visibleNodeCount > 15 ? 0.15 : 0.1,
      numIter: visibleNodeCount > 15 ? 3000 : 2000,
      nodeOverlap: 999999,
      randomize: true,
      initialTemp: 500,
      coolingFactor: 0.97,
      minTemp: 0.5,
    }).run()
  }
}

watch([characters, outlines, interactions, chapters, timelineChapter], buildGraph, { deep: true })
</script>

<style scoped>
.characters { max-width: 1400px; margin: 0 auto; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; }
.page-header h1 { font-size: 24px; color: #e0e0e0; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.graph-header { gap: 16px; align-items: flex-start; }
.graph-title-group { display: flex; flex-direction: column; gap: 4px; }
.graph-subtitle { color: var(--nb-text-secondary); font-size: 12px; }
.char-list-card :deep(.el-card__body) { max-height: 60vh; overflow-y: auto; }
.char-item { padding: 12px; cursor: pointer; border-radius: 8px; display: flex; justify-content: space-between; align-items: center; transition: background 0.2s; }
.char-item:hover { background: rgba(64,158,255,0.1); }
.char-item.active { background: rgba(64,158,255,0.2); }
.char-name { font-weight: 500; color: var(--nb-text-primary); }
.rel-editor { display: flex; flex-direction: column; gap: 8px; }
.rel-editor-row { display: flex; align-items: center; }
.rel-item { padding: 8px 0; color: var(--nb-text-primary); display: flex; flex-direction: column; gap: 4px; }
.text-content { color: var(--nb-text-secondary); line-height: 1.8; white-space: pre-wrap; }
.rich-text :deep(p) { margin: 0 0 6px; }
.rel-item { padding: 4px 0; color: var(--nb-text-secondary); }
.graph-timeline { margin-bottom: 16px; padding: 12px 14px; background: rgba(64, 158, 255, 0.08); border: 1px solid rgba(64, 158, 255, 0.18); border-radius: 10px; }
.graph-timeline-labels { display: flex; justify-content: space-between; color: var(--nb-text-secondary); font-size: 12px; margin-bottom: 8px; }
.graph-timeline-note { color: var(--nb-text-secondary); font-size: 12px; line-height: 1.5; margin-top: 8px; }
.cy-container { width: 100%; height: 600px; background: radial-gradient(circle at top, rgba(57,91,125,0.24), rgba(12,16,24,0.92) 60%); border: 1px solid var(--nb-card-border); border-radius: 8px; }
.graph-controls { display: flex; gap: 4px; }
</style>
