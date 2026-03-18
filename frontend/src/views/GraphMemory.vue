<template>
  <div class="graph-memory">
    <!-- ── Header ───────────────────────────────────────── -->
    <div class="page-header">
      <h1>Graph Memory</h1>
      <p class="subtitle">
        Knowledge graph (Neo4j) &amp; vector index (Qdrant) for project
        <strong>{{ projectId }}</strong>
      </p>
      <div class="header-actions">
        <button class="btn btn-secondary" :disabled="syncing" @click="syncGraph">
          {{ syncing ? 'Syncing…' : 'Sync to Neo4j' }}
        </button>
        <button class="btn btn-secondary" :disabled="rebuilding" @click="rebuildVector">
          {{ rebuilding ? 'Rebuilding…' : 'Rebuild Vectors' }}
        </button>
        <button class="btn btn-primary" @click="refresh">Refresh</button>
      </div>
    </div>

    <!-- ── Error banner ────────────────────────────────── -->
    <div v-if="error" class="error-banner">{{ error }}</div>

    <!-- ── Tabs ────────────────────────────────────────── -->
    <div class="tabs">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        class="tab-btn"
        :class="{ active: activeTab === tab.key }"
        @click="activeTab = tab.key"
      >
        {{ tab.label }}
      </button>
    </div>

    <!-- ── Tab: Knowledge Graph ────────────────────────── -->
    <section v-if="activeTab === 'graph'" class="tab-section">
      <div v-if="loadingGraph" class="loading">Loading graph…</div>
      <div v-else-if="graphData" class="graph-container">
        <!-- Summary counts -->
        <div class="stats-row">
          <div class="stat-card">
            <span class="stat-num">{{ graphData.nodes.length }}</span>
            <span class="stat-lbl">Nodes</span>
          </div>
          <div class="stat-card">
            <span class="stat-num">{{ graphData.edges.length }}</span>
            <span class="stat-lbl">Edges</span>
          </div>
          <div class="stat-card" v-for="(count, type) in nodeTypeCounts" :key="type">
            <span class="stat-num">{{ count }}</span>
            <span class="stat-lbl">{{ type }}</span>
          </div>
        </div>

        <!-- Node list -->
        <div class="entity-columns">
          <!-- Characters -->
          <div class="entity-group" v-if="nodesByType['Character']">
            <h3>Characters</h3>
            <div
              v-for="node in nodesByType['Character']"
              :key="node.id"
              class="entity-card"
              :class="{ selected: selectedNode?.id === node.id }"
              @click="selectNode(node)"
            >
              <div class="entity-name">{{ node.label }}</div>
              <div class="entity-meta">{{ node.properties.role ?? '' }}</div>
            </div>
          </div>

          <!-- Rules / World -->
          <div class="entity-group" v-if="nodesByType['Rule']">
            <h3>World Rules</h3>
            <div
              v-for="node in nodesByType['Rule']"
              :key="node.id"
              class="entity-card"
              @click="selectNode(node)"
            >
              <div class="entity-name">{{ node.label }}</div>
            </div>
          </div>

          <!-- Foreshadowing -->
          <div class="entity-group" v-if="nodesByType['Foreshadowing']">
            <h3>Foreshadowing</h3>
            <div
              v-for="node in nodesByType['Foreshadowing']"
              :key="node.id"
              class="entity-card"
              @click="selectNode(node)"
            >
              <div class="entity-name">{{ node.label }}</div>
              <div class="entity-meta">{{ node.properties.status ?? '' }}</div>
            </div>
          </div>

          <!-- Events -->
          <div class="entity-group" v-if="nodesByType['Event']">
            <h3>Events</h3>
            <div
              v-for="node in nodesByType['Event']"
              :key="node.id"
              class="entity-card"
              @click="selectNode(node)"
            >
              <div class="entity-name">{{ node.label }}</div>
            </div>
          </div>
        </div>

        <!-- Node detail panel -->
        <div v-if="selectedNode" class="node-detail">
          <h3>{{ selectedNode.type }}: {{ selectedNode.label }}</h3>
          <table class="props-table">
            <tbody>
              <tr v-for="(val, key) in selectedNode.properties" :key="key">
                <td class="prop-key">{{ key }}</td>
                <td class="prop-val">{{ val }}</td>
              </tr>
            </tbody>
          </table>
          <div class="relations">
            <h4>Relationships</h4>
            <div v-if="edgesForSelected.length === 0" class="muted">None</div>
            <div v-for="edge in edgesForSelected" :key="edge.id" class="rel-item">
              <span class="rel-from">{{ labelFor(edge.source) }}</span>
              <span class="rel-type">→ {{ edge.relation }} →</span>
              <span class="rel-to">{{ labelFor(edge.target) }}</span>
            </div>
          </div>
        </div>
      </div>
      <div v-else class="empty-state">No graph data. Click "Sync to Neo4j" to populate.</div>
    </section>

    <!-- ── Tab: Vector Index ───────────────────────────── -->
    <section v-if="activeTab === 'vector'" class="tab-section">
      <div v-if="loadingVector" class="loading">Loading vector stats…</div>
      <div v-else-if="vectorStatus" class="vector-container">
        <div class="stats-row">
          <div class="stat-card" v-for="col in vectorStatus.collections" :key="col.name">
            <span class="stat-num">{{ col.count }}</span>
            <span class="stat-lbl">{{ col.name }}</span>
          </div>
        </div>
        <p class="muted">
          Embedding model: <strong>{{ vectorStatus.embedding_model ?? 'paraphrase-multilingual-mpnet-base-v2' }}</strong>
          &nbsp;|&nbsp; Dimension: <strong>{{ vectorStatus.dimension ?? 768 }}</strong>
        </p>

        <!-- Semantic search -->
        <div class="search-panel">
          <h3>Semantic Search</h3>
          <div class="search-row">
            <input v-model="searchQuery" placeholder="Enter query text…" class="search-input" />
            <select v-model="searchCollection" class="search-select">
              <option value="">All collections</option>
              <option
                v-for="col in vectorStatus.collections"
                :key="col.name"
                :value="col.name"
              >{{ col.name }}</option>
            </select>
            <button class="btn btn-primary" :disabled="searching" @click="runSearch">
              {{ searching ? 'Searching…' : 'Search' }}
            </button>
          </div>
          <div v-if="searchResults.length > 0" class="search-results">
            <div v-for="(r, i) in searchResults" :key="i" class="search-result-item">
              <div class="result-score">score: {{ r.score?.toFixed(4) }}</div>
              <div class="result-text">{{ r.payload?.text ?? JSON.stringify(r.payload) }}</div>
            </div>
          </div>
          <div v-else-if="searchDone" class="muted">No results found.</div>
        </div>
      </div>
      <div v-else class="empty-state">No vector index data. Click "Rebuild Vectors" to index.</div>
    </section>

    <!-- ── Tab: Agent Sessions ─────────────────────────── -->
    <section v-if="activeTab === 'agent'" class="tab-section">
      <div class="agent-panel">
        <h3>Run Agent</h3>
        <div class="form-row">
          <label>Task type</label>
          <select v-model="agentTask">
            <option value="generate_chapter">generate_chapter</option>
            <option value="summarize">summarize</option>
            <option value="consistency_check">consistency_check</option>
          </select>
        </div>
        <div class="form-row">
          <label>Chapter #</label>
          <input v-model.number="agentChapterNum" type="number" min="1" />
        </div>
        <div class="form-row">
          <label>Outline (optional)</label>
          <textarea v-model="agentOutline" rows="4" placeholder="Paste chapter outline…" />
        </div>
        <button class="btn btn-primary" :disabled="agentRunning" @click="runAgent">
          {{ agentRunning ? 'Running…' : 'Run Agent' }}
        </button>

        <!-- Live stream output -->
        <div v-if="agentSessionId" class="agent-output">
          <h4>Session: {{ agentSessionId }}</h4>
          <div class="output-box" ref="outputBox">
            <div v-if="agentStatus" class="status-line">
              Status: <strong>{{ agentStatus }}</strong>
              <span v-if="agentQuality"> | Quality: {{ agentQuality.toFixed(2) }}</span>
            </div>
            <pre class="output-pre">{{ agentOutput }}</pre>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, nextTick } from 'vue'
import { useRoute } from 'vue-router'
import { graphApi, vectorApi, agentApi } from '@/api'

type GraphNode = { id: string; label: string; type: string; properties: Record<string, any> }
type GraphEdge = { id: string; source: string; target: string; relation: string }
type GraphData = { nodes: GraphNode[]; edges: GraphEdge[] }
type CollectionStat = { name: string; count: number }
type VectorStatus = { collections: CollectionStat[]; embedding_model?: string; dimension?: number }

const route = useRoute()
const projectId = computed(() => route.params.projectId as string)

// ── UI state ─────────────────────────────────────────────
const tabs = [
  { key: 'graph', label: 'Knowledge Graph' },
  { key: 'vector', label: 'Vector Index' },
  { key: 'agent', label: 'Agent Sessions' },
]
const activeTab = ref('graph')
const error = ref('')

// ── Graph state ─────────────────────────────────────────
const loadingGraph = ref(false)
const graphData = ref<GraphData | null>(null)
const selectedNode = ref<GraphNode | null>(null)
const syncing = ref(false)

const nodesByType = computed((): Record<string, GraphNode[]> => {
  if (!graphData.value) return {}
  return graphData.value.nodes.reduce((acc, n) => {
    ;(acc[n.type] ??= []).push(n)
    return acc
  }, {} as Record<string, GraphNode[]>)
})

const nodeTypeCounts = computed((): Record<string, number> => {
  return Object.fromEntries(
    Object.entries(nodesByType.value).map(([t, arr]) => [t, arr.length])
  )
})

const edgesForSelected = computed((): GraphEdge[] => {
  if (!selectedNode.value || !graphData.value) return []
  const id = selectedNode.value.id
  return graphData.value.edges.filter(e => e.source === id || e.target === id)
})

function labelFor(nodeId: string): string {
  return graphData.value?.nodes.find(n => n.id === nodeId)?.label ?? nodeId
}

function selectNode(node: GraphNode) {
  selectedNode.value = selectedNode.value?.id === node.id ? null : node
}

async function loadGraph() {
  loadingGraph.value = true
  error.value = ''
  try {
    const res = await graphApi.entities(projectId.value)
    graphData.value = res.data
  } catch (e: any) {
    error.value = e.response?.data?.error ?? e.message
  } finally {
    loadingGraph.value = false
  }
}

async function syncGraph() {
  syncing.value = true
  error.value = ''
  try {
    await graphApi.sync(projectId.value)
    await loadGraph()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? e.message
  } finally {
    syncing.value = false
  }
}

// ── Vector state ─────────────────────────────────────────
const loadingVector = ref(false)
const vectorStatus = ref<VectorStatus | null>(null)
const rebuilding = ref(false)
const searchQuery = ref('')
const searchCollection = ref('')
const searching = ref(false)
const searchResults = ref<any[]>([])
const searchDone = ref(false)

async function loadVectorStatus() {
  loadingVector.value = true
  error.value = ''
  try {
    const res = await vectorApi.status(projectId.value)
    vectorStatus.value = res.data
  } catch (e: any) {
    error.value = e.response?.data?.error ?? e.message
  } finally {
    loadingVector.value = false
  }
}

async function rebuildVector() {
  rebuilding.value = true
  error.value = ''
  try {
    await vectorApi.rebuild(projectId.value)
    await loadVectorStatus()
  } catch (e: any) {
    error.value = e.response?.data?.error ?? e.message
  } finally {
    rebuilding.value = false
  }
}

async function runSearch() {
  if (!searchQuery.value.trim()) return
  searching.value = true
  searchDone.value = false
  searchResults.value = []
  try {
    const res = await vectorApi.search(projectId.value, {
      query: searchQuery.value,
      collections: searchCollection.value ? [searchCollection.value] : undefined,
      top_k: 10,
    })
    searchResults.value = res.data.results ?? []
    searchDone.value = true
  } catch (e: any) {
    error.value = e.response?.data?.error ?? e.message
  } finally {
    searching.value = false
  }
}

// ── Agent state ──────────────────────────────────────────
const agentTask = ref('generate_chapter')
const agentChapterNum = ref(1)
const agentOutline = ref('')
const agentRunning = ref(false)
const agentSessionId = ref('')
const agentOutput = ref('')
const agentStatus = ref('')
const agentQuality = ref<number | null>(null)
const outputBox = ref<HTMLElement | null>(null)
let agentEventSource: EventSource | null = null

async function runAgent() {
  agentRunning.value = true
  agentOutput.value = ''
  agentStatus.value = 'starting'
  agentQuality.value = null
  agentSessionId.value = ''
  if (agentEventSource) { agentEventSource.close(); agentEventSource = null }

  try {
    const res = await agentApi.run(projectId.value, {
      task_type: agentTask.value,
      project_id: projectId.value,
      chapter_num: agentChapterNum.value,
      outline: agentOutline.value,
    })
    const sid = res.data.session_id
    agentSessionId.value = sid
    agentStatus.value = 'running'

    agentEventSource = agentApi.stream(
      sid,
      (payload) => {
        if (payload.token) {
          agentOutput.value += payload.token
        } else if (payload.node) {
          agentStatus.value = `node:${payload.node}`
        }
        nextTick(() => {
          if (outputBox.value) outputBox.value.scrollTop = outputBox.value.scrollHeight
        })
      },
      async () => {
        agentRunning.value = false
        // Final status poll
        try {
          const st = await agentApi.status(sid)
          agentStatus.value = st.data.status
          agentQuality.value = st.data.result?.quality_score ?? null
        } catch { /* ignore */ }
      },
    )
  } catch (e: any) {
    error.value = e.response?.data?.error ?? e.message
    agentRunning.value = false
  }
}

// ── Lifecycle ────────────────────────────────────────────
function refresh() {
  loadGraph()
  loadVectorStatus()
}

onMounted(refresh)
</script>

<style scoped>
.graph-memory { padding: 2rem; max-width: 1400px; margin: 0 auto; }

.page-header { display: flex; flex-wrap: wrap; align-items: baseline; gap: 1rem; margin-bottom: 1.5rem; }
.page-header h1 { margin: 0; }
.subtitle { color: var(--color-text-muted, #666); flex: 1; min-width: 200px; }
.header-actions { display: flex; gap: 0.5rem; }

.error-banner { background: #fee; border: 1px solid #f88; border-radius: 6px;
  padding: 0.75rem 1rem; color: #c00; margin-bottom: 1rem; }

.tabs { display: flex; gap: 0.25rem; border-bottom: 2px solid #e0e0e0; margin-bottom: 1.5rem; }
.tab-btn { padding: 0.5rem 1.25rem; border: none; background: none; cursor: pointer;
  font-size: 0.95rem; border-bottom: 2px solid transparent; margin-bottom: -2px;
  color: #555; transition: color 0.2s; }
.tab-btn.active { color: var(--color-primary, #6366f1); border-bottom-color: currentColor; font-weight: 600; }

.tab-section { animation: fadeIn 0.2s; }
@keyframes fadeIn { from { opacity: 0; transform: translateY(4px); } to { opacity: 1; } }

.loading { color: #888; padding: 2rem; text-align: center; }
.empty-state { color: #aaa; padding: 3rem; text-align: center; border: 2px dashed #ddd; border-radius: 8px; }
.muted { color: #888; font-size: 0.9rem; margin: 0.5rem 0; }

/* ── Graph ── */
.stats-row { display: flex; flex-wrap: wrap; gap: 1rem; margin-bottom: 1.5rem; }
.stat-card { background: #f9f9fb; border: 1px solid #e4e4e7; border-radius: 8px;
  padding: 1rem 1.5rem; text-align: center; min-width: 100px; }
.stat-num { display: block; font-size: 2rem; font-weight: 700; color: var(--color-primary, #6366f1); }
.stat-lbl { font-size: 0.8rem; color: #777; text-transform: uppercase; letter-spacing: 0.05em; }

.entity-columns { display: flex; flex-wrap: wrap; gap: 1.5rem; }
.entity-group { flex: 1; min-width: 200px; max-width: 300px; }
.entity-group h3 { font-size: 0.9rem; text-transform: uppercase; letter-spacing: 0.05em;
  color: #888; margin-bottom: 0.5rem; border-bottom: 1px solid #eee; padding-bottom: 0.25rem; }
.entity-card { background: #fff; border: 1px solid #e4e4e7; border-radius: 6px;
  padding: 0.6rem 0.9rem; margin-bottom: 0.4rem; cursor: pointer; transition: border-color 0.15s; }
.entity-card:hover, .entity-card.selected { border-color: var(--color-primary, #6366f1); }
.entity-name { font-weight: 500; }
.entity-meta { font-size: 0.8rem; color: #888; margin-top: 2px; }

.node-detail { margin-top: 2rem; background: #f9f9fb; border: 1px solid #e4e4e7;
  border-radius: 8px; padding: 1.25rem; }
.node-detail h3 { margin: 0 0 1rem; }
.props-table { width: 100%; border-collapse: collapse; margin-bottom: 1rem; }
.props-table td { padding: 0.35rem 0.5rem; vertical-align: top; }
.prop-key { font-weight: 500; color: #555; white-space: nowrap; width: 35%; }
.prop-val { color: #222; }
.relations h4 { margin: 0 0 0.5rem; font-size: 0.95rem; }
.rel-item { font-size: 0.9rem; padding: 0.3rem 0; }
.rel-from, .rel-to { font-weight: 500; }
.rel-type { color: #888; margin: 0 0.4rem; font-style: italic; }

/* ── Vector ── */
.vector-container { }
.search-panel { margin-top: 2rem; }
.search-panel h3 { margin-bottom: 0.75rem; }
.search-row { display: flex; gap: 0.5rem; flex-wrap: wrap; }
.search-input { flex: 1; min-width: 200px; padding: 0.5rem 0.75rem;
  border: 1px solid #d0d0d0; border-radius: 6px; font-size: 0.95rem; }
.search-select { padding: 0.5rem; border: 1px solid #d0d0d0; border-radius: 6px; }
.search-results { margin-top: 1rem; display: flex; flex-direction: column; gap: 0.75rem; }
.search-result-item { background: #fff; border: 1px solid #e4e4e7; border-radius: 6px; padding: 0.75rem 1rem; }
.result-score { font-size: 0.8rem; color: #888; margin-bottom: 0.25rem; }
.result-text { font-size: 0.9rem; white-space: pre-wrap; }

/* ── Agent ── */
.agent-panel { max-width: 800px; }
.agent-panel h3 { margin-bottom: 1rem; }
.form-row { display: flex; gap: 1rem; align-items: flex-start; margin-bottom: 0.75rem; }
.form-row label { width: 120px; padding-top: 0.4rem; font-size: 0.9rem; color: #555; }
.form-row input, .form-row select, .form-row textarea {
  flex: 1; padding: 0.45rem 0.75rem; border: 1px solid #d0d0d0;
  border-radius: 6px; font-size: 0.95rem; }
.form-row textarea { font-family: inherit; resize: vertical; }
.agent-output { margin-top: 1.5rem; }
.agent-output h4 { margin-bottom: 0.5rem; color: #555; }
.output-box { border: 1px solid #e4e4e7; border-radius: 8px; padding: 1rem;
  max-height: 400px; overflow-y: auto; background: #fafafa; }
.status-line { font-size: 0.85rem; color: #777; margin-bottom: 0.5rem; }
.output-pre { white-space: pre-wrap; font-family: inherit; font-size: 0.9rem; margin: 0; }

/* ── Buttons ── */
.btn { padding: 0.45rem 1rem; border-radius: 6px; border: none; cursor: pointer; font-size: 0.9rem; }
.btn:disabled { opacity: 0.55; cursor: not-allowed; }
.btn-primary { background: var(--color-primary, #6366f1); color: #fff; }
.btn-primary:hover:not(:disabled) { filter: brightness(0.9); }
.btn-secondary { background: #f4f4f5; color: #333; border: 1px solid #d4d4d8; }
.btn-secondary:hover:not(:disabled) { background: #e8e8ea; }
</style>
