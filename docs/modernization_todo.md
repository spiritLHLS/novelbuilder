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

## 后续建议

- [ ] 将 Python 依赖拆成 `requirements-base.txt`、`requirements-graph.txt`、`requirements-browser.txt`，让 `standard/app/sqlite` 进一步减少镜像体积。
- [ ] 为 `no-neo4j`、`no-qdrant` 等 overlay 档位改造成真正的独立 runtime stage，而不只是禁用服务。
- [ ] 增加端到端冒烟测试：启动 SQLite 档、登录、创建项目、保存 LLM Profile、创建蓝图任务。
- [ ] 对上传文件增加大小、类型、路径和内容解析层面的统一安全策略。
- [ ] 给任务队列增加更多可观测性指标：排队时长、重试原因、平均运行时、失败聚类。
- [ ] 将公网部署推荐配置整理成 `docker-compose.reverse-proxy.yml` 示例。
