# 生成架构

NovelBuilder 的生成流程分成“配置、规划、执行、审稿、固化”五层。Go 侧负责稳定的业务事务和任务调度，Python Sidecar 负责重型分析、RAG、图谱/向量和 Agent 编排。

## 端到端流程

```mermaid
sequenceDiagram
  participant U as 用户
  participant V as Vue SPA
  participant G as Go API
  participant Q as Task Queue
  participant P as Python Sidecar
  participant D as DB
  participant L as LLM Provider

  U->>V: 创建项目/配置模型
  V->>G: 保存 LLM Profile 和项目设定
  G->>D: 加密保存 API Key 与配置
  U->>V: 生成蓝图或章节
  V->>G: 创建生成请求
  G->>Q: 入队任务
  Q->>G: 执行任务处理器
  G->>D: 读取项目、角色、世界观、参考上下文
  G->>P: 请求分析/RAG/图谱补充
  P->>L: 需要时调用模型或嵌入服务
  G->>L: 调用写作/审稿模型
  G->>D: 保存章节、快照、质量报告
  V->>G: 查询任务状态和结果
```

## 关键组件

```mermaid
flowchart TB
  subgraph Frontend["Vue 前端"]
    Setup["/setup 初始化向导"]
    Settings["模型配置与路由"]
    Studio["创作工作台"]
    Queue["任务队列"]
  end

  subgraph Backend["Go 后端"]
    Auth["认证与登录限流"]
    Profiles["LLM Profile 加密存储"]
    Services["项目/章节/蓝图服务"]
    Workers["任务队列 Worker"]
    Quality["质量门与审稿"]
  end

  subgraph Sidecar["Python Sidecar"]
    Import["参考书搜索/下载/分析"]
    Agent["LangGraph Agent"]
    RAG["RAG 与向量检索"]
    Graph["Neo4j 图谱记忆"]
    Runtime["加速器检测"]
  end

  Frontend --> Backend
  Workers --> Services
  Services --> Sidecar
  Profiles --> Quality
  Sidecar --> RAG
  Sidecar --> Graph
```

## 任务顺序原则

1. 初始化数据库和系统设置后，才注册后台任务处理器。
2. 所有任务处理器注册完成后，才启动 Worker。
3. Docker Actions 先构建基础镜像，再构建派生镜像。
4. 首次使用先配置 LLM Profile，再创建项目和生成任务。

这四个顺序分别避免数据库缺表、任务找不到 handler、派生镜像引用不存在基础 tag，以及用户在未配置模型时直接触发失败任务。
