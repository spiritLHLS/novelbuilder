import { createRouter, createWebHistory } from 'vue-router'
import { useProjectStore } from '@/stores/project'
import { useAuthStore } from '@/stores/auth'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/Login.vue'),
      meta: { public: true },
    },
    {
      path: '/',
      redirect: '/projects',
    },
    {
      path: '/projects',
      name: 'projects',
      component: () => import('@/views/ProjectList.vue'),
    },
    {
      path: '/projects/:projectId/studio',
      name: 'studio',
      component: () => import('@/views/Studio.vue'),
    },
    {
      path: '/projects/:projectId/references',
      name: 'references',
      component: () => import('@/views/References.vue'),
    },
    {
      path: '/projects/:projectId/world',
      name: 'world',
      component: () => import('@/views/WorldBible.vue'),
    },
    {
      path: '/projects/:projectId/characters',
      name: 'characters',
      component: () => import('@/views/Characters.vue'),
    },
    {
      path: '/projects/:projectId/outline',
      name: 'outline',
      component: () => import('@/views/Outline.vue'),
    },
    {
      path: '/projects/:projectId/foreshadowing',
      name: 'foreshadowing',
      component: () => import('@/views/Foreshadowing.vue'),
    },
    {
      path: '/projects/:projectId/blueprint',
      name: 'blueprint',
      component: () => import('@/views/Blueprint.vue'),
    },
    {
      path: '/projects/:projectId/chapters',
      name: 'chapters',
      component: () => import('@/views/Chapters.vue'),
    },
    {
      path: '/projects/:projectId/chapters/:chapterId',
      name: 'chapter-detail',
      component: () => import('@/views/ChapterDetail.vue'),
    },
    {
      path: '/projects/:projectId/workflow',
      name: 'workflow',
      component: () => import('@/views/Workflow.vue'),
    },
    {
      path: '/projects/:projectId/quality',
      name: 'quality',
      component: () => import('@/views/Quality.vue'),
    },
    {
      path: '/projects/:projectId/agent-review',
      name: 'agent-review',
      component: () => import('@/views/AgentReview.vue'),
    },
    {
      path: '/projects/:projectId/propagation',
      name: 'propagation',
      component: () => import('@/views/EditPropagation.vue'),
    },
    {
      path: '/projects/:projectId/rag',
      name: 'rag',
      component: () => import('@/views/RAG.vue'),
    },
    {
      path: '/projects/:projectId/graph-memory',
      name: 'graph-memory',
      component: () => import('@/views/GraphMemory.vue'),
    },
    {
      path: '/workflows/:runId/diff',
      name: 'workflow-diff',
      component: () => import('@/views/Diff.vue'),
    },
    {
      path: '/settings/llm',
      name: 'llm-settings',
      component: () => import('@/views/LLMSettings.vue'),
    },
    {
      path: '/settings/system',
      name: 'system-settings',
      component: () => import('@/views/SystemSettings.vue'),
    },
    {
      path: '/settings/logs',
      name: 'system-logs',
      component: () => import('@/views/SystemLogs.vue'),
    },
    {
      path: '/projects/:projectId/glossary',
      name: 'glossary',
      component: () => import('@/views/Glossary.vue'),
    },
    {
      path: '/projects/:projectId/resources',
      name: 'resource-ledger',
      component: () => import('@/views/ResourceLedger.vue'),
    },
    {
      path: '/projects/:projectId/tasks',
      name: 'task-queue',
      component: () => import('@/views/TaskQueue.vue'),
    },
    {
      path: '/settings/prompt-presets',
      name: 'prompt-presets',
      component: () => import('@/views/PromptPresets.vue'),
    },
    // ── New inkos-parity features ──────────────────────────────────────────
    {
      path: '/projects/:projectId/chapters/:chapterId/audit',
      name: 'audit-report',
      component: () => import('@/views/AuditReport.vue'),
    },
    {
      path: '/projects/:projectId/chapters/:chapterId/anti-detect',
      name: 'anti-detect',
      component: () => import('@/views/AntiDetect.vue'),
    },
    {
      path: '/projects/:projectId/creative-brief',
      name: 'creative-brief',
      component: () => import('@/views/CreativeBrief.vue'),
    },
    {
      path: '/projects/:projectId/import-chapters',
      name: 'import-chapters',
      component: () => import('@/views/ImportChapters.vue'),
    },
    {
      path: '/settings/agent-routing',
      name: 'agent-routing',
      component: () => import('@/views/AgentRouting.vue'),
    },
    // ── Gap-fill features ──────────────────────────────────────────────────
    {
      path: '/projects/:projectId/analytics',
      name: 'analytics',
      component: () => import('@/views/Analytics.vue'),
    },
    {
      path: '/projects/:projectId/subplots',
      name: 'subplot-board',
      component: () => import('@/views/SubplotBoard.vue'),
    },
    {
      path: '/projects/:projectId/emotional-arcs',
      name: 'emotional-arcs',
      component: () => import('@/views/EmotionalArcs.vue'),
    },
    {
      path: '/projects/:projectId/character-matrix',
      name: 'character-matrix',
      component: () => import('@/views/CharacterMatrix.vue'),
    },
    {
      path: '/projects/:projectId/radar',
      name: 'radar',
      component: () => import('@/views/Radar.vue'),
    },
    {
      path: '/settings/genre-templates',
      name: 'genre-templates',
      component: () => import('@/views/GenreTemplates.vue'),
    },
    {
      path: '/projects/:projectId/fanqie',
      name: 'fanqie-upload',
      component: () => import('@/views/FanqieUpload.vue'),
    },
  ],
})

router.beforeEach(async (to, _from, next) => {
  const projectId = to.params.projectId as string
  if (projectId) {
    useProjectStore().setCurrentProject(projectId)
  }

  // Allow public routes (login page) without authentication.
  if (to.meta?.public) {
    return next()
  }

  const auth = useAuthStore()
  // On first navigation, verify the stored token with the backend.
  if (!auth.checked) {
    const ok = await auth.check()
    if (!ok) {
      return next({ name: 'login', query: { redirect: to.fullPath } })
    }
  } else if (!auth.token) {
    return next({ name: 'login', query: { redirect: to.fullPath } })
  }

  next()
})

export default router
