# NovelBuilder

[English README](README.md)

NovelBuilder 是一个 AI 长篇小说创作工作台。

## 架构

```text
Vue SPA
  -> Go API Gateway
     -> GORM 自动初始化数据库结构，PostgreSQL/SQLite 业务数据路径
     -> 任务队列、登录、LLM Profile 路由、质量门
  -> Python Sidecar
     -> 参考书分析、LangGraph Agent、审稿、去 AI 味、上传自动化
  -> PostgreSQL/SQLite、Redis、Qdrant、Neo4j
```

## Docker 档位

| 标签 | Dockerfile | 依赖 | 推荐资源 | 用途 |
| --- | --- | --- | --- | --- |
| `latest`, `full`, `YYYYMMDD` | `Dockerfile` | Go、Python、Vue、PostgreSQL、Redis、Qdrant、Neo4j、Playwright | 4 CPU、8 GB 内存、20 GB 磁盘 | 完整单容器部署 |
| `standard`, `YYYYMMDD-standard` | `Dockerfile.standard` | Go、Python、Vue、PostgreSQL、Redis | 2 CPU、4 GB 内存、10 GB 磁盘 | 不启用图谱/向量的日常写作 |
| `app`, `YYYYMMDD-app` | `Dockerfile.app` | Go、Python、Vue | 2 CPU、2 GB 内存，外部数据库另算 | 多容器 compose 或托管数据库 |
| `no-neo4j` | `Dockerfile.no-neo4j` | full 去掉 Neo4j | 3 CPU、6 GB 内存 | 保留向量检索，关闭图记忆 |
| `no-qdrant` | `Dockerfile.no-qdrant` | full 去掉 Qdrant | 3 CPU、6 GB 内存 | 保留图记忆，关闭向量检索 |
| `no-graph-vector` | `Dockerfile.no-graph-vector` | PostgreSQL、Redis | 2 CPU、4 GB 内存 | 只使用确定性上下文 |
| `no-redis` | `Dockerfile.no-redis` | PostgreSQL | 2 CPU、3 GB 内存 | 单用户降级模式，进程内会话 |
| `sqlite` | `Dockerfile.sqlite` | Go、Python、Vue、SQLite | 1 CPU、2 GB 内存、5 GB 磁盘 | 无外部服务的最小本地/二进制模式 |

## 快速启动

完整单容器：

```bash
docker compose up -d
open http://127.0.0.1:8080/setup
```

标准档：

```bash
docker compose -f docker-compose.standard.yml up -d
```

最小 SQLite 档：

```bash
docker compose -f docker-compose.sqlite.yml up -d
```

多容器：

```bash
docker compose -f docker-compose.multi.yml --profile full up -d
```

裸机源码安装：

```bash
./scripts/install.sh
./scripts/run-local.sh
```

Windows 源码安装：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\install.ps1
powershell -ExecutionPolicy Bypass -File .\scripts\run-local.ps1
```

二进制打包：

```bash
VERSION=dev ./scripts/build-binaries.sh
```

二进制包内包含 Go 后端、Vue `frontend/dist`、Python sidecar 和运行脚本。二进制模式默认使用 SQLite：`./data/novelbuilder.db`；如需 PostgreSQL，设置 `DB_DRIVER=postgres` 以及 `DB_HOST`、`DB_PORT`、`DB_USER`、`DB_PASSWORD`、`DB_NAME`。Redis、Qdrant、Neo4j 都可以关闭。

## 配置

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `APP_PROFILE` | `full` | 诊断页展示的部署档 |
| `DB_DRIVER` | 容器默认 `postgres`，二进制脚本默认 `sqlite` | `sqlite`/`sqlite3` 或 `postgres` |
| `SQLITE_PATH` | `/data/novelbuilder.db` 或 `./data/novelbuilder.db` | `DB_DRIVER=sqlite` 时使用 |
| `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME` | 本地 PostgreSQL 默认值 | `DB_DRIVER=postgres` 时使用 |
| `REDIS_ENABLED` | `true` | `false` 时使用进程内 session 和降级缓存 |
| `REDIS_ADDR`, `REDIS_URL` | 本地 Redis 默认值 | Go 使用 `REDIS_ADDR`，Python 使用 `REDIS_URL` |
| `NEO4J_URI` | 按档位设置 | 为空则关闭图谱能力 |
| `QDRANT_URL` | 按档位设置 | 为空则关闭向量能力 |
| `NB_ACCELERATOR` | `auto` | 可设为 `auto`、`cpu`、`cuda`、`rocm`、`npu` |

## 验证命令

```bash
cd backend && go test ./...
cd python-sidecar && python3 -m py_compile main.py routes_audit.py routes_analysis.py runtime_capabilities.py
cd frontend && npm run build
```
