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
import threading
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

# Sentinel — distinguishes "not yet tried" (None) from "permanently failed" (_FAILED)
_FAILED = object()
_embedder = None
_embedder_lock = threading.Lock()


def _load_embedder_transformers():
    """Load embedding model via transformers directly, avoiding the SentenceTransformer
    .to(device) meta-tensor bug present in sentence-transformers >= 3.0 + older PyTorch."""
    import torch
    import torch.nn.functional as F
    from transformers import AutoTokenizer, AutoModel

    model_name = "paraphrase-multilingual-mpnet-base-v2"
    tokenizer = AutoTokenizer.from_pretrained(model_name)
    # low_cpu_mem_usage=False prevents meta-tensor creation which breaks .to(device)
    model = AutoModel.from_pretrained(
        model_name,
        low_cpu_mem_usage=False,
        torch_dtype=torch.float32,
    )
    model.eval()

    class _Embedder:
        def __init__(self, tok, mod):
            self._tok = tok
            self._mod = mod

        def encode(self, text: str, normalize_embeddings: bool = True):
            inputs = self._tok(
                text,
                return_tensors="pt",
                truncation=True,
                max_length=512,
                padding=True,
            )
            with torch.no_grad():
                out = self._mod(**inputs)
            hidden = out.last_hidden_state           # (1, seq, 768)
            mask = inputs["attention_mask"].unsqueeze(-1).float()
            pooled = (hidden * mask).sum(1) / mask.sum(1).clamp(min=1e-9)  # (1, 768)
            if normalize_embeddings:
                pooled = F.normalize(pooled, p=2, dim=-1)
            return pooled[0].cpu().numpy()

    return _Embedder(tokenizer, model)


def _get_embedder():
    """Lazy-load embedding model. Thread-safe. Permanent failures are cached."""
    global _embedder
    if _embedder is not None:
        return None if _embedder is _FAILED else _embedder

    with _embedder_lock:
        # Re-check after acquiring lock (double-checked locking)
        if _embedder is not None:
            return None if _embedder is _FAILED else _embedder

        # Strategy 1: load via transformers directly (avoids meta-tensor issue)
        try:
            model = _load_embedder_transformers()
            _embedder = model
            logger.info("Embedding model loaded via transformers")
            return _embedder
        except Exception as exc1:
            logger.warning("transformers embedder failed: %s", repr(exc1))

        # Strategy 2: sentence-transformers with explicit CPU device
        try:
            from sentence_transformers import SentenceTransformer
            model = SentenceTransformer(
                "paraphrase-multilingual-mpnet-base-v2",
                device="cpu",
                model_kwargs={"low_cpu_mem_usage": False},
            )
            _embedder = model
            logger.info("Embedding model loaded via sentence-transformers")
            return _embedder
        except Exception as exc2:
            logger.warning("sentence-transformers embedder also failed: %s", repr(exc2))

        # Both strategies failed — mark as permanently unavailable
        _embedder = _FAILED
        logger.error("Embedding model unavailable; vector operations will be no-ops")
        return None


def embed(text: str) -> list[float] | None:
    model = _get_embedder()
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
            logger.warning("ensure_collection failed for %s: %s", name, repr(exc), exc_info=True)

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
            logger.error("Qdrant upsert failed: %s", repr(exc), exc_info=True)
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
                logger.error("Qdrant batch upsert failed: %s", repr(exc), exc_info=True)

    async def search(
        self,
        project_id: str,
        collection: str,
        query: str,
        limit: int = 5,
    ) -> list[dict]:
        """Semantic search. Returns [{id, score, content, metadata}]."""
        await self.ensure_collection(project_id, collection)
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
            logger.warning("Qdrant search failed on %s: %s", col, repr(exc), exc_info=True)
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
            logger.warning("Qdrant delete failed: %s", repr(exc), exc_info=True)

    async def delete_by_source_id(self, project_id: str, source_id: str) -> None:
        """Delete all points with a specific source_id across all collections."""
        for col in COLLECTIONS:
            col_name = self._collection_name(project_id, col)
            try:
                await self._client.delete(
                    collection_name=col_name,
                    points_selector=Filter(
                        must=[FieldCondition(key="source_id", match=MatchValue(value=source_id))]
                    ),
                )
            except Exception as exc:
                logger.warning("Qdrant delete_by_source_id failed for %s: %s", col_name, repr(exc), exc_info=True)

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
