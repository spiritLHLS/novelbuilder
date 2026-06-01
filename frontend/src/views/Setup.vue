<template>
  <div class="setup-page">
    <section class="setup-panel">
      <div>
        <p class="eyebrow">NovelBuilder</p>
        <h1>初始化向导</h1>
        <p class="subtitle">{{ subtitle }}</p>
      </div>

      <el-alert
        v-if="error"
        type="error"
        :title="error"
        show-icon
        :closable="false"
      />

      <el-skeleton v-if="loading" :rows="6" animated />

      <template v-else>
        <el-steps :active="activeStep" finish-status="success" align-center>
          <el-step title="运行检查" />
          <el-step title="登录" />
          <el-step title="配置模型" />
          <el-step title="创建项目" />
        </el-steps>

        <div class="status-grid">
          <div class="status-item">
            <span>版本</span>
            <strong>{{ status?.version || 'dev' }}</strong>
          </div>
          <div class="status-item">
            <span>部署档位</span>
            <strong>{{ status?.profile || 'unknown' }}</strong>
          </div>
          <div class="status-item">
            <span>数据库</span>
            <strong>{{ status?.backend?.database_driver || 'unknown' }}</strong>
          </div>
          <div class="status-item">
            <span>会话存储</span>
            <strong>{{ status?.backend?.session_store || 'unknown' }}</strong>
          </div>
          <div class="status-item">
            <span>Sidecar</span>
            <strong>{{ sidecarOk ? 'ready' : 'degraded' }}</strong>
          </div>
          <div class="status-item">
            <span>加速器</span>
            <strong>{{ accelerator }}</strong>
          </div>
        </div>

        <el-alert
          type="warning"
          title="首次部署后请立刻通过环境变量修改 ADMIN_PASSWORD；公网访问时同时配置 ALLOWED_ORIGINS 和反向代理的 HTTPS。"
          show-icon
          :closable="false"
        />

        <el-alert
          v-if="status && !sidecarOk"
          type="warning"
          title="Python Sidecar 暂不可用，参考书分析、图谱/向量、上传自动化等能力会降级。请查看容器日志或 /var/log/python-sidecar_err.log。"
          show-icon
          :closable="false"
        />

        <div class="guide-list">
          <div class="guide-item">
            <span class="step-number">1</span>
            <div>
              <strong>确认运行状态</strong>
              <p>当前登录限流为 {{ status?.security?.login_max_attempts || 5 }} 次失败后锁定 {{ lockoutMinutes }} 分钟。</p>
            </div>
          </div>
          <div class="guide-item">
            <span class="step-number">2</span>
            <div>
              <strong>登录后台</strong>
              <p>使用部署时设置的管理员账号登录，首次进入会看到完整使用弹窗。</p>
            </div>
          </div>
          <div class="guide-item">
            <span class="step-number">3</span>
            <div>
              <strong>配置模型与路由</strong>
              <p>在 AI 模型配置中添加可用 LLM Profile，再到多模型路由为 writer/reviewer 分配模型。</p>
            </div>
          </div>
          <div class="guide-item">
            <span class="step-number">4</span>
            <div>
              <strong>创建项目并开始生成</strong>
              <p>先建立项目、补齐世界观/角色/大纲，再生成蓝图和章节。</p>
            </div>
          </div>
        </div>
      </template>

      <div class="actions">
        <el-button :icon="Refresh" @click="fetchStatus" :loading="loading">刷新</el-button>
        <el-button type="primary" :icon="ArrowRight" @click="router.push('/login')">继续登录</el-button>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowRight, Refresh } from '@element-plus/icons-vue'
import { setupApi } from '@/api'

const router = useRouter()
const loading = ref(false)
const error = ref('')
const status = ref<any>(null)

const subtitle = computed(() => {
  if (!status.value) return '正在检查后端、数据库、Sidecar、会话存储和本地加速器。'
  return status.value.initialized ? '运行环境已就绪，可以继续登录并完成首次配置。' : '初始化仍在进行中。'
})

const accelerator = computed(() => {
  const caps = status.value?.sidecar?.capabilities
  return caps?.selected_accelerator || caps?.selected || 'cpu'
})

const sidecarOk = computed(() => Boolean(status.value?.sidecar?.ok))
const activeStep = computed(() => status.value?.initialized ? 1 : 0)
const lockoutMinutes = computed(() => {
  const seconds = Number(status.value?.security?.login_lockout_seconds || 900)
  return Math.max(1, Math.round(seconds / 60))
})

async function fetchStatus() {
  loading.value = true
  error.value = ''
  try {
    const res = await setupApi.status()
    status.value = res.data
  } catch (err: any) {
    error.value = err?.response?.data?.error || err?.message || 'Failed to load runtime status'
  } finally {
    loading.value = false
  }
}

onMounted(fetchStatus)
</script>

<style scoped>
.setup-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 32px;
  background: #f6f7fb;
}

.setup-panel {
  width: min(900px, 100%);
  display: grid;
  gap: 24px;
  padding: 32px;
  border: 1px solid #dde2ea;
  border-radius: 8px;
  background: #fff;
  box-shadow: 0 18px 45px rgba(15, 23, 42, 0.08);
}

.eyebrow {
  margin: 0 0 8px;
  color: #64748b;
  font-size: 13px;
  text-transform: uppercase;
  letter-spacing: 0;
}

h1 {
  margin: 0;
  color: #0f172a;
  font-size: 28px;
  line-height: 1.2;
}

.subtitle {
  margin: 10px 0 0;
  color: #475569;
}

.status-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}

.status-item {
  display: grid;
  gap: 8px;
  min-height: 88px;
  padding: 16px;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  background: #f8fafc;
}

.status-item span {
  color: #64748b;
  font-size: 13px;
}

.status-item strong {
  overflow-wrap: anywhere;
  color: #111827;
  font-size: 18px;
}

.actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}

.guide-list {
  display: grid;
  gap: 12px;
}

.guide-item {
  display: grid;
  grid-template-columns: 32px 1fr;
  gap: 12px;
  align-items: start;
  padding: 14px 0;
  border-top: 1px solid #e2e8f0;
}

.guide-item:first-child {
  border-top: 0;
}

.step-number {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: #2563eb;
  color: #fff;
  font-size: 13px;
  font-weight: 700;
}

.guide-item strong {
  color: #111827;
  font-size: 15px;
}

.guide-item p {
  margin: 6px 0 0;
  color: #475569;
  line-height: 1.6;
}
</style>
