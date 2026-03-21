-- NovelBuilder Database Schema - Part 3: Workflow Tables

-- ============================================================
-- Workflow Tables
-- ============================================================

CREATE TABLE workflow_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID REFERENCES projects(id) ON DELETE CASCADE,
    strict_review   BOOLEAN DEFAULT TRUE,
    current_step    VARCHAR(50) NOT NULL DEFAULT 'init',
    status          VARCHAR(20) DEFAULT 'running',
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

CREATE TABLE workflow_steps (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID REFERENCES workflow_runs(id) ON DELETE CASCADE,
    step_key        VARCHAR(50) NOT NULL,
    step_order      INT NOT NULL,
    gate_level      VARCHAR(20) NOT NULL,
    status          VARCHAR(20) NOT NULL DEFAULT 'pending',
    output_ref      UUID,
    snapshot_ref    UUID,
    review_comment  TEXT,
    version         INT DEFAULT 1,
    generated_at    TIMESTAMP,
    reviewed_at     TIMESTAMP,
    created_at      TIMESTAMP DEFAULT NOW(),
    CONSTRAINT ck_workflow_steps_status CHECK (status IN ('pending', 'generated', 'pending_review', 'approved', 'rejected', 'rolled_back', 'needs_recheck'))
);

CREATE TABLE workflow_reviews (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    step_id         UUID REFERENCES workflow_steps(id) ON DELETE CASCADE,
    action          VARCHAR(20) NOT NULL,
    operator        VARCHAR(20) DEFAULT 'admin',
    reason          TEXT,
    from_step_order INT,
    to_step_order   INT,
    created_at      TIMESTAMP DEFAULT NOW()
);

CREATE TABLE workflow_snapshots (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID REFERENCES workflow_runs(id) ON DELETE CASCADE,
    step_key        VARCHAR(50) NOT NULL,
    params          JSONB NOT NULL DEFAULT '{}',
    context_payload JSONB,
    output_payload  JSONB,
    quality_payload JSONB,
    created_at      TIMESTAMP DEFAULT NOW()
);

CREATE TABLE idempotency_keys (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key VARCHAR(128) NOT NULL,
    action          VARCHAR(200) NOT NULL,
    request_hash    VARCHAR(128),
    status_code     INT,
    response_body   JSONB,
    created_at      TIMESTAMP DEFAULT NOW(),
    UNIQUE (idempotency_key, action)
);
