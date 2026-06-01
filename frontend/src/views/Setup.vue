<template>
  <div class="setup-page">
    <section class="setup-panel">
      <div>
        <p class="eyebrow">NovelBuilder</p>
        <h1>Runtime initialization</h1>
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

      <div v-else class="status-grid">
        <div class="status-item">
          <span>Profile</span>
          <strong>{{ status?.profile || 'unknown' }}</strong>
        </div>
        <div class="status-item">
          <span>Database</span>
          <strong>{{ status?.backend?.database_driver || 'unknown' }}</strong>
        </div>
        <div class="status-item">
          <span>Session store</span>
          <strong>{{ status?.backend?.session_store || 'unknown' }}</strong>
        </div>
        <div class="status-item">
          <span>Accelerator</span>
          <strong>{{ accelerator }}</strong>
        </div>
      </div>

      <div class="actions">
        <el-button :icon="Refresh" @click="fetchStatus" :loading="loading">Refresh</el-button>
        <el-button type="primary" :icon="ArrowRight" @click="router.push('/login')">Continue</el-button>
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
  if (!status.value) return 'Checking backend, sidecar, session storage, and local acceleration.'
  return status.value.initialized ? 'The local runtime is ready.' : 'Initialization is still in progress.'
})

const accelerator = computed(() => {
  const caps = status.value?.sidecar?.capabilities
  return caps?.selected_accelerator || caps?.selected || 'cpu'
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
  width: min(760px, 100%);
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
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
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
</style>
