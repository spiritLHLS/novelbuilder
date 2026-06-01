<template>
  <el-dialog
    :model-value="modelValue"
    title="首次使用向导"
    width="min(760px, calc(100vw - 32px))"
    class="first-run-guide"
    append-to-body
    @close="closeGuide(false)"
  >
    <el-steps :active="active" finish-status="success" align-center class="guide-steps">
      <el-step v-for="step in steps" :key="step.title" :title="step.title" />
    </el-steps>

    <section class="guide-body">
      <div class="guide-copy">
        <p class="guide-kicker">{{ currentStep.kicker }}</p>
        <h3>{{ currentStep.heading }}</h3>
        <p>{{ currentStep.description }}</p>
      </div>
      <ol class="guide-checklist">
        <li v-for="item in currentStep.items" :key="item">{{ item }}</li>
      </ol>
      <div v-if="currentStep.path" class="guide-action">
        <el-button type="primary" :icon="Position" @click="goTo(currentStep.path)">
          前往{{ currentStep.actionLabel }}
        </el-button>
      </div>
    </section>

    <template #footer>
      <div class="guide-footer">
        <el-button @click="closeGuide(false)">稍后</el-button>
        <div class="guide-nav">
          <el-button :disabled="active === 0" @click="active--">上一步</el-button>
          <el-button v-if="!isLast" type="primary" @click="active++">下一步</el-button>
          <el-button v-else type="primary" @click="closeGuide(true)">完成</el-button>
        </div>
      </div>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Position } from '@element-plus/icons-vue'

const GUIDE_KEY = 'nb_first_run_guide_done'

const props = defineProps<{
  modelValue: boolean
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
}>()

const router = useRouter()
const active = ref(0)

interface GuideStep {
  title: string
  kicker: string
  heading: string
  description: string
  items: string[]
  path?: string
  actionLabel?: string
}

const steps: GuideStep[] = [
  {
    title: '检查',
    kicker: '启动后先看健康状态',
    heading: '确认运行环境已经就绪',
    description: '进入 setup 页确认后端、数据库、Sidecar 和加速器状态。Docker 用户也可以用健康检查判断服务是否可访问。',
    items: [
      '打开 /setup，确认 Runtime initialization 显示 ready。',
      '公网部署时先设置 ADMIN_PASSWORD，并按需设置 ALLOWED_ORIGINS。',
      'SQLite 档适合轻量体验，full 档会启用图谱、向量和上传自动化。',
    ],
    path: '/setup',
    actionLabel: '启动页',
  },
  {
    title: '模型',
    kicker: '生成前必须配置模型',
    heading: '添加至少一个可用 LLM Profile',
    description: 'NovelBuilder 把模型、密钥和 Agent 路由保存在数据库中，不需要把 API Key 写进前端代码。',
    items: [
      '在 AI 模型配置里添加 provider、base URL、model 和 API Key。',
      '保存后设为默认模型，再到多模型路由给 writer/reviewer 等角色分配模型。',
      '如果生成任务失败，先检查系统日志和任务队列里的错误信息。',
    ],
    path: '/settings/llm',
    actionLabel: '模型配置',
  },
  {
    title: '项目',
    kicker: '先建立一本书的工作空间',
    heading: '创建项目并填写基础设定',
    description: '项目会承载世界观、角色、大纲、章节、参考书和审稿记录。',
    items: [
      '在项目管理中创建项目，填写题材、目标字数和单章字数。',
      '进入创作工作台后，先补齐世界观、角色和术语表。',
      '长篇项目建议先生成整书蓝图，再分批生成章节大纲。',
    ],
    path: '/projects',
    actionLabel: '项目管理',
  },
  {
    title: '参考',
    kicker: '可选但很有用',
    heading: '导入参考书和续写素材',
    description: '参考书会进入分析、知识库和风格参考流程，用于提升长文一致性。',
    items: [
      '在参考书管理上传 txt/pdf，或从支持站点搜索和导入。',
      '导入后执行分析，再按需重建 RAG 知识库。',
      '续写旧文时使用导入续写，把已有章节转成项目上下文。',
    ],
  },
  {
    title: '生成',
    kicker: '最后进入生产流程',
    heading: '从蓝图到章节逐步推进',
    description: '建议先生成蓝图和章节大纲，再通过章节管理或工作流控制台生成正文并审稿。',
    items: [
      '蓝图通过后再生成章节，避免后续大量返工。',
      '任务队列会显示后台任务状态，失败任务可查看错误并重试。',
      '质量检测、多智能体评审和变更传播用于稳定长篇一致性。',
    ],
  },
]

const currentStep = computed(() => steps[active.value])
const isLast = computed(() => active.value === steps.length - 1)

watch(() => props.modelValue, (visible) => {
  if (visible) {
    active.value = 0
  }
})

function closeGuide(markDone: boolean) {
  if (markDone) {
    localStorage.setItem(GUIDE_KEY, '1')
  }
  emit('update:modelValue', false)
}

function goTo(path: string) {
  router.push(path)
  closeGuide(false)
}
</script>

<style scoped>
.guide-steps {
  margin-bottom: 22px;
}

.guide-body {
  display: grid;
  gap: 18px;
  min-height: 280px;
  padding: 4px 2px;
}

.guide-copy {
  display: grid;
  gap: 8px;
}

.guide-kicker {
  color: #64748b;
  font-size: 13px;
}

.guide-copy h3 {
  color: #111827;
  font-size: 22px;
  line-height: 1.3;
}

.guide-copy p {
  color: #475569;
  line-height: 1.7;
}

.guide-checklist {
  display: grid;
  gap: 10px;
  padding-left: 22px;
  color: #1f2937;
  line-height: 1.7;
}

.guide-action {
  display: flex;
  justify-content: flex-start;
}

.guide-footer {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}

.guide-nav {
  display: flex;
  gap: 8px;
}
</style>
