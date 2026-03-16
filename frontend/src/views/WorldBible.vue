<template>
  <div class="world-bible">
    <div class="page-header">
      <h1>世界圣经</h1>
      <el-button type="primary" @click="saveWorldBible" :loading="saving">保存修改</el-button>
    </div>

    <el-row :gutter="20">
      <!-- World Bible Content -->
      <el-col :span="14">
        <el-card shadow="hover">
          <template #header><span>世界设定</span></template>
          <el-form :model="worldBible" label-position="top">
            <el-form-item label="世界观概述">
              <el-input v-model="worldBible.world_view" type="textarea" :rows="4" />
            </el-form-item>
            <el-form-item label="时代背景">
              <el-input v-model="worldBible.era_background" type="textarea" :rows="3" />
            </el-form-item>
            <el-form-item label="地理环境">
              <el-input v-model="worldBible.geography" type="textarea" :rows="3" />
            </el-form-item>
            <el-form-item label="社会结构">
              <el-input v-model="worldBible.social_structure" type="textarea" :rows="3" />
            </el-form-item>
            <el-form-item label="力量体系">
              <el-input v-model="worldBible.power_system" type="textarea" :rows="3" />
            </el-form-item>
            <el-form-item label="核心冲突">
              <el-input v-model="worldBible.core_conflict" type="textarea" :rows="3" />
            </el-form-item>
            <el-form-item label="其他设定（JSON）">
              <el-input v-model="worldBible.extra_json" type="textarea" :rows="5"
                placeholder='{"customs": "...", "language": "..."}' />
            </el-form-item>
          </el-form>
        </el-card>
      </el-col>

      <!-- Constitution -->
      <el-col :span="10">
        <el-card shadow="hover" class="constitution-card">
          <template #header>
            <div class="card-header">
              <span>世界宪法</span>
              <el-button text type="primary" @click="addConstitution">添加条目</el-button>
            </div>
          </template>

          <div v-for="(item, idx) in constitutions" :key="idx" class="constitution-item">
            <div class="constitution-header">
              <el-tag :type="item.type === 'immutable' ? 'danger' : 'warning'" size="small">
                {{ item.type === 'immutable' ? '不可变' : '可变' }}
              </el-tag>
              <el-button text type="danger" size="small" @click="removeConstitution(idx)">
                <el-icon><Delete /></el-icon>
              </el-button>
            </div>
            <el-select v-model="item.type" size="small" style="width: 100px; margin-bottom: 8px;">
              <el-option label="不可变" value="immutable" />
              <el-option label="可变" value="mutable" />
            </el-select>
            <el-input v-model="item.rule" type="textarea" :rows="2" placeholder="规则内容" />
            <el-input v-model="item.reason" placeholder="设定原因" size="small"
              style="margin-top: 4px;" />
          </div>

          <el-divider />

          <h4 style="color: #f56c6c; margin-bottom: 12px;">禁止锚点</h4>
          <el-alert type="error" :closable="false" show-icon>
            以下元素绝不能出现在任何生成内容中
          </el-alert>
          <div class="forbidden-anchors">
            <el-tag v-for="(anchor, i) in forbiddenAnchors" :key="i" closable
              @close="removeForbiddenAnchor(i)" type="danger" size="large"
              style="margin: 4px;">
              {{ anchor }}
            </el-tag>
            <el-input v-model="newAnchor" size="small" style="width: 200px; margin-top: 8px;"
              placeholder="添加禁止锚点" @keyup.enter="addForbiddenAnchor">
              <template #append>
                <el-button @click="addForbiddenAnchor">添加</el-button>
              </template>
            </el-input>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { worldBibleApi } from '@/api'

const route = useRoute()
const projectId = route.params.projectId as string

const saving = ref(false)

const worldBible = ref({
  world_view: '',
  era_background: '',
  geography: '',
  social_structure: '',
  power_system: '',
  core_conflict: '',
  extra_json: '',
})

const constitutions = ref<Array<{ type: string; rule: string; reason: string }>>([])
const forbiddenAnchors = ref<string[]>([])
const newAnchor = ref('')

onMounted(async () => {
  try {
    const [wbRes, constRes] = await Promise.all([
      worldBibleApi.get(projectId),
      worldBibleApi.getConstitution(projectId),
    ])
    if (wbRes.data.data) {
      const d = wbRes.data.data
      worldBible.value = {
        world_view: d.world_view || '',
        era_background: d.era_background || '',
        geography: d.geography || '',
        social_structure: d.social_structure || '',
        power_system: d.power_system || '',
        core_conflict: d.core_conflict || '',
        extra_json: d.extra_json ? JSON.stringify(d.extra_json, null, 2) : '',
      }
    }
    if (constRes.data.data) {
      const c = constRes.data.data
      constitutions.value = c.rules || []
      forbiddenAnchors.value = c.forbidden_anchors || []
    }
  } catch {
    // new project, no data yet
  }
})

async function saveWorldBible() {
  saving.value = true
  try {
    const wbPayload: any = { ...worldBible.value }
    if (wbPayload.extra_json) {
      try { wbPayload.extra_json = JSON.parse(wbPayload.extra_json) } catch { /* keep as string */ }
    }
    await worldBibleApi.update(projectId, wbPayload)
    await worldBibleApi.updateConstitution(projectId, {
      rules: constitutions.value,
      forbidden_anchors: forbiddenAnchors.value,
    })
    ElMessage.success('世界圣经已保存')
  } catch {
    ElMessage.error('保存失败')
  } finally {
    saving.value = false
  }
}

function addConstitution() {
  constitutions.value.push({ type: 'immutable', rule: '', reason: '' })
}

function removeConstitution(idx: number) {
  constitutions.value.splice(idx, 1)
}

function addForbiddenAnchor() {
  const v = newAnchor.value.trim()
  if (v && !forbiddenAnchors.value.includes(v)) {
    forbiddenAnchors.value.push(v)
    newAnchor.value = ''
  }
}

function removeForbiddenAnchor(idx: number) {
  forbiddenAnchors.value.splice(idx, 1)
}
</script>

<style scoped>
.world-bible { max-width: 1400px; margin: 0 auto; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; }
.page-header h1 { font-size: 24px; color: #e0e0e0; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.constitution-item { background: #1a1a2e; padding: 12px; border-radius: 8px; margin-bottom: 12px; }
.constitution-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.constitution-card :deep(.el-card__body) { max-height: 70vh; overflow-y: auto; }
.forbidden-anchors { margin-top: 12px; }
</style>
