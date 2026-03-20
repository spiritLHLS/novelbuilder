<template>
  <div class="agent-routing p-6 max-w-4xl mx-auto">
    <div class="flex items-center justify-between mb-6">
      <div>
        <h1 class="text-2xl font-bold">多模型路由</h1>
        <p class="text-gray-500 mt-1">为不同 Agent 角色分配专属 AI 模型</p>
      </div>
      <button @click="$router.back()" class="px-4 py-2 border rounded hover:bg-gray-50">返回</button>
    </div>

    <!-- Error -->
    <div v-if="error" class="bg-red-50 border border-red-200 rounded p-3 text-red-700 text-sm mb-4">
      {{ error }}
    </div>

    <!-- Help text -->
    <div class="bg-blue-50 border border-blue-200 rounded p-4 mb-6 text-sm text-blue-800">
      <strong>全局路由：</strong>适用于所有项目。项目级路由在项目内优先生效（在项目详情页配置）。
      若某 Agent 无路由，则使用默认 AI 模型配置。
    </div>

    <!-- Routes table -->
    <div class="bg-white border rounded-lg overflow-hidden mb-6">
      <table class="w-full text-sm">
        <thead class="bg-gray-50 border-b">
          <tr>
            <th class="text-left px-4 py-3 font-medium">Agent 类型</th>
            <th class="text-left px-4 py-3 font-medium">AI 模型配置</th>
            <th class="text-left px-4 py-3 font-medium">模型</th>
            <th class="px-4 py-3 font-medium text-right">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="agent in agentTypes" :key="agent.value"
            class="border-b last:border-0 hover:bg-gray-50">
            <td class="px-4 py-3">
              <div class="font-medium">{{ agent.label }}</div>
              <div class="text-xs text-gray-400">{{ agent.value }}</div>
            </td>
            <td class="px-4 py-3">
              <select v-model="routeMap[agent.value]"
                class="border rounded p-1.5 text-sm w-full max-w-xs"
                @change="saveRoute(agent.value)">
                <option value="">— 使用默认 —</option>
                <option v-for="p in profiles" :key="p.id" :value="p.id">
                  {{ p.name }} ({{ p.provider }})
                </option>
              </select>
            </td>
            <td class="px-4 py-3 text-gray-500 text-xs">
              {{ getProfileModel(routeMap[agent.value]) || '—' }}
            </td>
            <td class="px-4 py-3 text-right">
              <button v-if="routeMap[agent.value]"
                @click="clearRoute(agent.value)"
                class="text-sm text-red-500 hover:underline">
                清除
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Agent descriptions -->
    <div class="bg-white border rounded-lg p-5">
      <h2 class="font-semibold mb-4">Agent 职责说明</h2>
      <div class="grid grid-cols-2 gap-4 text-sm">
        <div v-for="a in agentTypes" :key="a.value" class="flex gap-2">
          <span class="text-xl">{{ a.icon }}</span>
          <div>
            <div class="font-medium">{{ a.label }}</div>
            <div class="text-gray-500 text-xs">{{ a.desc }}</div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { agentRoutingApi, llmProfileApi } from '@/api'

const profiles = ref<any[]>([])
const routes = ref<any[]>([])
const routeMap = reactive<Record<string, string>>({})
const error = ref('')
const saving: Record<string, boolean> = reactive({})

const agentTypes = [
  { value: 'writer', label: '写手 Writer', icon: '✍️', desc: '负责生成章节正文' },
  { value: 'auditor', label: '审计员 Auditor', icon: '🔍', desc: '33维度质量检测' },
  { value: 'planner', label: '规划师 Planner', icon: '🗺️', desc: '大纲与剧情规划' },
  { value: 'reviser', label: '修改师 Reviser', icon: '✏️', desc: '章节修订与改进' },
  { value: 'radar', label: '雷达 Radar', icon: '📡', desc: '伏笔与连贯性检测' },
  { value: 'moderator', label: '主持人 Moderator', icon: '🎬', desc: '多轮审议协调' },
  { value: 'reference_analyzer', label: '参考书分析器', icon: '📚', desc: '大型参考小说深度分析（人物/世界观/大纲提取）' },
]

function getProfileModel(profileId: string): string {
  const p = profiles.value.find(x => x.id === profileId)
  return p?.model ?? ''
}

async function loadProfiles() {
  try {
    const res = await llmProfileApi.list()
    profiles.value = res.data.data ?? []
  } catch { /* ignore */ }
}

async function loadRoutes() {
  try {
    const res = await agentRoutingApi.listGlobal()
    routes.value = res.data.data ?? []
    routes.value.forEach((r: any) => {
      if (r.llm_profile_id) routeMap[r.agent_type] = r.llm_profile_id
    })
  } catch { /* ignore */ }
}

async function saveRoute(agentType: string) {
  const profileId = routeMap[agentType]
  saving[agentType] = true
  error.value = ''
  try {
    await agentRoutingApi.setGlobal(agentType, { llm_profile_id: profileId || null })
  } catch (e: any) {
    error.value = e.response?.data?.error || e.message
  } finally {
    saving[agentType] = false
  }
}

async function clearRoute(agentType: string) {
  routeMap[agentType] = ''
  await saveRoute(agentType)
}

onMounted(async () => {
  await Promise.all([loadProfiles(), loadRoutes()])
})
</script>
