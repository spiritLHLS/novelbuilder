"""
番茄小说网 (Fanqie Novel) 自动章节上传服务
=========================================

使用 Playwright 进行浏览器自动化，支持：
1. Cookie 认证（用户从浏览器复制 Cookie）
2. 作品列表获取
3. 单章 / 批量章节上传
4. 上传状态追踪

作者后台地址: https://fanqienovel.com/writer/home
"""

import asyncio
import json
import logging
import os
import re
import time as _time
from typing import Optional

import httpx
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel

router = APIRouter(prefix="/fanqie", tags=["fanqie"])
logger = logging.getLogger("fanqie")

# ── 常量 ──────────────────────────────────────────────────────────────────────
FANQIE_BASE = "https://fanqienovel.com"
WRITER_HOME = f"{FANQIE_BASE}/writer/home"
WRITER_LOGIN = f"{FANQIE_BASE}/writer/login"

# Playwright 浏览器实例（延迟初始化）
_playwright = None
_browser = None

# ── 请求模型 ──────────────────────────────────────────────────────────────────

class CookiesRequest(BaseModel):
    project_id: str
    cookies: str  # 浏览器 document.cookie 的原始字符串


class UploadRequest(BaseModel):
    project_id: str
    book_id: str
    title: str
    content: str
    chapter_id: str = ""  # 本地章节 ID（可选，用于状态追踪）


class BatchUploadRequest(BaseModel):
    project_id: str
    book_id: str
    chapters: list  # [{chapter_id, title, content}]


class LoginScreenshotRequest(BaseModel):
    project_id: str


# ── 工具函数 ──────────────────────────────────────────────────────────────────

def _parse_cookies(raw: str) -> list[dict]:
    """将原始 cookie 字符串解析为 Playwright 格式的 cookie 列表。"""
    cookies = []
    for pair in raw.split(";"):
        pair = pair.strip()
        if "=" not in pair:
            continue
        name, value = pair.split("=", 1)
        name = name.strip()
        value = value.strip()
        if not name:
            continue
        cookies.append({
            "name": name,
            "value": value,
            "domain": ".fanqienovel.com",
            "path": "/",
        })
    return cookies


async def _ensure_browser():
    """确保 Playwright 浏览器已启动。"""
    global _playwright, _browser
    if _browser is not None:
        return _browser

    try:
        from playwright.async_api import async_playwright
    except ImportError:
        raise HTTPException(
            503,
            "Playwright 未安装。请在 Docker 容器中安装: "
            "pip install playwright && playwright install chromium",
        )

    _playwright = await async_playwright().start()
    _browser = await _playwright.chromium.launch(
        headless=True,
        args=["--no-sandbox", "--disable-dev-shm-usage"],
    )
    return _browser


async def _create_context(cookies: list[dict]):
    """创建带有 Cookie 的浏览器上下文。"""
    browser = await _ensure_browser()
    context = await browser.new_context(
        user_agent=(
            "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "
            "AppleWebKit/537.36 (KHTML, like Gecko) "
            "Chrome/131.0.0.0 Safari/537.36"
        ),
        viewport={"width": 1440, "height": 900},
        locale="zh-CN",
    )
    if cookies:
        await context.add_cookies(cookies)
    return context


# ── Cookie 验证 ───────────────────────────────────────────────────────────────

@router.post("/validate")
async def validate_cookies(req: CookiesRequest):
    """验证 Cookie 是否有效（能否访问作者后台）。"""
    cookies = _parse_cookies(req.cookies)
    if not cookies:
        raise HTTPException(400, "Cookie 字符串为空或格式错误")

    context = await _create_context(cookies)
    try:
        page = await context.new_page()
        await page.goto(WRITER_HOME, wait_until="domcontentloaded", timeout=30000)
        await page.wait_for_timeout(2000)

        current_url = page.url
        # 如果被重定向到登录页，则 Cookie 无效
        if "login" in current_url.lower():
            return {
                "valid": False,
                "reason": "Cookie 已过期或无效，页面被重定向到登录页",
                "redirect_url": current_url,
            }

        # 尝试获取页面标题来确认登录成功
        title = await page.title()
        return {
            "valid": True,
            "page_title": title,
            "current_url": current_url,
        }
    except Exception as e:
        logger.exception("Cookie 验证失败")
        return {"valid": False, "reason": str(e)}
    finally:
        await context.close()


# ── 登录页截图（用于手动扫码） ────────────────────────────────────────────────

@router.post("/login-screenshot")
async def get_login_screenshot(req: LoginScreenshotRequest):
    """获取登录页截图（包含二维码），用于用户在前端手动扫码。"""
    context = await _create_context([])
    try:
        page = await context.new_page()
        await page.goto(WRITER_LOGIN, wait_until="domcontentloaded", timeout=30000)
        await page.wait_for_timeout(3000)

        import base64
        screenshot = await page.screenshot(full_page=False)
        b64 = base64.b64encode(screenshot).decode("utf-8")

        return {
            "screenshot": f"data:image/png;base64,{b64}",
            "url": page.url,
            "hint": "请使用番茄小说 / 头条 APP 扫描二维码登录，然后从浏览器复制 Cookie",
        }
    except Exception as e:
        logger.exception("获取登录页截图失败")
        raise HTTPException(500, f"获取登录页截图失败: {e}")
    finally:
        await context.close()


# ── 作品列表 ──────────────────────────────────────────────────────────────────

@router.post("/books")
async def list_books(req: CookiesRequest):
    """获取用户在番茄小说上的作品列表。"""
    cookies = _parse_cookies(req.cookies)
    if not cookies:
        raise HTTPException(400, "Cookie 不能为空")

    context = await _create_context(cookies)
    try:
        page = await context.new_page()
        await page.goto(WRITER_HOME, wait_until="domcontentloaded", timeout=30000)
        await page.wait_for_timeout(3000)

        current_url = page.url
        if "login" in current_url.lower():
            raise HTTPException(401, "Cookie 已过期，请重新登录获取")

        # 尝试从页面提取作品列表
        # 方法1: 尝试从页面的 JavaScript 数据中提取
        books = await page.evaluate("""() => {
            // 尝试从 window.__INITIAL_STATE__ 或类似数据中获取
            if (window.__INITIAL_STATE__) {
                return window.__INITIAL_STATE__;
            }
            // 尝试从 DOM 中提取
            const bookElements = document.querySelectorAll('[class*="book-item"], [class*="work-item"], [class*="novel-item"]');
            const books = [];
            bookElements.forEach(el => {
                const titleEl = el.querySelector('[class*="title"], [class*="name"], h3, h4');
                const linkEl = el.querySelector('a[href*="book"]');
                if (titleEl) {
                    const href = linkEl ? linkEl.getAttribute('href') : '';
                    const bookId = href ? href.match(/\\d+/) : null;
                    books.push({
                        title: titleEl.textContent.trim(),
                        book_id: bookId ? bookId[0] : '',
                        href: href,
                    });
                }
            });
            return {books: books};
        }""")

        return {
            "books": books.get("books", []) if isinstance(books, dict) else [],
            "raw_data": books,
            "page_url": current_url,
        }
    except HTTPException:
        raise
    except Exception as e:
        logger.exception("获取作品列表失败")
        raise HTTPException(500, f"获取作品列表失败: {e}")
    finally:
        await context.close()


# ── 单章上传 ──────────────────────────────────────────────────────────────────

@router.post("/upload")
async def upload_chapter(req: UploadRequest):
    """上传单个章节到番茄小说。

    流程:
    1. 打开作品的章节管理页
    2. 点击 "新建章节" 按钮
    3. 填入章节标题和正文
    4. 点击保存
    """
    # 从数据库获取 cookies（由 Go 层传递）
    # 这里由 Go handler 先查 DB，再把 cookies 放在请求体中转发
    raise HTTPException(501, "请通过 /fanqie/upload-with-cookies 端点上传")


@router.post("/upload-with-cookies")
async def upload_with_cookies(req: dict):
    """带 Cookie 的上传接口（由 Go handler 调用）。"""
    project_id = req.get("project_id", "")
    cookies_str = req.get("cookies", "")
    book_id = req.get("book_id", "")
    title = req.get("title", "")
    content = req.get("content", "")
    chapter_id = req.get("chapter_id", "")

    if not cookies_str or not book_id or not title or not content:
        raise HTTPException(400, "缺少必填参数: cookies, book_id, title, content")

    cookies = _parse_cookies(cookies_str)
    context = await _create_context(cookies)
    screenshot_b64 = ""

    try:
        page = await context.new_page()

        # 1. 导航到作品的章节管理页
        book_url = f"{FANQIE_BASE}/writer/book/{book_id}"
        await page.goto(book_url, wait_until="domcontentloaded", timeout=30000)
        await page.wait_for_timeout(2000)

        if "login" in page.url.lower():
            raise HTTPException(401, "Cookie 已过期")

        # 2. 查找 "新建章节" / "添加章节" 按钮并点击
        new_chapter_btn = (
            page.get_by_text("新建章节")
            .or_(page.get_by_text("添加章节"))
            .or_(page.get_by_text("新增章节"))
            .or_(page.get_by_text("写新章节"))
            .or_(page.locator('[class*="new-chapter"], [class*="add-chapter"], button[class*="create"]'))
        )

        try:
            await new_chapter_btn.first.click(timeout=10000)
        except Exception:
            # 尝试备用方式: 查找任何包含 "新建" 或 "添加" 的按钮
            fallback = page.locator('button, a').filter(has_text=re.compile(r"新建|添加|新增|创建"))
            if await fallback.count() > 0:
                await fallback.first.click()
            else:
                import base64
                screenshot = await page.screenshot(full_page=False)
                screenshot_b64 = base64.b64encode(screenshot).decode("utf-8")
                raise HTTPException(
                    500,
                    f"找不到新建章节按钮，当前页面: {page.url}",
                )

        await page.wait_for_timeout(2000)

        # 3. 填写章节标题
        title_input = (
            page.locator('input[placeholder*="标题"], input[placeholder*="章节"]')
            .or_(page.locator('input[type="text"]').first)
        )
        try:
            await title_input.fill(title, timeout=5000)
        except Exception:
            # 尝试通过标签查找
            title_label = page.get_by_label("标题").or_(page.get_by_label("章节名"))
            await title_label.fill(title, timeout=5000)

        # 4. 填写章节正文
        # 番茄的编辑器可能是富文本编辑器 (contenteditable div) 或 textarea
        content_filled = False

        # 尝试 textarea
        textarea = page.locator('textarea[placeholder*="正文"], textarea[placeholder*="内容"], textarea')
        if await textarea.count() > 0:
            await textarea.first.fill(content)
            content_filled = True

        # 尝试 contenteditable 区域
        if not content_filled:
            editor = page.locator(
                '[contenteditable="true"], '
                '[class*="editor-content"], '
                '[class*="ql-editor"], '
                '[class*="ProseMirror"], '
                '[role="textbox"]'
            )
            if await editor.count() > 0:
                await editor.first.click()
                # 使用 keyboard 输入以触发编辑器事件
                await page.keyboard.insert_text(content)
                content_filled = True

        # 尝试通过 JS 直接设置
        if not content_filled:
            await page.evaluate("""(text) => {
                // 尝试找到编辑器实例
                const editors = document.querySelectorAll(
                    '[contenteditable], textarea, .ql-editor, .ProseMirror'
                );
                for (const el of editors) {
                    if (el.tagName === 'TEXTAREA') {
                        el.value = text;
                        el.dispatchEvent(new Event('input', {bubbles: true}));
                    } else {
                        el.innerHTML = text.replace(/\\n/g, '<br>');
                        el.dispatchEvent(new Event('input', {bubbles: true}));
                    }
                    return true;
                }
                return false;
            }""", content)
            content_filled = True

        await page.wait_for_timeout(1000)

        # 5. 点击保存 / 发布按钮
        save_btn = (
            page.get_by_text("保存")
            .or_(page.get_by_text("发布"))
            .or_(page.get_by_text("提交"))
            .or_(page.locator('button[type="submit"]'))
        )
        try:
            await save_btn.first.click(timeout=5000)
        except Exception:
            fallback_save = page.locator('button').filter(
                has_text=re.compile(r"保存|发布|提交|确[认定]")
            )
            if await fallback_save.count() > 0:
                await fallback_save.first.click()

        await page.wait_for_timeout(3000)

        # 6. 检查结果
        import base64
        screenshot = await page.screenshot(full_page=False)
        screenshot_b64 = base64.b64encode(screenshot).decode("utf-8")

        # 尝试检测成功提示
        page_text = await page.inner_text("body")
        success_indicators = ["成功", "已保存", "已发布", "已提交"]
        is_success = any(ind in page_text for ind in success_indicators)

        return {
            "status": "success" if is_success else "uncertain",
            "chapter_id": chapter_id,
            "title": title,
            "current_url": page.url,
            "screenshot": f"data:image/png;base64,{screenshot_b64}",
            "message": "上传成功" if is_success else "操作已执行，请通过截图确认结果",
        }

    except HTTPException:
        raise
    except Exception as e:
        logger.exception("上传章节失败")
        error_data = {"status": "failed", "chapter_id": chapter_id, "error": str(e)}
        if screenshot_b64:
            error_data["screenshot"] = f"data:image/png;base64,{screenshot_b64}"
        raise HTTPException(500, json.dumps(error_data, ensure_ascii=False))
    finally:
        await context.close()


# ── 批量上传 ──────────────────────────────────────────────────────────────────

@router.post("/batch-upload")
async def batch_upload(req: dict):
    """批量上传多个章节（顺序执行，每章之间有延迟）。"""
    project_id = req.get("project_id", "")
    cookies_str = req.get("cookies", "")
    book_id = req.get("book_id", "")
    chapters = req.get("chapters", [])

    if not cookies_str or not book_id or not chapters:
        raise HTTPException(400, "缺少必填参数")

    results = []
    for i, ch in enumerate(chapters):
        try:
            result = await upload_with_cookies({
                "project_id": project_id,
                "cookies": cookies_str,
                "book_id": book_id,
                "title": ch.get("title", f"第{i+1}章"),
                "content": ch.get("content", ""),
                "chapter_id": ch.get("chapter_id", ""),
            })
            results.append(result)
        except HTTPException as e:
            results.append({
                "status": "failed",
                "chapter_id": ch.get("chapter_id", ""),
                "title": ch.get("title", ""),
                "error": e.detail,
            })
        except Exception as e:
            results.append({
                "status": "failed",
                "chapter_id": ch.get("chapter_id", ""),
                "title": ch.get("title", ""),
                "error": str(e),
            })

        # 每章上传间隔 3 秒，避免触发平台限流
        if i < len(chapters) - 1:
            await asyncio.sleep(3)

    success_count = sum(1 for r in results if r.get("status") == "success")
    return {
        "total": len(chapters),
        "success": success_count,
        "failed": len(chapters) - success_count,
        "results": results,
    }


# ── 健康检查 ──────────────────────────────────────────────────────────────────

@router.get("/health")
async def fanqie_health():
    """检查 Playwright 浏览器是否可用。"""
    try:
        browser = await _ensure_browser()
        return {
            "status": "ok",
            "browser": "chromium",
            "connected": browser.is_connected(),
        }
    except Exception as e:
        return {"status": "error", "message": str(e)}
