"""Pydantic request models used by the FastAPI sidecar entrypoint."""
from typing import Optional

from pydantic import BaseModel, Field


class AnalyzeRequest(BaseModel):
    file_path: str
    material_id: str
    project_id: str


class HumanizeRequest(BaseModel):
    text: str
    style_fingerprint: Optional[dict] = None
    intensity: float = 0.7


class MetricsRequest(BaseModel):
    text: str


class EmbedRequest(BaseModel):
    text: str


class AgentRunRequest(BaseModel):
    project_id: str
    task_type: str = "generate_chapter"
    user_prompt: str = ""
    chapter_num: Optional[int] = None
    outline_hint: Optional[str] = None
    style_profile: Optional[dict] = None
    llm_config: dict = Field(default_factory=dict)
    max_retries: int = 2


class BatchAgentRunRequest(BaseModel):
    """Request body for POST /agent/batch-run.

    Chapters are generated sequentially in the order given so that each
    chapter's summary and state update feed into the next runtime evidence pack.
    """
    project_id: str
    chapter_nums: list[int]
    outline_hints: dict = Field(default_factory=dict)
    style_profile: Optional[dict] = None
    llm_config: dict = Field(default_factory=dict)
    max_retries: int = 2


class GraphUpsertRequest(BaseModel):
    project_id: str
    entity_type: str
    entity_id: str
    name: str
    properties: dict = Field(default_factory=dict)
    relations: list[dict] = Field(default_factory=list)


class GraphQueryRequest(BaseModel):
    cypher: str
    params: dict = Field(default_factory=dict)


class VectorUpsertRequest(BaseModel):
    project_id: str
    collection: str
    content: str
    metadata: dict = Field(default_factory=dict)
    point_id: Optional[str] = None


class VectorSearchRequest(BaseModel):
    project_id: str
    collection: Optional[str] = None
    collections: Optional[list[str]] = None
    query: str
    limit: int = 5
    top_k: Optional[int] = None
    score_threshold: Optional[float] = None


class VectorRebuildRequest(BaseModel):
    project_id: str
    items: list[dict] = Field(default_factory=list)


class VectorDeleteBySourceRequest(BaseModel):
    project_id: str
    source_id: str


class VectorDeleteProjectRequest(BaseModel):
    project_id: str


class AuditChapterRequest(BaseModel):
    chapter_id: str
    project_id: str
    chapter_text: str
    chapter_num: int = 1
    context: dict = Field(default_factory=dict)
    llm_config: dict = Field(default_factory=dict)


class AntiDetectRequest(BaseModel):
    chapter_id: str
    text: str
    intensity: str = "medium"
    style_guide: str = ""
    anti_ai_wordlist: list[str] = Field(default_factory=list)
    banned_patterns: list[str] = Field(default_factory=list)
    llm_config: dict = Field(default_factory=dict)


class CreativeBriefRequest(BaseModel):
    brief_text: str
    genre: str = "现代都市"
    llm_config: dict = Field(default_factory=dict)


class ImportChaptersRequest(BaseModel):
    project_id: str
    import_id: str
    source_text: str
    split_pattern: str = r"第.{1,4}[章节回]"
    fanfic_mode: Optional[str] = None
    llm_config: dict = Field(default_factory=dict)


class NarrativeReviseRequest(BaseModel):
    chapter_id: str
    chapter_text: str
    failing_dimensions: list[str] = Field(default_factory=list)
    top_issues: list[str] = Field(default_factory=list)
    llm_config: dict = Field(default_factory=dict)
