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
docker volume rm novelbuilder-data
docker rm -f nb
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
  -v novelbuilder-data:/var/lib/postgresql/data \
  novelbuilder
docker logs -f nb
```

打开 http://localhost:8080，进入 **设置 → AI 模型配置** 添加 LLM Profile（填写 API Key），
再进入 **设置 → 系统设置** 调整质量阈值等参数。无需任何环境变量。

> **加密密钥** 由系统在首次启动时自动生成并存入数据库，无需手动指定 `ENCRYPTION_KEY`。
> **AI API Key** 通过前端 Settings → AI 模型配置 页面配置，加密存储在数据库中。
>
> 如需覆盖基础设施参数（自建 PG/Redis 等），可传入以下可选环境变量：
> `DB_HOST` `DB_PORT` `DB_USER` `DB_PASSWORD` `DB_NAME`
> `REDIS_ADDR` `SIDECAR_URL` `SERVER_PORT`

## API 端点（新增）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/projects/:id/agent/run` | 启动 Agent 生成任务 |
| GET | `/api/agent/sessions/:sid/status` | 查询任务状态 |
| GET | `/api/agent/sessions/:sid/stream` | SSE 流式进度 |
| GET | `/api/projects/:id/graph/entities` | 知识图谱实体 |
| POST | `/api/projects/:id/graph/query` | Cypher 查询 |
| POST | `/api/projects/:id/vector/rebuild` | 重建向量索引 |
| GET | `/api/projects/:id/vector/status` | 向量库统计 |
