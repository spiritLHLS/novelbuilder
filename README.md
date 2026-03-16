# NovelBuilder - AI小说生成平台

基于 Re3 + RecurrentGPT + DOC 架构的 AI 长篇小说生成平台。

## 技术栈

- **后端**: Go (Gin) — API 服务器、AI 网关、工作流引擎
- **Python 边车**: FastAPI — 参考书四层分析、八步拟人化管线、困惑度/突发度检测
- **前端**: Vue 3 + Vite + Element Plus + ECharts + Cytoscape
- **数据库**: PostgreSQL 16 (pgvector) + Redis 7
- **AI**: OpenAI / Anthropic / DeepSeek 多模型网关

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
# 设置 AI API 密钥
export OPENAI_API_KEY=your-key
export ANTHROPIC_API_KEY=your-key
export DEEPSEEK_API_KEY=your-key

# 构建并运行
docker build -t novelbuilder .
docker run -d -p 8080:8080 \
  -e OPENAI_API_KEY=$OPENAI_API_KEY \
  -e ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY \
  -e DEEPSEEK_API_KEY=$DEEPSEEK_API_KEY \
  --name novelbuilder novelbuilder
```

访问 http://localhost:8080

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