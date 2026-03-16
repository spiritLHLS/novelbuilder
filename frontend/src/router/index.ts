import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  routes: [
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
      path: '/workflows/:runId/diff',
      name: 'workflow-diff',
      component: () => import('@/views/Diff.vue'),
    },
  ],
})

router.beforeEach((to, _from, next) => {
  const projectStore = import('@/stores/project').then(m => {
    const store = m.useProjectStore()
    const projectId = to.params.projectId as string
    if (projectId) {
      store.setCurrentProject(projectId)
    }
  })
  projectStore.then(() => next())
})

export default router
