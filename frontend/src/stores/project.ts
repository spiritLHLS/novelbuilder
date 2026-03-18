import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { projectApi } from '@/api'

export interface Project {
  id: string
  title: string
  genre: string
  description: string
  target_words: number
  style_description: string
  status: string
  created_at: string
  updated_at: string
}

export const useProjectStore = defineStore('project', () => {
  const projects = ref<Project[]>([])
  const currentProjectId = ref<string | null>(null)
  const loading = ref(false)

  const currentProject = computed(() =>
    projects.value.find((p) => p.id === currentProjectId.value)
  )

  function setCurrentProject(id: string) {
    currentProjectId.value = id
  }

  async function fetchProjects() {
    loading.value = true
    try {
      const res = await projectApi.list()
      projects.value = res.data.data || []
    } finally {
      loading.value = false
    }
  }

  async function createProject(data: Partial<Project>) {
    const res = await projectApi.create(data)
    projects.value.push(res.data.data)
    return res.data.data
  }

  async function updateProject(id: string, data: Partial<Project>) {
    const res = await projectApi.update(id, data)
    const idx = projects.value.findIndex((p) => p.id === id)
    if (idx >= 0) projects.value[idx] = res.data.data
    return res.data.data
  }

  async function deleteProject(id: string) {
    await projectApi.delete(id)
    projects.value = projects.value.filter((p) => p.id !== id)
    if (currentProjectId.value === id) {
      currentProjectId.value = null
    }
  }

  return {
    projects,
    currentProjectId,
    currentProject,
    loading,
    setCurrentProject,
    fetchProjects,
    createProject,
    updateProject,
    deleteProject,
  }
})
