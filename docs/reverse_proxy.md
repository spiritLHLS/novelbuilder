# 反向代理示例 / Reverse Proxy Example

本仓库提供一个 Caddy 示例，可以和现有 compose 文件叠加使用：

```bash
NB_DOMAIN=novel.example.com \
docker compose -f docker-compose.standard.yml -f docker-compose.reverse-proxy.yml up -d
```

SQLite 档也可以同样叠加：

```bash
NB_DOMAIN=novel.example.com \
docker compose -f docker-compose.sqlite.yml -f docker-compose.reverse-proxy.yml up -d
```

多容器 `app` 档已经给 `app` 增加了 `novelbuilder` 网络别名，因此默认配置可直接使用；也可以显式指定上游：

```bash
NB_DOMAIN=novel.example.com NB_UPSTREAM=app:8080 \
docker compose -f docker-compose.multi.yml -f docker-compose.reverse-proxy.yml up -d
```

默认请求体上限是 `60MB`，略高于应用内参考文件上传限制。公网部署还应同时设置强 `ADMIN_PASSWORD`、精确的 `ALLOWED_ORIGINS` 和可信的 `TRUSTED_PROXIES`。

The same file works for English deployments. Set `NB_DOMAIN` to the public host. Multi-container deployments include a `novelbuilder` network alias for the `app` service; `NB_UPSTREAM=app:8080` remains available as an explicit override.
