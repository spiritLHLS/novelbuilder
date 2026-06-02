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

## 后续建议

- [ ] 继续将 `no-neo4j`、`no-qdrant` 等 overlay 档位改造成真正的独立 runtime stage，而不只是禁用服务。
- [ ] 增加非交互式端到端冒烟测试：启动 SQLite 档、调用 API 登录、创建项目、保存 LLM Profile、创建蓝图任务。
- [ ] 给任务队列增加更多聚合指标：重试原因、平均运行时、失败聚类和按项目分组的吞吐。
