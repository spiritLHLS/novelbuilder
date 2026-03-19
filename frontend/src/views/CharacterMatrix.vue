<template>
  <div class="p-6 max-w-6xl mx-auto space-y-6">
    <div class="flex items-center justify-between">
      <h1 class="text-2xl font-bold text-gray-900">🕸️ 角色关系矩阵</h1>
      <button @click="openAdd" class="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 text-sm">
        ＋ 添加关系
      </button>
    </div>

    <div v-if="error" class="p-4 bg-red-50 border border-red-200 rounded-lg text-red-700 text-sm">{{ error }}</div>

    <div v-if="loading" class="text-center py-20 text-gray-400">加载中…</div>

    <div v-else-if="characters.length < 2" class="text-center py-20 text-gray-400">
      <div class="text-5xl mb-3">👥</div>
      <p>需要至少 2 个角色才能构建关系矩阵</p>
    </div>

    <template v-else>
      <!-- Matrix table -->
      <div class="bg-white border border-gray-200 rounded-xl overflow-auto">
        <table class="min-w-full text-sm">
          <thead>
            <tr>
              <th class="w-36 p-3 bg-gray-50 border-b border-r text-gray-500 text-xs font-medium">↓角色 / 互动→</th>
              <th v-for="c in characters" :key="c.id"
                class="p-3 bg-gray-50 border-b border-r text-center text-xs font-medium text-gray-600 whitespace-nowrap">
                {{ c.name }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="rowChar in characters" :key="rowChar.id" class="group">
              <td class="p-3 border-b border-r bg-gray-50 font-medium text-gray-700 text-xs whitespace-nowrap">{{ rowChar.name }}</td>
              <td v-for="colChar in characters" :key="colChar.id"
                class="p-2 border-b border-r text-center align-middle"
                :class="rowChar.id === colChar.id ? 'bg-gray-100' : 'cursor-pointer hover:bg-blue-50'"
                @click="rowChar.id !== colChar.id && openCell(rowChar, colChar)">
                <template v-if="rowChar.id === colChar.id">
                  <span class="text-gray-300">—</span>
                </template>
                <template v-else>
                  <div v-if="getInteraction(rowChar.id, colChar.id)" class="space-y-1">
                    <span :class="['px-2 py-0.5 rounded-full text-xs font-medium', relColor(getInteraction(rowChar.id, colChar.id)!.relationship)]">
                      {{ getInteraction(rowChar.id, colChar.id)!.relationship }}
                    </span>
                    <div class="text-xs text-gray-400">×{{ getInteraction(rowChar.id, colChar.id)!.interaction_count }}</div>
                  </div>
                  <span v-else class="text-gray-200 text-lg">＋</span>
                </template>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Interaction list cards -->
      <div v-if="interactions.length" class="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div v-for="it in interactions" :key="it.id"
          class="bg-white border border-gray-200 rounded-xl p-4 space-y-2 hover:border-blue-300 transition-colors">
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-2">
              <span class="font-medium text-gray-800 text-sm">{{ it.char_a_name }}</span>
              <span class="text-gray-400">↔</span>
              <span class="font-medium text-gray-800 text-sm">{{ it.char_b_name }}</span>
            </div>
            <button @click="delInteraction(it)" class="text-red-400 hover:text-red-600 text-xs">删除</button>
          </div>
          <div class="flex flex-wrap gap-2 text-xs">
            <span :class="['px-2 py-0.5 rounded-full font-medium', relColor(it.relationship)]">{{ it.relationship }}</span>
            <span class="text-gray-500">首次相遇 第{{ it.first_meet_chapter }}章</span>
            <span class="text-gray-500">| 互动 {{ it.interaction_count }} 次</span>
          </div>
          <div v-if="it.notes" class="text-xs text-gray-500 italic">{{ it.notes }}</div>
          <div class="grid grid-cols-2 gap-2 text-xs mt-1">
            <div class="bg-blue-50 rounded p-2">
              <div class="font-medium text-blue-700 mb-1">{{ it.char_a_name }} 已知信息</div>
              <div class="text-blue-600 space-y-0.5">
                <div v-if="(it.info_known_by_a || []).length === 0" class="text-blue-400 italic">无</div>
                <div v-for="info in (it.info_known_by_a || [])" :key="info">• {{ info }}</div>
              </div>
            </div>
            <div class="bg-green-50 rounded p-2">
              <div class="font-medium text-green-700 mb-1">{{ it.char_b_name }} 已知信息</div>
              <div class="text-green-600 space-y-0.5">
                <div v-if="(it.info_known_by_b || []).length === 0" class="text-green-400 italic">无</div>
                <div v-for="info in (it.info_known_by_b || [])" :key="info">• {{ info }}</div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </template>

    <!-- Add / Edit Modal -->
    <div v-if="showModal" class="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div class="bg-white rounded-2xl shadow-xl w-full max-w-lg p-6 space-y-4 max-h-screen overflow-y-auto">
        <h2 class="text-lg font-semibold">{{ editTarget ? '编辑' : '添加' }}角色关系</h2>
        <div class="space-y-3">
          <div class="grid grid-cols-2 gap-3">
            <div>
              <label class="text-xs text-gray-500 mb-1 block">角色 A *</label>
              <select v-model="form.char_a_id" class="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500">
                <option value="">选择角色</option>
                <option v-for="c in characters" :key="c.id" :value="c.id">{{ c.name }}</option>
              </select>
            </div>
            <div>
              <label class="text-xs text-gray-500 mb-1 block">角色 B *</label>
              <select v-model="form.char_b_id" class="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500">
                <option value="">选择角色</option>
                <option v-for="c in characters" :key="c.id" :value="c.id">{{ c.name }}</option>
              </select>
            </div>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <div>
              <label class="text-xs text-gray-500 mb-1 block">关系类型</label>
              <input v-model="form.relationship" list="rel-types"
                class="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                placeholder="如：盟友、敌人、恋人" />
              <datalist id="rel-types">
                <option v-for="r in relTypes" :key="r" :value="r" />
              </datalist>
            </div>
            <div>
              <label class="text-xs text-gray-500 mb-1 block">首次相遇章节</label>
              <input v-model.number="form.first_meet_chapter" type="number" min="1"
                class="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
            </div>
          </div>
          <div>
            <label class="text-xs text-gray-500 mb-1 block">A 已知信息（每行一条）</label>
            <textarea v-model="infoAText" rows="3"
              class="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 resize-none"
              placeholder="角色A了解到哪些信息，每行一条" />
          </div>
          <div>
            <label class="text-xs text-gray-500 mb-1 block">B 已知信息（每行一条）</label>
            <textarea v-model="infoBText" rows="3"
              class="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 resize-none"
              placeholder="角色B了解到哪些信息，每行一条" />
          </div>
          <div>
            <label class="text-xs text-gray-500 mb-1 block">备注</label>
            <input v-model="form.notes" class="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
          </div>
        </div>
        <div class="flex gap-3 pt-2">
          <button @click="showModal = false" class="flex-1 border rounded-lg py-2 text-sm text-gray-600 hover:bg-gray-50">取消</button>
          <button @click="save" :disabled="!form.char_a_id || !form.char_b_id || saving || form.char_a_id === form.char_b_id"
            class="flex-1 bg-blue-600 text-white rounded-lg py-2 text-sm hover:bg-blue-700 disabled:opacity-50">
            {{ saving ? '保存中…' : '保存' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { charInteractionApi, characterApi } from '@/api'

const route = useRoute()
const projectId = route.params.projectId as string

const characters = ref<any[]>([])
const interactions = ref<any[]>([])
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const showModal = ref(false)
const editTarget = ref<any>(null)
const infoAText = ref('')
const infoBText = ref('')

const relTypes = ['盟友', '敌人', '恋人', '师徒', '竞争者', '家人', '朋友', '陌生人', '主仆', '搭档']

const form = ref({
  char_a_id: '',
  char_b_id: '',
  relationship: '',
  first_meet_chapter: 1,
  notes: '',
})

function getInteraction(aId: string, bId: string) {
  const [lo, hi] = aId < bId ? [aId, bId] : [bId, aId]
  return interactions.value.find(i => i.char_a_id === lo && i.char_b_id === hi)
}

const relColorMap: Record<string, string> = {
  盟友: 'bg-green-100 text-green-700',
  敌人: 'bg-red-100 text-red-700',
  恋人: 'bg-pink-100 text-pink-700',
  师徒: 'bg-purple-100 text-purple-700',
  竞争者: 'bg-orange-100 text-orange-700',
  家人: 'bg-blue-100 text-blue-700',
}
function relColor(rel: string) { return relColorMap[rel] ?? 'bg-gray-100 text-gray-600' }

function openAdd() {
  editTarget.value = null
  form.value = { char_a_id: '', char_b_id: '', relationship: '', first_meet_chapter: 1, notes: '' }
  infoAText.value = ''
  infoBText.value = ''
  showModal.value = true
}

function openCell(a: any, b: any) {
  const existing = getInteraction(a.id, b.id)
  editTarget.value = existing ?? null
  if (existing) {
    form.value = {
      char_a_id: existing.char_a_id,
      char_b_id: existing.char_b_id,
      relationship: existing.relationship,
      first_meet_chapter: existing.first_meet_chapter || 1,
      notes: existing.notes || '',
    }
    infoAText.value = (existing.info_known_by_a || []).join('\n')
    infoBText.value = (existing.info_known_by_b || []).join('\n')
  } else {
    form.value = { char_a_id: a.id, char_b_id: b.id, relationship: '盟友', first_meet_chapter: 1, notes: '' }
    infoAText.value = ''
    infoBText.value = ''
  }
  showModal.value = true
}

async function load() {
  loading.value = true
  try {
    const [charRes, intRes] = await Promise.all([
      characterApi.list(projectId),
      charInteractionApi.list(projectId),
    ])
    characters.value = charRes.data.data || []
    interactions.value = intRes.data.data || []
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!form.value.char_a_id || !form.value.char_b_id) return
  saving.value = true
  try {
    const payload = {
      ...form.value,
      info_known_by_a: infoAText.value.split('\n').map(s => s.trim()).filter(Boolean),
      info_known_by_b: infoBText.value.split('\n').map(s => s.trim()).filter(Boolean),
    }
    await charInteractionApi.upsert(projectId, payload)
    showModal.value = false
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  } finally {
    saving.value = false
  }
}

async function delInteraction(it: any) {
  if (!confirm(`删除 ${it.char_a_name} ↔ ${it.char_b_name} 的关系记录？`)) return
  try {
    await charInteractionApi.delete(it.id)
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  }
}

onMounted(load)
</script>
