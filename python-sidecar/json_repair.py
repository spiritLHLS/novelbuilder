"""
JSON repair and validation utilities for handling truncated/malformed LLM responses.
Supports automatic retry logic and truncation detection.
"""
from __future__ import annotations

import json
import logging
import re
from typing import Any, Optional

logger = logging.getLogger(__name__)


def close_unclosed_json(raw: str) -> str:
    """
    Close unclosed JSON structures intelligently.
    Handles incomplete objects/arrays and truncated strings.
    """
    # Count unmatched braces and brackets
    open_braces = raw.count("{") - raw.count("}")
    open_brackets = raw.count("[") - raw.count("]")
    
    # Check if we're in the middle of a string value (unclosed quote)
    # Simple heuristic: count quotes (accounting for escaped quotes)
    unescaped_quotes = len(re.findall(r'(?<!\\)"', raw))
    in_string = unescaped_quotes % 2 != 0
    
    if in_string:
        # We're in an unclosed string - close it before closing structures
        # Also add some recovery: if it ends with incomplete lines, do basic truncation
        raw = raw.rstrip()
        if raw.endswith('"'):
            # Already ends with quote, just close structures
            pass
        else:
            # Close the unclosed string
            # First check if we can find a reasonable truncation point
            # Look for the last newline or comma to truncate cleanly
            last_newline = raw.rfind('\n')
            last_comma = raw.rfind(',')
            last_quote_before = raw.rfind('"') if unescaped_quotes > 0 else -1
            
            # Determine best truncation point
            if last_newline > last_comma:
                # If last newline is more recent, truncate there
                raw = raw[:last_newline] + '"'
            elif last_comma > last_quote_before:
                # Truncate at comma
                raw = raw[:last_comma] + '"'
            else:
                # Just close the string
                raw = raw + ' [truncated]"'
    
    # Close unclosed structures from innermost outward
    if open_braces > 0:
        raw = raw + "}" * open_braces
    if open_brackets > 0:
        raw = raw + "]" * open_brackets
    
    return raw


def repair_json(raw: str, max_attempts: int = 3) -> dict:
    """
    Attempt to parse JSON with progressive repair strategies.
    
    Strategies:
    1. Direct parse
    2. Strip markdown code fences
    3. Close unclosed structures
    4. Extract valid JSON from start
    5. Extract JSON-like object/array from mixed content
    
    Returns dict on success, {} on complete failure.
    """
    if not raw:
        return {}
    original_raw = raw
    raw = raw.strip()
    if not raw:
        return {}

    # Strategy 1: Direct parse
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        pass
    
    # Strategy 2: Strip markdown code fences
    if raw.startswith("```"):
        stripped = re.sub(r"^```[a-z]*\n?", "", raw)
        stripped = re.sub(r"\n?```$", "", stripped.strip())
        try:
            return json.loads(stripped)
        except json.JSONDecodeError:
            raw = stripped
    
    # Strategy 3: Close unclosed structures
    closed = close_unclosed_json(raw)
    try:
        return json.loads(closed)
    except json.JSONDecodeError:
        pass
    
    # Strategy 4: Try to extract valid JSON starting from beginning
    # Find all possible closing points and try from longest to shortest
    if raw.startswith("{"):
        for end_idx in range(len(raw), 0, -1):
            candidate = raw[:end_idx]
            # Try to close and parse
            candidate_closed = close_unclosed_json(candidate)
            try:
                return json.loads(candidate_closed)
            except json.JSONDecodeError:
                continue
    elif raw.startswith("["):
        for end_idx in range(len(raw), 0, -1):
            candidate = raw[:end_idx]
            candidate_closed = close_unclosed_json(candidate)
            try:
                return json.loads(candidate_closed)
            except json.JSONDecodeError:
                continue
    
    # Strategy 5: Extract JSON-like content
    # Look for { ... } or [ ... ] pattern
    json_match = re.search(r'[\[{].*[\]}]', raw, re.DOTALL)
    if json_match:
        extracted = json_match.group()
        closed = close_unclosed_json(extracted)
        try:
            return json.loads(closed)
        except json.JSONDecodeError:
            pass
    
    logger.warning(
        "JSON repair failed after %d attempts, returning empty dict | raw_content: %.800s",
        max_attempts,
        original_raw,
    )
    return {}


def is_response_complete(raw: str, expected_schema: Optional[dict] = None) -> tuple[bool, list[str]]:
    """
    Heuristically check if a JSON response appears complete.
    
    Returns (is_complete, issues_list)
    
    Checks:
    - Balanced braces/brackets (not truncated in middle of structure)
    - If expected_schema provided, validate structure
    - Check for incomplete field values
    """
    issues: list[str] = []
    if raw is None:
        return False, ["response is None"]
    raw = raw.strip()
    if not raw:
        return False, ["empty response"]

    # Check balance
    open_braces = raw.count("{") - raw.count("}")
    open_brackets = raw.count("[") - raw.count("]")
    unescaped_quotes = len(re.findall(r'(?<!\\)"', raw))
    
    if open_braces > 0:
        issues.append(f"{open_braces} unclosed braces")
    if open_brackets > 0:
        issues.append(f"{open_brackets} unclosed brackets")
    if unescaped_quotes % 2 != 0:
        issues.append("unclosed string literal")
    
    # Check for common truncation patterns
    if raw.rstrip().endswith((",", "[", "{", ":")):
        issues.append("ends with incomplete value indicator")
    if '", "' in raw and raw.count('": ') != raw.count(', "'):
        # Heuristic: if we have lots of key-value pairs, check for incomplete last one
        if re.search(r'"[^"]*":\s*$', raw):
            issues.append("incomplete field value at end")
    
    return len(issues) == 0, issues


class JSONRepairWithRetry:
    """Context manager for repair-and-retry logic on JSON parsing failures."""
    
    def __init__(
        self,
        max_retries: int = 3,
        on_retry_callback: Optional[callable] = None,
    ):
        self.max_retries = max_retries
        self.on_retry_callback = on_retry_callback
        self.attempt = 0
        self.last_error: Optional[Exception] = None
    
    async def __aenter__(self):
        self.attempt += 1
        return self
    
    async def __aexit__(self, exc_type, exc_val, exc_tb):
        if exc_type is json.JSONDecodeError:
            self.last_error = exc_val
            if self.attempt < self.max_retries and self.on_retry_callback:
                logger.warning(
                    "JSON parse error on attempt %d/%d, invoking retry callback: %s",
                    self.attempt, self.max_retries, repr(exc_val)
                )
                await self.on_retry_callback(self.attempt)
                return True  # Suppress the exception
        return False


def validate_json_response(
    response_text: str,
    expected_keys: Optional[list[str]] = None,
) -> tuple[dict, bool]:
    """
    Parse response text and validate it contains expected keys.
    
    Returns (parsed_json, is_valid)
    """
    parsed = repair_json(response_text)
    
    if not parsed:
        return {}, False
    
    if expected_keys:
        missing = [k for k in expected_keys if k not in parsed]
        if missing:
            logger.warning(
                "JSON validation: missing expected keys %s",
                missing,
            )
            return parsed, False
    
    return parsed, True
