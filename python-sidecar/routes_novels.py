from __future__ import annotations

import asyncio
import json
import logging
import os
import re
import tempfile
import uuid
from importlib.resources import as_file
from pathlib import Path
from typing import Any
from typing import Optional

from fastapi import APIRouter, BackgroundTasks, HTTPException
from fastapi.responses import StreamingResponse
from pydantic import BaseModel

logger = logging.getLogger('python-agent')

router = APIRouter()

# NOVEL NETWORK SEARCH / DOWNLOAD  (backed by novel-downloader)
# ═══════════════════════════════════════════════════════════════════════════════

class NovelSearchReq(BaseModel):
    keyword: str
    sites: Optional[list[str]] = None
    # limit=0 means unlimited (all results from all sites)
    limit: int = 0
    per_site_limit: int = 10

class NovelBookInfoReq(BaseModel):
    site: str
    book_id: str


class NovelResolveURLReq(BaseModel):
    url: str

class NovelFetchImportReq(BaseModel):
    site: str
    book_id: str
    title: str
    author: str = ""
    chapter_ids: list[str]


def _parse_bool_env(name: str) -> bool | None:
    raw = os.getenv(name)
    if raw is None:
        return None
    value = raw.strip().lower()
    if value in {"1", "true", "yes", "on"}:
        return True
    if value in {"0", "false", "no", "off"}:
        return False
    logger.warning("Ignoring invalid boolean environment value: %s=%r", name, raw)
    return None


def _load_nd_config_data(tmpdir: str, workers: int | None = None) -> dict[str, Any]:
    from novel_downloader.infra.config import load_config
    from novel_downloader.infra.paths import DEFAULT_CONFIG_FILE

    explicit_path = os.getenv("ND_SETTINGS_PATH", "").strip()
    try:
        if explicit_path:
            config_data = load_config(Path(explicit_path))
        else:
            try:
                config_data = load_config()
            except FileNotFoundError:
                with as_file(DEFAULT_CONFIG_FILE) as bundled_path:
                    config_data = load_config(bundled_path)
    except Exception as exc:
        logger.exception("Failed to load novel-downloader settings")
        raise HTTPException(status_code=500, detail=f"Invalid novel-downloader config: {exc}")

    config_data = dict(config_data)
    general = dict(config_data.get("general") or {})
    general["raw_data_dir"] = tmpdir
    general["cache_dir"] = tmpdir
    general["output_dir"] = tmpdir

    if workers is not None:
        general["workers"] = workers

    scalar_env_overrides: dict[str, tuple[str, type]] = {
        "request_interval": ("ND_REQUEST_INTERVAL", float),
        "retry_times": ("ND_RETRY_TIMES", int),
        "backoff_factor": ("ND_BACKOFF_FACTOR", float),
        "timeout": ("ND_TIMEOUT", float),
        "backend": ("ND_BACKEND", str),
        "impersonate": ("ND_IMPERSONATE", str),
    }
    for key, (env_name, caster) in scalar_env_overrides.items():
        raw = os.getenv(env_name, "").strip()
        if not raw:
            continue
        try:
            general[key] = caster(raw)
        except ValueError:
            logger.warning("Ignoring invalid environment override: %s=%r", env_name, raw)

    bool_env_overrides = {
        "fetch_inaccessible": "ND_FETCH_INACCESSIBLE",
        "cache_book_info": "ND_CACHE_BOOK_INFO",
        "cache_chapter": "ND_CACHE_CHAPTER",
        "http2": "ND_HTTP2",
    }
    for key, env_name in bool_env_overrides.items():
        value = _parse_bool_env(env_name)
        if value is not None:
            general[key] = value

    config_data["general"] = general
    return config_data


def _make_nd_client(site: str, tmpdir: str, workers: int = 3):
    from novel_downloader.infra.config import ConfigAdapter
    from novel_downloader.plugins import registrar

    config_data = _load_nd_config_data(tmpdir, workers=workers)
    adapter = ConfigAdapter(config=config_data)
    return registrar.get_client(site, adapter.get_client_config(site))


@router.post("/novels/search")
async def novels_search(req: NovelSearchReq):
    """Search for novels by keyword across all registered sites."""
    try:
        from novel_downloader.plugins.search import search as nd_search
    except ImportError:
        raise HTTPException(
            status_code=503,
            detail="novel-downloader not installed; run: pip install novel-downloader",
        )
    try:
        raw = await nd_search(
            req.keyword,
            sites=req.sites or None,
            limit=req.limit if req.limit > 0 else None,
            per_site_limit=req.per_site_limit,
            timeout=15.0,
        )
    except Exception as exc:
        logger.exception("Novel search failed")
        raise HTTPException(status_code=502, detail=f"Search failed: {exc}")

    results = []
    for r in raw:
        d = dict(r) if not isinstance(r, dict) else r
        results.append({
            "site":           str(d.get("site", "")),
            "book_id":        str(d.get("book_id", "")),
            "book_url":       str(d.get("book_url", "")),
            "cover_url":      str(d.get("cover_url", "")),
            "title":          str(d.get("title", "")),
            "author":         str(d.get("author", "")),
            "latest_chapter": str(d.get("latest_chapter", "")),
            "update_date":    str(d.get("update_date", "")),
            "word_count":     str(d.get("word_count", "")),
        })
    return {"results": results}


def _normalize_result(r: object) -> dict:
    """Convert a SearchResult (TypedDict or dict-like) to a plain serialisable dict."""
    d = dict(r) if not isinstance(r, dict) else r  # type: ignore[arg-type]
    return {
        "site":           str(d.get("site", "")),
        "book_id":        str(d.get("book_id", "")),
        "book_url":       str(d.get("book_url", "")),
        "cover_url":      str(d.get("cover_url", "")),
        "title":          str(d.get("title", "")),
        "author":         str(d.get("author", "")),
        "latest_chapter": str(d.get("latest_chapter", "")),
        "update_date":    str(d.get("update_date", "")),
        "word_count":     str(d.get("word_count", "")),
    }


class NovelSearchStreamReq(BaseModel):
    keyword: str
    sites: Optional[list[str]] = None
    per_site_limit: int = 10


@router.post("/novels/search-stream")
async def novels_search_stream(req: NovelSearchStreamReq):
    """
    Stream search results site-by-site as NDJSON.

    Emits one JSON line per site that responds:
      {"type": "batch", "site": "xxx", "results": [...]}

    Finalises with:
      {"type": "done", "total": N}

    On import error:
      {"type": "error", "message": "..."}
    """
    try:
        from novel_downloader.plugins.search import search_stream as nd_search_stream
    except ImportError:
        raise HTTPException(
            status_code=503,
            detail="novel-downloader not installed; run: pip install novel-downloader",
        )

    async def event_gen():
        total = 0
        try:
            async for batch in nd_search_stream(
                req.keyword,
                sites=req.sites or None,
                per_site_limit=req.per_site_limit,
                timeout=15.0,
                nsfw=False,
            ):
                if not batch:
                    continue
                results = [_normalize_result(r) for r in batch]
                total += len(results)
                site_name = results[0]["site"] if results else ""
                yield (
                    json.dumps(
                        {"type": "batch", "site": site_name, "results": results},
                        ensure_ascii=False,
                    )
                    + "\n"
                )
        except Exception as exc:
            logger.exception("Novel search stream failed")
            yield (
                json.dumps({"type": "error", "message": str(exc)}, ensure_ascii=False)
                + "\n"
            )
            return
        yield (
            json.dumps({"type": "done", "total": total}, ensure_ascii=False) + "\n"
        )

    return StreamingResponse(
        event_gen(),
        media_type="application/x-ndjson",
        headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"},
    )


@router.get("/novels/sites")
async def novels_list_sites():
    """Return the safe-by-default searchable sites plus Legado support metadata."""
    try:
        import aiohttp
        from novel_downloader.plugins.registry import registrar
        from novel_downloader.plugins.sites.legado.manager import book_source_manager
    except ImportError:
        raise HTTPException(
            status_code=503,
            detail="novel-downloader not installed; run: pip install novel-downloader",
        )
    classes = registrar.get_searcher_classes(None, load_all_if_none=True)
    sites: list[str] = []
    async with aiohttp.ClientSession() as session:
        for cls in classes:
            try:
                if cls(session).nsfw:
                    continue
            except Exception:
                logger.debug("Failed to inspect searcher %s", cls, exc_info=True)
                continue
            key = getattr(cls, "site_key", None) or cls.__module__.split(".")[-2]
            sites.append(key)

    if not book_source_manager.sources:
        try:
            book_source_manager.load_builtin_sources()
        except Exception:
            logger.exception("Failed to load builtin Legado sources")

    unique_sites = sorted(dict.fromkeys(sites))
    return {
        "sites": unique_sites,
        "count": len(unique_sites),
        "search_site_count": len(unique_sites),
        "legado_source_count": len(book_source_manager.sources),
        "supports_url_resolution": True,
    }


@router.post("/novels/resolve-url")
async def novels_resolve_url(req: NovelResolveURLReq):
    """Resolve a supported book URL into a site/book_id pair usable by novel-downloader."""
    try:
        from novel_downloader.infra.book_url_resolver import resolve_book_url
    except ImportError:
        raise HTTPException(
            status_code=503,
            detail="novel-downloader not installed; run: pip install novel-downloader",
        )

    raw_url = req.url.strip()
    if not raw_url:
        raise HTTPException(status_code=400, detail="url is required")

    try:
        resolved = resolve_book_url(raw_url)
    except Exception as exc:
        logger.exception("Novel URL resolve failed")
        raise HTTPException(status_code=502, detail=f"Failed to resolve novel URL: {exc}")

    if not resolved:
        raise HTTPException(status_code=404, detail="Unsupported or unrecognized novel URL")

    site = str(resolved.get("site_key") or "")
    book_id = resolved.get("book_id")
    chapter_id = resolved.get("chapter_id")
    if not book_id:
        raise HTTPException(
            status_code=400,
            detail="The URL points to a chapter or unsupported entry; please paste the book detail URL instead",
        )

    source_kind = "direct"
    source_name = ""
    source_url = ""
    if site == "legado":
        try:
            from novel_downloader.plugins.sites.legado.manager import book_source_manager

            source = book_source_manager.get_source_for_url(raw_url)
            source_kind = "legado"
            if source is not None:
                source_name = str(getattr(source, "book_source_name", "") or "")
                source_url = str(getattr(source, "book_source_url", "") or "")
        except Exception:
            logger.debug("Failed to inspect resolved Legado source", exc_info=True)

    return {
        "url": raw_url,
        "site": site,
        "book_id": str(book_id),
        "chapter_id": str(chapter_id or ""),
        "source_kind": source_kind,
        "source_name": source_name,
        "source_url": source_url,
    }


@router.post("/novels/book-info")
async def novels_book_info(req: NovelBookInfoReq):
    """Fetch book metadata and full chapter catalogue for a site/book_id pair."""
    try:
        import tempfile
    except ImportError:
        raise HTTPException(
            status_code=503,
            detail="novel-downloader not installed; run: pip install novel-downloader",
        )
    try:
        with tempfile.TemporaryDirectory(prefix="nb_info_") as tmpdir:
            client = _make_nd_client(req.site, tmpdir)
            async with client:
                info = dict(await client.get_book_info(req.book_id))
    except Exception as exc:
        logger.exception("Book info fetch failed for %s/%s", req.site, req.book_id)
        raise HTTPException(status_code=502, detail=f"Failed to fetch book info: {exc}")

    volumes = []
    total_chapters = 0
    for vol in info.get("volumes") or []:
        chapters = []
        for ch in vol.get("chapters") or []:
            chapters.append({
                "chapter_id": str(ch.get("chapterId") or ch.get("chapter_id", "")),
                "title":      str(ch.get("title", "")),
                "accessible": bool(ch.get("accessible", True)),
            })
        volumes.append({
            "volume_name": str(vol.get("volume_name", "正文")),
            "chapters":    chapters,
        })
        total_chapters += len(chapters)

    return {
        "site":           req.site,
        "book_id":        req.book_id,
        "title":          str(info.get("book_name", "")),
        "author":         str(info.get("author", "")),
        "summary":        str(info.get("summary", "")),
        "cover_url":      str(info.get("cover_url", "")),
        "volumes":        volumes,
        "total_chapters": total_chapters,
    }


@router.post("/novels/fetch-import")
async def novels_fetch_import(req: NovelFetchImportReq):
    """
    Stream-download selected chapters with 3-concurrent workers and
    exponential-backoff retry (up to 3 attempts per chapter).

    Yields NDJSON lines:
      {"type": "log",      "level": "info"|"warn"|"error", "message": "..."}
      {"type": "progress", "done": N, "total": M, "chapter_title": "..."}
      {"type": "chapter",  "chapter_no": N, "chapter_id": "...", "title": "...", "content": "..."}
      {"type": "done",     "file_path": "...", "total_chapters": N, "skipped_chapters": K}
      {"type": "error",    "message": "..."}  -- only on fatal failure
    """
    try:
        import asyncio
        import tempfile
    except ImportError:
        raise HTTPException(
            status_code=503,
            detail="novel-downloader not installed; run: pip install novel-downloader",
        )

    import uuid as _uuid

    # How many chapters to fetch in parallel
    CONCURRENCY = int(os.getenv("ND_CONCURRENCY", "3"))
    # Max retry attempts per chapter (after the initial attempt)
    MAX_RETRIES = int(os.getenv("ND_MAX_RETRIES", "3"))

    async def event_generator():
        total = len(req.chapter_ids)
        # chapter_no (1-based) -> dict with title/content
        results: dict[int, dict] = {}
        skipped: set[int] = set()

        # Queue used to stream events from concurrent tasks back to the generator
        queue: asyncio.Queue = asyncio.Queue()
        # Semaphore caps concurrent in-flight chapter fetches
        sem = asyncio.Semaphore(CONCURRENCY)
        completed_count = 0

        def _log(level: str, msg: str) -> str:
            logger.info("fetch-import [%s] %s", level, msg)
            return json.dumps({"type": "log", "level": level, "message": msg}, ensure_ascii=False) + "\n"

        try:
            with tempfile.TemporaryDirectory(prefix="nb_fetch_") as tmpdir:
                client = _make_nd_client(req.site, tmpdir, workers=CONCURRENCY)

                async with client:
                    async def fetch_one(idx: int, chap_id: str) -> None:
                        """Download one chapter with retry and post result to queue."""
                        async with sem:
                            ch_data = None
                            last_exc: Optional[Exception] = None
                            for attempt in range(MAX_RETRIES + 1):
                                try:
                                    ch_data = await client.get_chapter(req.book_id, chap_id)
                                    break  # success
                                except Exception as exc:
                                    last_exc = exc
                                    if attempt < MAX_RETRIES:
                                        delay = 2 ** attempt  # 1s, 2s, 4s
                                        await queue.put(("log", "warn",
                                            f"章节 {chap_id}(序{idx}) 第{attempt+1}次失败，{delay}s后重试: {exc}"))
                                        await asyncio.sleep(delay)
                                    else:
                                        await queue.put(("log", "error",
                                            f"章节 {chap_id}(序{idx}) 已重试{MAX_RETRIES}次，跳过: {last_exc}"))
                            # Post the result (chapter data or None for skip)
                            await queue.put(("result", idx, chap_id, ch_data))

                    # Kick off all chapter fetch tasks concurrently
                    tasks = [
                        asyncio.create_task(fetch_one(idx, chap_id))
                        for idx, chap_id in enumerate(req.chapter_ids, start=1)
                    ]

                    # Drain events from queue until all tasks are done
                    pending = total
                    while pending > 0:
                        item = await queue.get()
                        if item[0] == "log":
                            _, level, msg = item
                            yield json.dumps({"type": "log", "level": level, "message": msg}, ensure_ascii=False) + "\n"

                        elif item[0] == "result":
                            _, idx, chap_id, ch_data = item
                            pending -= 1
                            completed_count += 1

                            if ch_data:
                                ch_dict = dict(ch_data)
                                results[idx] = ch_dict
                                ch_title = str(ch_dict.get("title", ""))
                            else:
                                skipped.add(idx)
                                ch_title = ""

                            yield json.dumps({
                                "type":          "progress",
                                "done":          completed_count,
                                "total":         total,
                                "chapter_title": ch_title,
                            }, ensure_ascii=False) + "\n"

                            if ch_data:
                                ch_dict = results[idx]
                                yield json.dumps({
                                    "type":       "chapter",
                                    "chapter_no": idx,
                                    "chapter_id": chap_id,
                                    "title":      str(ch_dict.get("title", "")),
                                    "content":    str(ch_dict.get("content", "")),
                                }, ensure_ascii=False) + "\n"

                    # Ensure all tasks are properly awaited
                    await asyncio.gather(*tasks, return_exceptions=True)

        except Exception as exc:
            logger.exception("fetch-import: fatal error")
            yield json.dumps({"type": "error", "message": str(exc)}, ensure_ascii=False) + "\n"
            return

        # Build the combined text file (chapters sorted by original order)
        upload_dir = os.getenv("UPLOAD_DIR", "/data/uploads")
        os.makedirs(upload_dir, exist_ok=True)
        file_name = str(_uuid.uuid4()) + ".txt"
        file_path = os.path.join(upload_dir, file_name)

        lines: list[str] = []
        if req.title:
            lines += [req.title, ""]
        if req.author:
            lines += [f"作者：{req.author}", ""]
        lines.append("")
        for idx in sorted(results.keys()):
            ch = results[idx]
            lines.append(ch.get("title", ""))
            lines.append(ch.get("content", ""))
            lines.append("")

        with open(file_path, "w", encoding="utf-8") as f:
            f.write("\n".join(lines))

        downloaded = len(results)
        n_skipped = len(skipped)
        logger.info("fetch-import done: %d downloaded, %d skipped", downloaded, n_skipped)
        yield json.dumps({
            "type":             "done",
            "file_path":        file_path,
            "total_chapters":   downloaded,
            "skipped_chapters": n_skipped,
        }, ensure_ascii=False) + "\n"

    return StreamingResponse(
        event_generator(),
        media_type="application/x-ndjson",
        headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"},
    )


