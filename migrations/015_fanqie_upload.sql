-- 015_fanqie_upload.sql  –  番茄小说网自动上传支持
--
-- fanqie_accounts: 每个项目绑定一个番茄小说作者账号
-- fanqie_uploads:  追踪每章的上传状态

-- ── 番茄账号绑定 ────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS fanqie_accounts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    book_id         TEXT NOT NULL DEFAULT '',       -- 番茄平台上的 book_id
    book_title      TEXT NOT NULL DEFAULT '',       -- 番茄平台上的书名
    cookies         TEXT NOT NULL DEFAULT '',       -- 浏览器 Cookie（加密存储）
    status          TEXT NOT NULL DEFAULT 'unconfigured',  -- unconfigured / active / expired
    last_validated_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(project_id)
);

-- ── 章节上传记录 ────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS fanqie_uploads (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    chapter_id        UUID NOT NULL REFERENCES chapters(id) ON DELETE CASCADE,
    fanqie_chapter_id TEXT NOT NULL DEFAULT '',     -- 番茄平台返回的远端章节 ID
    status            TEXT NOT NULL DEFAULT 'pending',  -- pending / uploading / success / failed
    error_message     TEXT NOT NULL DEFAULT '',
    uploaded_at       TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(project_id, chapter_id)
);

CREATE INDEX IF NOT EXISTS idx_fanqie_uploads_project ON fanqie_uploads(project_id);
CREATE INDEX IF NOT EXISTS idx_fanqie_uploads_status  ON fanqie_uploads(project_id, status);
