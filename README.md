# NovelBuilder - AI小说生成平台

基于 Re3 + RecurrentGPT + DOC 架构的 AI 长篇小说生成平台。

## 技术栈

- **后端**: Go (Gin) — API 服务器、AI 网关、工作流引擎
- **Python 边车**: FastAPI — 参考书四层分析、八步拟人化管线、困惑度/突发度检测
- **前端**: Vue 3 + Vite + Element Plus + ECharts + Cytoscape
- **数据库**: PostgreSQL 16 (pgvector) + Redis 7
- **AI**: 数据库驱动的 LLM 配置（支持 OpenAI / Anthropic / OpenAI-compatible，如 DeepSeek）

## 项目结构

```
├── backend/              # Go 后端
│   ├── cmd/server/       # 入口
│   └── internal/         # 核心逻辑
│       ├── config/       # 配置加载
│       ├── database/     # PostgreSQL & Redis
│       ├── gateway/      # AI 多模型网关
│       ├── handlers/     # HTTP 处理器
│       ├── models/       # 数据模型
│       ├── services/     # 业务逻辑
│       └── workflow/     # 工作流状态机
├── python-sidecar/       # Python 分析服务
│   ├── analyzers/        # 四层分析器
│   └── humanizer/        # 拟人化管线
├── frontend/             # Vue 前端
│   └── src/
│       ├── api/          # API 客户端
│       ├── router/       # 路由
│       ├── stores/       # Pinia 状态
│       └── views/        # 页面组件
├── configs/              # 配置文件
├── migrations/           # 数据库迁移
└── docker/               # Docker 部署
```

## 快速启动

### Docker 一键部署

```bash
# 构建并运行
docker build -t novelbuilder .
docker run -d -p 8080:8080 \
  --name novelbuilder novelbuilder
```

访问 http://localhost:8080

首次启动后，请在前端「AI 模型配置」页面新增至少一个模型并设为默认：

- 路径：`/settings/llm`
- 支持只配置一个模型/一个 API Key 处理全部任务
- API Key 仅存数据库，接口返回脱敏信息（`has_api_key`、`masked_api_key`）

说明：

- 现已默认使用数据库中的默认模型配置
- `configs/config.yaml` 中 `ai_gateway` 配置仅作为兜底（数据库无默认配置时才生效）

### 本地开发

```bash
# 后端
cd backend && go mod tidy && go run ./cmd/server

# Python 边车
cd python-sidecar && pip install -r requirements.txt && uvicorn main:app --port 8081

# 前端
cd frontend && npm install && npm run dev
```

## 核心功能

- 参考书四层分析（风格指纹/叙事结构/氛围萃取/情节隔离）
- 世界圣经 & 宪法管理（不可变/可变规则 + 禁止锚点）
- DOC 三层大纲（宏观/中观/微观）+ 张力曲线
- 角色管理 + Cytoscape 关系图谱
- 伏笔全生命周期追踪（埋设→触发→回收）
- 蓝图一键生成（世界设定+角色+大纲+伏笔+卷册）
- Re3 双轨上下文注入 + RecurrentGPT 记忆 + Lost-in-Middle 排布
- SSE 流式章节生成
- 四角色 AI 审核链（编辑/读者/逻辑/反AI）
- 八步拟人化管线 + 困惑度/突发度检测
- 工作流状态机 + 快照回滚
- ECharts 质量监控仪表盘
- LLM Profile 管理（数据库存储、默认模型切换、单模型全任务运行）

## 模型配置与 API

当前版本的模型配置方式：

1. 模型配置持久化到 PostgreSQL（`llm_profiles`）
2. 运行时优先读取数据库默认模型
3. 若数据库未配置默认模型，才回退到 `config.yaml`

相关迁移：

- `migrations/003_llm_profiles.sql`

相关接口：

- `GET /api/llm-profiles`
- `POST /api/llm-profiles`
- `GET /api/llm-profiles/:id`
- `PUT /api/llm-profiles/:id`
- `DELETE /api/llm-profiles/:id`
- `POST /api/llm-profiles/:id/set-default`