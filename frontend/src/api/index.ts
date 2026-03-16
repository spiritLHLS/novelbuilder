import axios from 'axios'
import type { AxiosResponse } from 'axios'

const api = axios.create({
  baseURL: '/api',
  timeout: 300000,
  headers: {
    'Content-Type': 'application/json',
  },
})

api.interceptors.response.use(
  (response: AxiosResponse) => response,
  (error) => {
    const msg = error.response?.data?.message || error.response?.data?.error || error.message
    console.error('API Error:', msg)
    return Promise.reject(error)
  }
)

// Projects
export const projectApi = {
  list: () => api.get('/projects'),
  get: (id: string) => api.get(`/projects/${id}`),
  create: (data: any) => api.post('/projects', data),
  update: (id: string, data: any) => api.put(`/projects/${id}`, data),
  delete: (id: string) => api.delete(`/projects/${id}`),
}

// Blueprints
export const blueprintApi = {
  generate: (projectId: string, data: any) => api.post(`/projects/${projectId}/blueprint/generate`, data),
  get: (projectId: string) => api.get(`/projects/${projectId}/blueprint`),
  submitReview: (projectId: string, id: string) => api.post(`/projects/${projectId}/blueprints/${id}/submit-review`),
  approve: (projectId: string, id: string, comment?: string) => api.post(`/projects/${projectId}/blueprints/${id}/approve`, { review_comment: comment }),
  reject: (projectId: string, id: string, comment?: string) => api.post(`/projects/${projectId}/blueprints/${id}/reject`, { review_comment: comment }),
}

// World Bible
export const worldBibleApi = {
  get: (projectId: string) => api.get(`/projects/${projectId}/world-bible`),
  update: (projectId: string, data: any) => api.put(`/projects/${projectId}/world-bible`, data),
  getConstitution: (projectId: string) => api.get(`/projects/${projectId}/constitution`),
  updateConstitution: (projectId: string, data: any) => api.put(`/projects/${projectId}/constitution`, data),
}

// Characters
export const characterApi = {
  list: (projectId: string) => api.get(`/projects/${projectId}/characters`),
  get: (projectId: string, id: string) => api.get(`/projects/${projectId}/characters/${id}`),
  create: (projectId: string, data: any) => api.post(`/projects/${projectId}/characters`, data),
  update: (projectId: string, id: string, data: any) => api.put(`/projects/${projectId}/characters/${id}`, data),
  delete: (projectId: string, id: string) => api.delete(`/projects/${projectId}/characters/${id}`),
}

// Outlines
export const outlineApi = {
  list: (projectId: string) => api.get(`/projects/${projectId}/outlines`),
  create: (projectId: string, data: any) => api.post(`/projects/${projectId}/outlines`, data),
  update: (projectId: string, id: string, data: any) => api.put(`/projects/${projectId}/outlines/${id}`, data),
  delete: (projectId: string, id: string) => api.delete(`/projects/${projectId}/outlines/${id}`),
}

// Foreshadowings
export const foreshadowingApi = {
  list: (projectId: string) => api.get(`/projects/${projectId}/foreshadowings`),
  create: (projectId: string, data: any) => api.post(`/projects/${projectId}/foreshadowings`, data),
  updateStatus: (projectId: string, id: string, status: string) => api.put(`/projects/${projectId}/foreshadowings/${id}/status`, { status }),
  delete: (projectId: string, id: string) => api.delete(`/projects/${projectId}/foreshadowings/${id}`),
}

// Volumes
export const volumeApi = {
  list: (projectId: string) => api.get(`/projects/${projectId}/volumes`),
  submitReview: (projectId: string, id: string) => api.post(`/projects/${projectId}/volumes/${id}/submit-review`),
  approve: (projectId: string, id: string, comment?: string) => api.post(`/projects/${projectId}/volumes/${id}/approve`, { review_comment: comment }),
  reject: (projectId: string, id: string, comment?: string) => api.post(`/projects/${projectId}/volumes/${id}/reject`, { review_comment: comment }),
}

// Chapters
export const chapterApi = {
  list: (projectId: string) => api.get(`/projects/${projectId}/chapters`),
  get: (projectId: string, id: string) => api.get(`/projects/${projectId}/chapters/${id}`),
  generate: (projectId: string, data: any) =>
    api.post(`/projects/${projectId}/chapters/generate`, data),
  streamGenerate: async (
    projectId: string,
    data: any,
    onChunk: (content: string) => void,
    onDone: () => void,
    signal?: AbortSignal,
  ) => {
    const response = await fetch(`/api/projects/${projectId}/chapters/stream`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(data),
      signal,
    })
    const reader = response.body?.getReader()
    if (!reader) throw new Error('Stream not available')
    const decoder = new TextDecoder()
    let buffer = ''
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''
      for (const line of lines) {
        if (line.startsWith('data: ')) {
          try {
            const d = JSON.parse(line.slice(6))
            if (d.done) { onDone(); return }
            else if (d.content) onChunk(d.content)
          } catch { /* skip */ }
        }
      }
    }
    onDone()
  },
}

// Quality
export const qualityApi = {
  runCheck: (projectId: string, chapterId: string) =>
    api.post(`/projects/${projectId}/chapters/${chapterId}/quality-check`),
}

// Workflow
export const workflowApi = {
  start: (projectId: string) => api.post(`/projects/${projectId}/workflow/start`),
  getHistory: (projectId: string) => api.get(`/projects/${projectId}/workflow/history`),
  rollback: (projectId: string, data: any) =>
    api.post(`/projects/${projectId}/workflow/rollback`, data),
}

// References
export const referenceApi = {
  list: (projectId: string) => api.get(`/projects/${projectId}/references`),
  get: (id: string) => api.get(`/references/${id}`),
  upload: (projectId: string, formData: FormData) =>
    api.post(`/projects/${projectId}/references/upload`, formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    }),
  updateMigrationConfig: (id: string, config: any) =>
    api.put(`/references/${id}/migration-config`, config),
  analyze: (id: string) => api.post(`/references/${id}/analyze`),
}

export default api
