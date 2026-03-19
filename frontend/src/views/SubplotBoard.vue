<template>
  <div class="p-6 max-w-6xl mx-auto space-y-6">
    <div class="flex items-center justify-between">
      <h1 class="text-2xl font-bold text-gray-900">🎬 支线进度板</h1>
      <button @click="showCreate = true"
        class="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 text-sm">
        ＋ 新增支线
      </button>
    </div>

    <div v-if="error" class="p-4 bg-red-50 border border-red-200 rounded-lg text-red-700 text-sm">{{ error }}</div>

    <!-- Kanban columns by line_label -->
    <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4" v-if="grouped">
      <div v-for="label in lineLabels" :key="label" class="space-y-3">
        <div class="flex items-center gap-2 font-semibold text-gray-700">
          <span class="w-7 h-7 rounded-full flex items-center justify-center text-white text-xs"
            :class="labelColor(label)">{{ label }}</span>
          <span>{{ label }} 线</span>
          <span class="ml-auto text-xs text-gray-400">{{ (grouped[label] || []).length }} 条</span>
        </div>
        <div v-if="!grouped[label]?.length" class="text-xs text-gray-400 italic py-4 text-center border border-dashed rounded-xl">暂无支线</div>
        <div v-for="sp in grouped[label]" :key="sp.id"
          class="bg-white border border-gray-200 rounded-xl p-4 space-y-2 cursor-pointer hover:shadow-md transition-shadow"
          @click="select(sp)">
          <div class="flex items-start justify-between gap-2">
            <span class="font-medium text-gray-800 text-sm">{{ sp.title }}</span>
            <span class="px-1.5 py-0.5 rounded-md text-xs font-medium shrink-0"
              :class="statusColor(sp.status)">{{ statusLabel(sp.status) }}</span>
          </div>
          <p v-if="sp.description" class="text-xs text-gray-500 line-clamp-2">{{ sp.description }}</p>
          <div class="flex items-center gap-2 text-xs text-gray-400">
            <span v-if="sp.start_chapter">第{{ sp.start_chapter }}章起</span>
            <span v-if="sp.resolve_chapter"> → 第{{ sp.resolve_chapter }}章结</span>
            <span v-if="sp.tags?.length" class="ml-auto flex gap-1">
              <span v-for="t in sp.tags.slice(0,2)" :key="t"
                class="bg-gray-100 px-1 rounded">{{ t }}</span>
            </span>
          </div>
        </div>
      </div>
    </div>

    <!-- Create Modal -->
    <div v-if="showCreate" class="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div class="bg-white rounded-2xl shadow-xl w-full max-w-md p-6 space-y-4">
        <h2 class="text-lg font-semibold">新增支线</h2>
        <div class="space-y-3">
          <div>
            <label class="text-xs text-gray-500 mb-1 block">标题 *</label>
            <input v-model="form.title" class="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" placeholder="支线标题" />
          </div>
          <div class="grid grid-cols-2 gap-3">
            <div>
              <label class="text-xs text-gray-500 mb-1 block">线标签</label>
              <select v-model="form.line_label" class="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500">
                <option v-for="l in ['A','B','C','D']" :key="l">{{ l }}</option>
              </select>
            </div>
            <div>
              <label class="text-xs text-gray-500 mb-1 block">优先级</label>
              <select v-model="form.priority" class="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500">
                <option value="1">高</option>
                <option value="3">中</option>
                <option value="5">低</option>
              </select>
            </div>
          </div>
          <div>
            <label class="text-xs text-gray-500 mb-1 block">描述</label>
            <textarea v-model="form.description" rows="3" class="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 resize-none" placeholder="支线简介"></textarea>
          </div>
          <div>
            <label class="text-xs text-gray-500 mb-1 block">起始章节（可选）</label>
            <input v-model.number="form.start_chapter" type="number" min="1" class="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
          </div>
        </div>
        <div class="flex gap-3 pt-2">
          <button @click="showCreate = false" class="flex-1 border rounded-lg py-2 text-sm text-gray-600 hover:bg-gray-50">取消</button>
          <button @click="create" :disabled="!form.title || creating" class="flex-1 bg-blue-600 text-white rounded-lg py-2 text-sm hover:bg-blue-700 disabled:opacity-50">
            {{ creating ? '创建中…' : '创建' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Detail Panel -->
    <div v-if="selected" class="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div class="bg-white rounded-2xl shadow-xl w-full max-w-lg p-6 space-y-4 max-h-[90vh] overflow-y-auto">
        <div class="flex items-center justify-between">
          <h2 class="text-lg font-semibold">{{ selected.title }}</h2>
          <button @click="selected = null" class="text-gray-400 hover:text-gray-600 text-2xl leading-none">&times;</button>
        </div>

        <!-- Status update -->
        <div class="flex flex-wrap gap-2">
          <button v-for="s in statuses" :key="s.value"
            @click="updateStatus(s.value)"
            :class="[selected.status === s.value ? 'ring-2 ring-blue-500' : '', statusColor(s.value), 'px-3 py-1 rounded-full text-xs font-medium cursor-pointer']">
            {{ s.label }}
          </button>
        </div>

        <!-- Checkpoints -->
        <div class="space-y-3">
          <h3 class="font-medium text-gray-700 text-sm">进度检查点</h3>
          <div v-if="!checkpoints.length" class="text-xs text-gray-400 italic">暂无检查点</div>
          <div v-for="cp in checkpoints" :key="cp.id"
            class="flex items-center gap-3 border-b pb-2">
            <div class="flex-1">
              <div class="text-sm text-gray-800">{{ cp.note || '—' }}</div>
              <div class="text-xs text-gray-400">第{{ cp.chapter_num }}章 · 进度 {{ cp.progress }}%</div>
            </div>
            <div class="w-16 bg-gray-100 rounded-full h-2">
              <div class="bg-blue-500 h-2 rounded-full" :style="{ width: cp.progress + '%' }"></div>
            </div>
          </div>

          <!-- Add checkpoint -->
          <div class="border-t pt-3 space-y-2">
            <div class="grid grid-cols-2 gap-2">
              <div>
                <label class="text-xs text-gray-500">章节号</label>
                <input v-model.number="cpForm.chapter_num" type="number" min="1"
                  class="w-full border rounded-lg px-2 py-1 text-sm mt-0.5 focus:outline-none focus:ring-1 focus:ring-blue-500" />
              </div>
              <div>
                <label class="text-xs text-gray-500">进度 (%)</label>
                <input v-model.number="cpForm.progress" type="number" min="0" max="100"
                  class="w-full border rounded-lg px-2 py-1 text-sm mt-0.5 focus:outline-none focus:ring-1 focus:ring-blue-500" />
              </div>
            </div>
            <input v-model="cpForm.note" placeholder="备注"
              class="w-full border rounded-lg px-2 py-1 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500" />
            <div class="flex gap-2">
              <button @click="addCheckpoint" :disabled="!cpForm.chapter_num"
                class="px-3 py-1 bg-blue-600 text-white text-xs rounded-lg hover:bg-blue-700 disabled:opacity-50">
                添加检查点
              </button>
              <button @click="deleteSubplot" class="px-3 py-1 border border-red-300 text-red-600 text-xs rounded-lg hover:bg-red-50 ml-auto">
                删除支线
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { subplotApi } from '@/api'

const route = useRoute()
const projectId = route.params.projectId as string

const subplots = ref<any[]>([])
const checkpoints = ref<any[]>([])
const selected = ref<any>(null)
const showCreate = ref(false)
const creating = ref(false)
const error = ref('')

const form = ref({ title: '', line_label: 'A', priority: 3, description: '', start_chapter: null as number | null })
const cpForm = ref({ chapter_num: null as number | null, progress: 0, note: '' })

const lineLabels = ['A', 'B', 'C', 'D']
const statuses = [
  { value: 'active',   label: '进行中' },
  { value: 'paused',   label: '暂停' },
  { value: 'stalled',  label: '停滞' },
  { value: 'resolved', label: '完结' },
]

const grouped = computed(() => {
  const g: Record<string, any[]> = {}
  for (const l of lineLabels) g[l] = []
  for (const sp of subplots.value) {
    const k = sp.line_label in g ? sp.line_label : 'A'
    g[k].push(sp)
  }
  return g
})

function labelColor(l: string) {
  return { A: 'bg-blue-500', B: 'bg-purple-500', C: 'bg-green-500', D: 'bg-orange-500' }[l] || 'bg-gray-500'
}

function statusColor(s: string) {
  return {
    active:   'bg-blue-100 text-blue-700',
    paused:   'bg-yellow-100 text-yellow-700',
    stalled:  'bg-red-100 text-red-700',
    resolved: 'bg-green-100 text-green-700',
  }[s] || 'bg-gray-100 text-gray-600'
}

function statusLabel(s: string) {
  return { active: '进行中', paused: '暂停', stalled: '停滞', resolved: '完结' }[s] || s
}

async function load() {
  try {
    const res = await subplotApi.list(projectId)
    subplots.value = res.data.data || []
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  }
}

async function select(sp: any) {
  selected.value = sp
  cpForm.value = { chapter_num: null, progress: 0, note: '' }
  try {
    const res = await subplotApi.listCheckpoints(sp.id)
    checkpoints.value = res.data.data || []
  } catch { checkpoints.value = [] }
}

async function create() {
  if (!form.value.title) return
  creating.value = true
  try {
    await subplotApi.create(projectId, {
      ...form.value,
      priority: Number(form.value.priority),
      start_chapter: form.value.start_chapter || null,
    })
    showCreate.value = false
    form.value = { title: '', line_label: 'A', priority: 3, description: '', start_chapter: null }
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  } finally {
    creating.value = false
  }
}

async function updateStatus(status: string) {
  if (!selected.value) return
  try {
    await subplotApi.update(selected.value.id, { status })
    selected.value.status = status
    const sp = subplots.value.find(s => s.id === selected.value.id)
    if (sp) sp.status = status
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  }
}

async function addCheckpoint() {
  if (!cpForm.value.chapter_num || !selected.value) return
  try {
    await subplotApi.addCheckpoint(selected.value.id, cpForm.value)
    const res = await subplotApi.listCheckpoints(selected.value.id)
    checkpoints.value = res.data.data || []
    cpForm.value = { chapter_num: null, progress: 0, note: '' }
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  }
}

async function deleteSubplot() {
  if (!selected.value || !confirm(`确定删除支线《${selected.value.title}》吗？`)) return
  try {
    await subplotApi.delete(selected.value.id)
    selected.value = null
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  }
}

onMounted(load)
</script>
