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
COLLECTIONS = ["chapter_summaries", "style_samples", "character_voices", "world_knowledge", "sensory_samples"]

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

    # Allow overriding the model path/name via env var so Docker images can pre-bake
    # the model weights without needing HuggingFace Hub access at runtime.
    # Use the full HF path so transformers can resolve it without sentence-transformers.
    model_name = os.getenv(
        "EMBEDDING_MODEL_PATH",
        "sentence-transformers/paraphrase-multilingual-mpnet-base-v2",
    )
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

        def encode(self, text: str, normalize_embeddings: bool = True, show_progress_bar: bool = False):
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
            st_name = os.getenv(
                "EMBEDDING_MODEL_PATH",
                "paraphrase-multilingual-mpnet-base-v2",
            )
            model = SentenceTransformer(
                st_name,
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
    vec = model.encode(text, normalize_embeddings=True, show_progress_bar=False)
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
        """Create collection if it doesn't exist (idempotent, race-safe)."""
        name = self._collection_name(project_id, collection)
        try:
            await self._client.create_collection(
                collection_name=name,
                vectors_config=VectorParams(size=VECTOR_DIM, distance=Distance.COSINE),
            )
            logger.info("Created Qdrant collection: %s", name)
        except Exception as exc:
            # 409 Conflict means the collection already exists (pre-existing or
            # concurrent creation by another coroutine) — this is not an error.
            if getattr(exc, "status_code", None) == 409:
                return
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
        try:
            max_embed_workers = max(1, int(os.getenv("VECTOR_EMBED_CONCURRENCY", "4")))
        except ValueError:
            max_embed_workers = 4
        semaphore = asyncio.Semaphore(max_embed_workers)

        async def _embed_item(content: str):
            async with semaphore:
                return await loop.run_in_executor(None, embed, content)

        vecs = await asyncio.gather(*[_embed_item(item["content"]) for item in items])

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
        return await self.search_collections(project_id, [collection], query, limit)

    async def search_collections(
        self,
        project_id: str,
        collections: list[str],
        query: str,
        limit: int = 5,
        score_threshold: float | None = None,
    ) -> list[dict]:
        """Search multiple logical collections while embedding the query once."""
        collections = [c for c in dict.fromkeys(collections) if c]
        if not collections:
            return []
        limit = max(1, min(int(limit or 5), 50))
        for collection in collections:
            await self.ensure_collection(project_id, collection)
        loop = asyncio.get_event_loop()
        vec = await loop.run_in_executor(None, embed, query)
        if vec is None:
            return []

        async def _search_one(collection: str) -> list[dict]:
            col = self._collection_name(project_id, collection)
            try:
                results = await self._client.search(
                    collection_name=col,
                    query_vector=vec,
                    limit=limit,
                    with_payload=True,
                )
                hits = []
                for r in results:
                    if score_threshold is not None and r.score < score_threshold:
                        continue
                    payload = r.payload or {}
                    hits.append({
                        "id": str(r.id),
                        "score": r.score,
                        "collection": collection,
                        "content": payload.get("content", ""),
                        "metadata": {k: v for k, v in payload.items() if k != "content"},
                    })
                return hits
            except Exception as exc:
                logger.warning("Qdrant search failed on %s: %s", col, repr(exc), exc_info=True)
                return []

        try:
            grouped = await asyncio.gather(*[_search_one(collection) for collection in collections])
            hits = [hit for group in grouped for hit in group]
            hits.sort(key=lambda h: h.get("score", 0), reverse=True)
            return hits[:limit]
        except Exception as exc:
            logger.warning("Qdrant multi-collection search failed: %s", repr(exc), exc_info=True)
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
            # 404 = collection doesn't exist yet, nothing to delete
            if getattr(exc, "status_code", None) == 404:
                return
            logger.warning("Qdrant delete failed: %s", repr(exc), exc_info=True)

    async def delete_all_project_vectors(self, project_id: str) -> None:
        """Delete all vectors across every collection for a project."""
        for col in COLLECTIONS:
            await self.delete_project_collection(project_id, col)

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
                # 404 = collection doesn't exist yet, nothing to delete — not an error
                if getattr(exc, "status_code", None) == 404:
                    continue
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
