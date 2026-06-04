import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import en from 'element-plus/es/locale/lang/en'

export type AppLocale = 'zh-CN' | 'en-US'

const LOCALE_KEY = 'nb-locale'

const messages = {
  'zh-CN': {
    appSubtitle: 'AI小说生成平台',
    projects: '项目管理',
    writingFlow: '写书流程',
    assetsKnowledge: '资产与知识',
    qualityPublish: '质量与发布',
    system: '系统',
    studio: '创作中枢',
    blueprint: '蓝图规划',
    chapters: '章节生成',
    workflow: '流程状态',
    projectTasks: '任务总控',
    references: '参考书与导入',
    rag: 'RAG 知识库',
    graphMemory: '图谱与向量记忆',
    world: '世界设定',
    characters: '角色档案',
    outline: '章节大纲',
    glossary: '术语与资源',
    quality: '质量审稿',
    agentReview: '智能体评审',
    analytics: '统计分析',
    fanqie: '发布上传',
    globalTasks: '全局任务',
    users: '用户与权限',
    llmSettings: 'AI 模型配置',
    agentRouting: '多模型路由',
    promptPresets: '提示词预设',
    systemSettings: '系统设置',
    genreTemplates: '题材规则',
    systemLogs: '系统日志',
    lightMode: '切换亮色',
    darkMode: '切换暗色',
    languageToggle: 'English',
    guide: '使用向导',
    logout: '退出登录',
    logoutSuccess: '已退出登录',
  },
  'en-US': {
    appSubtitle: 'AI fiction studio',
    projects: 'Projects',
    writingFlow: 'Writing',
    assetsKnowledge: 'Knowledge',
    qualityPublish: 'Quality',
    system: 'System',
    studio: 'Studio',
    blueprint: 'Blueprint',
    chapters: 'Chapters',
    workflow: 'Workflow',
    projectTasks: 'Project Tasks',
    references: 'References',
    rag: 'RAG Library',
    graphMemory: 'Graph Memory',
    world: 'World Bible',
    characters: 'Characters',
    outline: 'Outline',
    glossary: 'Glossary',
    quality: 'Quality Review',
    agentReview: 'Agent Review',
    analytics: 'Analytics',
    fanqie: 'Publish',
    globalTasks: 'Global Tasks',
    users: 'Users',
    llmSettings: 'AI Models',
    agentRouting: 'Model Routing',
    promptPresets: 'Prompt Presets',
    systemSettings: 'Settings',
    genreTemplates: 'Genre Rules',
    systemLogs: 'Logs',
    lightMode: 'Light mode',
    darkMode: 'Dark mode',
    languageToggle: '中文',
    guide: 'Guide',
    logout: 'Sign out',
    logoutSuccess: 'Signed out',
  },
} as const

type MessageKey = keyof typeof messages['zh-CN']

export const useLocaleStore = defineStore('locale', () => {
  const stored = localStorage.getItem(LOCALE_KEY) as AppLocale | null
  const locale = ref<AppLocale>(stored === 'en-US' ? 'en-US' : 'zh-CN')

  const elementLocale = computed(() => (locale.value === 'en-US' ? en : zhCn))

  function setLocale(next: AppLocale) {
    locale.value = next
  }

  function toggleLocale() {
    locale.value = locale.value === 'zh-CN' ? 'en-US' : 'zh-CN'
  }

  function t(key: MessageKey): string {
    return messages[locale.value][key] ?? messages['zh-CN'][key]
  }

  watch(locale, (next) => {
    localStorage.setItem(LOCALE_KEY, next)
    document.documentElement.lang = next
  }, { immediate: true })

  return { locale, elementLocale, setLocale, toggleLocale, t }
})
