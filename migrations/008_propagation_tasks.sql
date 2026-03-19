-- NovelBuilder Database Schema - Part 8: Change Propagation & Task Queue

-- ============================================================
-- Change Propagation System (Re3 / PEARL-inspired)
-- ============================================================

-- Dependency graph: what each generated artifact was built from.
-- Populated asynchronously after every chapter generation.
CREATE TABLE content_dependencies (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    dependent_type VARCHAR(30) NOT NULL,  -- 'chapter'
    dependent_id   UUID        NOT NULL,
    source_type    VARCHAR(30) NOT NULL,  -- 'world_bible','character','blueprint','outline'
    source_id      UUID        NOT NULL,
    created_at     TIMESTAMP   NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_content_dep UNIQUE (dependent_type, dependent_id, source_type, source_id)
);

CREATE INDEX idx_content_dep_source    ON content_dependencies(source_type, source_id);
CREATE INDEX idx_content_dep_dependent ON content_dependencies(dependent_type, dependent_id);
CREATE INDEX idx_content_dep_project   ON content_dependencies(project_id);

-- User-initiated change event (records old→new snapshot of any edited entity).
CREATE TABLE change_events (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    entity_type    VARCHAR(30) NOT NULL,  -- 'character','world_bible','outline','foreshadowing','blueprint'
    entity_id      UUID        NOT NULL,
    change_summary TEXT        NOT NULL DEFAULT '',
    old_snapshot   JSONB,
    new_snapshot   JSONB,
    status         VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at     TIMESTAMP   NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMP   NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_change_events_status CHECK (status IN ('pending','analyzed','patching','done','cancelled'))
);

CREATE INDEX idx_change_events_project ON change_events(project_id, created_at DESC);

-- AI-generated propagation plan for a change event.
CREATE TABLE patch_plans (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    change_event_id UUID        NOT NULL REFERENCES change_events(id) ON DELETE CASCADE,
    project_id      UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    impact_summary  TEXT        NOT NULL DEFAULT '',
    total_items     INT         NOT NULL DEFAULT 0,
    done_items      INT         NOT NULL DEFAULT 0,
    status          VARCHAR(20) NOT NULL DEFAULT 'ready',
    created_at      TIMESTAMP   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP   NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_patch_plans_status CHECK (status IN ('ready','executing','done','cancelled'))
);

CREATE INDEX idx_patch_plans_event   ON patch_plans(change_event_id);
CREATE INDEX idx_patch_plans_project ON patch_plans(project_id);

-- Individual tasks within a patch plan (one per affected artifact).
CREATE TABLE patch_items (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id            UUID        NOT NULL REFERENCES patch_plans(id) ON DELETE CASCADE,
    item_type          VARCHAR(30) NOT NULL,  -- 'chapter','outline','foreshadowing'
    item_id            UUID        NOT NULL,
    item_order         INT         NOT NULL DEFAULT 0,
    impact_description TEXT        NOT NULL DEFAULT '',
    patch_instruction  TEXT        NOT NULL DEFAULT '',
    status             VARCHAR(20) NOT NULL DEFAULT 'pending',
    result_snapshot    JSONB,
    created_at         TIMESTAMP   NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMP   NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_patch_items_status CHECK (status IN ('pending','approved','executing','done','skipped','failed'))
);

CREATE INDEX idx_patch_items_plan ON patch_items(plan_id, item_order);

CREATE TRIGGER trg_change_events_updated_at BEFORE UPDATE ON change_events
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_patch_plans_updated_at BEFORE UPDATE ON patch_plans
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_patch_items_updated_at BEFORE UPDATE ON patch_items
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Background Task Queue (rq_worker-style retry/cancel)
-- ============================================================

CREATE TABLE task_queue (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID         REFERENCES projects(id) ON DELETE SET NULL,
    task_type     VARCHAR(100) NOT NULL,
    payload       JSONB        NOT NULL DEFAULT '{}',
    status        VARCHAR(20)  NOT NULL DEFAULT 'pending',
    priority      INT          NOT NULL DEFAULT 5,
    attempts      INT          NOT NULL DEFAULT 0,
    max_attempts  INT          NOT NULL DEFAULT 3,
    error_message TEXT         NOT NULL DEFAULT '',
    scheduled_at  TIMESTAMP    NOT NULL DEFAULT NOW(),
    started_at    TIMESTAMP,
    completed_at  TIMESTAMP,
    created_at    TIMESTAMP    NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMP    NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_task_status CHECK (status IN ('pending','running','done','failed','cancelled'))
);

-- Partial index on pending tasks sorted for next-to-run query (FOR UPDATE SKIP LOCKED)
CREATE INDEX idx_task_queue_pending ON task_queue(priority DESC, scheduled_at ASC)
    WHERE status = 'pending';
CREATE INDEX idx_task_queue_project ON task_queue(project_id);

CREATE TRIGGER trg_task_queue_updated_at BEFORE UPDATE ON task_queue
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
