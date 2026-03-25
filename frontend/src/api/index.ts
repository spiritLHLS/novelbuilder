import axios from 'axios'
import type { AxiosResponse } from 'axios'

const TOKEN_KEY = 'nb_token'

const api = axios.create({
  baseURL: '/api',
  timeout: 300000,
  headers: {
    'Content-Type': 'application/json',
  },
})

// Attach the session token to every request.
api.interceptors.request.use((config) => {
  const token = localStorage.getItem(TOKEN_KEY)
  if (token) {
    config.headers['Authorization'] = `Bearer ${token}`
  }
  return config
})

api.interceptors.response.use(
  (response: AxiosResponse) => response,
  (error) => {
    const msg = error.response?.data?.message || error.response?.data?.error || error.message
    console.error('API Error:', msg)
    // Redirect to login on auth failure (skip login endpoint itself to avoid loops).
    if (error.response?.status === 401 && !error.config?.url?.includes('/auth/')) {
      localStorage.removeItem(TOKEN_KEY)
      window.location.href = '/login'
    }
    return Promise.reject(error)
  }
)

// Auth
export const authApi = {
  login: (username: string, password: string) => api.post('/auth/login', { username, password }),
  logout: () => api.post('/auth/logout'),
  check: () => api.get('/auth/check'),
}

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
  export: (projectId: string) =>
    api.get(`/projects/${projectId}/world-bible/export`),
  import: (projectId: string, bundle: any) =>
    api.post(`/projects/${projectId}/world-bible/import`, bundle),
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
  update: (_projectId: string, id: string, data: any) => api.put(`/foreshadowings/${id}`, data),
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
  delete: (_projectId: string, id: string) => api.delete(`/chapters/${id}`),
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
}

// Quality
export const qualityApi = {
  runCheck: (_projectId: string, chapterId: string) =>
    api.post(`/chapters/${chapterId}/quality-check`),
}

// Workflow
export const workflowApi = {
  start: (projectId: string) => api.post(`/projects/${projectId}/workflow/start`),
  getHistory: (projectId: string) => api.get(`/workflows/${projectId}/history`),
  rollback: (runId: string, data: any) => api.post(`/workflows/${runId}/rollback`, data),
  getDiff: (runId: string, fromStep: string, toStep: string) =>
    api.get(`/workflows/${runId}/diff`, { params: { fromStep, toStep } }),
  approveStep: (stepId: string, comment?: string) => 
    api.post(`/workflow-steps/${stepId}/approve`, { comment }),
  rejectStep: (stepId: string, comment?: string) => 
    api.post(`/workflow-steps/${stepId}/reject`, { comment }),
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
  importFromURL: (projectId: string, data: { url: string; title: string; author: string; genre: string }) =>
    api.post(`/projects/${projectId}/references/import-url`, data),
  searchNovels: (projectId: string, keyword: string, sites?: string[] | null, perSiteLimit = 10) =>
    api.post(`/projects/${projectId}/references/search`, { keyword, sites: sites ?? null, limit: 0, per_site_limit: perSiteLimit }),
  getBookInfo: (projectId: string, site: string, bookId: string) =>
    api.post(`/projects/${projectId}/references/book-info`, { site, book_id: bookId }),
  /** Start a background download; returns {ref_id, status, fetch_total} immediately. */
  startFetchImport: (projectId: string, data: {
    site: string; book_id: string; title: string; author: string; genre: string; chapter_ids: string[]
  }) => api.post(`/projects/${projectId}/references/fetch-import`, data),
  updateMigrationConfig: (id: string, config: any) =>
    api.put(`/references/${id}/migration-config`, config),
  analyze: (id: string) => api.post(`/references/${id}/analyze`),
  delete: (id: string) => api.delete(`/references/${id}`),
  // Chapter management
  listChapters: (refId: string) => api.get(`/references/${refId}/chapters`),
  deleteChapter: (chapterId: string) => api.delete(`/reference-chapters/${chapterId}`),
  batchDeleteChapters: (refId: string, ids: string[]) =>
    api.post(`/references/${refId}/chapters/batch-delete`, { ids }),
  // Export / import
  exportSingle: (refId: string) =>
    api.get(`/references/${refId}/export`, { responseType: 'blob' }),
  exportBatch: (projectId: string, refIds: string[]) =>
    api.post(`/projects/${projectId}/references/export-batch`, { ids: refIds }, { responseType: 'blob' }),
  importLocal: (projectId: string, bundle: any) =>
    api.post(`/projects/${projectId}/references/import-local`, bundle),
  // Resume a failed/interrupted download
  resumeDownload: (refId: string) => api.post(`/references/${refId}/resume-download`),
  // Deep analysis (chunked, background)
  startDeepAnalysis: (id: string) => api.post(`/references/${id}/deep-analyze`),
  getDeepAnalysisJob: (id: string) => api.get(`/references/${id}/deep-analyze/job`),
  cancelDeepAnalysis: (id: string) => api.post(`/references/${id}/deep-analyze/cancel`),
  resetDeepAnalysis: (id: string) => api.post(`/references/${id}/deep-analyze/reset`),
  importDeepAnalysisResult: (id: string) => api.post(`/references/${id}/deep-analyze/import`),
}

export interface ReferenceChapter {
  id: string
  ref_id: string
  chapter_no: number
  chapter_id: string
  title: string
  word_count: number
  is_deleted: boolean
  created_at: string
}

export interface NovelSearchResult {
  site: string
  book_id: string
  book_url: string
  cover_url: string
  title: string
  author: string
  latest_chapter: string
  update_date: string
  word_count: string
}

export interface FetchChapterInfo {
  chapter_id: string
  title: string
  accessible: boolean
}

export interface FetchVolumeInfo {
  volume_name: string
  chapters: FetchChapterInfo[]
}

export interface FetchBookInfo {
  site: string
  book_id: string
  title: string
  author: string
  summary: string
  cover_url: string
  volumes: FetchVolumeInfo[]
  total_chapters: number
}

export type FetchImportEvent =
  | { type: 'progress'; done: number; total: number; chapter_title: string }
  | { type: 'done'; file_path: string; total_chapters: number; skipped_chapters: number; ref_id?: string }
  | { type: 'error'; message: string }

export type NovelSearchStreamEvent =
  | { type: 'batch'; site: string; results: NovelSearchResult[] }
  | { type: 'done'; total: number }
  | { type: 'error'; message: string }

export async function* streamSearchNovels(
  projectId: string,
  keyword: string,
  options?: { sites?: string[] | null; perSiteLimit?: number; signal?: AbortSignal },
): AsyncGenerator<NovelSearchStreamEvent> {
  const resp = await fetch(`/api/projects/${projectId}/references/search-stream`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      keyword,
      sites: options?.sites ?? null,
      per_site_limit: options?.perSiteLimit ?? 10,
    }),
    signal: options?.signal,
  })
  if (!resp.ok) {
    const text = await resp.text()
    throw new Error(text || `HTTP ${resp.status}`)
  }
  const reader = resp.body!.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() ?? ''
      for (const line of lines) {
        const trimmed = line.trim()
        if (trimmed) {
          try {
            yield JSON.parse(trimmed) as NovelSearchStreamEvent
          } catch {
            // ignore malformed lines
          }
        }
      }
    }
  } finally {
    reader.cancel()
  }
}

export async function* streamFetchImport(
  projectId: string,
  data: {
    site: string
    book_id: string
    title: string
    author: string
    genre: string
    chapter_ids: string[]
  },
): AsyncGenerator<FetchImportEvent> {
  const resp = await fetch(`/api/projects/${projectId}/references/fetch-import`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  })
  if (!resp.ok) {
    const text = await resp.text()
    throw new Error(text || `HTTP ${resp.status}`)
  }
  const reader = resp.body!.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split('\n')
    buffer = lines.pop() ?? ''
    for (const line of lines) {
      const trimmed = line.trim()
      if (trimmed) {
        try {
          yield JSON.parse(trimmed) as FetchImportEvent
        } catch {
          // ignore malformed lines
        }
      }
    }
  }
}

// Service Logs
export const logsApi = {
  /** Return available service names. */
  listServices: () => api.get<{ services: string[] }>('/logs'),
  /** Return the last `lines` log lines for `service`. */
  getLines: (service: string, lines = 200) =>
    api.get<{ service: string; lines: string[]; total: number }>(
      '/logs', { params: { service, lines } }
    ),
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
  /** Test connectivity with the given credentials (or a saved profile_id). */
  test: (data: {
    profile_id?: string
    base_url?: string
    api_key?: string
    model_name?: string
    api_style?: string
    provider?: string
  }) => api.post<{ ok: boolean; model?: string; duration_ms?: number; error?: string }>('/llm-profiles/test', data),
}

// RAG knowledge-base
export const ragApi = {
  getStatus: (projectId: string) => api.get(`/projects/${projectId}/rag/status`),
  rebuild: (projectId: string) => api.post(`/projects/${projectId}/rag/rebuild`),
  rebuildStatus: (projectId: string) => api.get(`/projects/${projectId}/rag/rebuild-status`),
}

export const propagationApi = {
  /** Create a change event and run AI impact analysis; returns a PatchPlan. */
  createChangeEvent: (projectId: string, data: {
    entity_type: string
    entity_id: string
    change_summary: string
    old_snapshot?: unknown
    new_snapshot?: unknown
  }) => api.post(`/projects/${projectId}/change-events`, data),

  listChangeEvents: (projectId: string) =>
    api.get(`/projects/${projectId}/change-events`),

  getPatchPlan: (planId: string) => api.get(`/patch-plans/${planId}`),

  updateItemStatus: (itemId: string, status: 'approved' | 'skipped' | 'pending') =>
    api.put(`/patch-items/${itemId}/status`, { status }),

  executeItem: (itemId: string) => api.post(`/patch-items/${itemId}/execute`),
}

// Prompt Presets
export const promptPresetApi = {
  listGlobal: () => api.get('/prompt-presets'),
  listForProject: (projectId: string) => api.get(`/projects/${projectId}/prompt-presets`),
  get: (id: string) => api.get(`/prompt-presets/${id}`),
  create: (data: any, projectId?: string) =>
    api.post(`/prompt-presets${projectId ? `?project_id=${projectId}` : ''}`, data),
  update: (id: string, data: any) => api.put(`/prompt-presets/${id}`, data),
  delete: (id: string) => api.delete(`/prompt-presets/${id}`),
}

// Glossary 术语表
export const glossaryApi = {
  list: (projectId: string) => api.get(`/projects/${projectId}/glossary`),
  create: (projectId: string, data: any) => api.post(`/projects/${projectId}/glossary`, data),
  delete: (id: string) => api.delete(`/glossary/${id}`),
}

// Task Queue
export const taskApi = {
  list: (projectId: string, params?: { page?: number; page_size?: number; status?: string; type?: string }) => 
    api.get(`/projects/${projectId}/tasks`, { params }),
  get: (id: string) => api.get(`/tasks/${id}`),
  enqueue: (data: any) => api.post('/tasks', data),
  cancel: (id: string) => api.post(`/tasks/${id}/cancel`),
  retry: (id: string) => api.post(`/tasks/${id}/retry`),
}

// Resource Ledger (InkOS particle_ledger)
export const resourceApi = {
  list: (projectId: string) => api.get(`/projects/${projectId}/resources`),
  get: (id: string) => api.get(`/resources/${id}`),
  create: (projectId: string, data: any) => api.post(`/projects/${projectId}/resources`, data),
  update: (id: string, data: any) => api.put(`/resources/${id}`, data),
  delete: (id: string) => api.delete(`/resources/${id}`),
  recordChange: (resourceId: string, data: any) => api.post(`/resources/${resourceId}/changes`, data),
  listChanges: (resourceId: string) => api.get(`/resources/${resourceId}/changes`),
}

// Vocab Fatigue Stats
export const vocabApi = {
  getStats: (projectId: string, topN = 50) =>
    api.get(`/projects/${projectId}/quality/vocab-stats?top=${topN}`),
}

// Webhook Notifications
export const webhookApi = {
  list: (projectId: string) => api.get(`/projects/${projectId}/webhooks`),
  create: (projectId: string, data: any) => api.post(`/projects/${projectId}/webhooks`, data),
  update: (id: string, data: any) => api.put(`/webhooks/${id}`, data),
  delete: (id: string) => api.delete(`/webhooks/${id}`),
}

// ── LangGraph Agent ─────────────────────────────────────────────────────────
export const agentApi = {
  /** Start an agent session (returns { session_id }). */
  run: (projectId: string, data: {
    task_type: string
    project_id: string
    chapter_num?: number
    outline?: string
    style?: string
    llm_profile_id?: string
    [key: string]: any
  }) => api.post(`/projects/${projectId}/agent/run`, data),

  /** Poll session status. */
  status: (sessionId: string) => api.get(`/agent/sessions/${sessionId}/status`),

  /**
   * Open an SSE stream for the agent session.
   * Calls onChunk for each token/event, onDone when finished.
   * Returns the EventSource so caller can close it.
   */
  stream: (
    sessionId: string,
    onChunk: (data: any) => void,
    onDone: () => void,
    signal?: AbortSignal,
  ): EventSource => {
    const url = `/api/agent/sessions/${sessionId}/stream`
    const es = new EventSource(url)
    es.onmessage = (ev) => {
      try {
        const payload = JSON.parse(ev.data)
        if (payload.done || payload.error) {
          es.close()
          onDone()
        } else {
          onChunk(payload)
        }
      } catch { /* ignore non-JSON frames */ }
    }
    es.onerror = () => { es.close(); onDone() }
    if (signal) signal.addEventListener('abort', () => { es.close(); onDone() })
    return es
  },
}

// ── Knowledge Graph (Neo4j via Python sidecar) ───────────────────────────────
export const graphApi = {
  /** Get all entities + relationships for a project as a graph. */
  entities: (projectId: string) =>
    api.get(`/projects/${projectId}/graph/entities`),

  /** Execute a read-only Cypher query. */
  query: (projectId: string, data: { cypher: string; params?: Record<string, any> }) =>
    api.post(`/projects/${projectId}/graph/query`, data),

  /** Upsert a single entity node. */
  upsert: (projectId: string, data: {
    entity_type: string
    entity_id: string
    properties: Record<string, any>
  }) => api.post(`/projects/${projectId}/graph/upsert`, data),

  /**
   * Batch-sync all PG entities for this project into Neo4j.
   * Safe: uses single SELECT per table + batch upsert (no N+1).
   */
  sync: (projectId: string) =>
    api.post(`/projects/${projectId}/graph/sync`),
}

// ── Vector Store (Qdrant via Python sidecar) ─────────────────────────────────
export const vectorApi = {
  /** Get per-collection statistics for a project. */
  status: (projectId: string) =>
    api.get(`/projects/${projectId}/vector/status`),

  /** Rebuild / re-index all vectors for a project. */
  rebuild: (projectId: string, data?: { force?: boolean }) =>
    api.post(`/projects/${projectId}/vector/rebuild`, data ?? {}),

  /** Semantic search across one or more collections. */
  search: (projectId: string, data: {
    query: string
    collections?: string[]
    top_k?: number
    score_threshold?: number
  }) => api.post(`/projects/${projectId}/vector/search`, data),
}

// ── System Settings ──────────────────────────────────────────────────────────
// Manages app-level key-value settings stored in the system_settings table.
// encryption_key is write-protected (auto-generated on first boot).
export const systemSettingsApi = {
  /** Get all non-sensitive settings as { key: value } map. */
  getAll: () => api.get('/settings'),
  /** Upsert a single setting. */
  set: (key: string, value: string) => api.put(`/settings/${key}`, { value }),
  /** Delete a setting (resets it to its application default). */
  delete: (key: string) => api.delete(`/settings/${key}`),
}

// ── 33-Dimension Audit ────────────────────────────────────────────────────────
export const auditApi = {
  /** Run a full 33-dimension audit on a chapter. */
  run: (chapterId: string) => api.post(`/chapters/${chapterId}/audit`),
  /** Fetch the latest audit report for a chapter. */
  getReport: (chapterId: string) => api.get(`/chapters/${chapterId}/audit-report`),
}

// ── Anti-AI Rewrite (去AI味) ──────────────────────────────────────────────────
export const antiDetectApi = {
  /** Rewrite chapter content to remove AI-flavor patterns. */
  rewrite: (chapterId: string, intensity: 'light' | 'medium' | 'heavy' = 'medium') =>
    api.post(`/chapters/${chapterId}/anti-detect`, { intensity }),
}

// ── Book Rules (style guide + anti-AI wordlists) ──────────────────────────────
export const bookRulesApi = {
  get: (projectId: string) => api.get(`/projects/${projectId}/book-rules`),
  update: (projectId: string, data: {
    rules_content?: string
    style_guide?: string
    anti_ai_wordlist?: string[]
    banned_patterns?: string[]
  }) => api.put(`/projects/${projectId}/book-rules`, data),
}

// ── Creative Brief (创作简报) ──────────────────────────────────────────────────
export const creativeBriefApi = {
  /** Generate world_bible + book_rules from a free-form creative brief. */
  generate: (projectId: string, data: { brief_text: string; genre?: string }) =>
    api.post(`/projects/${projectId}/creative-brief`, data),
}

// ── Chapter Import (续写已有作品) ─────────────────────────────────────────────
export const importApi = {
  create: (projectId: string, data: {
    source_text: string
    split_pattern?: string
    fanfic_mode?: string | null
  }) => api.post(`/projects/${projectId}/import-chapters`, data),
  list: (projectId: string) => api.get(`/projects/${projectId}/import-chapters`),
  get: (importId: string) => api.get(`/imports/${importId}`),
  process: (importId: string) => api.post(`/imports/${importId}/process`),
}

// ── Fan Fiction ───────────────────────────────────────────────────────────────
export const fanficApi = {
  update: (projectId: string, data: { fanfic_mode?: string | null; fanfic_source_text?: string }) =>
    api.put(`/projects/${projectId}/fanfic`, data),
}

// ── Per-Agent Model Routing ───────────────────────────────────────────────────
export const agentRoutingApi = {
  /** List global agent routes (no project scope). */
  listGlobal: () => api.get('/agent-routes'),
  /** Set a global agent route. */
  setGlobal: (agentType: string, data: { llm_profile_id: string | null }) =>
    api.put(`/agent-routes/${agentType}`, data),
  /** Delete a global agent route. */
  deleteGlobal: (agentType: string) => api.delete(`/agent-routes/${agentType}`),
  /** List project-scoped agent routes (merged with globals). */
  listProject: (projectId: string) => api.get(`/projects/${projectId}/agent-routes`),
  /** Set a project-scoped agent route. */
  setProject: (projectId: string, agentType: string, data: { llm_profile_id: string | null }) =>
    api.put(`/projects/${projectId}/agent-routes/${agentType}`, data),
  /** Delete a project-scoped agent route. */
  deleteProject: (projectId: string, agentType: string) =>
    api.delete(`/projects/${projectId}/agent-routes/${agentType}`),
}

// ── Auto-Write Daemon ─────────────────────────────────────────────────────────
export const autoWriteApi = {
  /** Enable auto-write with given interval, or disable (interval = 0). */
  set: (projectId: string, intervalMinutes: number) =>
    api.put(`/projects/${projectId}/auto-write`, { interval_minutes: intervalMinutes }),
}

// ── Analytics Dashboard ───────────────────────────────────────────────────────
export const analyticsApi = {
  get: (projectId: string) => api.get(`/projects/${projectId}/analytics`),
}

// ── Export (extended with EPUB) ───────────────────────────────────────────────
export const exportExtApi = {
  epub: (projectId: string) =>
    api.get(`/projects/${projectId}/export/epub`, { responseType: 'blob' }),
}

// ── Batch Chapter Write ───────────────────────────────────────────────────────
export const batchWriteApi = {
  /** Count-based: enqueue `count` sequential generate_next_chapter tasks. */
  generate: (projectId: string, count: number, outlineHints?: string[]) =>
    api.post(`/projects/${projectId}/chapters/batch-generate`, {
      count,
      outline_hints: outlineHints,
    }),

  /** Volume-based: enqueue one chapter_generate task per chapter in the volume's range. */
  generateByVolume: (projectId: string, volumeId: string, outlineHints?: string[]) =>
    api.post(`/projects/${projectId}/chapters/batch-generate`, {
      volume_id: volumeId,
      outline_hints: outlineHints,
    }),
}

// ── Subplot Board ─────────────────────────────────────────────────────────────
export const subplotApi = {
  list: (projectId: string) => api.get(`/projects/${projectId}/subplots`),
  create: (projectId: string, data: any) => api.post(`/projects/${projectId}/subplots`, data),
  update: (id: string, data: any) => api.put(`/subplots/${id}`, data),
  delete: (id: string) => api.delete(`/subplots/${id}`),
  listCheckpoints: (subplotId: string) => api.get(`/subplots/${subplotId}/checkpoints`),
  addCheckpoint: (subplotId: string, data: any) => api.post(`/subplots/${subplotId}/checkpoints`, data),
}

// ── Emotional Arcs ────────────────────────────────────────────────────────────
export const emotionalArcApi = {
  list: (projectId: string) => api.get(`/projects/${projectId}/emotional-arcs`),
  upsert: (projectId: string, data: any) => api.post(`/projects/${projectId}/emotional-arcs`, data),
  delete: (id: string) => api.delete(`/emotional-arcs/${id}`),
}

// ── Character Interaction Matrix ──────────────────────────────────────────────
export const charInteractionApi = {
  list: (projectId: string) => api.get(`/projects/${projectId}/character-interactions`),
  upsert: (projectId: string, data: any) => api.post(`/projects/${projectId}/character-interactions`, data),
  delete: (id: string) => api.delete(`/character-interactions/${id}`),
}

// ── Radar Market Scan ─────────────────────────────────────────────────────────
export const radarApi = {
  scan: (projectId: string, data: { genre?: string; platform?: string; focus?: string }) =>
    api.post(`/projects/${projectId}/radar/scan`, data),
  history: (projectId: string) => api.get(`/projects/${projectId}/radar/history`),
}

// ── Genre Templates (题材专属规则) ─────────────────────────────────────────────
export const genreTemplateApi = {
  list: () => api.get('/genre-templates'),
  get: (genre: string) => api.get(`/genre-templates/${encodeURIComponent(genre)}`),
  upsert: (genre: string, data: {
    rules_content?: string
    language_constraints?: string
    rhythm_rules?: string
    audit_dimensions_extra?: Record<string, any>
  }) => api.put(`/genre-templates/${encodeURIComponent(genre)}`, data),
  delete: (genre: string) => api.delete(`/genre-templates/${encodeURIComponent(genre)}`),
}
