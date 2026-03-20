-- 018: Add per-profile parameter omission flags and API style selector
-- omit_max_tokens / omit_temperature: skip the corresponding field from the LLM
-- payload for providers that reject unknown parameters (e.g. some Codex endpoints).
-- api_style: 'chat_completions' (default, POST /chat/completions) or
--            'responses' (POST /responses, OpenAI Responses API format).
ALTER TABLE llm_profiles
    ADD COLUMN IF NOT EXISTS omit_max_tokens  BOOLEAN     NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS omit_temperature BOOLEAN     NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS api_style        VARCHAR(50) NOT NULL DEFAULT 'chat_completions';
