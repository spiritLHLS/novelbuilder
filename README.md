# NovelBuilder -- Re3 Agentic Novel Writing Platform

> AI 长篇小说创作平台：基于 LangGraph Agent + Graphiti 图记忆 + Qdrant 向量检索 + Re3 双轨上下文注入 + RecurrentGPT 记忆 + Lost-in-Middle 排布 + 33 维质量审计 + 番茄小说网自动上传

---

## 整体架构

```
+---------------------------------------------------------------------------+
|                        Single Docker Container                             |
|                                                                            |
|  +----------+  HTTP   +--------------+  HTTP   +------------------------+  |
|  |  Vue SPA |  -----> |  Go API GW   |  -----> |  Python Agent Service  |  |
|  |  :80     |         |  :8080       |         |  (LangGraph) :8081     |  |
|  +----------+         +------+-------+         +-----------+------------+  |
|                              |                              |              |
|                    PostgreSQL|16                   +---------+----------+   |
|                    + pgvector|                     |         |          |   |
|                    Redis 7   |                  Neo4j 5   Qdrant 1.x  LLM  |
|                    (CRUD /   |                  :7687     :6333    (cloud)  |
|                    TaskQueue)|                  (Graph    (Vector           |
|                              |                  Memory)   Search)          |
+---------------------------------------------------------------------------+
```

### 核心数据流

```
User Request
     |
     v
Go API Gateway (身份校验 / 参数验证 / 多模型路由)
     |
     v
LangGraph Agent (编排大脑)
     |
     +-> Planner Node         任务分解 -> plan_steps[]
     |
     +-> Recall Memory Node   graphiti -> Neo4j 图查询 (长期记忆)
     |
     +-> [并行] Graph Retrieval   Neo4j Cypher 结构化查询
     |       +  Vector Retrieval  Qdrant 语义相似检索
     |
     +-> Context Assembler    Re3 双轨 + Lost-in-Middle 排布
     |       +-- Track-1 World   世界观 / 角色核心 (Neo4j)
     |       +-- Track-2 Narrative 叙事连贯 / 上文摘要 (Qdrant)
     |       排布: [锚首:规则] [中部:检索] [锚尾:写作指令+outline]
     |
     +-> Generator Node       LLM 调用 -> draft (质量反馈注入)
     |
     +-> Update Memory Node   graphiti 提取实体 -> Neo4j 更新
     |       + RecurrentGPT   短期工作记忆写入 Redis
     |       + Qdrant 更新章节向量
     |
     +-> Quality Check Node   7 项检查 + 33 维度审计 -> 不合格则 re-generate
```

---

## 核心技术设计

### Re3 双轨上下文注入 (Dual-Track Context Injection)

| 轨道 | 内容 | 来源 |
|------|------|------|
| Track-1 世界知识轨 | 世界宪法、角色核心、世界观规则、活跃伏笔 | Neo4j 图数据库 |
| Track-2 叙事连贯轨 | 前N章摘要、情节弧线、风格样本、章节向量 | Qdrant 向量检索 + Redis |

### RecurrentGPT 记忆机制

- **短期记忆** -> Redis -> 当前工作记忆（最近 20 段落）
- **长期记忆** -> Neo4j -> graphiti 实体节点（事件/角色弧线/位置/物品）
- **摘要压缩** -> 每章结束后 LLM 生成章节摘要 -> 存进 graphiti

### Lost-in-Middle 排布

```
Prompt 结构:
+----------------------------------+  <- ANCHOR_TOP (最重要)
| 世界宪法不变规则                  |
| 核心角色设定 + 关系图             |
+----------------------------------+  <- MIDDLE (次要，LLM 易遗忘区)
| 近期章节摘要                      |
| 向量检索相似段落                  |
| 人物关系图谱                      |
| 活跃伏笔列表                     |
+----------------------------------+  <- ANCHOR_BOTTOM (最重要)
| 当前章节 Outline / 提示          |
| 写作风格指令 + 题材规则           |
| 质量反馈（重试时注入上次缺陷）    |
+----------------------------------+
```

---

## 功能清单

### 创作工坊

| 功能 | 说明 |
|------|------|
| 项目管理 | 创建/编辑/删除小说项目，每个项目独立数据空间 |
| 参考书管理 | 上传/URL导入/在线搜索80+小说站，支持断点续爬 |
| 参考书深度分析 | 分块后台解析参考书（风格/人物/情节/氛围四层分析器） |
| 知识库(RAG) | 基于 Qdrant 的向量知识库，支持重建/语义检索 |
| 世界观设定 | 世界宪法(不可变规则) + 世界百科 + 导入导出 |
| 角色管理 | 角色CRUD + 力导向关系图（Cytoscape.js）+ 拖拽/缩放/高亮 |
| 大纲编辑 | 分层大纲（章/节/场景） |
| 伏笔管理 | 伏笔创建/状态跟踪（active/resolved/abandoned） |
| 术语表 | 项目专属术语对照（避免AI生成同义替换） |
| 资源账本 | InkOS 风格资源追踪（粒子账本） |
| 数据分析 | 项目维度统计仪表盘 |
| 图谱记忆 | Neo4j 知识图谱可视化（实体/关系/事件浏览） |

### 生成管线

| 功能 | 说明 |
|------|------|
| 整书蓝图 | 全书结构自动生成（含卷划分/章节大纲） |
| 章节管理 | 单章生成/续写/重新生成/批量生成/章节详情编辑 |
| 工作流控制台 | 蓝图/卷/章节审批流（草稿->待审->通过/拒绝） |
| 质量检测 | 7 项启发式检查 + 33维度专业审计 + 可视化报告 |
| 多智能体评审 | 多角度 AI 评审（情节/角色/文笔/结构） |
| 变更传播 | 修改角色设定后自动生成补丁计划，逐章修复受影响内容 |
| 任务队列 | 后台异步任务管理（生成/重建/分析等），支持取消/重试 |

### 创作工具

| 功能 | 说明 |
|------|------|
| 创作简报 | 一句话灵感 -> 自动展开为完整项目骨架 |
| 导入续写 | 从已有作品导入章节，AI 分析后续写 |
| 子情节管理 | 子线索追踪（创建/检查点/状态管理） |
| 情绪弧线 | 按章节绘制情绪走向曲线 |
| 角色关系矩阵 | 角色间互动强度热力图 |
| 雷达分析 | 多平台市场扫描（起点/晋江/番茄/七猫/Webnovel） |
| 番茄上传 | Playwright 浏览器自动化，自动将章节上传到番茄小说网 |

### 审计与优化

| 功能 | 说明 |
|------|------|
| 33维度审计 | 情节/角色/文笔/结构/节奏/世界观等全方位质量评分 |
| 审计修订 | 根据审计报告自动修订章节（保留原意，修正缺陷） |
| 去AI味(Anti-Detect) | 拟人化改写管线（降低AI生成痕迹检测率） |
| 快照与回滚 | 章节历史版本快照，支持一键回滚 |
| 词汇疲劳 | 项目级用词频率统计（避免重复用词） |

### 系统管理

| 功能 | 说明 |
|------|------|
| AI 模型配置 | 多 LLM Profile 管理（API Key 加密存储），支持测试连接 |
| 多模型路由 | 按 Agent 类型分配不同模型（全局/项目级） |
| 提示词预设 | 可复用提示词模板（全局/项目级） |
| 系统设置 | 质量阈值/字数范围/功能开关等运行时参数 |
| 题材规则 | 按题材(玄幻/都市/科幻等)定义专属写作规则/审计维度 |
| 系统日志 | 六大服务运行日志实时查看 |
| Webhook 通知 | 关键事件（章节生成/审批通过等）推送到外部 URL |
| 自动写作守护 | 开启后按设定间隔自动生成下一章 |
| 运行诊断 | /api/doctor 端点检查全部后端服务健康状态 |

---

## 技术栈

| 层 | 技术 | 用途 |
|----|------|------|
| Agent 编排 | LangGraph 0.2.x | 多步推理状态机 |
| 图记忆 | Zep Graphiti-Core | 实体/关系/事件记忆提取 |
| 图数据库 | Neo4j 5.x Community | 知识图谱存储 |
| 向量数据库 | Qdrant 1.12 | 语义向量检索 |
| 关系数据库 | PostgreSQL 16 + pgvector | 结构化业务数据 + 向量扩展 |
| 缓存/队列 | Redis 7 | 短期记忆 / 任务队列 |
| AI 网关 | Go 1.22 (Gin) | 多模型路由 / API Key加密存储 / 静态文件托管 |
| Python 服务 | FastAPI 0.115 + LangGraph | Agent 逻辑 + 分析管线 + 浏览器自动化 |
| 前端 | Vue 3.4 + Vite 5 + Element Plus | SPA 交互界面 |
| 图可视化 | Cytoscape.js 3.28 | 角色关系图 / 知识图谱 |
| 浏览器自动化 | Playwright | 番茄小说网自动上传 |
| 小说爬取 | novel-downloader (子模块) | 80+ 站点小说搜索/下载 |

---

## 项目结构

```
novelbuilder/
+-- backend/                          # Go API 网关
|   +-- cmd/server/main.go            # 入口（含自动写作守护进程）
|   +-- internal/
|       +-- config/                    # 配置加载
|       +-- crypto/                    # API Key AES 加密
|       +-- database/                  # PostgreSQL + Redis 连接池
|       +-- gateway/                   # 网关入口
|       +-- handlers/                  # Gin HTTP 处理器
|       |   +-- handlers.go            # 路由注册（50+ Handler）
|       |   +-- handler_agent.go       # Agent/批量生成
|       |   +-- handler_auth.go        # 登录认证
|       |   +-- handler_chapters.go    # 章节生成/审批
|       |   +-- handler_characters.go  # 角色管理
|       |   +-- handler_fanqie.go      # 番茄小说上传代理
|       |   +-- handler_projects.go    # 项目CRUD
|       |   +-- handler_references.go  # 参考书管理
|       |   +-- handler_system.go      # 系统设置/日志/诊断
|       |   +-- handler_workflow.go    # 工作流审批
|       |   +-- handler_audit.go       # 33维审计/快照
|       +-- middleware/                # JWT认证/请求ID
|       +-- models/                    # 请求/响应数据模型
|       +-- retry/                     # 指数退避重试
|       +-- services/                  # 30+ 业务服务层
|       +-- workflow/                  # 审批状态机
+-- python-sidecar/
|   +-- main.py                        # FastAPI应用（Agent/图谱/向量端点）
|   +-- routes_analysis.py             # 风格分析/拟人化/指标评估
|   +-- routes_audit.py                # 33维审计/去AI味/创作简报/导入分析
|   +-- routes_deep_analysis.py        # 参考书深度分块分析
|   +-- routes_novels.py               # 小说搜索/下载（调用 novel-downloader）
|   +-- routes_fanqie.py               # 番茄小说 Playwright 自动上传
|   +-- json_repair.py                 # LLM 输出 JSON 修复
|   +-- llm_utils.py                   # LLM 调用封装
|   +-- agent/                         # LangGraph Agent
|   |   +-- state.py                   # AgentState TypedDict
|   |   +-- graph.py                   # StateGraph 定义 + 编译
|   |   +-- nodes/                     # 各节点实现
|   |       +-- planner.py             # 任务分解
|   |       +-- recall.py              # 记忆召回
|   |       +-- retriever.py           # 双轨检索（Neo4j + Qdrant）
|   |       +-- assembler.py           # Re3 Lost-in-Middle 上下文排布
|   |       +-- generator.py           # LLM 生成（含质量反馈注入）
|   |       +-- memory_updater.py      # 记忆更新（Neo4j + Redis + Qdrant）
|   |       +-- quality.py             # 质量评估
|   +-- analyzers/                     # 参考书四层分析器
|   |   +-- style_analyzer.py          # 文风分析
|   |   +-- narrative_analyzer.py      # 叙事结构
|   |   +-- plot_extractor.py          # 情节提取
|   |   +-- atmosphere_analyzer.py     # 氛围分析
|   +-- humanizer/                     # 拟人化管线
|   |   +-- pipeline.py                # 去AI味处理流程
|   |   +-- metrics.py                 # 拟人化指标评估
|   +-- graph_store/                   # Neo4j 直接操作
|   +-- vector_store/                  # Qdrant 操作
|   +-- novel-downloader/              # 小说下载库 (git submodule)
+-- frontend/
|   +-- src/
|       +-- api/index.ts               # API 客户端（全部端点封装）
|       +-- router/index.ts            # 前端路由（38 个页面）
|       +-- stores/                    # Pinia 状态管理
|       +-- views/                     # 38 个功能页面
|           +-- ProjectList.vue        # 项目仪表盘
|           +-- Studio.vue             # 创作工作台
|           +-- Characters.vue         # 角色管理 + 关系图
|           +-- Chapters.vue           # 章节列表
|           +-- ChapterDetail.vue      # 章节编辑器
|           +-- AuditReport.vue        # 33维审计报告
|           +-- AntiDetect.vue         # 去AI味
|           +-- FanqieUpload.vue       # 番茄小说上传
|           +-- ...                    # 其余功能页面
+-- migrations/                        # PostgreSQL DDL (按序执行)
|   +-- 001_core_entities.sql          # 项目/角色/章节/世界观等核心表
|   +-- 002_blueprint_chapters.sql     # 蓝图/卷/大纲
|   +-- 003_workflow.sql               # 审批工作流
|   +-- 004_analysis_vector.sql        # 分析/向量相关表
|   +-- 005_indexes_triggers.sql       # 索引与触发器
|   +-- 006_ai_llm.sql                # LLM Profile / Agent路由
|   +-- 007_propagation_tasks.sql      # 变更传播 / 任务队列
|   +-- 008_content_tools.sql          # 审计/去AI味/简报/导入等
|   +-- 009_resources_infra.sql        # 资源账本/Webhook/书籍规则
|   +-- 010_subplot_analytics.sql      # 子情节/情绪弧线/雷达/互动矩阵
|   +-- 012_reference_chapters.sql     # 参考书章节管理
|   +-- 013_seed_genre_templates.sql   # 题材规则种子数据
|   +-- 014_deep_analysis.sql          # 深度分析任务/结果
|   +-- 015_fanqie_upload.sql          # 番茄小说上传账号/记录
+-- configs/config.yaml                # 默认配置（可被环境变量覆盖）
+-- docker/
|   +-- docker-entrypoint.sh           # 容器启动脚本（初始化全部服务）
|   +-- supervisord.conf               # 六服务进程管理
|   +-- wait-for-pg.sh                 # PostgreSQL 就绪检测
|   +-- wait-for-port.sh               # TCP 端口就绪检测
+-- Dockerfile                         # 多阶段构建，单容器全组件
+-- docker-compose.yml                 # Compose 编排（推荐方式）
```

---

## 启动

### 完全重置（删除旧数据）

```bash
docker rm -f nb
docker volume rm novelbuilder-pg novelbuilder-qdrant novelbuilder-neo4j
```

### 构建并运行

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
  -e ADMIN_USERNAME=admin \
  -e ADMIN_PASSWORD=your_password_here \
  novelbuilder
docker logs -f nb
```

> 三个 `-v` 数据卷缺一不可:
> - `novelbuilder-pg` -- 关系型数据库（项目、角色、章节等所有业务数据）
> - `novelbuilder-qdrant` -- 向量索引（知识库重建结果，缺少会导致重建后的索引在容器重启后丢失）
> - `novelbuilder-neo4j` -- 知识图谱（Agent 长期记忆）
>
> 使用具名卷（named volume）确保数据在容器重建后仍然保留。

或者使用 Docker Compose（推荐，卷由 Compose 统一管理）：

```bash
docker compose up -d
docker compose logs -f
```

打开 http://localhost:8080 ，进入 **设置 - AI 模型配置** 添加 LLM Profile（填写 API Key），
再进入 **设置 - 系统设置** 调整质量阈值等参数。无需任何额外环境变量。

### 环境变量

加密密钥由系统在首次启动时自动生成并存入数据库，无需手动指定。
AI API Key 通过前端 设置 - AI 模型配置 页面配置，加密存储在数据库中。

| 环境变量 | 默认值 | 说明 |
|----------|--------|------|
| `ADMIN_USERNAME` | `admin` | 登录用户名 |
| `ADMIN_PASSWORD` | `admin` | 登录密码（生产环境请务必修改） |
| `SESSION_TTL_HOURS` | `24` | 登录会话有效期（小时） |

如需覆盖基础设施参数（自建 PG/Redis 等），可传入以下可选环境变量：

| 环境变量 | 说明 |
|----------|------|
| `DB_HOST` `DB_PORT` `DB_USER` `DB_PASSWORD` `DB_NAME` | PostgreSQL 连接参数 |
| `REDIS_ADDR` | Redis 地址（默认 127.0.0.1:6379） |
| `SIDECAR_URL` | Python sidecar 地址（默认 http://127.0.0.1:8081） |
| `SERVER_PORT` | Go 后端监听端口（默认 8080） |

---

## API 端点概览

以下列出主要端点分组，完整路由定义见 `backend/internal/handlers/handlers.go`。

### 项目管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET/POST | `/api/projects` | 项目列表 / 创建 |
| GET/PUT/DELETE | `/api/projects/:id` | 项目详情 / 更新 / 删除 |

### 章节生成

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/projects/:id/chapters/generate` | 生成单章（加入任务队列） |
| POST | `/api/projects/:id/chapters/continue` | 续写下一章 |
| POST | `/api/projects/:id/chapters/batch-generate` | 批量生成（按数量或按卷） |
| POST | `/api/projects/:id/agent/run` | 启动 LangGraph Agent 单章生成 |
| GET | `/api/agent/sessions/:sid/stream` | SSE 流式进度（单章） |
| POST | `/api/projects/:id/agent/batch-run` | Agent 多章批量（串行，保持记忆连贯） |
| GET | `/api/agent/batch/:bid/stream` | SSE 流式进度（批量） |

### 知识图谱 / 向量库

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/projects/:id/graph/entities` | 知识图谱实体列表 |
| POST | `/api/projects/:id/graph/query` | Cypher 只读查询 |
| POST | `/api/projects/:id/graph/upsert` | 插入/更新图谱实体 |
| POST | `/api/projects/:id/graph/sync` | 同步 PG 数据 -> Neo4j |
| POST | `/api/projects/:id/vector/rebuild` | 重建向量索引 |
| POST | `/api/projects/:id/vector/search` | 向量语义检索 |

### 质量与审计

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/chapters/:id/quality-check` | 7 项启发式质量检查 |
| POST | `/api/chapters/:id/audit` | 33 维度专业审计 |
| POST | `/api/chapters/:id/audit-revise` | 根据审计报告自动修订 |
| POST | `/api/chapters/:id/anti-detect` | 去AI味改写 |
| GET | `/api/chapters/:id/snapshots` | 章节历史快照列表 |
| POST | `/api/chapters/:id/restore` | 回滚到指定快照 |

### 参考书

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/projects/:id/references/search` | 在线搜索小说（80+站点） |
| POST | `/api/projects/:id/references/fetch-import` | 下载并导入参考书 |
| POST | `/api/references/:id/deep-analyze` | 启动深度分析（后台分块） |
| GET | `/api/references/:id/deep-analyze/job` | 查询深度分析进度 |

### 番茄小说上传

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/projects/:id/fanqie/account` | 获取番茄账号配置 |
| POST | `/api/projects/:id/fanqie/configure` | 保存番茄账号(Cookie+作品ID) |
| POST | `/api/projects/:id/fanqie/validate` | 验证 Cookie 有效性 |
| POST | `/api/projects/:id/fanqie/books` | 获取番茄作品列表 |
| POST | `/api/projects/:id/fanqie/upload/:chapter_id` | 上传单章到番茄 |
| POST | `/api/projects/:id/fanqie/batch-upload` | 批量上传多章 |
| GET | `/api/projects/:id/fanqie/uploads` | 上传记录与状态 |

### 系统

| 方法 | 路径 | 说明 |
|------|------|------|
| GET/PUT/DELETE | `/api/settings/:key` | 系统设置读写 |
| GET/POST/PUT/DELETE | `/api/llm-profiles` | LLM 模型配置CRUD |
| GET/PUT/DELETE | `/api/agent-routes/:agent_type` | Agent模型路由 |
| GET | `/api/doctor` | 全服务健康诊断 |
| GET | `/api/logs` | 服务运行日志 |
| GET | `/api/health` | 健康检查 |

---

## 章节生成详细设计

### Task Queue 路径（快速单章/批量）

```
前端 -> POST /api/projects/:id/chapters/generate
         |
         v
    Go Handler (GenerateChapter)
    - 工作流检查 (CanGenerateNextChapter)
    - 写入 task_queue 表（任务类型: chapter_generate）
         |
         v
    Task Queue Worker (后台 goroutine)
    - 解析 chapter_num / context_hint
    - 调用 chapterService.Generate()
    - 写入 chapters 表（status: generated）
    - 触发 webhook（chapter_generated 事件）
```

### LangGraph Agent 路径（高质量单章）

```
前端 -> POST /api/projects/:id/agent/run
         |
         v
    Go Handler (AgentRun)
    - 解析 AgentRunRequest
    - 自动注入 LLM Profile（从数据库读取，含解密 API Key）
    - 多模型路由：按 agent_type 选择专属模型或默认模型
    - 代理调用 Python sidecar POST /agent/run
         |
         v
    Python Agent Service
    - 创建后台 asyncio 任务
    - 初始化 AgentState
    - 调用 LangGraph StateGraph.arun()
         |
         v (LangGraph 节点执行顺序)
    1. planner_node       任务分解为 3-6 个 plan_steps
    2. recall_memory_node  Redis 短期记忆 + Neo4j graphiti 长期记忆
    3. parallel_retrieve   [Neo4j 角色/关系/伏笔] || [Qdrant 摘要/风格样本]
    4. assemble_context    Re3 Lost-in-Middle 排布（含题材规则/书籍规则）
    5. generator_node      LLM 调用 + 质量反馈注入（重试时）
    6. update_memory_node  Neo4j 实体更新 + Redis 段落追加 + Qdrant 章节向量
    7. quality_check_node  7 项检查 -> 不合格且重试次数未满 -> 回退到 generator
```

### Agent 批量路径（串行，保持记忆连贯）

通过 `POST /api/projects/:id/agent/batch-run` 触发。所有章节在 Python sidecar 内
串行运行完整 LangGraph pipeline，确保每章的记忆更新（Neo4j + Redis + Qdrant）
完整传递给下一章。

```
请求体:
{
  "chapter_nums": [1, 2, 3, 4, 5],
  "outline_hints": {"1": "主角出场", "2": "冲突初现"},
  "max_retries": 1
}

SSE 流:
GET /api/agent/batch/:bid/stream
data: {"completed":1,"total":5,"current_chapter":2,"status":"running"}
data: {"completed":2,"total":5,"current_chapter":3,"status":"running"}
data: {"status":"done","completed":5,"total":5,"chapters":{...}}
```

串行设计原因：每章生成后通过 update_memory_node 将事件写入 Neo4j 图记忆，Redis
短期记忆在章节间累积，Qdrant chapter_summaries 在每章结束后更新。并行生成会导致
所有章节共享同一份旧记忆，破坏叙事一致性。

---

## 番茄小说上传

基于 Playwright 浏览器自动化实现，运行在 Python sidecar 中。

### 使用流程

1. 在浏览器中登录番茄小说作者后台 (https://fanqienovel.com/writer/home)
2. 按 F12 打开开发者工具，在控制台输入 `document.cookie` 复制结果
3. 在 NovelBuilder 的 "番茄上传" 页面粘贴 Cookie 并保存
4. 选择目标作品（可通过"获取作品列表"自动获取）
5. 勾选要上传的章节，点击"批量上传"

### 技术实现

- Playwright Chromium 浏览器在 Docker 容器内 headless 运行
- Cookie 注入到浏览器上下文，自动导航到作者后台
- 通过 DOM 交互完成章节标题/正文填写和保存操作
- 每章上传间隔 3 秒避免平台限流
- 上传结果以截图形式反馈给前端，确认操作成功
- 上传状态持久化到 PostgreSQL，支持断点续传

---

## 容器内服务

单容器通过 Supervisor 管理六个服务进程：

| 服务 | 端口 | 优先级 | 说明 |
|------|------|--------|------|
| PostgreSQL 16 | 5432 | 10 | 关系数据库 + pgvector |
| Neo4j 5 CE | 7687 | 15 | 知识图谱 |
| Redis 7 | 6379 | 20 | 短期记忆 / 队列 |
| Qdrant 1.12 | 6333 | 25 | 向量检索 |
| Python sidecar | 8081 | 30 | Agent + 分析 + 上传 |
| Go backend | 8080 | 40 | API 网关 + 静态文件 |

容器启动时自动执行以下初始化：
- PostgreSQL initdb + 用户创建 + 全部迁移脚本执行
- Neo4j 初始密码设置
- Qdrant/Redis 存储目录创建
- 然后由 Supervisor 拉起全部服务

---
