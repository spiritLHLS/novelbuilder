<template>
  <el-config-provider>
    <div class="app-wrapper" :class="{ dark: isDark }">
      <aside class="app-sidebar">
        <div class="sidebar-logo">
          <span class="logo-icon">📖</span>
          <div class="logo-text">
            <h2>NovelBuilder</h2>
            <p>AI小说生成平台</p>
          </div>
        </div>
        <nav class="sidebar-nav">
          <a class="nav-item" :class="{ active: route.path === '/projects' }"
            @click.prevent="router.push('/projects')" href="#">
            <el-icon><Folder /></el-icon><span>项目管理</span>
          </a>
          <template v-if="currentProjectId">
            <div class="nav-group-title">创作工坊</div>
            <a v-for="item in workshopItems" :key="item.path" class="nav-item"
              :class="{ active: route.path === item.path }"
              @click.prevent="router.push(item.path)" href="#">
              <el-icon><component :is="item.icon" /></el-icon><span>{{ item.label }}</span>
            </a>
            <div class="nav-group-title">生成管线</div>
            <a v-for="item in pipelineItems" :key="item.path" class="nav-item"
              :class="{ active: route.path === item.path }"
              @click.prevent="router.push(item.path)" href="#">
              <el-icon><component :is="item.icon" /></el-icon><span>{{ item.label }}</span>
            </a>
            <div class="nav-group-title">创作工具</div>
            <a v-for="item in toolItems" :key="item.path" class="nav-item"
              :class="{ active: route.path === item.path }"
              @click.prevent="router.push(item.path)" href="#">
              <el-icon><component :is="item.icon" /></el-icon><span>{{ item.label }}</span>
            </a>
          </template>
          <div class="nav-group-title">系统</div>
          <a v-for="item in systemItems" :key="item.path" class="nav-item"
            :class="{ active: route.path === item.path }"
            @click.prevent="router.push(item.path)" href="#">
            <el-icon><component :is="item.icon" /></el-icon><span>{{ item.label }}</span>
          </a>
        </nav>
        <div class="sidebar-footer">
          <button class="theme-btn" @click="themeStore.toggleTheme()">
            <span>{{ isDark ? '☀️' : '🌙' }}</span>
            <span>{{ isDark ? '切换亮色' : '切换暗色' }}</span>
          </button>
        </div>
      </aside>
      <main class="app-main">
        <router-view />
      </main>
    </div>
  </el-config-provider>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useProjectStore } from '@/stores/project'
import { useThemeStore } from '@/stores/theme'

const route = useRoute()
const router = useRouter()
const projectStore = useProjectStore()
const themeStore = useThemeStore()

const isDark = computed(() => themeStore.theme === 'dark')
const currentProjectId = computed(() => projectStore.currentProjectId)

const workshopItems = computed(() => {
  const pid = currentProjectId.value
  return [
    { path: `/projects/${pid}/studio`, icon: 'Edit', label: '创作工作台' },
    { path: `/projects/${pid}/references`, icon: 'Reading', label: '参考书管理' },
    { path: `/projects/${pid}/rag`, icon: 'Management', label: '知识库管理' },
    { path: `/projects/${pid}/world`, icon: 'Place', label: '世界观设定' },
    { path: `/projects/${pid}/characters`, icon: 'User', label: '角色管理' },
    { path: `/projects/${pid}/outline`, icon: 'List', label: '大纲编辑' },
    { path: `/projects/${pid}/foreshadowing`, icon: 'Connection', label: '伏笔管理' },
    { path: `/projects/${pid}/glossary`, icon: 'Collection', label: '术语表' },
    { path: `/projects/${pid}/resources`, icon: 'Coin', label: '资源账本' },
    { path: `/projects/${pid}/analytics`, icon: 'TrendCharts', label: '数据分析' },
    { path: `/projects/${pid}/graph-memory`, icon: 'Share', label: '图谱记忆' },
  ]
})

const pipelineItems = computed(() => {
  const pid = currentProjectId.value
  return [
    { path: `/projects/${pid}/blueprint`, icon: 'Document', label: '整书蓝图' },
    { path: `/projects/${pid}/chapters`, icon: 'Notebook', label: '章节管理' },
    { path: `/projects/${pid}/workflow`, icon: 'SetUp', label: '工作流控制台' },
    { path: `/projects/${pid}/quality`, icon: 'DataAnalysis', label: '质量检测' },
    { path: `/projects/${pid}/agent-review`, icon: 'ChatDotRound', label: '多智能体评审' },
    { path: `/projects/${pid}/propagation`, icon: 'Connection', label: '变更传播' },
    { path: `/projects/${pid}/tasks`, icon: 'Timer', label: '任务队列' },
  ]
})

const toolItems = computed(() => {
  const pid = currentProjectId.value
  return [
    { path: `/projects/${pid}/creative-brief`, icon: 'DocumentAdd', label: '创作简报' },
    { path: `/projects/${pid}/import-chapters`, icon: 'Upload', label: '导入续写' },
    { path: `/projects/${pid}/subplots`, icon: 'Menu', label: '子情节管理' },
    { path: `/projects/${pid}/emotional-arcs`, icon: 'DataLine', label: '情绪弧线' },
    { path: `/projects/${pid}/character-matrix`, icon: 'Grid', label: '角色关系矩阵' },
    { path: `/projects/${pid}/radar`, icon: 'Aim', label: '雷达分析' },
  ]
})

const systemItems = [
  { path: '/settings/llm', icon: 'Setting', label: 'AI 模型配置' },
  { path: '/settings/agent-routing', icon: 'Share', label: '多模型路由' },
  { path: '/settings/prompt-presets', icon: 'DocumentCopy', label: '提示词预设' },
  { path: '/settings/system', icon: 'Tools', label: '系统设置' },
  { path: '/settings/genre-templates', icon: 'Files', label: '题材规则' },
]
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
.sidebar-footer { border-top: 1px solid var(--nb-border-sidebar); padding: 10px; flex-shrink: 0; }
.theme-btn {
  display: flex; align-items: center; gap: 8px; width: 100%; padding: 7px 10px;
  border: 1px solid var(--nb-border-sidebar); border-radius: 6px; background: transparent;
  color: var(--nb-text-sidebar); font-size: 12px; cursor: pointer;
  transition: background-color 0.12s, color 0.12s;
}
.theme-btn:hover { background-color: var(--nb-bg-sidebar-hover); color: var(--nb-accent); }
.app-main { flex: 1; overflow-y: auto; background-color: var(--nb-main-bg); padding: 24px; transition: background-color 0.2s; }
</style>
