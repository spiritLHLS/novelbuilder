<template>
  <div class="world-bible">
    <div class="page-header">
      <h1>世界圣经</h1>
      <el-button type="primary" @click="openEditDialog"><el-icon><Edit /></el-icon>编辑世界观设定</el-button>
    </div>

    <el-row :gutter="20">
      <!-- World Bible Display -->
      <el-col :span="14">
        <el-card shadow="hover">
          <template #header>
            <div class="card-header">
              <span>世界设定</span>
              <el-button text type="primary" size="small" @click="openEditDialog">编辑</el-button>
            </div>
          </template>
          <div class="wb-field" v-if="worldBible.world_view">
            <div class="wb-label">世界观概述</div>
            <div class="wb-value">{{ worldBible.world_view }}</div>
          </div>
          <div class="wb-field" v-if="worldBible.era_background">
            <div class="wb-label">时代背景</div>
            <div class="wb-value">{{ worldBible.era_background }}</div>
          </div>
          <div class="wb-field" v-if="worldBible.geography">
            <div class="wb-label">地理环境</div>
            <div class="wb-value">{{ worldBible.geography }}</div>
          </div>
          <div class="wb-field" v-if="worldBible.social_structure">
            <div class="wb-label">社会结构</div>
            <div class="wb-value">{{ worldBible.social_structure }}</div>
          </div>
          <div class="wb-field" v-if="worldBible.power_system">
            <div class="wb-label">力量体系</div>
            <div class="wb-value">{{ worldBible.power_system }}</div>
          </div>
          <div class="wb-field" v-if="worldBible.core_conflict">
            <div class="wb-label">核心冲突</div>
            <div class="wb-value">{{ worldBible.core_conflict }}</div>
          </div>
          <el-empty v-if="isEmpty" description="暂无世界观设定，点击『编辑世界观设定』开始填写" />
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
          <el-divider />
          <el-button type="primary" style="width:100%" @click="saveConstitution" :loading="savingConst">保存宪法</el-button>
        </el-card>
      </el-col>
    </el-row>

    <!-- Edit Dialog -->
    <el-dialog v-model="showEditDlg" title="编辑世界观设定" width="700px" :close-on-click-modal="false">
      <el-form :model="editForm" label-position="top">
        <el-form-item label="世界观概述">
          <el-input v-model="editForm.world_view" type="textarea" :rows="4" />
        </el-form-item>
        <el-form-item label="时代背景">
          <el-input v-model="editForm.era_background" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item label="地理环境">
          <el-input v-model="editForm.geography" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item label="社会结构">
          <el-input v-model="editForm.social_structure" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item label="力量体系">
          <el-input v-model="editForm.power_system" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item label="核心冲突">
          <el-input v-model="editForm.core_conflict" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item label="其他设定（JSON）">
          <el-input v-model="editForm.extra_json" type="textarea" :rows="4"
            placeholder='{"customs": "...", "language": "..."}' />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showEditDlg = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveWorldBible">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Edit, Delete } from '@element-plus/icons-vue'
import { worldBibleApi } from '@/api'

const route = useRoute()
const projectId = route.params.projectId as string

const saving = ref(false)
const savingConst = ref(false)
const showEditDlg = ref(false)

const worldBible = ref({
  world_view: '',
  era_background: '',
  geography: '',
  social_structure: '',
  power_system: '',
  core_conflict: '',
  extra_json: '',
})

const editForm = ref({ ...worldBible.value })

const isEmpty = computed(() =>
  !worldBible.value.world_view && !worldBible.value.era_background &&
  !worldBible.value.geography && !worldBible.value.social_structure &&
  !worldBible.value.power_system && !worldBible.value.core_conflict
)

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
      // content JSONB is returned nested under d.content
      const c = d.content || {}
      worldBible.value = {
        world_view: c.world_view || '',
        era_background: c.era_background || '',
        geography: c.geography || '',
        social_structure: c.social_structure || '',
        power_system: c.power_system || '',
        core_conflict: c.core_conflict || '',
        extra_json: c.extra_json ? JSON.stringify(c.extra_json, null, 2) : '',
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

function openEditDialog() {
  editForm.value = { ...worldBible.value }
  showEditDlg.value = true
}

async function saveWorldBible() {
  saving.value = true
  try {
    const payload: any = { ...editForm.value }
    if (payload.extra_json) {
      try { payload.extra_json = JSON.parse(payload.extra_json) } catch { /* keep as string */ }
    }
    await worldBibleApi.update(projectId, payload)
    worldBible.value = { ...editForm.value }
    showEditDlg.value = false
    ElMessage.success('世界圣经已保存')
  } catch {
    ElMessage.error('保存失败')
  } finally {
    saving.value = false
  }
}

async function saveConstitution() {
  savingConst.value = true
  try {
    await worldBibleApi.updateConstitution(projectId, {
      rules: constitutions.value,
      forbidden_anchors: forbiddenAnchors.value,
    })
    ElMessage.success('宪法已保存')
  } catch {
    ElMessage.error('保存失败')
  } finally {
    savingConst.value = false
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
.wb-field { margin-bottom: 20px; }
.wb-label { font-size: 12px; color: #888; margin-bottom: 6px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.05em; }
.wb-value { color: var(--nb-text-primary, #e0e0e0); line-height: 1.8; white-space: pre-wrap; }
.constitution-item { background: var(--nb-card-bg); border: 1px solid var(--nb-card-border); padding: 12px; border-radius: 8px; margin-bottom: 12px; }
.constitution-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.constitution-card :deep(.el-card__body) { max-height: 70vh; overflow-y: auto; }
.forbidden-anchors { margin-top: 12px; }
</style>
