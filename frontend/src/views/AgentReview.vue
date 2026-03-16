<template>
  <div class="agent-review-page">
    <!-- Config Panel -->
    <el-card class="config-card" shadow="never">
      <template #header>
        <div class="card-header">
          <span>🤖 多智能体深度评审</span>
          <el-tag type="info" size="small">5位专家 AI 辩论</el-tag>
        </div>
      </template>

      <el-form :model="form" inline label-width="80px">
        <el-form-item label="评审范围">
          <el-select v-model="form.scope" style="width: 160px">
            <el-option label="完整项目" value="full" />
            <el-option label="整书蓝图" value="blueprint" />
            <el-option label="指定章节" value="chapter" />
          </el-select>
        </el-form-item>

        <el-form-item v-if="form.scope === 'chapter'" label="目标章节">
          <el-select v-model="form.targetId" style="width: 200px" placeholder="选择章节">
            <el-option
              v-for="ch in chapters"
              :key="ch.id"
              :label="`第${ch.chapter_num}章 ${ch.title}`"
              :value="ch.id"
            />
          </el-select>
        </el-form-item>

        <el-form-item label="辩论轮次">
          <el-input-number v-model="form.rounds" :min="1" :max="5" style="width: 120px" />
        </el-form-item>

        <el-form-item>
          <el-button
            type="primary"
            :loading="isStreaming"
            :disabled="isStreaming"
            @click="startReview"
          >
            <el-icon><VideoPlay /></el-icon>
            {{ isStreaming ? '评审中...' : '开始评审' }}
          </el-button>
          <el-button v-if="isStreaming" @click="stopReview">
            <el-icon><VideoPause /></el-icon>
            停止
          </el-button>
          <el-button v-if="messages.length > 0 && !isStreaming" @click="exportReport">
            <el-icon><Download /></el-icon>
            导出报告
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- Agent Legend -->
    <div class="agent-legend">
      <el-tag
        v-for="agent in agentLegend"
        :key="agent.role"
        :color="agent.bg"
        :style="{ color: agent.color, borderColor: agent.border, marginRight: '8px' }"
        size="small"
      >
        {{ agent.label }}
      </el-tag>
    </div>

    <el-row :gutter="16">
      <!-- Chat Panel -->
      <el-col :span="16">
        <el-card class="chat-card" shadow="never">
          <template #header>
            <div class="card-header">
              <span>💬 辩论实录</span>
              <el-badge v-if="messages.length > 0" :value="messages.length" type="info" />
            </div>
          </template>

          <div ref="chatScroll" class="chat-container">
            <div v-if="messages.length === 0 && !isStreaming" class="chat-empty">
              <el-empty description="点击「开始评审」启动多智能体辩论" />
            </div>

            <!-- Streaming indicator -->
            <div v-if="isStreaming && messages.length === 0" class="thinking-indicator">
              <el-icon class="spin"><Loading /></el-icon>
              <span>智能体正在分析项目材料...</span>
            </div>

            <transition-group name="msg-fade" tag="div">
              <div
                v-for="(msg, idx) in messages"
                :key="idx"
                class="message-item"
                :class="getRoleClass(msg.agent)"
              >
                <div class="message-header">
                  <span class="agent-name" :style="{ color: getAgentColor(msg.agent) }">
                    {{ msg.agent_name }}
                  </span>
                  <el-tag
                    v-for="tag in msg.tags"
                    :key="tag"
                    :type="getTagType(tag)"
                    size="small"
                    style="margin-left: 4px"
                  >
                    {{ getTagLabel(tag) }}
                  </el-tag>
                  <span class="round-badge">第 {{ msg.round }} 轮</span>
                </div>
                <div class="message-content" v-html="formatContent(msg.content)" />
              </div>
            </transition-group>

            <!-- Typing indicator -->
            <div v-if="isStreaming && messages.length > 0" class="typing-indicator">
              <span>{{ currentAgentName }} 正在发言</span>
              <span class="dots"><span>.</span><span>.</span><span>.</span></span>
            </div>
          </div>
        </el-card>
      </el-col>

      <!-- Issues & Sessions Panel -->
      <el-col :span="8">
        <!-- Issues list -->
        <el-card class="issues-card" shadow="never" style="margin-bottom: 16px">
          <template #header>
            <div class="card-header">
              <span>⚠️ 发现问题</span>
              <el-badge v-if="issues.length > 0" :value="issues.length" type="warning" />
            </div>
          </template>

          <div v-if="issues.length === 0" class="issues-empty">
            <p style="color: #909399; text-align: center; font-size: 13px">评审完成后显示问题列表</p>
          </div>

          <div v-for="(issue, idx) in issues" :key="idx" class="issue-item">
            <div class="issue-header">
              <el-tag :type="getSeverityType(issue.severity)" size="small">
                {{ severityLabel(issue.severity) }}
              </el-tag>
              <el-tag type="info" size="small" style="margin-left: 4px">
                {{ categoryLabel(issue.category) }}
              </el-tag>
            </div>
            <div class="issue-title">{{ issue.title }}</div>
            <div v-if="issue.suggestion" class="issue-suggestion">
              💡 {{ issue.suggestion }}
            </div>
          </div>
        </el-card>

        <!-- Consensus -->
        <el-card v-if="consensusText" class="consensus-card" shadow="never" style="margin-bottom: 16px">
          <template #header>
            <span>🎯 主持人综合报告</span>
          </template>
          <div class="consensus-text" v-html="formatContent(consensusText)" />
        </el-card>

        <!-- Past Sessions -->
        <el-card class="sessions-card" shadow="never">
          <template #header>
            <div class="card-header">
              <span>📋 历史评审</span>
              <el-button link size="small" @click="loadSessions">刷新</el-button>
            </div>
          </template>
          <div v-if="sessions.length === 0" style="color: #909399; font-size: 13px; text-align: center">
            暂无历史评审
          </div>
          <div
            v-for="s in sessions"
            :key="s.id"
            class="session-item"
            @click="loadSession(s.id)"
          >
            <div>
              <el-tag :type="s.status === 'completed' ? 'success' : 'warning'" size="small">
                {{ s.status === 'completed' ? '已完成' : '进行中' }}
              </el-tag>
              <span style="margin-left: 8px; font-size: 12px">{{ scopeLabel(s.review_scope) }}</span>
            </div>
            <div style="font-size: 11px; color: #909399; margin-top: 4px">
              {{ formatTime(s.created_at) }}
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, nextTick, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { agentReviewApi, chapterApi } from '@/api/index'
import { ElMessage } from 'element-plus'

const route = useRoute()
const projectId = route.params.projectId as string

// Form state
const form = reactive({ scope: 'full', targetId: '', rounds: 3 })

// Chat state
const messages = ref<any[]>([])
const issues = ref<any[]>([])
const consensusText = ref('')
const isStreaming = ref(false)
const currentAgentName = ref('')
const chatScroll = ref<HTMLElement>()

// Data
const chapters = ref<any[]>([])
const sessions = ref<any[]>([])

let abortController: AbortController | null = null

const agentLegend = [
  { role: 'outline_critic',      label: '📐 大纲批评家',      color: '#409eff', bg: '#ecf5ff', border: '#b3d8ff' },
  { role: 'timeline_inspector',  label: '⏱️ 时间线审核员',    color: '#67c23a', bg: '#f0f9eb', border: '#c2e7b0' },
  { role: 'plot_coherence',      label: '🔗 剧情连贯性专家',  color: '#e6872c', bg: '#fdf6ec', border: '#f5dab1' },
  { role: 'character_analyst',   label: '👥 角色设计分析师',  color: '#9b59b6', bg: '#f5f0fb', border: '#d9c4f0' },
  { role: 'devils_advocate',     label: '😈 魔鬼代言人',      color: '#f56c6c', bg: '#fef0f0', border: '#fbc4c4' },
  { role: 'moderator',           label: '🎯 主持人',          color: '#f0a020', bg: '#fefbe9', border: '#f8e377' },
]

const agentColorMap: Record<string, string> = Object.fromEntries(agentLegend.map(a => [a.role, a.color]))

function getAgentColor(role: string) {
  return agentColorMap[role] ?? '#606266'
}

function getRoleClass(role: string) {
  return `role-${role.replace(/_/g, '-')}`
}

function getTagType(tag: string) {
  const map: Record<string, string> = {
    issue: 'danger', suggestion: 'success', agreement: 'info',
    disagreement: 'warning', final: 'primary', consensus: 'success',
  }
  return map[tag] ?? 'info'
}

function getTagLabel(tag: string) {
  const map: Record<string, string> = {
    issue: '问题', suggestion: '建议', agreement: '认同',
    disagreement: '质疑', final: '终轮', consensus: '共识',
  }
  return map[tag] ?? tag
}

function getSeverityType(sev: string) {
  return sev === 'critical' ? 'danger' : sev === 'major' ? 'warning' : 'info'
}

function severityLabel(sev: string) {
  return sev === 'critical' ? '严重' : sev === 'major' ? '主要' : '次要'
}

function categoryLabel(cat: string) {
  const m: Record<string, string> = { outline: '大纲', timeline: '时间线', plot: '剧情', character: '角色', general: '综合' }
  return m[cat] ?? cat
}

function scopeLabel(scope: string) {
  return scope === 'full' ? '完整项目' : scope === 'blueprint' ? '蓝图' : '章节'
}

function formatTime(iso: string) {
  return new Date(iso).toLocaleString('zh-CN')
}

function formatContent(text: string) {
  // Very simple markdown-like rendering: bold, line breaks
  return text
    .replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>')
    .replace(/\n/g, '<br/>')
}

async function scrollToBottom() {
  await nextTick()
  if (chatScroll.value) {
    chatScroll.value.scrollTop = chatScroll.value.scrollHeight
  }
}

async function startReview() {
  if (form.scope === 'chapter' && !form.targetId) {
    ElMessage.warning('请先选择目标章节')
    return
  }

  messages.value = []
  issues.value = []
  consensusText.value = ''
  isStreaming.value = true
  currentAgentName.value = ''

  abortController = new AbortController()

  agentReviewApi.stream(
    projectId,
    { scope: form.scope, target_id: form.targetId || undefined, rounds: form.rounds },
    (msg: any) => {
      if (msg.agent === 'moderator') {
        consensusText.value = msg.content
      }
      currentAgentName.value = msg.agent_name ?? ''
      messages.value.push(msg)
      scrollToBottom()
    },
    async () => {
      isStreaming.value = false
      currentAgentName.value = ''
      // Load the latest session to get structured issues
      await loadSessions()
      if (sessions.value.length > 0) {
        const sess = await agentReviewApi.get(sessions.value[0].id)
        if (sess.data?.issues) issues.value = sess.data.issues
      }
    },
    abortController.signal,
  )
}

function stopReview() {
  abortController?.abort()
  isStreaming.value = false
}

async function loadSessions() {
  try {
    const res = await agentReviewApi.list(projectId)
    sessions.value = res.data ?? []
  } catch {}
}

async function loadSession(id: string) {
  try {
    const res = await agentReviewApi.get(id)
    const data = res.data
    messages.value = data.messages ?? []
    issues.value = data.issues ?? []
    if (data.consensus) consensusText.value = data.consensus
    scrollToBottom()
  } catch {
    ElMessage.error('加载历史评审失败')
  }
}

function exportReport() {
  const lines: string[] = ['# 多智能体评审报告\n']
  for (const msg of messages.value) {
    lines.push(`## [第${msg.round}轮] ${msg.agent_name}`)
    lines.push(msg.content)
    lines.push('')
  }
  lines.push('---\n## 主持人综合报告')
  lines.push(consensusText.value)

  const blob = new Blob([lines.join('\n')], { type: 'text/markdown' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `agent-review-${Date.now()}.md`
  a.click()
  URL.revokeObjectURL(url)
}

onMounted(async () => {
  try {
    const res = await chapterApi.list(projectId)
    chapters.value = res.data ?? []
  } catch {}
  await loadSessions()
})
</script>

<style scoped>
.agent-review-page {
  padding: 20px;
  max-width: 1400px;
}

.config-card {
  margin-bottom: 12px;
}

.card-header {
  display: flex;
  align-items: center;
  gap: 12px;
}

.agent-legend {
  margin-bottom: 16px;
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.chat-card {
  height: calc(100vh - 280px);
  display: flex;
  flex-direction: column;
}

.chat-card :deep(.el-card__body) {
  flex: 1;
  overflow: hidden;
  padding: 0;
}

.chat-container {
  height: 100%;
  overflow-y: auto;
  padding: 16px;
}

.chat-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 200px;
}

.message-item {
  margin-bottom: 16px;
  padding: 12px 16px;
  border-radius: 8px;
  border-left: 4px solid transparent;
  background: #f8f9fa;
  animation: slideIn 0.3s ease;
}

.message-item.role-outline-critic      { border-left-color: #409eff; background: #f0f6ff; }
.message-item.role-timeline-inspector  { border-left-color: #67c23a; background: #f0faf0; }
.message-item.role-plot-coherence      { border-left-color: #e6872c; background: #fffbf0; }
.message-item.role-character-analyst   { border-left-color: #9b59b6; background: #faf5ff; }
.message-item.role-devils-advocate     { border-left-color: #f56c6c; background: #fff5f5; }
.message-item.role-moderator           { border-left-color: #f0a020; background: #fffde9; }

.message-header {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 8px;
}

.agent-name {
  font-weight: 600;
  font-size: 14px;
}

.round-badge {
  margin-left: auto;
  font-size: 11px;
  color: #c0c4cc;
}

.message-content {
  font-size: 14px;
  line-height: 1.7;
  color: #303133;
}

.typing-indicator {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 16px;
  color: #909399;
  font-size: 13px;
}

.thinking-indicator {
  display: flex;
  align-items: center;
  gap: 8px;
  justify-content: center;
  padding: 40px;
  color: #909399;
}

.dots span {
  animation: blink 1.4s infinite;
  font-size: 20px;
}
.dots span:nth-child(2) { animation-delay: 0.2s; }
.dots span:nth-child(3) { animation-delay: 0.4s; }

.spin {
  animation: spin 1s linear infinite;
}

.issues-card {
  max-height: 320px;
  overflow-y: auto;
}

.issues-card :deep(.el-card__body) {
  overflow-y: auto;
  max-height: 260px;
}

.issue-item {
  padding: 8px 0;
  border-bottom: 1px solid #f0f0f0;
}
.issue-item:last-child { border-bottom: none; }

.issue-header {
  margin-bottom: 4px;
}

.issue-title {
  font-size: 13px;
  color: #303133;
  font-weight: 500;
  margin-top: 4px;
}

.issue-suggestion {
  font-size: 12px;
  color: #67c23a;
  margin-top: 4px;
}

.consensus-card :deep(.el-card__body) {
  max-height: 300px;
  overflow-y: auto;
}

.consensus-text {
  font-size: 13px;
  line-height: 1.8;
  color: #303133;
}

.session-item {
  padding: 8px 0;
  border-bottom: 1px solid #f0f0f0;
  cursor: pointer;
  transition: background 0.2s;
}
.session-item:hover { background: #f5f7fa; }
.session-item:last-child { border-bottom: none; }

.msg-fade-enter-active { transition: all 0.3s ease; }
.msg-fade-enter-from { opacity: 0; transform: translateY(10px); }

@keyframes slideIn {
  from { opacity: 0; transform: translateY(8px); }
  to   { opacity: 1; transform: translateY(0); }
}

@keyframes blink {
  0%, 80%, 100% { opacity: 0; }
  40% { opacity: 1; }
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to   { transform: rotate(360deg); }
}
</style>
