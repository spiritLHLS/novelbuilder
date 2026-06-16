<template>
  <el-config-provider :locale="localeStore.elementLocale">
    <!-- Public bootstrap pages get a clean fullscreen layout (no sidebar) -->
    <router-view v-if="isFullscreenRoute" />

    <div v-else class="app-wrapper" :class="{ dark: isDark }">
      <aside class="app-sidebar">
        <div class="sidebar-logo">
          <span class="logo-icon">📖</span>
          <div class="logo-text">
            <h2>NovelBuilder</h2>
            <p>{{ localeStore.t('appSubtitle') }}</p>
          </div>
        </div>
        <nav class="sidebar-nav">
          <a v-if="showWritingNav" class="nav-item" :class="{ active: route.path === '/projects' }"
            @click.prevent="router.push('/projects')" href="#">
            <el-icon><Folder /></el-icon><span>{{ localeStore.t('projects') }}</span>
          </a>
          <template v-if="showWritingNav && currentProjectId">
            <div class="nav-group-title">{{ localeStore.t('writingFlow') }}</div>
            <a v-for="item in workshopItems" :key="item.path" class="nav-item"
              :class="{ active: route.path === item.path }"
              @click.prevent="router.push(item.path)" href="#">
              <el-icon><component :is="item.icon" /></el-icon><span>{{ item.label }}</span>
            </a>
            <div class="nav-group-title">{{ localeStore.t('assetsKnowledge') }}</div>
            <a v-for="item in pipelineItems" :key="item.path" class="nav-item"
              :class="{ active: route.path === item.path }"
              @click.prevent="router.push(item.path)" href="#">
              <el-icon><component :is="item.icon" /></el-icon><span>{{ item.label }}</span>
            </a>
            <div class="nav-group-title">{{ localeStore.t('qualityPublish') }}</div>
            <a v-for="item in toolItems" :key="item.path" class="nav-item"
              :class="{ active: route.path === item.path }"
              @click.prevent="router.push(item.path)" href="#">
              <el-icon><component :is="item.icon" /></el-icon><span>{{ item.label }}</span>
            </a>
          </template>
          <div v-if="systemItems.length" class="nav-group-title">{{ localeStore.t('system') }}</div>
          <a v-for="item in systemItems" :key="item.path" class="nav-item"
            :class="{ active: route.path === item.path }"
            @click.prevent="router.push(item.path)" href="#">
            <el-icon><component :is="item.icon" /></el-icon><span>{{ item.label }}</span>
          </a>
        </nav>
      </aside>
      <main class="app-main">
        <div class="app-topbar">
          <div class="topbar-context">
            <el-tag v-if="isAdminUser" size="small" effect="plain">{{ adminViewMode === 'admin' ? localeStore.t('adminView') : localeStore.t('writingView') }}</el-tag>
          </div>
          <div class="topbar-actions">
            <el-segmented
              v-if="isAdminUser"
              v-model="adminViewMode"
              :options="adminViewOptions"
              class="view-switch"
              @change="handleAdminViewChange"
            />
            <div class="user-info" v-if="auth.username">
              <el-icon><UserFilled /></el-icon>
              <span class="username">{{ auth.username }}</span>
            </div>
            <button class="topbar-btn" @click="themeStore.toggleTheme()">
              <span>{{ isDark ? '☀️' : '🌙' }}</span>
              <span>{{ isDark ? localeStore.t('lightMode') : localeStore.t('darkMode') }}</span>
            </button>
            <button class="topbar-btn" @click="localeStore.toggleLocale()">
              <el-icon><Switch /></el-icon>
              <span>{{ localeStore.t('languageToggle') }}</span>
            </button>
            <button class="topbar-btn" @click="openGuide">
              <el-icon><QuestionFilled /></el-icon>
              <span>{{ localeStore.t('guide') }}</span>
            </button>
            <button class="topbar-btn logout" @click="handleLogout">
              <el-icon><SwitchButton /></el-icon>
              <span>{{ localeStore.t('logout') }}</span>
            </button>
          </div>
        </div>
        <div class="app-view">
          <router-view />
        </div>
      </main>
      <DownloadWidget />
      <FirstRunGuide v-if="showGuideHost" v-model="guideVisible" />
    </div>
  </el-config-provider>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useProjectStore } from '@/stores/project'
import { useThemeStore } from '@/stores/theme'
import { useLocaleStore } from '@/stores/locale'
import { useDownloadStore } from '@/stores/download'
import { useAuthStore } from '@/stores/auth'
import DownloadWidget from '@/components/DownloadWidget.vue'
import FirstRunGuide from '@/components/FirstRunGuide.vue'

const route = useRoute()
const router = useRouter()
const projectStore = useProjectStore()
const themeStore = useThemeStore()
const localeStore = useLocaleStore()
const downloadStore = useDownloadStore()
const auth = useAuthStore()
const GUIDE_KEY = 'nb_first_run_guide_done'
const ADMIN_VIEW_KEY = 'nb_admin_view_mode'
const guideVisible = ref(false)
type AdminViewMode = 'admin' | 'write'
const storedAdminView = localStorage.getItem(ADMIN_VIEW_KEY)
const adminViewMode = ref<AdminViewMode>(storedAdminView === 'write' ? 'write' : 'admin')
const adminViewOptions = computed(() => [
  { label: localeStore.t('admin'), value: 'admin' },
  { label: localeStore.t('writing'), value: 'write' },
])

onMounted(() => {
  downloadStore.restoreAndPoll()
  if (auth.token && !localStorage.getItem(GUIDE_KEY)) {
    guideVisible.value = true
  }
  syncAdminRouteForView()
})

const isDark = computed(() => themeStore.theme === 'dark')
const currentProjectId = computed(() => projectStore.currentProjectId)
const isFullscreenRoute = computed(() => route.name === 'login' || route.name === 'setup')
const showGuideHost = computed(() => Boolean(auth.token) && !isFullscreenRoute.value)
const isAdminUser = computed(() => auth.role === 'admin')
const showWritingNav = computed(() => auth.role !== 'admin' || adminViewMode.value === 'write')
const showSystemNav = computed(() => auth.role === 'admin' ? adminViewMode.value === 'admin' : true)

watch(() => auth.token, (token) => {
  if (token && !localStorage.getItem(GUIDE_KEY)) {
    guideVisible.value = true
  }
})

watch(() => auth.role, () => {
  syncAdminRouteForView()
})

watch(() => route.path, () => {
  syncAdminRouteForView()
})

async function handleLogout() {
  await auth.logout()
  ElMessage.info(localeStore.t('logoutSuccess'))
  router.push('/login')
}

function openGuide() {
  guideVisible.value = true
}

function isSystemRoute(path: string) {
  return path === '/tasks' || path.startsWith('/settings')
}

function handleAdminViewChange(value: string | number | boolean) {
  adminViewMode.value = value === 'write' ? 'write' : 'admin'
  localStorage.setItem(ADMIN_VIEW_KEY, adminViewMode.value)
  syncAdminRouteForView(true)
}

function syncAdminRouteForView(force = false) {
  if (!isAdminUser.value || isFullscreenRoute.value) return
  if (adminViewMode.value === 'admin' && !isSystemRoute(route.path)) {
    if (force || route.name !== 'user-management') {
      router.replace('/settings/users')
    }
    return
  }
  if (adminViewMode.value === 'write' && isSystemRoute(route.path)) {
    router.replace('/projects')
  }
}

const workshopItems = computed(() => {
  const pid = currentProjectId.value
  return [
    { path: `/projects/${pid}/studio`, icon: 'Edit', label: localeStore.t('studio') },
    { path: `/projects/${pid}/creative-brief`, icon: 'MagicStick', label: localeStore.t('creativeBrief') },
    { path: `/projects/${pid}/blueprint`, icon: 'Document', label: localeStore.t('blueprint') },
    { path: `/projects/${pid}/chapters`, icon: 'Notebook', label: localeStore.t('chapters') },
    { path: `/projects/${pid}/import-chapters`, icon: 'Upload', label: localeStore.t('importChapters') },
    { path: `/projects/${pid}/workflow`, icon: 'SetUp', label: localeStore.t('workflow') },
    { path: `/projects/${pid}/tasks`, icon: 'Timer', label: localeStore.t('projectTasks') },
  ]
})

const pipelineItems = computed(() => {
  const pid = currentProjectId.value
  return [
    { path: `/projects/${pid}/references`, icon: 'Reading', label: localeStore.t('references') },
    { path: `/projects/${pid}/rag`, icon: 'Management', label: localeStore.t('rag') },
    { path: `/projects/${pid}/graph-memory`, icon: 'Share', label: localeStore.t('graphMemory') },
    { path: `/projects/${pid}/world`, icon: 'Place', label: localeStore.t('world') },
    { path: `/projects/${pid}/characters`, icon: 'Avatar', label: localeStore.t('characters') },
    { path: `/projects/${pid}/outline`, icon: 'List', label: localeStore.t('outline') },
    { path: `/projects/${pid}/foreshadowing`, icon: 'Bell', label: localeStore.t('foreshadowing') },
    { path: `/projects/${pid}/subplots`, icon: 'Tickets', label: localeStore.t('subplots') },
    { path: `/projects/${pid}/emotional-arcs`, icon: 'TrendCharts', label: localeStore.t('emotionalArcs') },
    { path: `/projects/${pid}/character-matrix`, icon: 'Connection', label: localeStore.t('characterMatrix') },
    { path: `/projects/${pid}/resources`, icon: 'Box', label: localeStore.t('resources') },
    { path: `/projects/${pid}/glossary`, icon: 'Collection', label: localeStore.t('glossary') },
  ]
})

const toolItems = computed(() => {
  const pid = currentProjectId.value
  return [
    { path: `/projects/${pid}/quality`, icon: 'DataAnalysis', label: localeStore.t('quality') },
    { path: `/projects/${pid}/agent-review`, icon: 'ChatDotRound', label: localeStore.t('agentReview') },
    { path: `/projects/${pid}/analytics`, icon: 'TrendCharts', label: localeStore.t('analytics') },
    { path: `/projects/${pid}/radar`, icon: 'Aim', label: localeStore.t('radar') },
    { path: `/projects/${pid}/propagation`, icon: 'Refresh', label: localeStore.t('propagation') },
    { path: `/projects/${pid}/fanqie`, icon: 'Promotion', label: localeStore.t('fanqie') },
  ]
})

const systemItems = computed(() => {
  if (!showSystemNav.value) {
    return []
  }
  if (auth.role !== 'admin') {
    return [
      { path: '/settings/prompt-presets', icon: 'DocumentCopy', label: localeStore.t('promptPresets') },
    ]
  }
  return [
    { path: '/tasks', icon: 'Timer', label: localeStore.t('globalTasks') },
    { path: '/settings/users', icon: 'User', label: localeStore.t('users') },
    { path: '/settings/llm', icon: 'Setting', label: localeStore.t('llmSettings') },
    { path: '/settings/agent-routing', icon: 'Share', label: localeStore.t('agentRouting') },
    { path: '/settings/prompt-presets', icon: 'DocumentCopy', label: localeStore.t('promptPresets') },
    { path: '/settings/system', icon: 'Tools', label: localeStore.t('systemSettings') },
    { path: '/settings/genre-templates', icon: 'Files', label: localeStore.t('genreTemplates') },
    { path: '/settings/logs', icon: 'Monitor', label: localeStore.t('systemLogs') },
  ]
})
</script>

<style>
*, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
body {
  font-family: 'PingFang SC', 'Microsoft YaHei', 'Source Han Sans CN', sans-serif;
  background-color: var(--nb-main-bg);
  color: var(--nb-text-primary);
}
.app-wrapper { display: flex; height: 100vh; overflow: hidden; background-color: var(--nb-main-bg); }
.app-sidebar {
  width: 220px; min-width: 220px;
  background-color: var(--nb-bg-sidebar);
  border-right: 1px solid var(--nb-border-sidebar);
  display: flex; flex-direction: column; overflow: hidden;
  transition: background-color 0.2s, border-color 0.2s;
}
.sidebar-logo {
  display: flex; align-items: center; gap: 10px; padding: 16px;
  border-bottom: 1px solid var(--nb-border-sidebar); flex-shrink: 0;
}
.logo-icon { font-size: 22px; }
.logo-text h2 { font-size: 14px; font-weight: 700; color: var(--nb-logo-text); line-height: 1.2; }
.logo-text p { font-size: 10px; color: var(--nb-logo-subtitle); margin-top: 2px; }
.sidebar-nav { flex: 1; overflow-y: auto; padding: 6px 0; }
.nav-group-title {
  font-size: 10px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em;
  color: var(--nb-menu-group-title); padding: 10px 14px 3px;
}
.nav-item {
  display: flex; align-items: center; gap: 8px; padding: 7px 14px;
  font-size: 13px; color: var(--nb-text-sidebar); text-decoration: none; cursor: pointer;
  transition: background-color 0.12s, color 0.12s;
}
.nav-item:hover { background-color: var(--nb-bg-sidebar-hover); color: var(--nb-accent); }
.nav-item.active { background-color: rgba(64,158,255,0.15); color: #409eff; font-weight: 500; }
.nav-item .el-icon { font-size: 14px; flex-shrink: 0; }
.app-main {
  flex: 1;
  overflow-y: auto;
  background-color: var(--nb-main-bg);
  transition: background-color 0.2s;
}
.app-topbar {
  min-height: 54px;
  padding: 10px 18px;
  border-bottom: 1px solid var(--nb-border-sidebar);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  background-color: var(--nb-main-bg);
  position: sticky;
  top: 0;
  z-index: 20;
}
.topbar-context {
  min-width: 0;
}
.topbar-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  min-width: 0;
}
.view-switch {
  flex-shrink: 0;
}
.user-info {
  display: flex; align-items: center; gap: 6px; padding: 5px 10px 8px;
  font-size: 12px; color: var(--nb-text-primary); opacity: 0.85;
  white-space: nowrap; overflow: hidden;
}
.user-info .username { overflow: hidden; text-overflow: ellipsis; }
.topbar-btn {
  display: inline-flex; align-items: center; gap: 6px; padding: 7px 10px;
  border: 1px solid var(--nb-border-sidebar); border-radius: 6px; background: transparent;
  color: var(--nb-text-primary); font-size: 12px; cursor: pointer;
  transition: background-color 0.12s, color 0.12s;
}
.topbar-btn:hover { background-color: var(--nb-bg-sidebar-hover); color: var(--nb-accent); }
.topbar-btn.logout { border-color: transparent; opacity: 0.78; }
.topbar-btn.logout:hover { background-color: rgba(245,108,108,0.12); color: #f56c6c; opacity: 1; }
.app-view { padding: 24px; }

@media (max-width: 760px) {
  .app-sidebar {
    width: 72px;
    min-width: 72px;
  }

  .sidebar-logo {
    justify-content: center;
    padding: 14px 8px;
  }

  .logo-text,
  .nav-group-title,
  .nav-item span,
  .user-info .username {
    display: none;
  }

  .nav-item {
    justify-content: center;
    padding: 9px 8px;
  }

  .app-topbar {
    align-items: flex-start;
    padding: 8px 10px;
    flex-direction: column;
  }

  .topbar-actions {
    width: 100%;
    justify-content: flex-end;
    flex-wrap: wrap;
  }

  .topbar-btn span:last-child {
    display: none;
  }

  .app-view {
    padding: 16px;
  }
}
</style>
