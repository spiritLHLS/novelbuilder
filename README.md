# NovelBuilder — Re³ Agentic Novel Writing Platform

> **AI 长篇小说驱动平台**：基于 LangGraph Agent + Graphiti 图记忆 + Qdrant 向量检索 + Re³ 双轨上下文注入 + RecurrentGPT 记忆 + Lost-in-Middle 排布

---

## 整体架构

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                        Single Docker Container                               │
│                                                                              │
│  ┌──────────┐  HTTP   ┌──────────────┐  HTTP   ┌────────────────────────┐  │
│  │  Vue SPA  │ ──────▶ │  Go API GW   │ ──────▶ │  Python Agent Service  │  │
│  │  :80      │         │  :8080       │         │  (LangGraph) :8081     │  │
│  └──────────┘         └──────┬───────┘         └───────────┬────────────┘  │
│                              │                              │               │
│                    PostgreSQL │                    ┌─────────┼──────────┐   │
│                    Redis      │                    │         │          │   │
│                    (CRUD /    │                 Neo4j    Qdrant    LLM API │
│                    TaskQueue) │                 :7687     :6333    (cloud) │
│                              │                  (Graph   (Vector          │
│                              │                  Memory)  Search)          │
└──────────────────────────────────────────────────────────────────────────────┘
```

### 核心数据流

```
User Request
     │
     ▼
Go API Gateway (身份校验 / 参数验证 / 路由)
     │
     ▼
LangGraph Agent (编排大脑)
     │
     ├─▶ Planner Node         任务分解 → plan_steps[]
     │
     ├─▶ Recall Memory Node   graphiti → Neo4j 图查询 (长期记忆)
     │
     ├─▶ [并行] Graph Retrieval   Neo4j Cypher 结构化查询
     │       +  Vector Retrieval  Qdrant 语义相似检索
     │
     ├─▶ Context Assembler    Re³ 双轨 + Lost-in-Middle 排布
     │       ├── Track-1 World   世界观 / 角色核心 (Neo4j)
     │       └── Track-2 Narrative 叙事连贯 / 上文摘要 (Qdrant)
     │       排布: [锚首:规则] [中部:检索] [锚尾:写作指令+outline]
     │
     ├─▶ Generator Node       LLM 调用 → draft
     │
     ├─▶ Update Memory Node   graphiti 提取实体 → Neo4j 更新
     │       + RecurrentGPT   短期工作记忆写入 Redis
     │       + Qdrant 更新章节向量
     │
     └─▶ Quality Check Node   质量评估 → 不合格则 re-generate
```

---

## 核心技术设计

### Re³ 双轨上下文注入 (Dual-Track Context Injection)

| 轨道 | 内容 | 来源 |
|------|------|------|
| Track-1 世界知识轨 | 世界宪法、角色核心、世界观规则 | Neo4j 图数据库 |
| Track-2 叙事连贯轨 | 前N章摘要、伏笔、情节弧线 | Qdrant 向量检索 + Redis |

### RecurrentGPT 记忆机制

- **短期记忆** → Redis → 当前工作记忆（最近3-5段落）
- **长期记忆** → Neo4j → graphiti 实体节点（事件/角色弧线）
- **摘要压缩** → 每章结束后 LLM 生成章节摘要 → 存进 graphiti

### Lost-in-Middle 排布

```
Prompt 结构:
┌─────────────────────────────────┐  ← ANCHOR_TOP (最重要)
│ 世界宪法不变规则                │
│ 核心角色设定                    │
├─────────────────────────────────┤  ← MIDDLE (次要，LLM 易遗忘区)
│ 近期章节摘要                    │
│ 向量检索相似段落                │
│ 人物关系图谱                    │
├─────────────────────────────────┤  ← ANCHOR_BOTTOM (最重要)
│ 当前章节 Outline                │
│ 写作风格指令                    │
│ 生成任务正文                    │
└─────────────────────────────────┘
```

---

## 技术栈

| 层 | 技术 | 用途 |
|----|------|------|
| Agent 编排 | LangGraph | 多步推理状态机 |
| 图记忆 | Zep Graphiti-Core | 实体/关系/事件记忆 |
| 图数据库 | Neo4j 5.x Community | 知识图谱存储 |
| 向量数据库 | Qdrant 1.x | 语义向量检索 |
| 关系数据库 | PostgreSQL 16 | 结构化业务数据 |
| 缓存/队列 | Redis 7 | 短期记忆 / 任务队列 |
| AI 网关 | Go (Gin) | 多模型路由 / 加密存储 |
| Python 服务 | FastAPI + LangGraph | Agent 逻辑 + 分析管线 |
| 前端 | Vue 3 + Vite | SPA 交互界面 |

---

## 项目结构

```
novelbuilder/
├── backend/                    # Go API 网关
├── python-sidecar/
│   ├── agent/                  # LangGraph Agent
│   │   ├── state.py            # AgentState TypedDict
│   │   ├── graph.py            # StateGraph 定义
│   │   └── nodes/              # 各节点实现
│   ├── graph_store/            # Neo4j 直接操作
│   ├── vector_store/           # Qdrant 操作
│   ├── analyzers/              # 参考书四层分析
│   ├── humanizer/              # 拟人化管线
│   └── novel-downloader/       # 小说下载库 (git submodule)
├── frontend/
│   └── src/
│       └── views/
│           └── GraphMemory.vue # 知识图谱可视化
├── migrations/
│   ├── 001_init.sql
│   └── 002_agent.sql           # Agent / 图谱元数据表
├── configs/config.yaml
└── Dockerfile                  # 单容器全组件
```

---

## 启动

```bash
docker rm -f nb
```

```bash
docker volume rm novelbuilder-pg
docker volume rm novelbuilder-qdrant
docker volume rm novelbuilder-neo4j
```

```bash
rm -rf novelbuilder
docker system prune -a
```

```bash
# novel-downloader 是 git 子模块，克隆时需带 --recurse-submodules
git clone --recurse-submodules https://github.com/spiritLHLS/novelbuilder.git
cd novelbuilder
docker build --no-cache -t novelbuilder .
docker run -d \
  --name nb \
  -p 8080:8080 \
  -v novelbuilder-pg:/var/lib/postgresql/data \
  -v novelbuilder-qdrant:/var/lib/qdrant \
  -v novelbuilder-neo4j:/opt/neo4j/data \
  -e ADMIN_USERNAME=spiritlhl \
  -e ADMIN_PASSWORD=spiritlhl136@136 \
  novelbuilder
docker logs -f nb
```

> **重要：三个`-v`缺一不可。**
> - `novelbuilder-pg` — 关系型数据库（项目、角色、章节等所有业务数据）
> - `novelbuilder-qdrant` — 向量索引（知识库重建结果）← **缺少此项会导致重建的索引在容器重启后全部丢失**
> - `novelbuilder-neo4j` — 知识图谱（Agent 长期记忆）
>
> 使用具名卷（named volume）而非匿名卷（anonymous volume）确保数据在容器重建后仍然保留。
> 也可以使用下方的 `docker-compose.yml` 方式，自动管理所有卷。

或者使用 Docker Compose（推荐，卷由 Compose 统一管理）：

```bash
docker compose up -d
docker compose logs -f
```

打开 http://localhost:8080，进入 **设置 → AI 模型配置** 添加 LLM Profile（填写 API Key），
再进入 **设置 → 系统设置** 调整质量阈值等参数。无需任何环境变量。

> **加密密钥** 由系统在首次启动时自动生成并存入数据库，无需手动指定 `ENCRYPTION_KEY`。
> **AI API Key** 通过前端 Settings → AI 模型配置 页面配置，加密存储在数据库中。
>
> **登录认证**（默认凭据，建议通过环境变量覆盖）：
>
> | 环境变量 | 默认值 | 说明 |
> |----------|--------|------|
> | `ADMIN_USERNAME` | `spiritlhl` | 登录用户名 |
> | `ADMIN_PASSWORD` | `spiritlhl136@136` | 登录密码（生产环境请务必修改） |
> | `SESSION_TTL_HOURS` | `24` | 登录会话有效期（小时） |
>
> 如需覆盖基础设施参数（自建 PG/Redis 等），可传入以下可选环境变量：
> `DB_HOST` `DB_PORT` `DB_USER` `DB_PASSWORD` `DB_NAME`
> `REDIS_ADDR` `SIDECAR_URL` `SERVER_PORT`

## API 端点

### 章节生成

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/projects/:id/chapters/generate` | 生成单章（加入任务队列） |
| POST | `/api/projects/:id/chapters/continue` | 续写下一章（同步调用） |
| POST | `/api/projects/:id/chapters/batch-generate` | 批量生成（按数量或按卷） |
| POST | `/api/projects/:id/agent/run` | 启动 LangGraph Agent 单章生成 |
| GET | `/api/agent/sessions/:sid/status` | 查询单章 Agent 任务状态 |
| GET | `/api/agent/sessions/:sid/stream` | SSE 流式进度（单章） |
| POST | `/api/projects/:id/agent/batch-run` | 启动 Agent 多章批量（以卷为单位，顺序生成） |
| GET | `/api/agent/batch/:bid/status` | 查询批量生成状态 |
| GET | `/api/agent/batch/:bid/stream` | SSE 流式进度（批量） |

### 知识图谱 / 向量库

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/projects/:id/graph/entities` | 知识图谱实体列表 |
| POST | `/api/projects/:id/graph/query` | Cypher 只读查询 |
| POST | `/api/projects/:id/graph/upsert` | 插入/更新图谱实体 |
| POST | `/api/projects/:id/graph/sync` | 同步 PG 数据 → Neo4j |
| POST | `/api/projects/:id/vector/rebuild` | 重建向量索引 |
| GET | `/api/projects/:id/vector/status` | 向量库统计 |
| POST | `/api/projects/:id/vector/search` | 向量语义检索 |

---

## 章节生成详细设计

### 单章节生成流程

```
前端 → POST /api/projects/:id/chapters/generate
         │
         ▼
    Go Handler (GenerateChapter)
    • 工作流检查 (CanGenerateNextChapter)
    • 写入 task_queue 表（任务类型: chapter_generate）
         │
         ▼
    Task Queue Worker (后台 goroutine)
    • 解析 chapter_num / context_hint
    • 调用 chapterService.Generate()
         │
         ▼
    ChapterService.Generate()
    • 组装 GenerateChapterRequest（含章节字数范围、提示）
    • 调用 Go 内部生成管道（非 LangGraph 路径，适合快速单章）
    • 写入 chapters 表（status: generated）
    • 触发 webhook （chapter_generated 事件）
```

### LangGraph Agent 单章生成流程（高质量路径）

```
前端 → POST /api/projects/:id/agent/run
         │
         ▼
    Go Handler (AgentRun)
    • 解析 AgentRunRequest
    • 自动注入默认 LLM Profile（从数据库读取，含解密 API Key）
    • 代理调用 Python sidecar POST /agent/run
         │
         ▼
    Python Agent Service
    • 创建后台 asyncio 任务
    • 初始化 AgentState（project_id, chapter_num, outline_hint 等）
    • 调用 LangGraph StateGraph.arun()
         │
         ▼ (LangGraph 节点执行顺序)
    1. planner_node
       • LLM 调用：将写作任务拆解为 3-6 个 plan_steps
       • 缺省计划：召回记忆 → 检索 → 组装上下文 → 生成 → 更新记忆 → 质量评估
    2. recall_memory_node (RecurrentGPT)
       • Redis：读取最近 5 段落（短期工作记忆）
       • graphiti + Neo4j：搜索与当前任务相关的长期事实
    3. parallel_retrieve_node（并行）
       ├─ retrieve_world_node（Neo4j）
       │   • 单次 Cypher 查询：角色核心设定 + 关系图
       │   • 不变规则（immutable_rules）
       │   • 活跃伏笔（foreshadowings.status = 'active'）
       └─ retrieve_narrative_node（Qdrant）
           • chapter_summaries 集合：最相关的 5 条章节摘要
           • style_samples 集合：参考书风格样本（top 3）
    4. assemble_context_node（Re³ Lost-in-Middle 排布）
       ┌──────────────────────────────┐ ← ANCHOR_TOP（最重要，模型注意力最强）
       │ 世界宪法不变规则              │
       │ 核心角色设定 + 关系图          │
       ├──────────────────────────────┤ ← MIDDLE（次要，LLM 易遗忘区）
       │ 近期 N 章摘要（Qdrant 检索）  │
       │ 风格样本（参考书片段）         │
       │ 活跃伏笔列表                  │
       │ 短期工作记忆（Redis 段落）    │
       ├──────────────────────────────┤ ← ANCHOR_BOTTOM（最重要）
       │ 当前章节 Outline / 提示       │
       │ 写作任务指令                  │
       └──────────────────────────────┘
    5. generator_node
       • 使用组装好的 prompt 调用 LLM（高 temperature: 0.85）
       • 同步生成章节摘要（低 temperature: 0.3）
    6. update_memory_node (RecurrentGPT 更新)
       • Redis：RPush 最新段落到短期记忆（保留最近 20 条）
       • graphiti：将章节摘要作为新 episode 写入 Neo4j
    7. quality_check_node
       • 启发式检查：字数 ≥ 500、无占位符、不含过多省略号
       • 未达阈值（< 0.6）且重试次数 < max_retries → 回退到 generate 节点
       • 通过 → final_text 输出，done = True
```

### 批量章节生成（Task Queue 路径）

批量生成通过 `POST /api/projects/:id/chapters/batch-generate` 触发，后端将每个章节
作为独立的任务写入 `task_queue` 表，由后台 Worker 依次执行。

```
请求体（按数量）:
{
  "count": 5,                          // 生成 5 章（从当前最大章节号顺序递增）
  "outline_hints": ["第1章简介", ...]   // 可选：每章的提示（按顺序对应）
}

请求体（按卷）:
{
  "volume_id": "<uuid>",               // 该卷下 chapter_start ~ chapter_end 全部生成
  "outline_hints": ["第1章简介", ...]   // 可选：每章提示（按章节顺序对应）
}
```

**按卷生成**：系统读取目标卷的 `chapter_start` / `chapter_end`，为该区间内
每一章编号生成一个 `chapter_generate` 任务（明确指定章节号），入队后由 Worker
按优先级顺序处理。每个任务执行时自动从路由配置中解析 LLM 凭证，不在 Payload 中
存储 API Key。

### Agent 批量生成（以卷为单位，顺序 LangGraph 路径）

通过 `POST /api/projects/:id/agent/batch-run` 触发，所有章节在 Python sidecar 内
**串行**运行完整 LangGraph pipeline，确保每章的记忆更新（Neo4j + Redis + Qdrant）
能够完整传递给下一章，保持叙事连贯性。

```
请求体:
{
  "chapter_nums": [1, 2, 3, 4, 5],    // 有序章节列表（卷的章节范围）
  "outline_hints": {                   // 可选：per-chapter 提示
    "1": "第一章：主角出场，交代背景",
    "2": "第二章：冲突初现"
  },
  "max_retries": 1
}

响应:
{
  "batch_id": "<uuid>",
  "status": "running",
  "total": 5
}

进度轮询:
GET /api/agent/batch/:bid/status
{
  "status": "running",
  "total": 5,
  "completed": 2,
  "current_chapter": 3,
  "chapters": {
    "1": {"status": "done", "final_text": "...", "quality_score": 0.92},
    "2": {"status": "done", ...},
    "3": {"status": "error", "error": "LLM timeout"}   // 单章失败不中断整体
  }
}

SSE 流:
GET /api/agent/batch/:bid/stream
data: {"completed":1,"total":5,"current_chapter":2,"status":"running"}
data: {"completed":2,"total":5,"current_chapter":3,"status":"running"}
data: {"status":"done","completed":5,"total":5,"chapters":{...}}
```

**串行设计原因**：
- 每章生成后通过 `update_memory_node` 将事件写入 Neo4j graphiti 图记忆
- Redis 短期记忆（最近 N 段落）在章节间累积，为下一章提供即时上下文
- Qdrant `chapter_summaries` 在每章结束后更新，保证语义检索的连贯性
- 并行生成会导致所有章节共享同一份「旧记忆」，破坏叙事一致性

---
