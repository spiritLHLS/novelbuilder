<template>
  <div class="genre-templates p-6 max-w-5xl mx-auto">
    <div class="flex items-center justify-between mb-6">
      <div>
        <h1 class="text-2xl font-bold">题材专属规则</h1>
        <p class="text-gray-500 mt-1">为不同网文题材配置专属写作规则、语言约束和节奏要求</p>
      </div>
      <div class="flex gap-3">
        <button @click="openCreate" class="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 text-sm">
          + 新建题材规则
        </button>
        <button @click="$router.back()" class="px-4 py-2 border rounded hover:bg-gray-50 text-sm">返回</button>
      </div>
    </div>

    <!-- Error -->
    <div v-if="error" class="bg-red-50 border border-red-200 rounded p-3 text-red-700 text-sm mb-4">
      {{ error }}
    </div>

    <!-- Help -->
    <div class="bg-blue-50 border border-blue-200 rounded p-4 mb-6 text-sm text-blue-800">
      题材规则会在生成章节时自动注入系统提示词。项目题材需在项目设置中的「世界观」里指定，
      系统会自动匹配对应的题材规则。
    </div>

    <!-- Loading -->
    <div v-if="loading" class="text-center py-12 text-gray-400">加载中…</div>

    <!-- Empty -->
    <div v-else-if="templates.length === 0" class="text-center py-12 text-gray-400">
      暂无题材规则，点击「新建题材规则」开始添加
    </div>

    <!-- Templates list -->
    <div v-else class="space-y-4">
      <div v-for="tpl in templates" :key="tpl.genre"
        class="bg-white border rounded-lg overflow-hidden">
        <div class="flex items-center justify-between px-5 py-4 bg-gray-50 border-b">
          <div class="flex items-center gap-3">
            <span class="font-semibold text-lg">{{ tpl.genre }}</span>
            <span class="text-xs text-gray-400 bg-gray-100 px-2 py-0.5 rounded-full">
              {{ tpl.genre }}
            </span>
          </div>
          <div class="flex gap-2">
            <button @click="openEdit(tpl)"
              class="px-3 py-1.5 text-sm border rounded hover:bg-gray-50">
              编辑
            </button>
            <button @click="confirmDelete(tpl.genre)"
              class="px-3 py-1.5 text-sm text-red-600 border border-red-200 rounded hover:bg-red-50">
              删除
            </button>
          </div>
        </div>
        <div class="px-5 py-4 grid grid-cols-3 gap-4 text-sm">
          <div>
            <div class="text-xs font-medium text-gray-500 uppercase tracking-wider mb-2">写作规则</div>
            <div class="text-gray-700 whitespace-pre-line line-clamp-4">{{ tpl.rules_content || '—' }}</div>
          </div>
          <div>
            <div class="text-xs font-medium text-gray-500 uppercase tracking-wider mb-2">语言约束</div>
            <div class="text-gray-700 whitespace-pre-line line-clamp-4">{{ tpl.language_constraints || '—' }}</div>
          </div>
          <div>
            <div class="text-xs font-medium text-gray-500 uppercase tracking-wider mb-2">节奏规则</div>
            <div class="text-gray-700 whitespace-pre-line line-clamp-4">{{ tpl.rhythm_rules || '—' }}</div>
          </div>
        </div>
      </div>
    </div>

    <!-- Create/Edit Dialog -->
    <div v-if="dialogVisible"
      class="fixed inset-0 bg-black/40 flex items-center justify-center z-50">
      <div class="bg-white rounded-lg shadow-xl w-[700px] max-h-[90vh] overflow-y-auto">
        <div class="flex items-center justify-between px-6 py-4 border-b">
          <h2 class="text-lg font-semibold">{{ editingGenre ? '编辑题材规则' : '新建题材规则' }}</h2>
          <button @click="dialogVisible = false" class="text-gray-400 hover:text-gray-600 text-xl leading-none">×</button>
        </div>
        <div class="px-6 py-5 space-y-4">
          <!-- Genre name -->
          <div>
            <label class="block text-sm font-medium mb-1">题材名称 <span class="text-red-500">*</span></label>
            <input v-model="form.genre" :disabled="!!editingGenre"
              class="w-full border rounded px-3 py-2 text-sm disabled:bg-gray-50 disabled:text-gray-500"
              placeholder="例如：玄幻、都市、历史、末世" />
            <p v-if="!editingGenre" class="text-xs text-gray-400 mt-1">
              题材名称创建后不可修改，需要删除重建
            </p>
          </div>
          <!-- Rules content -->
          <div>
            <label class="block text-sm font-medium mb-1">写作规则</label>
            <textarea v-model="form.rules_content" rows="5"
              class="w-full border rounded px-3 py-2 text-sm resize-y"
              placeholder="描述该题材的核心写作规则，例如：打斗场面需有激烈冲突感，修炼突破时需有内心描写…" />
          </div>
          <!-- Language constraints -->
          <div>
            <label class="block text-sm font-medium mb-1">语言约束</label>
            <textarea v-model="form.language_constraints" rows="4"
              class="w-full border rounded px-3 py-2 text-sm resize-y"
              placeholder="描述该题材的语言风格要求，例如：多用四字成语，避免现代网络用语…" />
          </div>
          <!-- Rhythm rules -->
          <div>
            <label class="block text-sm font-medium mb-1">节奏规则</label>
            <textarea v-model="form.rhythm_rules" rows="4"
              class="w-full border rounded px-3 py-2 text-sm resize-y"
              placeholder="描述该题材的节奏要求，例如：每章结尾需有悬念钩子，动作场景短句为主…" />
          </div>
        </div>
        <div class="flex justify-end gap-3 px-6 py-4 border-t bg-gray-50">
          <button @click="dialogVisible = false"
            class="px-4 py-2 border rounded text-sm hover:bg-gray-50">
            取消
          </button>
          <button @click="submit" :disabled="saving"
            class="px-4 py-2 bg-blue-600 text-white rounded text-sm hover:bg-blue-700 disabled:opacity-50">
            {{ saving ? '保存中…' : '保存' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { genreTemplateApi } from '@/api'

interface GenreTemplate {
  genre: string
  rules_content: string
  language_constraints: string
  rhythm_rules: string
  audit_dimensions_extra?: Record<string, any>
}

const templates = ref<GenreTemplate[]>([])
const loading = ref(false)
const error = ref('')
const saving = ref(false)
const dialogVisible = ref(false)
const editingGenre = ref<string | null>(null)

const form = reactive({
  genre: '',
  rules_content: '',
  language_constraints: '',
  rhythm_rules: '',
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    const res = await genreTemplateApi.list()
    templates.value = res.data.data ?? []
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingGenre.value = null
  form.genre = ''
  form.rules_content = ''
  form.language_constraints = ''
  form.rhythm_rules = ''
  dialogVisible.value = true
}

function openEdit(tpl: GenreTemplate) {
  editingGenre.value = tpl.genre
  form.genre = tpl.genre
  form.rules_content = tpl.rules_content ?? ''
  form.language_constraints = tpl.language_constraints ?? ''
  form.rhythm_rules = tpl.rhythm_rules ?? ''
  dialogVisible.value = true
}

async function submit() {
  if (!form.genre.trim()) {
    error.value = '题材名称不能为空'
    return
  }
  saving.value = true
  error.value = ''
  try {
    await genreTemplateApi.upsert(form.genre.trim(), {
      rules_content: form.rules_content,
      language_constraints: form.language_constraints,
      rhythm_rules: form.rhythm_rules,
    })
    dialogVisible.value = false
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  } finally {
    saving.value = false
  }
}

async function confirmDelete(genre: string) {
  if (!confirm(`确定删除「${genre}」的题材规则吗？此操作不可恢复。`)) return
  error.value = ''
  try {
    await genreTemplateApi.delete(genre)
    await load()
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  }
}

onMounted(load)
</script>
