"""
AgentState — the single typed dictionary that flows through every node in the
LangGraph state machine.  All fields are optional so nodes can be composed freely.
"""
from __future__ import annotations

from typing import Annotated, Any, Literal, Optional
from typing_extensions import TypedDict
from langchain_core.messages import AnyMessage
from langgraph.graph.message import add_messages


# ── sub-structures ───────────────────────────────────────────────────────────

class WorldContext(TypedDict, total=False):
    """Track-1: world-knowledge from Neo4j / graphiti."""
    constitution_rules: list[str]        # immutable world rules
    character_cores: list[dict]          # {name, role, core_traits, relationships}
    world_bible_summary: str             # compressed world-bible text
    foreshadowing_active: list[str]      # unresolved foreshadowing items


class NarrativeContext(TypedDict, total=False):
    """Track-2: narrative-continuity from Qdrant + Redis."""
    recent_chapter_summaries: list[str]  # last 3-5 chapter summaries
    current_arc_summary: str             # current volume/arc compressed summary
    plot_momentum: str                   # description of story momentum
    style_samples: list[str]             # reference style snippets from Qdrant


class PlanStep(TypedDict):
    step: int
    description: str
    status: Literal["pending", "in_progress", "done"]


class GraphEntity(TypedDict, total=False):
    id: str
    name: str
    entity_type: str          # Character | Event | Place | Concept | Arc
    properties: dict
    relations: list[dict]     # [{type, target_id, target_name}]


class VectorHit(TypedDict):
    id: str
    score: float
    content: str
    metadata: dict


# ── main state ───────────────────────────────────────────────────────────────

class AgentState(TypedDict, total=False):
    # ── task inputs ──────────────────────────────────────────────────────────
    project_id: str
    session_id: str
    task_type: Literal[
        "generate_chapter", "review_chapter", "world_build",
        "character_develop", "outline_expand"
    ]
    user_prompt: str
    chapter_num: Optional[int]
    outline_hint: Optional[str]       # brief one-line outline for current chapter
    style_profile: Optional[dict]     # style fingerprint from reference material
    llm_config: dict                  # {base_url, api_key, model, max_tokens, temperature}

    # ── planning ─────────────────────────────────────────────────────────────
    plan_steps: list[PlanStep]
    current_step: int

    # ── Re³ dual-track context ────────────────────────────────────────────────
    world_track: WorldContext
    narrative_track: NarrativeContext

    # ── RecurrentGPT memory ──────────────────────────────────────────────────
    short_term_paragraphs: list[str]  # last N generated paragraphs (Redis-backed)
    long_term_facts: list[GraphEntity]  # recalled from graphiti
    working_notes: str                  # scratchpad updated each node

    # ── retrieval results ─────────────────────────────────────────────────────
    graph_entities: list[GraphEntity]
    vector_hits: list[VectorHit]

    # ── assembled context (Lost-in-Middle arranged) ───────────────────────────
    anchor_top: str      # most critical: world rules + character cores
    context_middle: str  # secondary: retrieved summaries / style samples
    anchor_bottom: str   # most critical: chapter outline + writing instruction

    # ── generation ────────────────────────────────────────────────────────────
    draft: str
    quality_score: float            # 0.0..1.0
    quality_issues: list[str]
    retry_count: int
    max_retries: int

    # ── output ────────────────────────────────────────────────────────────────
    final_text: str
    chapter_summary: str            # auto-generated summary for memory update

    # ── LangGraph messages (required for ToolNode compatibility) ──────────────
    messages: Annotated[list[AnyMessage], add_messages]

    # ── control ───────────────────────────────────────────────────────────────
    error: Optional[str]
    done: bool
