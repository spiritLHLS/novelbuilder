<template>
  <div class="glossary-page">
    <div class="page-header">
      <h2>术语表 / Glossary</h2>
      <p class="subtitle">管理项目专属词汇，自动注入生成提示词</p>
    </div>

    <!-- Toolbar -->
    <div class="toolbar">
      <el-input
        v-model="search"
        placeholder="搜索术语..."
        :prefix-icon="Search"
        clearable
        style="width: 240px"
        @input="filterTerms"
      />
      <el-select v-model="filterCategory" placeholder="全部分类" clearable style="width: 160px" @change="filterTerms">
        <el-option label="全部" value="" />
        <el-option label="人名" value="character" />
        <el-option label="地名" value="place" />
        <el-option label="物品" value="item" />
        <el-option label="概念" value="concept" />
        <el-option label="其他" value="other" />
      </el-select>
      <el-button type="primary" :icon="Plus" @click="openAddDialog">新增术语</el-button>
    </div>

    <!-- Table -->
    <el-table :data="filteredTerms" v-loading="loading" class="terms-table" stripe>
      <el-table-column label="术语" prop="term" min-width="160" />
      <el-table-column label="定义" prop="definition" min-width="280" show-overflow-tooltip />
      <el-table-column label="分类" prop="category" width="120">
        <template #default="{ row }">
          <el-tag :type="categoryTagType(row.category)" size="small">
            {{ categoryLabel(row.category) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="100" align="center">
        <template #default="{ row }">
          <el-button type="danger" text :icon="Delete" size="small" @click="deleteTerm(row.id)" />
        </template>
      </el-table-column>
    </el-table>

    <div v-if="!loading && filteredTerms.length === 0" class="empty-state">
      <el-empty description="暂无术语，点击「新增术语」添加" />
    </div>

    <!-- Add Dialog -->
    <el-dialog v-model="dialogVisible" title="新增术语" width="480px" :close-on-click-modal="false">
      <el-form :model="form" :rules="rules" ref="formRef" label-width="80px">
        <el-form-item label="术语" prop="term">
          <el-input v-model="form.term" placeholder="请输入术语名称" />
        </el-form-item>
        <el-form-item label="定义" prop="definition">
          <el-input
            v-model="form.definition"
            type="textarea"
            :rows="3"
            placeholder="请输入术语定义或解释"
          />
        </el-form-item>
        <el-form-item label="分类" prop="category">
          <el-select v-model="form.category" placeholder="选择分类" style="width: 100%">
            <el-option label="人名" value="character" />
            <el-option label="地名" value="place" />
            <el-option label="物品" value="item" />
            <el-option label="概念" value="concept" />
            <el-option label="其他" value="other" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitAdd">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search, Plus, Delete } from '@element-plus/icons-vue'
import type { FormInstance, FormRules } from 'element-plus'
import { glossaryApi } from '@/api'

interface GlossaryTerm {
  id: string
  project_id: string
  term: string
  definition: string
  category: string
  created_at: string
}

const route = useRoute()
const projectId = computed(() => route.params.projectId as string)

const terms = ref<GlossaryTerm[]>([])
const loading = ref(false)
const search = ref('')
const filterCategory = ref('')
const dialogVisible = ref(false)
const submitting = ref(false)
const formRef = ref<FormInstance>()

const form = ref({ term: '', definition: '', category: 'concept' })
const rules: FormRules = {
  term: [{ required: true, message: '请输入术语名称', trigger: 'blur' }],
  definition: [{ required: true, message: '请输入术语定义', trigger: 'blur' }],
  category: [{ required: true, message: '请选择分类', trigger: 'change' }],
}

const filteredTerms = computed(() => {
  return terms.value.filter(t => {
    const matchSearch = !search.value || t.term.includes(search.value) || t.definition.includes(search.value)
    const matchCategory = !filterCategory.value || t.category === filterCategory.value
    return matchSearch && matchCategory
  })
})

const categoryLabel = (c: string) => {
  const map: Record<string, string> = { character: '人名', place: '地名', item: '物品', concept: '概念', other: '其他' }
  return map[c] ?? c
}

const categoryTagType = (c: string): '' | 'success' | 'warning' | 'danger' | 'info' => {
  const map: Record<string, '' | 'success' | 'warning' | 'danger' | 'info'> = {
    character: '',
    place: 'success',
    item: 'warning',
    concept: 'info',
    other: 'info',
  }
  return map[c] ?? 'info'
}

async function loadTerms() {
  loading.value = true
  try {
    const res = await glossaryApi.list(projectId.value)
    terms.value = res.data ?? []
  } catch {
    ElMessage.error('加载术语列表失败')
  } finally {
    loading.value = false
  }
}

function filterTerms() {
  // reactivity via computed
}

function openAddDialog() {
  form.value = { term: '', definition: '', category: 'concept' }
  dialogVisible.value = true
}

async function submitAdd() {
  if (!formRef.value) return
  await formRef.value.validate(async valid => {
    if (!valid) return
    submitting.value = true
    try {
      await glossaryApi.create(projectId.value, form.value)
      ElMessage.success('术语添加成功')
      dialogVisible.value = false
      await loadTerms()
    } catch {
      ElMessage.error('添加失败')
    } finally {
      submitting.value = false
    }
  })
}

async function deleteTerm(id: string) {
  await ElMessageBox.confirm('确定删除该术语？', '删除确认', { type: 'warning' })
  try {
    await glossaryApi.delete(id)
    ElMessage.success('已删除')
    terms.value = terms.value.filter(t => t.id !== id)
  } catch {
    ElMessage.error('删除失败')
  }
}

onMounted(loadTerms)
</script>

<style scoped>
.glossary-page {
  padding: 24px;
  max-width: 1000px;
}

.page-header {
  margin-bottom: 20px;
}

.page-header h2 {
  font-size: 22px;
  font-weight: 600;
  color: #e0e0e0;
}

.subtitle {
  font-size: 13px;
  color: #888;
  margin-top: 4px;
}

.toolbar {
  display: flex;
  gap: 12px;
  align-items: center;
  margin-bottom: 16px;
}

.terms-table {
  background: transparent;
}

.empty-state {
  margin-top: 40px;
  text-align: center;
}
</style>
