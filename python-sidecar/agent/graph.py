"""
LangGraph StateGraph — the agent orchestration graph.

Flow:
  START
    └─▶ planner
          └─▶ recall_memory
                └─▶ [parallel fan-out via Send]
                      ├─▶ retrieve_world
                      └─▶ retrieve_narrative
                          (both write back to state; LangGraph merges)
                └─▶ assemble_context
                      └─▶ generate
                            └─▶ update_memory
                                  └─▶ quality_check
                                        ├─▶ generate  (if retry)
                                        └─▶ END        (if done)

Node naming keeps one async context so the entire graph runs in a single
asyncio event loop (FastAPI's event loop).
"""
from __future__ import annotations

import asyncio
import logging
from typing import Any

from langgraph.graph import StateGraph, END, START

from agent.state import AgentState
from agent.nodes.planner import planner_node
from agent.nodes.memory import recall_memory_node, update_memory_node
from agent.nodes.retrieval import retrieve_world_node, retrieve_narrative_node
from agent.nodes.context_assembler import assemble_context_node
from agent.nodes.generator import generator_node
from agent.nodes.quality import quality_check_node, should_retry

logger = logging.getLogger(__name__)


# ── parallel retrieval wrapper ────────────────────────────────────────────────

async def parallel_retrieve_node(state: AgentState) -> dict[str, Any]:
    """
    Run world and narrative retrieval concurrently.
    This is a single merged node that fans the two retrievals out in parallel
    and merges results, avoiding sequential latency.
    """
    world_result, narrative_result = await asyncio.gather(
        retrieve_world_node(state),
        retrieve_narrative_node(state),
    )
    # Merge both result dicts
    merged = {}
    merged.update(world_result)
    merged.update(narrative_result)
    return merged


# ── synchronous wrappers for async nodes ──────────────────────────────────────
# LangGraph supports both sync and async nodes; we mark async ones properly.

def _wrap_sync(fn):
    """Wrap a synchronous node to be used in the graph."""
    return fn


# ── build graph ───────────────────────────────────────────────────────────────

def build_graph() -> StateGraph:
    graph = StateGraph(AgentState)

    # Register nodes
    graph.add_node("planner", planner_node)
    graph.add_node("recall_memory", recall_memory_node)
    graph.add_node("retrieve", parallel_retrieve_node)
    graph.add_node("assemble_context", assemble_context_node)
    graph.add_node("generate", generator_node)
    graph.add_node("update_memory", update_memory_node)
    graph.add_node("quality_check", quality_check_node)

    # Edges
    graph.add_edge(START, "planner")
    graph.add_edge("planner", "recall_memory")
    graph.add_edge("recall_memory", "retrieve")
    graph.add_edge("retrieve", "assemble_context")
    graph.add_edge("assemble_context", "generate")
    graph.add_edge("generate", "update_memory")
    graph.add_edge("update_memory", "quality_check")

    # Conditional: retry generation or finish
    graph.add_conditional_edges(
        "quality_check",
        should_retry,
        {
            "retry": "generate",
            "done": END,
        },
    )

    return graph.compile()


# Module-level compiled graph (singleton)
_compiled_graph = None


def get_graph():
    global _compiled_graph
    if _compiled_graph is None:
        _compiled_graph = build_graph()
    return _compiled_graph


async def run_agent(initial_state: AgentState) -> AgentState:
    """Run the agent graph and return the final state."""
    graph = get_graph()
    final = await graph.ainvoke(initial_state)
    return final
