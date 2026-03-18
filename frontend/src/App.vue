<template>
  <el-config-provider>
    <el-container class="app-container">
      <el-aside v-if="showSidebar" width="240px" class="app-sidebar">
        <div class="logo">
          <h2>📖 NovelBuilder</h2>
          <p class="subtitle">AI小说生成平台</p>
        </div>
        <el-menu
          :default-active="activeMenu"
          :router="true"
          class="sidebar-menu"
          background-color="#1a1a2e"
          text-color="#a0a0b0"
          active-text-color="#409eff"
        >
          <el-menu-item index="/projects">
            <el-icon><Folder /></el-icon>
            <span>项目管理</span>
          </el-menu-item>
          <template v-if="currentProjectId">
            <el-menu-item-group title="创作工坊">
              <el-menu-item :index="`/projects/${currentProjectId}/studio`">
                <el-icon><Edit /></el-icon>
                <span>创作工作台</span>
              </el-menu-item>
              <el-menu-item :index="`/projects/${currentProjectId}/references`">
                <el-icon><Reading /></el-icon>
                <span>参考书管理</span>
              </el-menu-item>
              <el-menu-item :index="`/projects/${currentProjectId}/rag`">
                <el-icon><Management /></el-icon>
                <span>知识库管理</span>
              </el-menu-item>
              <el-menu-item :index="`/projects/${currentProjectId}/world`">
                <el-icon><Place /></el-icon>
                <span>世界观设定</span>
              </el-menu-item>
              <el-menu-item :index="`/projects/${currentProjectId}/characters`">
                <el-icon><User /></el-icon>
                <span>角色管理</span>
              </el-menu-item>
              <el-menu-item :index="`/projects/${currentProjectId}/outline`">
                <el-icon><List /></el-icon>
                <span>大纲编辑</span>
              </el-menu-item>
              <el-menu-item :index="`/projects/${currentProjectId}/foreshadowing`">
                <el-icon><Connection /></el-icon>
                <span>伏笔管理</span>
              </el-menu-item>
            </el-menu-item-group>
            <el-menu-item-group title="生成管线">
              <el-menu-item :index="`/projects/${currentProjectId}/blueprint`">
                <el-icon><Document /></el-icon>
                <span>整书蓝图</span>
              </el-menu-item>
              <el-menu-item :index="`/projects/${currentProjectId}/chapters`">
                <el-icon><Notebook /></el-icon>
                <span>章节管理</span>
              </el-menu-item>
              <el-menu-item :index="`/projects/${currentProjectId}/workflow`">
                <el-icon><SetUp /></el-icon>
                <span>工作流控制台</span>
              </el-menu-item>
              <el-menu-item :index="`/projects/${currentProjectId}/quality`">
                <el-icon><DataAnalysis /></el-icon>
                <span>质量检测</span>
              </el-menu-item>
              <el-menu-item :index="`/projects/${currentProjectId}/agent-review`">
                <el-icon><ChatDotRound /></el-icon>
                <span>多智能体评审</span>
              </el-menu-item>
            </el-menu-item-group>
          </template>
          <el-menu-item-group title="系统">
            <el-menu-item index="/settings/llm">
              <el-icon><Setting /></el-icon>
              <span>AI 模型配置</span>
            </el-menu-item>
          </el-menu-item-group>
        </el-menu>
      </el-aside>
      <el-main class="app-main">
        <router-view />
      </el-main>
    </el-container>
  </el-config-provider>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useProjectStore } from '@/stores/project'

const route = useRoute()
const projectStore = useProjectStore()

const showSidebar = computed(() => true)
const activeMenu = computed(() => route.path)
const currentProjectId = computed(() => projectStore.currentProjectId)
</script>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

body {
  font-family: 'PingFang SC', 'Microsoft YaHei', sans-serif;
  background-color: #0f0f1a;
  color: #e0e0e0;
}

.app-container {
  height: 100vh;
}

.app-sidebar {
  background-color: #1a1a2e;
  border-right: 1px solid #2a2a3e;
  overflow-y: auto;
}

.logo {
  padding: 20px;
  text-align: center;
  border-bottom: 1px solid #2a2a3e;
}

.logo h2 {
  color: #409eff;
  font-size: 20px;
}

.logo .subtitle {
  color: #666;
  font-size: 12px;
  margin-top: 4px;
}

.sidebar-menu {
  border-right: none !important;
}

.app-main {
  background-color: #0f0f1a;
  padding: 20px;
  overflow-y: auto;
}

.el-menu-item-group__title {
  color: #666 !important;
  font-size: 12px !important;
}
</style>
