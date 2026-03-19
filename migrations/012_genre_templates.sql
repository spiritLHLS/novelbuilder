-- 012_genre_templates.sql
-- Per-genre writing rule templates injected into chapter-generation system prompts.

CREATE TABLE IF NOT EXISTS genre_templates (
    id                    UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    genre                 TEXT         UNIQUE NOT NULL,          -- e.g. "玄幻", "都市", "历史"
    rules_content         TEXT         NOT NULL DEFAULT '',      -- general writing rules for the genre
    language_constraints  TEXT         NOT NULL DEFAULT '',      -- vocabulary / register rules
    rhythm_rules          TEXT         NOT NULL DEFAULT '',      -- pacing / sentence-length guidelines
    audit_dimensions_extra JSONB       NOT NULL DEFAULT '{}',   -- extra audit dimension weights for this genre
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Seed a handful of common Chinese web-fiction genres so the settings page
-- has sensible defaults out of the box.  Writers can edit/delete these freely.

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
 '高度紧张的危机场景用极短句；相对安全的营地场景节奏放缓；章节间需保持悬念钩子。')
ON CONFLICT (genre) DO NOTHING;
