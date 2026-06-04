# NovelBuilder 现代化 Todo

本清单按“先稳定部署，再改善体积、体验和安全”的顺序维护。

## 已完成

- [x] 修复 Docker Actions 构建顺序：`full`、`standard`、`app` 先构建，派生镜像后构建。
- [x] 修复派生镜像 registry 引用：Docker Hub 使用 Docker Hub 基础 tag，GHCR 使用 GHCR 基础 tag，并使用同一 run 内稳定 tag 避免跨日期竞态。
- [x] Go 构建统一使用 `-trimpath -ldflags "-s -w -buildid="`。
- [x] 二进制打包脚本支持 `UPX_ENABLED=auto|true|false`，自动压缩 Linux/Windows 产物。
- [x] Docker 构建切换到 `npm ci`，禁用 pip 缓存和 Python bytecode 写入。
- [x] 修正 Playwright Docker 安装命令，避免 shell 把 `>=` 当作重定向。
- [x] 本地运行脚本会安装 `python-sidecar/novel-downloader` 子模块，避免导入站点插件时报错。
- [x] 登录接口增加失败次数限制、窗口计数和锁定时间，降低爆破风险。
- [x] Go API CORS 改成 `ALLOWED_ORIGINS` 白名单，代理信任改成显式 `TRUSTED_PROXIES`。
- [x] `/setup` 改成首次启动检查页，登录后增加一次性应用内使用向导。
- [x] README 补齐环境变量、部署档位、构建瘦身说明和架构图。
- [x] Python 依赖拆成 `requirements-base.txt`、`requirements-graph.txt`、`requirements-vector.txt`、`requirements-browser.txt`，并让 `standard/app/sqlite` 按档位安装。
- [x] `sqlite` 改为独立最小 runtime stage，不再从 `app` 镜像继承 graph/vector 重依赖。
- [x] GitHub Actions 升级到 Node 24 兼容 action 版本，并显式设置 `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24=true`。
- [x] 参考书上传增加扩展名、内容类型、50 MiB 大小和 Sidecar 路径边界检查。
- [x] 任务队列列表增加排队时长和运行时长字段，便于定位任务拥堵。
- [x] 向量搜索协议统一支持单集合/多集合、`top_k` 和 `score_threshold`，并避免跨集合重复计算 query embedding。
- [x] 增加 `docker-compose.reverse-proxy.yml`、Caddyfile 和双语说明文档。
- [x] 增加 `scripts/ci_check.sh`、前后端 API 契约检查和 Go/Python Sidecar 契约检查，覆盖非交互式静态冒烟。
- [x] 增加 `scripts/smoke_sqlite.sh` 端到端烟测：临时 SQLite 启动、登录、创建项目、保存 LLM Profile、读取任务统计。
- [x] 任务队列增加聚合指标：状态/类型分布、重试数、平均排队与运行时、失败聚类和 24h 项目吞吐。
- [x] 前端生产构建增加 Rolldown vendor 分组，降低入口包体并稳定大依赖缓存边界。
- [x] 升级 `pdfminer.six` 到漏洞修复版本，降低参考资料 PDF 解析链路的已知依赖风险。
- [x] 增加 `scripts/python_dependency_audit.sh`，用于在隔离环境中审计 Python 直接 pin 依赖并暴露剩余漏洞。
- [x] 数据库连接池配置增加下限/上限归一化：open 至少 20、idle 至少 5、连接生命周期最多 60 分钟。
- [x] Python Sidecar PostgreSQL 连接池默认提升到 min=5/max=20，并由 CI 检查防回退。
- [x] 拆分 `handler_references.go` 中的 RAG/Deep Analysis handler，降低单文件规模并保持路由签名不变。
- [x] 拆分 `blueprint_service.go` 中的无状态 helper 和导入导出 DTO，核心服务文件降至 1000 行以下。
- [x] 拆分 `chapter_build_service.go` 的提示词规则、标题摘要和 humanize 辅助逻辑，章节构建主流程文件降至 1000 行以下。
- [x] 拆分 `python-sidecar/main.py` 中的 Pydantic 请求模型，FastAPI 入口文件降至 1000 行以下。
- [x] 拆分 `python-sidecar/llm_utils.py` 的 RPM 限流与 max_tokens fallback 包装逻辑，LLM 路由主文件继续降至 1000 行以下。
- [x] 拆分 `gateway.go` 的 Go LLM RPM 滑动窗口限流，provider 路由主文件继续降至 1000 行以下。
- [x] 任务队列页面补充页面内加载错误状态、刷新 loading 和筛选空态文案。
- [x] 任务队列失败重试增加退避上限和永久失败完成时间，卡住的 running 任务恢复时按重试状态重新调度。
- [x] 任务队列增加受鉴权保护的 SSE 实时快照流，前端任务总控按筛选和分页自动订阅并保留手动/轮询兜底。
- [x] 前端富文本渲染统一经过转义和 URL 协议净化，AgentReview 不再手写未转义 `v-html` 内容。
- [x] Docker 与运行时代码中的 PostgreSQL/Neo4j 固定默认密码已移除，改为 `.env`/环境变量显式提供并由 CI 防回退。
- [x] 增加 `scripts/secret_history_scan.sh`，优先调用 `gitleaks`，否则用 `git grep` 扫描当前和历史中的高置信密钥模式。
- [x] 增加受管理员会话保护的 `/api/docs` Swagger UI 和 `/api/docs/openapi.json` 动态 OpenAPI 路由索引。
- [x] 将 `no-neo4j`、`no-qdrant` 改为独立 Docker 构建和专用入口脚本，禁用组件不再携带对应系统/Python runtime 或要求对应密码。
- [x] 将 `no-graph-vector`、`no-redis` 也改为独立 Docker 构建；变体 workflow 不再传递未使用的 `BASE_IMAGE`。
- [x] 升级 LangChain/LangGraph/Graphiti/Torch 等 Python AI 栈依赖，补齐 Graphiti 0.28 初始化兼容和 `httpx` SOCKS 代理支持；`pip-audit --no-deps` 已无直接依赖漏洞。
- [x] 增加 `.gitleaks.toml` 继承默认规则并精确放行历史 `Idempotency-Key` 误报；`scripts/secret_history_scan.sh` 已无历史泄露告警。

## 后续建议

- [ ] 若公开仓库曾使用过真实凭据，发布前仍应执行凭据轮换；历史重写属于破坏性维护流程，需单独安排。
