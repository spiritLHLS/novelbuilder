"""
Qdrant vector store wrapper for NovelBuilder.

Collections (per project):
  • chapter_summaries   — chapter-level summaries for narrative continuity
  • style_samples       — reference material style snippets
  • character_voices    — character dialogue samples
  • world_knowledge     — world bible chunks

Embedding: sentence-transformers (local, no API cost).
Dimension: 768 (paraphrase-multilingual-mpnet-base-v2)
"""
from __future__ import annotations

import asyncio
import hashlib
import logging
import os
import uuid
from typing import Any

from qdrant_client import AsyncQdrantClient
from qdrant_client.models import (
    Distance,
    VectorParams,
    PointStruct,
    Filter,
    FieldCondition,
    MatchValue,
)

logger = logging.getLogger(__name__)

VECTOR_DIM = 768
COLLECTIONS = ["chapter_summaries", "style_samples", "character_voices", "world_knowledge"]


def _get_embedder():
    """Lazy-load sentence-transformers model."""
    try:
        from sentence_transformers import SentenceTransformer
        return SentenceTransformer("paraphrase-multilingual-mpnet-base-v2")
    except Exception as exc:
        logger.warning("sentence-transformers unavailable: %s", exc)
        return None


_embedder = None


def get_embedder():
    global _embedder
    if _embedder is None:
        _embedder = _get_embedder()
    return _embedder


def embed(text: str) -> list[float] | None:
    model = get_embedder()
    if model is None:
        return None
    vec = model.encode(text, normalize_embeddings=True)
    return vec.tolist()


def _stable_id(content: str) -> str:
    """Generate a stable UUID from text content (for idempotent upserts)."""
    h = hashlib.md5(content.encode()).hexdigest()
    return str(uuid.UUID(h))


class QdrantStore:
    """Async Qdrant client wrapper — singleton."""

    _instance: "QdrantStore | None" = None

    @classmethod
    def get_instance(cls) -> "QdrantStore":
        if cls._instance is None:
            cls._instance = cls()
        return cls._instance

    def __init__(self) -> None:
        url = os.getenv("QDRANT_URL", "http://127.0.0.1:6333")
        self._client = AsyncQdrantClient(url=url)
        logger.info("Qdrant client created: %s", url)

    def _collection_name(self, project_id: str, collection: str) -> str:
        """Map project_id + logical collection to a Qdrant collection name."""
        safe = project_id.replace("-", "_")
        return f"{safe}__{collection}"

    async def ensure_collection(self, project_id: str, collection: str) -> None:
        """Create collection if it doesn't exist."""
        name = self._collection_name(project_id, collection)
        try:
            existing = await self._client.get_collections()
            existing_names = {c.name for c in existing.collections}
            if name not in existing_names:
                await self._client.create_collection(
                    collection_name=name,
                    vectors_config=VectorParams(size=VECTOR_DIM, distance=Distance.COSINE),
                )
                logger.info("Created Qdrant collection: %s", name)
        except Exception as exc:
            logger.warning("ensure_collection failed for %s: %s", name, exc)

    async def upsert(
        self,
        project_id: str,
        collection: str,
        content: str,
        metadata: dict | None = None,
        point_id: str | None = None,
    ) -> str:
        """Embed and upsert a single document. Returns the point ID."""
        await self.ensure_collection(project_id, collection)
        vec = await asyncio.get_event_loop().run_in_executor(None, embed, content)
        if vec is None:
            logger.warning("Embedding failed for content (len=%d), skipping upsert", len(content))
            return ""

        pid = point_id or _stable_id(content)
        payload = {"project_id": project_id, "content": content, **(metadata or {})}
        col = self._collection_name(project_id, collection)

        try:
            await self._client.upsert(
                collection_name=col,
                points=[PointStruct(id=pid, vector=vec, payload=payload)],
            )
        except Exception as exc:
            logger.error("Qdrant upsert failed: %s", exc)
            return ""
        return pid

    async def upsert_batch(
        self,
        project_id: str,
        collection: str,
        items: list[dict],  # [{content, metadata?, id?}]
    ) -> None:
        """Batch upsert — single API call, prevents N+1 embedding calls."""
        if not items:
            return
        await self.ensure_collection(project_id, collection)
        col = self._collection_name(project_id, collection)

        loop = asyncio.get_event_loop()
        # Embed all items concurrently in thread pool
        vecs = await asyncio.gather(
            *[loop.run_in_executor(None, embed, item["content"]) for item in items]
        )

        points = []
        for item, vec in zip(items, vecs):
            if vec is None:
                continue
            pid = item.get("id") or _stable_id(item["content"])
            payload = {
                "project_id": project_id,
                "content": item["content"],
                **(item.get("metadata") or {}),
            }
            points.append(PointStruct(id=pid, vector=vec, payload=payload))

        if points:
            try:
                await self._client.upsert(collection_name=col, points=points)
                logger.info("Batch upsert %d points to %s", len(points), col)
            except Exception as exc:
                logger.error("Qdrant batch upsert failed: %s", exc)

    async def search(
        self,
        project_id: str,
        collection: str,
        query: str,
        limit: int = 5,
    ) -> list[dict]:
        """Semantic search. Returns [{id, score, content, metadata}]."""
        col = self._collection_name(project_id, collection)
        loop = asyncio.get_event_loop()
        vec = await loop.run_in_executor(None, embed, query)
        if vec is None:
            return []
        try:
            results = await self._client.search(
                collection_name=col,
                query_vector=vec,
                limit=limit,
                with_payload=True,
            )
            return [
                {
                    "id": str(r.id),
                    "score": r.score,
                    "content": r.payload.get("content", ""),
                    "metadata": {k: v for k, v in r.payload.items() if k != "content"},
                }
                for r in results
            ]
        except Exception as exc:
            logger.warning("Qdrant search failed on %s: %s", col, exc)
            return []

    async def delete_project_collection(self, project_id: str, collection: str) -> None:
        """Delete all points for a project within a collection."""
        col = self._collection_name(project_id, collection)
        try:
            await self._client.delete(
                collection_name=col,
                points_selector=Filter(
                    must=[FieldCondition(key="project_id", match=MatchValue(value=project_id))]
                ),
            )
        except Exception as exc:
            logger.warning("Qdrant delete failed: %s", exc)

    async def get_collection_stats(self, project_id: str) -> list[dict]:
        """Return count for each logical collection for the given project."""
        stats = []
        for col in COLLECTIONS:
            col_name = self._collection_name(project_id, col)
            try:
                info = await self._client.get_collection(col_name)
                count = info.points_count or 0
            except Exception:
                count = 0
            stats.append({"collection": col, "count": count})
        return stats
