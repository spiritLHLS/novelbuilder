-- LLM Profile table: stores user-configured AI model profiles in database
-- Replaces hardcoded config.yaml model definitions, allowing runtime management

CREATE TABLE IF NOT EXISTS llm_profiles (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(100) NOT NULL UNIQUE,
    provider    VARCHAR(50)  NOT NULL DEFAULT 'openai',
    base_url    VARCHAR(500) NOT NULL DEFAULT 'https://api.openai.com/v1',
    api_key     TEXT         NOT NULL,
    model_name  VARCHAR(200) NOT NULL,
    max_tokens  INT          NOT NULL DEFAULT 8192,
    temperature FLOAT        NOT NULL DEFAULT 0.7,
    is_default  BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Ensure only one default profile at a time (partial unique index)
CREATE UNIQUE INDEX IF NOT EXISTS idx_llm_profiles_single_default
    ON llm_profiles (is_default) WHERE (is_default = TRUE);

CREATE INDEX IF NOT EXISTS idx_llm_profiles_is_default ON llm_profiles (is_default);
