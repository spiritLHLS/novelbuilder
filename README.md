# NovelBuilder

> 基于 AI agent 的 AI 长篇小说创作平台

---

## 目录

- [整体架构](#整体架构)
- [核心技术设计](#核心技术设计)
- [功能清单](#功能清单)
- [技术栈](#技术栈)
- [项目结构](#项目结构)
- [启动](#启动)
- [系统分层架构](#系统分层架构)
- [核心业务流程](#核心业务流程)
- [质量保障体系](#质量保障体系)
- [容器内服务拓扑](#容器内服务拓扑)

---

## 整体架构

<details>
<summary>展开查看架构图与核心数据流</summary>

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

</details>

---

## 核心技术设计

<details>
<summary>展开查看 Re3 双轨、RecurrentGPT 记忆机制、Lost-in-Middle 排布</summary>

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

</details>

---

## 功能清单

<details>
<summary>展开查看全部功能（创作工坊 / 生成管线 / 创作工具 / 审计优化 / 系统管理）</summary>

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

</details>

---

## 技术栈

<details>
<summary>展开查看完整技术栈</summary>

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

</details>

---

## 项目结构

<details>
<summary>展开查看完整目录树</summary>

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

</details>

---

## 启动

### 完全重置（删除旧数据）

```bash
docker rm -f nb
docker volume rm novelbuilder-pg novelbuilder-qdrant novelbuilder-neo4j
```

### 使用预构建镜像运行（推荐）

```bash
docker run -d \
  --name nb \
  -p 8080:8080 \
  -v novelbuilder-pg:/var/lib/postgresql/data \
  -v novelbuilder-qdrant:/var/lib/qdrant \
  -v novelbuilder-neo4j:/opt/neo4j/data \
  -e ADMIN_USERNAME=admin \
  -e ADMIN_PASSWORD=your_password_here \
  ghcr.io/spiritlhls/novelbuilder:latest
```

或使用 Docker Hub 镜像：

```bash
docker run -d \
  --name nb \
  -p 8080:8080 \
  -v novelbuilder-pg:/var/lib/postgresql/data \
  -v novelbuilder-qdrant:/var/lib/qdrant \
  -v novelbuilder-neo4j:/opt/neo4j/data \
  -e ADMIN_USERNAME=admin \
  -e ADMIN_PASSWORD=your_password_here \
  spiritlhl/novelbuilder:latest
```

### 本地构建并运行

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

<details>
<summary>可选：覆盖基础设施连接参数</summary>

如需覆盖基础设施参数（自建 PG/Redis 等），可传入以下可选环境变量：

| 环境变量 | 说明 |
|----------|------|
| `DB_HOST` `DB_PORT` `DB_USER` `DB_PASSWORD` `DB_NAME` | PostgreSQL 连接参数 |
| `REDIS_ADDR` | Redis 地址（默认 127.0.0.1:6379） |
| `SIDECAR_URL` | Python sidecar 地址（默认 http://127.0.0.1:8081） |
| `SERVER_PORT` | Go 后端监听端口（默认 8080） |

</details>

---

## 系统分层架构

<details>
<summary>展开查看各层详细说明（Frontend / Go Gateway / Go Services / Python Agent / 数据持久化）</summary>

### 表现层（Frontend）

**Vue 3 + Vite + Element Plus + Tailwind CSS**

- 38 个功能页面（项目管理、创作工坊、数据展示、系统配置等）
- 4 个 Pinia 全局状态仓库（用户认证、项目、工作流、任务队列）
- API 客户端层（150+ 端点统一封装，支持 SSE 流式监听、多模型路由、错误重试）
- 实时交互组件（Cytoscape.js 角色关系图、ECharts 数据可视化、Monaco 编辑器）

**关键依赖**：
- `axios` + 自定义拦截器（自动 JWT 注入、请求去重、全局错误处理）
- `event-source-polyfill`（SSE 流式推送，用于长任务进度上报）
- `pinia`（状态共享，避免 prop drilling）

---

### 网关层（Go API Gateway）

**Gin HTTP 框架 + 50+ Handler + 多模型路由**

**核心职责**：
1. **身份认证和授权**
   - JWT 验证（来自 Authorization 头或查询参数（SSE））
   - Redis Session 存储与过期管理
   - 请求 ID 链路追踪

2. **参数认证和访问控制**
   - 工作流权限检查（草稿→待审→通过/拒绝）
   - 资源所有权验证（project_id 校验）
   - 请求签名校验（Webhook 推送）

3. **多模型路由和 LLM Profile 管理**
   - 加密存储 API Key（AES-256-GCM）
   - 运行时路由决策（按 agent_type 或默认 profile 选择 LLM）
   - 模型故障转移（API 错误时自动切换备用模型）

4. **业务逻辑编排**
   - Task Queue 任务分发（章节生成/分析/上传等异步任务）
   - PostgreSQL 事务管理（强一致性 ACID 操作）
   - Redis 操作指挥（缓存、短期记忆、任务队列）

5. **反向代理和文件服务**
   - 向 Python sidecar 转发 Agent/分析 请求
   - 静态文件托管（前端 SPA dist）

**关键依赖**：
- `jackc/pgx/v5`（PostgreSQL 驱动，连接池）
- `redis/go-redis/v9`（Redis 客户端）
- `go.uber.org/zap`（结构化日志）

---

### 业务服务层（Go Services）

**30+ 服务模块**，围绕"项目 → 大纲 → 章节 → 质量"的工程流程构建

| 服务模块 | 职责 | 依赖 |
|---------|------|------|
| **Project Service** | 项目 CRUD、元数据管理、配置隔离 | PostgreSQL |
| **Chapter Service** | 章节 CRUD、生成、续写、相似度检测、摘要生成 | PostgreSQL + Redis + Qdrant |
| **Chapter Build Service** | 系统提示词构建（Lost-in-Middle）、反复生成控制 | PostgreSQL + Redis + Neo4j + Qdrant |
| **Blueprint Service** | 整书蓝图生成、卷划分、自动伏笔时序分配 | PostgreSQL + LLM Gateway |
| **Outline Service** | 大纲管理、分层编辑、事件跟踪 | PostgreSQL |
| **Character Service** | 角色 CRUD、状态追踪、关系图谱维护 | PostgreSQL |
| **World Service** | 世界宪法、设定管理、伏笔CRUD与验证 | PostgreSQL |
| **Foreshadowing Service** | 伏笔时序验证、auto-tracking (planted → resolved) | PostgreSQL |
| **Workflow Service** | 审批状态机、工作流权限检查、变更传播 | PostgreSQL |
| **Audit Service** | 33 维度审计、快照管理、拟人化改写编排 | PostgreSQL + Python sidecar |
| **RAG Service** | Qdrant 向量操作（检索、存储、重建） | Qdrant + Python sidecar |
| **Reference Service** | 参考书 URL/离线导入、深度分析任务分发 | PostgreSQL + novel-downloader |
| **Task Queue Service** | 异步任务分发、重试机制、进度追踪 | PostgreSQL + Redis |
| **Webhook Service** | 事件注册、触发、推送、重试 | PostgreSQL + HTTP |
| **LLM Gateway** | 多 API 端点适配、速率限制、故障转移 | Redis（rate limit tokens） |
| **Graphiti Service** | Neo4j 图谱更新、实体提取、关系维护 | Neo4j |

**共享基础设施**：
- PostgreSQL 连接池（pgxpool）
- Redis 单机客户端（go-redis）
- HTTP 客户端（带重试和超时）

---

### Python Agent Service

**FastAPI + LangGraph + 分析管线**

**核心组件**：

1. **LangGraph Agent** — 8 节点状态机
   - `planner` → `recall_memory` → `parallel_retrieve` → `assemble_context` → `generator` → `update_memory` → `quality_check` → （条件重试或结束）
   - 输入：Project、Chapter、LLM Config、质量约束
   - 输出：生成的章节内容 + 更新后的 agentstate（含记忆增量）

2. **分析管线**
   - **Reference Analyzer**：参考书分块、四层分析（风格/叙事/情节/氛围）
   - **Style Analyzer**：文风特征提取、用词、句式、节奏分析
   - **Anti-Detect Pipeline**：9 步拟人化处理（去 AI 味）
   - **Audit Engine**：33 维度启发式+LLM 评估引擎、评分聚合

3. **浏览器自动化**
   - **Playwright Headless**：番茄小说网自动登录、章节上传、结果截图

4. **工具库**
   - `llm_utils.build_llm()`：LLM 实例工厂（支持 OpenAI/Anthropic/Gemini）
   - `json_repair()`：LLM 输出 JSON 修复（容错）
   - `novel-downloader`（git 子模块）：小说站搜索/下载

**关键依赖**：
- `LangGraph 0.2.x`（状态图编制）
- `langchain + langchain-openai/anthropic/google-genai`（LLM 调用）
- `Zep Graphiti-Core`（实体/关系/事件记忆提取）
- `Playwright`（浏览器自动化）
- `Qdrant Python SDK`（向量操作）

**内部通信**：
- Go ← → Python：HTTP JSON（同步）
- Go → Python SSE：流式推送（进度/日志）

---

### 数据持久化层

| 组件 | 用途 | 特性 |
|------|------|------|
| **PostgreSQL 16** | 业务数据（项目、角色、章节、审批、任务） + pgvector 扩展 | ACID 强一致性、复杂查询、事务支持 |
| **Redis 7** | 短期记忆（RecurrentGPT 工作窗口）、Task Queue、Session、Rate Limit tokens | 高速读写、过期自清理 |
| **Neo4j 5 Community** | 知识图谱（实体/关系/事件、graphiti 长期记忆） | 图查询、实体关系推导、ACID |
| **Qdrant 1.12** | 向量索引（章节摘要、参考书风格、感官样本） | 向量检索、过滤、HNSW 算法 |

**数据同步机制**：
- PostgreSQL ← → Neo4j：通过 `GraphService.SyncPGDataToNeo4j()`（按需触发）
- PostgreSQL ← → Qdrant：通过 `RAGService`（章节生成后异步上传）
- PostgreSQL ← → Redis：通过 Task Queue（TTL 自动过期）

</details>

---

## 核心业务流程

<details>
<summary>展开查看章节生成路径</summary>

### 章节生成编排

**标准路径**（Task Queue，所有生成场景均走此路径）：
```
请求 → Go Handler → 工作流权限检查 → 写 task_queue 表 → 返回任务 ID (202)
      ↓
后台 Worker → GenerateChapterWithQualityRetries() → 调用 LLM Gateway
           → 质量门（最多 3 次重试）→ 人性化处理
           → 生成摘要/标题 → 写 chapters 表
           → 异步：上传 Qdrant + Neo4j，章节状态结算（角色/伏笔）
           → 触发 Webhook
```

> 单章（`/chapters/generate`、`/chapters/continue`、`/chapters/:id/regenerate`）
> 和批量（`/chapters/batch-generate`）均入同一 Task Queue，Workers 并发消费。

**可选交互式路径**（Python LangGraph Agent，独立于章节生成主路径）：
```
请求 → Go Handler → 查询 LLM Profile → 解密 API Key → 代理到 Python sidecar
      ↓
Python Agent（8 节点 StateGraph 串行执行）
           → 每步输出通过 SSE 推回前端
           → 最后写回 Neo4j / Redis / Qdrant
           → 返回 session_id；前端通过 SSE 拉取最终内容
```

> 该路径为**可选**的人机交互式写作辅助，不替代标准任务队列路径。

</details>

---

## 质量保障体系

<details>
<summary>展开查看双层检查与反馈回路</summary>

**双层检查**：

1. **启发式检查（7 项，Go 端）** — 快速、无 LLM 成本
   - 字数、片段数、大纲覆盖率、词重复度等

2. **33 维度审计（Python 端）** — 精细分析
   - **情节维度**（6 项）：outline deviation、conflict escalation、tension management 等
   - **角色维度**（5 项）：character memory、character motivation、relationship dynamics 等
   - **文笔维度**（8 项）：dialogue naturalness、scene description、sensory detail 等
   - **结构维度**（7 项）：narrative pace、emotional arc、hook strength、chapter length 等
   - **风格维度**（4 项）：ai pattern detection、cliche density、sentence rhythm、vocabulary richness 等
   - **综合维度**（3 项）：grammar、readability、genre compliance

**反馈回路**：
```
生成 draft → 启发式检查 → 33 维审计 → 不合格 ?
           ↓ yes               ↓ no
        改写指令 → Agent 重试（max 3 轮）  跳过
```

</details>

---

## 容器内服务拓扑

<details>
<summary>展开查看服务启动顺序与依赖矩阵</summary>

**单容器多进程架构** — 通过 Supervisor 编排

### 服务启动依赖关系

```
容器启动
      ↓
docker-entrypoint.sh 初始化阶段
      │
      ├─→ PostgreSQL initdb → 迁移脚本执行 → 种子数据加载
      │   （关系数据库就绪）
      │
      ├─→ Neo4j 初始化密码
      │   （知识图谱就绪）
      │
      ├─→ Qdrant 目录创建
      │   （向量数据库就绪）
      │
      └─→ Redis 目录创建
          （缓存/队列就绪）
      
      ↓（全部基础设施准备完毕）
      
Supervisor 启动六大服务（按优先级）
      │
      ├─→ [10] PostgreSQL + pgvector     （关系数据库层）
      │
      ├─→ [15] Neo4j CE                  （图数据库层）
      │
      ├─→ [20] Redis                     （缓存/队列层）
      │
      ├─→ [25] Qdrant                    （向量检索层）
      │
      ├─→ [30] Python sidecar FastAPI    （Agent + 分析层）
      │         └─ LangGraph Agent（8 节点）
      │         └─ 分析管线（参考书/风格/审计/上传）
      │
      └─→ [40] Go backend (Gin)          （网关层）
          └─ 50+ Handler
          └─ PostgreSQL / Redis / 代理 Python 的所有业务逻辑
```

### 服务间依赖矩阵

| 依赖者 | 依赖的服务 | 原因 |
|--------|-----------|------|
| Go backend | PostgreSQL | 业务数据 CRUD、事务 |
| Go backend | Redis | Session、Task Queue、缓存、RecurrentGPT 短期记忆 |
| Go backend | Python sidecar | LangGraph Agent、参考书分析、审计引擎、番茄上传代理 |
| Python sidecar | PostgreSQL | 查询项目/角色/大纲等业务上下文（参考化） |
| Python sidecar | Redis | RecurrentGPT 记忆（短期窗口）、Rate Limit tokens |
| Python sidecar | Neo4j | Graphiti 长期记忆、知识图谱查询 |
| Python sidecar | Qdrant | 向量检索（参考书样本、章节摘要） |
| Python sidecar | LLM (云) | Agent 生成、审计评估 |
| Qdrant | PostgreSQL | （通过 Python 代理）向量 metadata 查询 |
| Neo4j | PostgreSQL | （通过 Go 的 sync 端点）同步业务数据 |

</details>

---
