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
  submitReview: (_projectId: string, id: string) => api.post(`/blueprints/${id}/submit-review`),
  approve: (_projectId: string, id: string, comment?: string) => api.post(`/blueprints/${id}/approve`, { review_comment: comment }),
  reject: (_projectId: string, id: string, comment?: string) => api.post(`/blueprints/${id}/reject`, { review_comment: comment }),
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
  get: (_projectId: string, id: string) => api.get(`/characters/${id}`),
  create: (projectId: string, data: any) => api.post(`/projects/${projectId}/characters`, data),
  update: (_projectId: string, id: string, data: any) => api.put(`/characters/${id}`, data),
  delete: (_projectId: string, id: string) => api.delete(`/characters/${id}`),
}

// Outlines
export const outlineApi = {
  list: (projectId: string) => api.get(`/projects/${projectId}/outlines`),
  create: (projectId: string, data: any) => api.post(`/projects/${projectId}/outlines`, data),
  update: (_projectId: string, id: string, data: any) => api.put(`/outlines/${id}`, data),
  delete: (_projectId: string, id: string) => api.delete(`/outlines/${id}`),
}

// Foreshadowings
export const foreshadowingApi = {
  list: (projectId: string) => api.get(`/projects/${projectId}/foreshadowings`),
  create: (projectId: string, data: any) => api.post(`/projects/${projectId}/foreshadowings`, data),
  updateStatus: (_projectId: string, id: string, status: string) => api.put(`/foreshadowings/${id}/status`, { status }),
  delete: (_projectId: string, id: string) => api.delete(`/foreshadowings/${id}`),
}

// Volumes
export const volumeApi = {
  list: (projectId: string) => api.get(`/projects/${projectId}/volumes`),
  submitReview: (_projectId: string, id: string) => api.post(`/volumes/${id}/submit-review`),
  approve: (_projectId: string, id: string, comment?: string) => api.post(`/volumes/${id}/approve`, { review_comment: comment }),
  reject: (_projectId: string, id: string, comment?: string) => api.post(`/volumes/${id}/reject`, { review_comment: comment }),
}

// Chapters
export const chapterApi = {
  list: (projectId: string) => api.get(`/projects/${projectId}/chapters`),
  get: (_projectId: string, id: string) => api.get(`/chapters/${id}`),
  generate: (projectId: string, data: any) =>
    api.post(`/projects/${projectId}/chapters/generate`, data),
  continueGenerate: (projectId: string) =>
    api.post(`/projects/${projectId}/chapters/continue`),
  submitReview: (_projectId: string, id: string) => api.post(`/chapters/${id}/submit-review`),
  approve: (_projectId: string, id: string, comment?: string, version?: number) =>
    api.post(`/chapters/${id}/approve`, { review_comment: comment, version }),
  reject: (_projectId: string, id: string, comment?: string, version?: number) =>
    api.post(`/chapters/${id}/reject`, { review_comment: comment, version }),
  regenerate: (_projectId: string, id: string, data?: any) =>
    api.post(`/chapters/${id}/regenerate`, data),
  qualityCheck: (chapterId: string) =>
    api.post(`/chapters/${chapterId}/quality-check`),
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
  runCheck: (_projectId: string, chapterId: string) =>
    api.post(`/chapters/${chapterId}/quality-check`),
}

// Workflow
export const workflowApi = {
  start: (projectId: string) => api.post(`/projects/${projectId}/workflow/start`),
  getHistory: (runId: string) => api.get(`/workflows/${runId}/history`),
  rollback: (runId: string, data: any) => api.post(`/workflows/${runId}/rollback`, data),
  getDiff: (runId: string, fromStep: string, toStep: string) =>
    api.get(`/workflows/${runId}/diff`, { params: { fromStep, toStep } }),
}

// Export
export const exportApi = {
  txt: (projectId: string) =>
    api.get(`/projects/${projectId}/export/txt`, { responseType: 'blob' }),
  markdown: (projectId: string) =>
    api.get(`/projects/${projectId}/export/markdown`, { responseType: 'blob' }),
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

// Agent Review
export const agentReviewApi = {
  start: (projectId: string, data: any) => api.post(`/projects/${projectId}/agent-reviews`, data),
  list: (projectId: string) => api.get(`/projects/${projectId}/agent-reviews`),
  get: (id: string) => api.get(`/agent-reviews/${id}`),
  stream: (
    projectId: string,
    params: { scope: string; target_id?: string; rounds?: number },
    onMessage: (msg: any) => void,
    onDone: () => void,
    signal?: AbortSignal,
  ) => {
    const query = new URLSearchParams({
      scope: params.scope,
      ...(params.target_id ? { target_id: params.target_id } : {}),
      rounds: String(params.rounds ?? 3),
    })
    const url = `/api/projects/${projectId}/agent-reviews/stream?${query}`
    const es = new EventSource(url)
    es.onmessage = (ev) => {
      try {
        const data = JSON.parse(ev.data)
        if (data.done || data.error) {
          es.close()
          onDone()
        } else {
          onMessage(data)
        }
      } catch {}
    }
    es.onerror = () => {
      es.close()
      onDone()
    }
    if (signal) {
      signal.addEventListener('abort', () => { es.close(); onDone() })
    }
    return es
  },
}

export default api

// LLM Profiles
export const llmProfileApi = {
  list: () => api.get('/llm-profiles'),
  get: (id: string) => api.get(`/llm-profiles/${id}`),
  create: (data: any) => api.post('/llm-profiles', data),
  update: (id: string, data: any) => api.put(`/llm-profiles/${id}`, data),
  delete: (id: string) => api.delete(`/llm-profiles/${id}`),
  setDefault: (id: string) => api.post(`/llm-profiles/${id}/set-default`),
}

// RAG knowledge-base
export const ragApi = {
  getStatus: (projectId: string) => api.get(`/projects/${projectId}/rag/status`),
  rebuild: (projectId: string) => api.post(`/projects/${projectId}/rag/rebuild`),
}
