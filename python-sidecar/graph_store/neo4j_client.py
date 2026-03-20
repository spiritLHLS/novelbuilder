"""
Neo4j client wrapper — all graph operations for NovelBuilder.

Design principles:
  • Single Cypher queries for list operations (no N+1)
  • All writes use MERGE to be idempotent
  • Async driver for compatibility with FastAPI event loop
"""
from __future__ import annotations

import logging
import os
from typing import Any

from neo4j import AsyncGraphDatabase, AsyncDriver

logger = logging.getLogger(__name__)


class Neo4jClient:
    """Singleton async Neo4j client."""

    _instance: "Neo4jClient | None" = None
    _driver: AsyncDriver | None = None

    @classmethod
    def get_instance(cls) -> "Neo4jClient":
        if cls._instance is None:
            cls._instance = cls()
        return cls._instance

    def __init__(self) -> None:
        uri = os.getenv("NEO4J_URI", "bolt://127.0.0.1:7687")
        user = os.getenv("NEO4J_USER", "neo4j")
        password = os.getenv("NEO4J_PASSWORD", "novelbuilder")
        self._driver = AsyncGraphDatabase.driver(uri, auth=(user, password))
        logger.info("Neo4j driver created: %s", uri)

    async def close(self) -> None:
        if self._driver:
            await self._driver.close()

    async def query(self, cypher: str, params: dict | None = None) -> list[dict[str, Any]]:
        """Execute a read Cypher query and return list of row dicts."""
        if self._driver is None:
            return []
        async with self._driver.session() as session:
            result = await session.run(cypher, params or {})
            records = await result.data()
            return records

    async def execute(self, cypher: str, params: dict | None = None) -> None:
        """Execute a write Cypher query."""
        if self._driver is None:
            return
        async with self._driver.session() as session:
            await session.run(cypher, params or {})

    # ── Schema initialisation ─────────────────────────────────────────────────

    async def ensure_schema(self) -> None:
        """Create indexes and constraints. Idempotent."""
        constraints = [
            "CREATE CONSTRAINT project_id IF NOT EXISTS FOR (p:Project) REQUIRE p.id IS UNIQUE",
            "CREATE CONSTRAINT character_id IF NOT EXISTS FOR (c:Character) REQUIRE c.id IS UNIQUE",
            "CREATE CONSTRAINT rule_id IF NOT EXISTS FOR (r:Rule) REQUIRE r.id IS UNIQUE",
            "CREATE CONSTRAINT foreshadowing_id IF NOT EXISTS FOR (f:Foreshadowing) REQUIRE f.id IS UNIQUE",
            "CREATE CONSTRAINT event_id IF NOT EXISTS FOR (e:Event) REQUIRE e.id IS UNIQUE",
        ]
        for stmt in constraints:
            try:
                await self.execute(stmt)
            except Exception as exc:
                logger.debug("Constraint already exists or error: %s", repr(exc), exc_info=True)

    # ── Project ───────────────────────────────────────────────────────────────

    async def upsert_project(self, project_id: str, title: str, genre: str) -> None:
        await self.execute(
            """
            MERGE (p:Project {id: $id})
            SET p.title = $title, p.genre = $genre
            """,
            {"id": project_id, "title": title, "genre": genre},
        )

    # ── Characters ────────────────────────────────────────────────────────────

    async def upsert_character(
        self,
        project_id: str,
        char_id: str,
        name: str,
        role_type: str,
        core_traits: str,
    ) -> None:
        """Upsert character node and link to project (single write, no N+1)."""
        await self.execute(
            """
            MERGE (p:Project {id: $pid})
            MERGE (c:Character {id: $cid})
            SET c.name = $name, c.role_type = $role, c.core_traits = $traits
            MERGE (p)-[:HAS_CHARACTER]->(c)
            """,
            {"pid": project_id, "cid": char_id, "name": name,
             "role": role_type, "traits": core_traits},
        )

    async def upsert_character_relation(
        self,
        from_id: str,
        to_id: str,
        rel_type: str,
        description: str = "",
    ) -> None:
        """Create / update a relationship between two characters."""
        await self.execute(
            """
            MATCH (a:Character {id: $from_id})
            MATCH (b:Character {id: $to_id})
            MERGE (a)-[r:RELATES_TO {type: $rel_type}]->(b)
            SET r.description = $desc
            """,
            {"from_id": from_id, "to_id": to_id,
             "rel_type": rel_type, "desc": description},
        )

    async def get_all_characters(self, project_id: str) -> list[dict]:
        """
        Single Cypher to get all characters + their relations (no N+1).
        """
        return await self.query(
            """
            MATCH (p:Project {id: $pid})-[:HAS_CHARACTER]->(c:Character)
            OPTIONAL MATCH (c)-[r:RELATES_TO]->(other:Character)
            RETURN c.id AS id, c.name AS name, c.role_type AS role,
                   c.core_traits AS traits,
                   collect({
                     rel_type: r.type,
                     target_id: other.id,
                     target_name: other.name,
                     description: r.description
                   }) AS relations
            """,
            {"pid": project_id},
        )

    # ── World rules ───────────────────────────────────────────────────────────

    async def upsert_rule(
        self,
        project_id: str,
        rule_id: str,
        content: str,
        immutable: bool,
        priority: int = 5,
    ) -> None:
        await self.execute(
            """
            MERGE (p:Project {id: $pid})
            MERGE (r:Rule {id: $rid})
            SET r.content = $content, r.immutable = $immutable, r.priority = $priority
            MERGE (p)-[:HAS_RULE]->(r)
            """,
            {"pid": project_id, "rid": rule_id, "content": content,
             "immutable": immutable, "priority": priority},
        )

    # ── Foreshadowings ────────────────────────────────────────────────────────

    async def upsert_foreshadowing(
        self,
        project_id: str,
        fs_id: str,
        content: str,
        status: str,
        priority: int,
    ) -> None:
        await self.execute(
            """
            MERGE (p:Project {id: $pid})
            MERGE (f:Foreshadowing {id: $fid})
            SET f.content = $content, f.status = $status, f.priority = $priority
            MERGE (p)-[:HAS_FORESHADOWING]->(f)
            """,
            {"pid": project_id, "fid": fs_id, "content": content,
             "status": status, "priority": priority},
        )

    # ── Events (graphiti-extracted) ───────────────────────────────────────────

    async def upsert_event(
        self,
        project_id: str,
        event_id: str,
        description: str,
        chapter_num: int,
        involved_chars: list[str],
    ) -> None:
        """Upsert a story event and link it to characters."""
        await self.execute(
            """
            MERGE (p:Project {id: $pid})
            MERGE (e:Event {id: $eid})
            SET e.description = $desc, e.chapter_num = $ch
            MERGE (p)-[:HAS_EVENT]->(e)
            """,
            {"pid": project_id, "eid": event_id, "desc": description, "ch": chapter_num},
        )
        for char_id in involved_chars:
            await self.execute(
                """
                MATCH (c:Character {id: $cid})
                MATCH (e:Event {id: $eid})
                MERGE (c)-[:INVOLVED_IN]->(e)
                """,
                {"cid": char_id, "eid": event_id},
            )

    # ── Generic entity list (for frontend graph view) ─────────────────────────

    async def get_project_graph(self, project_id: str) -> dict:
        """
        Return all nodes and edges for the project in a single query.
        Used by the frontend GraphMemory view.
        """
        nodes_raw = await self.query(
            """
            MATCH (p:Project {id: $pid})-[rel]->(n)
            RETURN labels(n) AS labels, n.id AS id, n.name AS name,
                   properties(n) AS props, type(rel) AS edge_type
            """,
            {"pid": project_id},
        )

        edges_raw = await self.query(
            """
            MATCH (p:Project {id: $pid})-[:HAS_CHARACTER]->(a:Character)
            MATCH (a)-[r]->(b)
            RETURN a.id AS from_id, a.name AS from_name,
                   type(r) AS rel_type,
                   b.id AS to_id, b.name AS to_name
            """,
            {"pid": project_id},
        )

        nodes = [
            {
                "id": r.get("id", ""),
                "label": (r.get("labels") or ["Unknown"])[0],
                "name": r.get("name", r.get("id", "")),
                "props": r.get("props", {}),
            }
            for r in nodes_raw
            if r.get("id")
        ]

        edges = [
            {
                "from": r["from_id"],
                "from_name": r["from_name"],
                "to": r["to_id"],
                "to_name": r["to_name"],
                "type": r["rel_type"],
            }
            for r in edges_raw
            if r.get("from_id") and r.get("to_id")
        ]

        return {"nodes": nodes, "edges": edges}
