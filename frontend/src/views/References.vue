<template>
  <div class="references">
    <div class="page-header">
      <h1>参考书管理</h1>
      <el-upload
        :action="`/api/projects/${projectId}/references/upload`"
        :on-success="handleUploadSuccess"
        :show-file-list="false"
        :data="{ title: '', author: '', genre: '' }"
      >
        <el-button type="primary"><el-icon><Upload /></el-icon>上传参考书</el-button>
      </el-upload>
    </div>

    <el-table :data="references" v-loading="loading" style="width: 100%">
      <el-table-column prop="title" label="书名" />
      <el-table-column prop="author" label="作者" width="120" />
      <el-table-column prop="genre" label="类型" width="100" />
      <el-table-column prop="analysis_status" label="分析状态" width="120">
        <template #default="{ row }">
          <el-tag :type="row.analysis_status === 'completed' ? 'success' : 'info'" size="small">
            {{ row.analysis_status === 'completed' ? '已分析' : '待分析' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="250">
        <template #default="{ row }">
          <el-button size="small" @click="viewAnalysis(row)">查看分析</el-button>
          <el-button size="small" type="primary" @click="startAnalysis(row.id)"
            :loading="analyzing === row.id">分析</el-button>
          <el-button size="small" type="warning" @click="showMigration(row)">迁移配置</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- Analysis Result Dialog -->
    <el-dialog v-model="showAnalysisDialog" title="四层分析报告" width="800px" top="5vh">
      <template v-if="selectedRef">
        <el-tabs>
          <el-tab-pane label="风格指纹层">
            <div class="analysis-section">
              <h3>Layer 1: 风格指纹</h3>
              <div v-if="selectedRef.style_fingerprint" class="chart-container">
                <v-chart :option="styleChartOption" style="height: 300px" autoresize />
                <pre class="json-view">{{ JSON.stringify(selectedRef.style_fingerprint, null, 2) }}</pre>
              </div>
              <el-empty v-else description="尚未分析" />
            </div>
          </el-tab-pane>
          <el-tab-pane label="叙事结构层">
            <div class="analysis-section">
              <h3>Layer 2: 叙事结构</h3>
              <pre class="json-view" v-if="selectedRef.narrative_structure">{{ JSON.stringify(selectedRef.narrative_structure, null, 2) }}</pre>
              <el-empty v-else description="尚未分析" />
            </div>
          </el-tab-pane>
          <el-tab-pane label="氛围萃取层">
            <div class="analysis-section">
              <h3>Layer 3: 氛围萃取</h3>
              <div v-if="selectedRef.atmosphere_profile" class="chart-container">
                <v-chart :option="atmosphereChartOption" style="height: 300px" autoresize />
                <pre class="json-view">{{ JSON.stringify(selectedRef.atmosphere_profile, null, 2) }}</pre>
              </div>
              <el-empty v-else description="尚未分析" />
            </div>
          </el-tab-pane>
          <el-tab-pane label="隔离区">
            <div class="analysis-section">
              <h3>Layer 4: 情节元素隔离区</h3>
              <el-alert type="warning" :closable="false" show-icon>
                隔离区中的情节元素被锁定，不会直接用于生成，仅提供结构参考。
              </el-alert>
            </div>
          </el-tab-pane>
        </el-tabs>
      </template>
    </el-dialog>

    <!-- Migration Config Dialog -->
    <el-dialog v-model="showMigrationDialog" title="氛围迁移配置" width="600px">
      <el-form :model="migrationForm" label-width="120px">
        <el-form-item label="迁移强度">
          <el-slider v-model="migrationForm.intensity" :min="0" :max="100" :step="5" show-stops />
        </el-form-item>
        <el-form-item label="允许迁移层">
          <el-checkbox-group v-model="migrationForm.layers">
            <el-checkbox label="style">风格层</el-checkbox>
            <el-checkbox label="narrative">叙事层</el-checkbox>
            <el-checkbox label="atmosphere">氛围层</el-checkbox>
          </el-checkbox-group>
        </el-form-item>
        <el-form-item label="禁止迁移项">
          <el-input v-model="migrationForm.forbidden" type="textarea" :rows="3"
            placeholder="列出不希望迁移的特定元素，每行一个" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showMigrationDialog = false">取消</el-button>
        <el-button type="primary" @click="saveMigration">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { referenceApi } from '@/api'
import VChart from 'vue-echarts'

const route = useRoute()
const projectId = route.params.projectId as string

const loading = ref(false)
const references = ref<any[]>([])
const analyzing = ref<string | null>(null)
const showAnalysisDialog = ref(false)
const showMigrationDialog = ref(false)
const selectedRef = ref<any>(null)
const selectedRefId = ref('')

const migrationForm = ref({
  intensity: 50,
  layers: ['style', 'atmosphere'],
  forbidden: '',
})

const styleChartOption = computed(() => {
  const fp = selectedRef.value?.style_fingerprint
  if (!fp) return {}
  const indicators = [
    { name: '词汇丰富度', max: 1 },
    { name: '平均句长', max: 50 },
    { name: '突发度', max: 1 },
    { name: '比喻密度', max: 0.1 },
    { name: '对话占比', max: 1 },
  ]
  return {
    backgroundColor: 'transparent',
    radar: { indicator: indicators, shape: 'circle' },
    series: [{
      type: 'radar',
      data: [{
        value: [
          fp.vocabulary_richness?.ttr || 0,
          fp.sentence?.avg_length || 0,
          fp.sentence?.burstiness_index || 0,
          fp.rhetoric?.metaphor_density || 0,
          fp.punctuation?.dialogue_quote_count ? 0.5 : 0,
        ],
        name: '风格指纹',
      }],
    }],
  }
})

const atmosphereChartOption = computed(() => {
  const ap = selectedRef.value?.atmosphere_profile
  if (!ap) return {}
  const sensory = ap.sensory_profile || {}
  return {
    backgroundColor: 'transparent',
    radar: {
      indicator: [
        { name: '视觉', max: 1 },
        { name: '听觉', max: 1 },
        { name: '嗅觉', max: 1 },
        { name: '触觉', max: 1 },
        { name: '味觉', max: 1 },
      ],
      shape: 'circle',
    },
    series: [{
      type: 'radar',
      data: [{
        value: [
          sensory.visual?.ratio || 0,
          sensory.auditory?.ratio || 0,
          sensory.olfactory?.ratio || 0,
          sensory.tactile?.ratio || 0,
          sensory.gustatory?.ratio || 0,
        ],
        name: '感官分布',
      }],
    }],
  }
})

onMounted(fetchRefs)

async function fetchRefs() {
  loading.value = true
  try {
    const res = await referenceApi.list(projectId)
    references.value = res.data.data || []
  } finally {
    loading.value = false
  }
}

function handleUploadSuccess(response: any) {
  ElMessage.success('上传成功')
  if (response.data) {
    references.value.push(response.data)
  }
}

async function startAnalysis(id: string) {
  analyzing.value = id
  try {
    await referenceApi.analyze(id)
    ElMessage.success('分析完成')
    await fetchRefs()
  } catch {
    ElMessage.error('分析失败')
  } finally {
    analyzing.value = null
  }
}

async function viewAnalysis(ref: any) {
  try {
    const res = await referenceApi.get(ref.id)
    selectedRef.value = res.data.data
    showAnalysisDialog.value = true
  } catch {
    ElMessage.error('获取分析结果失败')
  }
}

function showMigration(ref: any) {
  selectedRefId.value = ref.id
  if (ref.migration_config) {
    migrationForm.value = { ...ref.migration_config }
  }
  showMigrationDialog.value = true
}

async function saveMigration() {
  try {
    await referenceApi.updateMigrationConfig(selectedRefId.value, migrationForm.value)
    ElMessage.success('迁移配置已保存')
    showMigrationDialog.value = false
  } catch {
    ElMessage.error('保存失败')
  }
}
</script>

<style scoped>
.references { max-width: 1200px; margin: 0 auto; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; }
.page-header h1 { font-size: 24px; color: #e0e0e0; }
.analysis-section { padding: 16px 0; }
.analysis-section h3 { margin-bottom: 16px; color: #409eff; }
.json-view { background: #1a1a2e; padding: 16px; border-radius: 8px; font-size: 12px; color: #a0a0b0; max-height: 400px; overflow: auto; margin-top: 16px; }
.chart-container { background: #1a1a2e; border-radius: 8px; padding: 16px; }
</style>
