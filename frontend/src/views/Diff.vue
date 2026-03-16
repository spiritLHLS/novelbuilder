<template>
  <div class="diff-page">
    <div class="page-header">
      <h1>工作流快照对比</h1>
      <div class="header-actions">
        <el-button @click="$router.back()">
          <el-icon><ArrowLeft /></el-icon>返回
        </el-button>
      </div>
    </div>

    <!-- Step Selector -->
    <el-card shadow="never" class="selector-card">
      <el-form :inline="true">
        <el-form-item label="工作流 Run ID">
          <el-input v-model="runId" placeholder="输入 Run ID" style="width: 280px" />
        </el-form-item>
        <el-form-item label="左侧步骤">
          <el-input v-model="fromStep" placeholder="e.g. blueprint" style="width: 160px" />
        </el-form-item>
        <el-form-item label="右侧步骤">
          <el-input v-model="toStep" placeholder="e.g. chapter_1" style="width: 160px" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="loadDiff" :loading="loading">对比</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- Error -->
    <el-alert v-if="error" :title="error" type="error" show-icon closable style="margin-bottom: 16px" />

    <!-- Diff Panels -->
    <el-row :gutter="20" v-if="diffData">
      <el-col :span="12">
        <el-card shadow="hover" class="diff-panel">
          <template #header>
            <span class="panel-title">
              <el-tag type="info">FROM</el-tag>&nbsp;{{ diffData.from.step_key }}
            </span>
          </template>
          <el-tabs v-model="leftTab">
            <el-tab-pane label="输出" name="output">
              <pre class="json-view">{{ formatJSON(diffData.from.output_payload) }}</pre>
            </el-tab-pane>
            <el-tab-pane label="参数" name="params">
              <pre class="json-view">{{ formatJSON(diffData.from.params) }}</pre>
            </el-tab-pane>
            <el-tab-pane label="上下文" name="context">
              <pre class="json-view">{{ formatJSON(diffData.from.context_payload) }}</pre>
            </el-tab-pane>
            <el-tab-pane label="质量" name="quality">
              <pre class="json-view">{{ formatJSON(diffData.from.quality_payload) }}</pre>
            </el-tab-pane>
          </el-tabs>
        </el-card>
      </el-col>

      <el-col :span="12">
        <el-card shadow="hover" class="diff-panel">
          <template #header>
            <span class="panel-title">
              <el-tag type="success">TO</el-tag>&nbsp;{{ diffData.to.step_key }}
            </span>
          </template>
          <el-tabs v-model="rightTab">
            <el-tab-pane label="输出" name="output">
              <pre class="json-view">{{ formatJSON(diffData.to.output_payload) }}</pre>
            </el-tab-pane>
            <el-tab-pane label="参数" name="params">
              <pre class="json-view">{{ formatJSON(diffData.to.params) }}</pre>
            </el-tab-pane>
            <el-tab-pane label="上下文" name="context">
              <pre class="json-view">{{ formatJSON(diffData.to.context_payload) }}</pre>
            </el-tab-pane>
            <el-tab-pane label="质量" name="quality">
              <pre class="json-view">{{ formatJSON(diffData.to.quality_payload) }}</pre>
            </el-tab-pane>
          </el-tabs>
        </el-card>
      </el-col>
    </el-row>

    <!-- Inline text diff for output payloads -->
    <el-card v-if="diffData" shadow="hover" style="margin-top: 20px">
      <template #header><span>文本差异（输出内容）</span></template>
      <div class="text-diff">
        <span
          v-for="(part, idx) in textDiffLines"
          :key="idx"
          :class="['diff-line', part.type]"
        >{{ part.text }}</span>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { workflowApi } from '@/api'
import { ArrowLeft } from '@element-plus/icons-vue'

const route = useRoute()

const runId = ref<string>((route.params.runId as string) || '')
const fromStep = ref<string>((route.query.fromStep as string) || '')
const toStep = ref<string>((route.query.toStep as string) || '')
const loading = ref(false)
const error = ref<string | null>(null)
const leftTab = ref('output')
const rightTab = ref('output')

interface SnapshotPayload {
  step_key: string
  params: any
  context_payload: any
  output_payload: any
  quality_payload: any
}

interface DiffData {
  from: SnapshotPayload
  to: SnapshotPayload
}

const diffData = ref<DiffData | null>(null)

async function loadDiff() {
  if (!runId.value || !fromStep.value || !toStep.value) {
    error.value = '请填写 Run ID 及两个步骤名称'
    return
  }
  loading.value = true
  error.value = null
  try {
    const res = await workflowApi.getDiff(runId.value, fromStep.value, toStep.value)
    diffData.value = res.data?.data as DiffData
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  } finally {
    loading.value = false
  }
}

function formatJSON(val: any): string {
  if (val === null || val === undefined) return '(空)'
  try {
    const obj = typeof val === 'string' ? JSON.parse(val) : val
    return JSON.stringify(obj, null, 2)
  } catch {
    return String(val)
  }
}

// Simple line-level diff for output payload text
interface DiffLine {
  type: 'added' | 'removed' | 'unchanged'
  text: string
}

const textDiffLines = computed<DiffLine[]>(() => {
  if (!diffData.value) return []
  const fromText = formatJSON(diffData.value.from.output_payload)
  const toText = formatJSON(diffData.value.to.output_payload)

  const fromLines = fromText.split('\n')
  const toLines = toText.split('\n')

  const result: DiffLine[] = []
  const maxLen = Math.max(fromLines.length, toLines.length)

  for (let i = 0; i < maxLen; i++) {
    const f = fromLines[i]
    const t = toLines[i]
    if (f === t) {
      result.push({ type: 'unchanged', text: f ?? '' })
    } else {
      if (f !== undefined) result.push({ type: 'removed', text: '- ' + f })
      if (t !== undefined) result.push({ type: 'added', text: '+ ' + t })
    }
  }
  return result
})

onMounted(() => {
  if (runId.value && fromStep.value && toStep.value) {
    loadDiff()
  }
})
</script>

<style scoped>
.diff-page {
  padding: 24px;
  max-width: 1400px;
  margin: 0 auto;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
}

.page-header h1 {
  margin: 0;
  font-size: 22px;
  font-weight: 600;
}

.selector-card {
  margin-bottom: 20px;
}

.diff-panel .panel-title {
  font-weight: 600;
  font-size: 15px;
}

.json-view {
  background: #1e1e2e;
  color: #cdd6f4;
  padding: 16px;
  border-radius: 6px;
  overflow-x: auto;
  font-size: 12px;
  line-height: 1.6;
  max-height: 500px;
  white-space: pre-wrap;
  word-break: break-all;
}

.text-diff {
  font-family: 'Courier New', monospace;
  font-size: 12px;
  line-height: 1.8;
  background: #1e1e2e;
  padding: 16px;
  border-radius: 6px;
  max-height: 400px;
  overflow-y: auto;
  white-space: pre-wrap;
}

.diff-line {
  display: block;
}

.diff-line.added {
  color: #a6e3a1;
  background: rgba(166, 227, 161, 0.08);
}

.diff-line.removed {
  color: #f38ba8;
  background: rgba(243, 139, 168, 0.08);
}

.diff-line.unchanged {
  color: #6c7086;
}
</style>
