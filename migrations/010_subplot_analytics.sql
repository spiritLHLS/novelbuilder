-- NovelBuilder Database Schema - Part 10: Subplot & Analytics
-- Subplot board, character emotional arcs, character interaction matrix, radar scan results

-- ============================================================
-- Subplot Board (A/B/C plot-line tracking)
-- ============================================================

CREATE TABLE subplots (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID         NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title           VARCHAR(200) NOT NULL,
    line_label      VARCHAR(10)  NOT NULL DEFAULT 'A',  -- A|B|C|D
    description     TEXT         NOT NULL DEFAULT '',
    status          VARCHAR(20)  NOT NULL DEFAULT 'active',  -- active|paused|resolved|stalled
    priority        INT          NOT NULL DEFAULT 3,
    start_chapter   INT,
    resolve_chapter INT,
    tags            TEXT[]       NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_subplot_status CHECK (status IN ('active','paused','resolved','stalled'))
);

CREATE INDEX idx_subplots_project ON subplots(project_id);

CREATE TABLE subplot_checkpoints (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    subplot_id  UUID         NOT NULL REFERENCES subplots(id) ON DELETE CASCADE,
    chapter_id  UUID         REFERENCES chapters(id) ON DELETE SET NULL,
    chapter_num INT,
    note        TEXT         NOT NULL DEFAULT '',
    progress    INT          NOT NULL DEFAULT 0  CHECK (progress BETWEEN 0 AND 100),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_subplot_checkpoints_subplot ON subplot_checkpoints(subplot_id, chapter_num);

CREATE TRIGGER trg_subplots_updated_at BEFORE UPDATE ON subplots
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Character Emotional Arcs (per-character per-chapter emotion tracking)
-- ============================================================

CREATE TABLE emotional_arc_entries (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID         NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    character_id    UUID         NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    chapter_id      UUID         REFERENCES chapters(id) ON DELETE SET NULL,
    chapter_num     INT          NOT NULL,
    emotion         VARCHAR(50)  NOT NULL DEFAULT 'neutral',
    intensity       FLOAT        NOT NULL DEFAULT 0.5 CHECK (intensity BETWEEN 0 AND 1),
    note            TEXT         NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_emotional_arc_project ON emotional_arc_entries(project_id, chapter_num);
CREATE INDEX idx_emotional_arc_character ON emotional_arc_entries(character_id, chapter_num);
CREATE UNIQUE INDEX uq_emotional_arc_char_chapter ON emotional_arc_entries(character_id, chapter_num);

CREATE TRIGGER trg_emotional_arc_entries_updated_at BEFORE UPDATE ON emotional_arc_entries
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Character Interaction Matrix (track who-met-who + info boundaries)
-- ============================================================

CREATE TABLE character_interactions (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID         NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    char_a_id       UUID         NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    char_b_id       UUID         NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    first_meet_chapter INT,
    last_interact_chapter INT,
    relationship    VARCHAR(100) NOT NULL DEFAULT 'acquaintance',
    info_known_by_a JSONB        NOT NULL DEFAULT '[]',  -- facts char_a knows about char_b
    info_known_by_b JSONB        NOT NULL DEFAULT '[]',  -- facts char_b knows about char_a
    interaction_count INT        NOT NULL DEFAULT 1,
    notes           TEXT         NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_character_interaction UNIQUE (project_id, char_a_id, char_b_id),
    CONSTRAINT ck_char_order CHECK (char_a_id < char_b_id)  -- canonical ordering
);

CREATE INDEX idx_char_interactions_project ON character_interactions(project_id);
CREATE INDEX idx_char_interactions_char_a ON character_interactions(char_a_id);
CREATE INDEX idx_char_interactions_char_b ON character_interactions(char_b_id);

CREATE TRIGGER trg_character_interactions_updated_at BEFORE UPDATE ON character_interactions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Radar Scan Results (market trend analysis cache)
-- ============================================================

CREATE TABLE radar_scan_results (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID         REFERENCES projects(id) ON DELETE CASCADE,
    genre       VARCHAR(50)  NOT NULL DEFAULT '',
    platform    VARCHAR(50)  NOT NULL DEFAULT 'general',
    result      JSONB        NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_radar_scan_results_project ON radar_scan_results(project_id, created_at DESC);

-- ============================================================
-- Genre Templates (per-genre writing rule templates)
-- Injected into chapter-generation system prompts.
-- ============================================================

CREATE TABLE IF NOT EXISTS genre_templates (
    id                    UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    genre                 TEXT         UNIQUE NOT NULL,
    rules_content         TEXT         NOT NULL DEFAULT '',
    language_constraints  TEXT         NOT NULL DEFAULT '',
    rhythm_rules          TEXT         NOT NULL DEFAULT '',
    audit_dimensions_extra JSONB       NOT NULL DEFAULT '{}',
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Seed common Chinese web-fiction genres; writers can edit/delete freely.
INSERT INTO genre_templates (genre, rules_content, language_constraints, rhythm_rules) VALUES
('玄幻',
 '1. 世界观要自洽，力量体系（修炼等级/法则/禁忌）须一致。\n2. 主角成长弧线要有阶段感，不可一步登天。\n3. 战斗描写应结合力量体系，避免泛化"劈出一掌"的描写。\n4. 重要天材地宝/法宝出现时需结合世界背景合理化。',
 '用语宏大而不失细腻；慎用现代网络用语；地名、人名需符合东方玄幻世界观。',
 '主线战斗段落节奏快、短句居多；世界观铺垫段落允许长句，但每段落不超过200字。'),
('都市',
 '1. 人物关系网络要贴近现实，职场/商战/情感逻辑可信。\n2. 主角能力提升需有现实根基，避免过度开挂。\n3. 商战、职场情节需有基本商业逻辑支撑。\n4. 爽点密度适中，每章至少一个情绪高点。',
 '对话风格贴近当代都市年轻人用语；商业/职场术语使用准确；避免过时的网络流行语。',
 '章节节奏明快；对话密度高；每章结尾需留有悬念或反转提示。'),
('历史',
 '1. 历史背景、官职、礼仪需符合朝代设定，允许合理架空但需内部一致。\n2. 主角改变历史的行为需有合理动机和代价。\n3. 文言风格与白话的混用需保持统一基调。\n4. 重要历史事件改写需铺垫充分。',
 '对话中可适当使用文言敬称、官职称谓；避免明显现代词汇（如"OK""拜托了"等）；地名须符合朝代设定。',
 '叙事节奏较缓，注重铺垫；战争/政治段落节奏可加快；整体以第三人称全知视角为主。'),
('末世',
 '1. 末世环境（丧尸/核战/异能爆发等）需在第一章明确交代，后续保持一致。\n2. 资源匮乏的紧张感贯穿全文，不可随意"开仓"。\n3. 人性黑暗面的描写需服务于主题，避免为猎奇而猎奇。\n4. 主角异能/进化逻辑需与世界设定自洽。',
 '末世语境下可使用简短、碎片化的表达增强紧迫感；军事术语使用需准确；避免过度温馨的日常用语破坏氛围。',
 '高度紧张的危机场景用极短句；相对安全的营地场景节奏放缓；章节间需保持悬念钩子。'),
('西幻',
 '1. 世界观以西方中世纪或高魔幻背景为基底，种族（精灵/矮人/兽人等）、职业（骑士/法师/游侠等）设定需内部自洽。\n2. 魔法体系须有明确规则与代价，避免"万能魔法"破坏张力。\n3. 主角成长弧线须呼应史诗叙事结构，重视英雄旅程的阶段感。\n4. 阵营对立（光明/黑暗、秩序/混乱）应有灰色地带，避免扁平化。\n5. 战斗场面须体现职业特色与战术配合，而非单纯数值碾压。',
 '人名、地名、技能名采用西式风格（可音译或创造），避免直接使用中文传统词汇；魔法/神明相关术语保持统一体系；对话风格可略带古典气息但不失可读性。',
 '史诗冒险段落节奏舒展，允许大段世界观描写；战斗与危机场景切换至短句快节奏；章节高潮前应有充分的情绪蓄势铺垫。')
ON CONFLICT (genre) DO NOTHING;
