<template>
  <div class="workflow">
    <div class="page-header">
      <h1>工作流控制台</h1>
      <div>
        <el-button type="primary" @click="startWorkflow" :loading="starting" :disabled="!!currentRun">
          <el-icon><VideoPlay /></el-icon>启动工作流
        </el-button>
        <el-button v-if="currentRun" type="warning" @click="showRollback">回滚</el-button>
        <el-button v-if="currentRun" @click="goDiff">快照对比</el-button>
      </div>
    </div>

    <!-- Current Run Status -->
    <el-card v-if="currentRun" shadow="hover" class="run-status">
      <el-row :gutter="20" align="middle">
        <el-col :span="4">
          <div class="stat-label">当前运行</div>
          <div class="stat-value">{{ currentRun.id?.substring(0, 8) }}</div>
        </el-col>
        <el-col :span="4">
          <div class="stat-label">阶段</div>
          <el-tag :type="phaseType(currentRun.current_step)" size="large">
            {{ phaseLabel(currentRun.current_step) }}
          </el-tag>
        </el-col>
        <el-col :span="4">
          <div class="stat-label">步骤数</div>
          <div class="stat-value">{{ currentRun.steps?.length || 0 }}</div>
        </el-col>
        <el-col :span="4">
          <div class="stat-label">审核模式</div>
          <el-tag :type="currentRun.strict_review ? 'danger' : 'success'" size="small">
            {{ currentRun.strict_review ? '严格审核' : '自动审核' }}
          </el-tag>
        </el-col>
        <el-col :span="8">
          <div class="stat-label">启动时间</div>
          <div class="stat-value">{{ formatDate(currentRun.created_at) }}</div>
        </el-col>
      </el-row>
    </el-card>

    <el-row :gutter="20" style="margin-top: 20px;">
      <!-- Steps Timeline -->
      <el-col :span="14">
        <el-card shadow="hover">
          <template #header><span>工作流步骤</span></template>
          <div class="steps-container" v-if="steps.length">
            <el-timeline>
              <el-timeline-item
                v-for="step in steps" :key="step.id"
                :type="stepType(step.status)"
                :timestamp="formatDate(step.created_at)"
                placement="top"
                :hollow="step.status === 'pending'"
              >
                <el-card shadow="never" class="step-card" :class="step.status">
                  <div class="step-header">
                    <span class="step-name">{{ stepKeyLabel(step.step_key) }}</span>
                    <el-tag :type="stepType(step.status)" size="small">{{ stepLabel(step.status) }}</el-tag>
                  </div>
                  <p v-if="step.review_comment" class="step-desc">{{ step.review_comment }}</p>

                  <!-- Step Actions: navigate to the appropriate review page -->
                  <div class="step-actions" v-if="step.status === 'pending_review' || step.status === 'generated'">
                    <el-button size="small" type="primary" @click="goReview(step)">前往审核</el-button>
                  </div>
                </el-card>
              </el-timeline-item>
            </el-timeline>
          </div>
          <el-empty v-else description="暂无步骤，请先启动工作流" />
        </el-card>
      </el-col>

      <!-- Snapshots & History -->
      <el-col :span="10">
        <!-- Workflow Rules -->
        <el-card shadow="hover">
          <template #header><span>工作流规则</span></template>
          <div class="rule-list">
            <div class="rule-item">
              <el-tag type="danger" size="small">WF_001</el-tag>
              <span>蓝图批准后才能生成章节</span>
            </div>
            <div class="rule-item">
              <el-tag type="danger" size="small">WF_002</el-tag>
              <span>章节按顺序生成</span>
            </div>
            <div class="rule-item">
              <el-tag type="danger" size="small">WF_003</el-tag>
              <span>前一章需通过审核</span>
            </div>
            <div class="rule-item">
              <el-tag type="warning" size="small">WF_004</el-tag>
              <span>AI质检分数 ≥ 阈值</span>
            </div>
            <div class="rule-item">
              <el-tag type="warning" size="small">WF_005</el-tag>
              <span>驳回后必须修改</span>
            </div>
            <div class="rule-item">
              <el-tag type="info" size="small">WF_006</el-tag>
              <span>幂等性保护</span>
            </div>
            <div class="rule-item">
              <el-tag type="info" size="small">WF_007</el-tag>
              <span>卷完结需通过严格审核</span>
            </div>
          </div>
        </el-card>

        <!-- Run History -->
        <el-card shadow="hover" style="margin-top: 16px;">
          <template #header><span>历史运行</span></template>
          <el-table :data="history" size="small">
            <el-table-column label="ID" width="100">
              <template #default="{ row }">{{ row.id?.substring(0, 8) }}</template>
            </el-table-column>
            <el-table-column label="阶段">
              <template #default="{ row }">{{ phaseLabel(row.current_step) }}</template>
            </el-table-column>
            <el-table-column prop="status" label="状态" width="80" />
            <el-table-column label="时间" width="160">
              <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
            </el-table-column>
          </el-table>
          <el-empty v-if="!history.length" description="暂无历史" :image-size="40" />
        </el-card>
      </el-col>
    </el-row>

    <!-- Rollback Dialog -->
    <el-dialog v-model="showRollbackDialog" title="回滚操作" width="500px">
      <el-alert type="warning" :closable="false" show-icon style="margin-bottom: 16px;">
        回滚将撤销指定步骤之后的所有操作，此操作不可逆。
      </el-alert>
      <el-form label-position="top">
        <el-form-item label="回滚到步骤">
          <el-select v-model="rollbackTargetStep" style="width: 100%;">
            <el-option v-for="s in steps" :key="s.id" :label="stepKeyLabel(s.step_key)" :value="s.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="回滚原因">
          <el-input v-model="rollbackReason" type="textarea" :rows="3" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showRollbackDialog = false">取消</el-button>
        <el-button type="danger" @click="executeRollback" :loading="rollingBack">确认回滚</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { workflowApi } from '@/api'

const route = useRoute()
const router = useRouter()
const projectId = route.params.projectId as string

const currentRun = ref<any>(null)
const steps = ref<any[]>([])
const history = ref<any[]>([])
const starting = ref(false)

const showRollbackDialog = ref(false)
const rollbackTargetStep = ref('')
const rollbackReason = ref('')
const rollingBack = ref(false)

let pollTimer: NodeJS.Timeout | null = null

function phaseType(step: string) {
  const m: Record<string, string> = {
    blueprint: 'primary', chapter_generate: '', review: 'warning', completed: 'success',
  }
  return (m[step] || 'info') as any
}

function phaseLabel(step: string) {
  const m: Record<string, string> = {
    blueprint: '蓝图阶段', chapter_generate: '生成阶段', review: '审核阶段', completed: '已完成',
  }
  return m[step] || step || '-'
}

function stepKeyLabel(key: string) {
  const m: Record<string, string> = {
    blueprint: '整书蓝图',
    chapter_generate: '章节生成',
    review: '审核阶段',
  }
  if (m[key]) return m[key]
  // Handle chapter_N patterns
  const match = key?.match(/^chapter[_-]?(\d+)$/)
  if (match) return `第 ${match[1]} 章`
  return key || '-'
}

function stepType(status: string) {
  const m: Record<string, string> = {
    pending: 'info', generated: '', pending_review: 'warning', approved: 'success', rejected: 'danger',
    needs_recheck: 'warning', rolled_back: 'info',
  }
  return (m[status] || 'info') as any
}

function stepLabel(status: string) {
  const m: Record<string, string> = {
    pending: '等待中', generated: '已生成', pending_review: '待审核',
    approved: '已通过', rejected: '已驳回', needs_recheck: '需重检', rolled_back: '已回滚',
  }
  return m[status] || status
}

function formatDate(d: string) {
  return d ? new Date(d).toLocaleString('zh-CN') : '-'
}

// Navigate to the appropriate page to perform the review action for this step.
function goReview(step: any) {
  if (step.step_key === 'blueprint') {
    router.push({ name: 'blueprint', params: { projectId } })
  } else if (step.output_ref) {
    // For chapter steps, output_ref holds the chapter ID.
    router.push({ name: 'chapter-detail', params: { projectId, chapterId: step.output_ref } })
  } else {
    router.push({ name: 'chapters', params: { projectId } })
  }
}

onMounted(() => {
  fetchWorkflow()
  // Auto-refresh every 5 seconds to show real-time workflow progress
  pollTimer = setInterval(fetchWorkflow, 5000)
})

onUnmounted(() => {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
})

async function fetchWorkflow() {
  try {
    const res = await workflowApi.getHistory(projectId)
    const runs: any[] = res.data.data || []
    if (runs.length) {
      currentRun.value = runs[0]
      steps.value = currentRun.value.steps || []
      history.value = runs
    } else {
      currentRun.value = null
      steps.value = []
      history.value = []
    }
  } catch { /* empty */ }
}

async function startWorkflow() {
  starting.value = true
  try {
    await workflowApi.start(projectId)
    ElMessage.success('工作流已启动')
    await fetchWorkflow()
  } catch {
    ElMessage.error('启动失败')
  } finally {
    starting.value = false
  }
}

function showRollback() {
  rollbackTargetStep.value = ''
  rollbackReason.value = ''
  showRollbackDialog.value = true
}

function goDiff() {
  if (!currentRun.value?.id) return
  router.push({ name: 'workflow-diff', params: { runId: currentRun.value.id } })
}

async function executeRollback() {
  if (!rollbackTargetStep.value) { ElMessage.warning('请选择回滚目标'); return }
  rollingBack.value = true
  try {
    await workflowApi.rollback(currentRun.value.id, {
      target_step_id: rollbackTargetStep.value,
      reason: rollbackReason.value,
    })
    ElMessage.success('回滚完成')
    showRollbackDialog.value = false
    await fetchWorkflow()
  } catch {
    ElMessage.error('回滚失败')
  } finally {
    rollingBack.value = false
  }
}
</script>

<style scoped>
.workflow { max-width: 1400px; margin: 0 auto; }
.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; }
.page-header h1 { font-size: 24px; color: var(--nb-text-primary); }
.run-status { }
.stat-label { color: var(--nb-text-secondary); font-size: 13px; margin-bottom: 4px; }
.stat-value { color: var(--nb-text-primary); font-size: 16px; font-weight: 500; }
.steps-container { max-height: 70vh; overflow-y: auto; }
.step-card { background: transparent; }
.step-card.approved { border-left: 3px solid #67c23a; }
.step-card.rejected { border-left: 3px solid #f56c6c; }
.step-card.pending_review { border-left: 3px solid #e6a23c; }
.step-card.generated { border-left: 3px solid #409eff; }
.step-header { display: flex; justify-content: space-between; align-items: center; }
.step-name { color: var(--nb-text-primary); font-weight: 500; }
.step-desc { color: var(--nb-text-secondary); font-size: 13px; margin-top: 8px; }
.step-actions { margin-top: 12px; }
.rule-list { }
.rule-item { display: flex; align-items: center; gap: 8px; padding: 8px 0; border-bottom: 1px solid var(--nb-divider); color: var(--nb-text-secondary); font-size: 13px; }
</style>
