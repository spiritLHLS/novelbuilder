# JSON Parsing Error Handling - Comprehensive Fix Documentation

## Problem Summary

The novelbuilder system experienced JSON parsing failures when LLM responses were truncated or malformed. This typically happened during deep analysis of complex character/world structures, resulting in errors like:

```
WARNING:python-agent:LLM JSON parse failed after repair, returning empty dict | raw_content: {
  "characters": [
    {
      "name": "莱恩",
      ...
      "relationships": [
        {"name": "忒弥斯"  // <- TRUNCATED HERE
```

The JSON was being cut off mid-value, and the original repair logic couldn't handle incomplete nested objects/strings.

## Root Causes Identified

1. **Insufficient max_tokens**: LLM responses were being truncated because `max_tokens` (4096) was insufficient for complex character parsing with many relationships
2. **Inadequate JSON repair logic**: Original repair only closed unmatched braces/brackets, couldn't handle:
   - Truncated string values mid-sentence
   - Incomplete objects within arrays
   - Mixed markdown code fences
3. **No response validation**: No check to detect if response was incomplete before attempting JSON parsing
4. **No automatic retry on truncation**: When truncation was detected, there was no retry with adjusted parameters
5. **Scattered repair logic**: JSON repair code was duplicated in 3 different files (routes_deep_analysis.py, routes_audit.py, routes_analysis.py, main.py)

## Solutions Implemented

### 1. Centralized JSON Repair Utility (`json_repair.py`)

Created a new module with intelligent multi-strategy repair logic:

**Functions:**
- `close_unclosed_json(raw: str) -> str`: Intelligently closes unclosed structures
  - Handles unclosed string values by finding reasonable truncation points
  - Closes unmatched braces and brackets
  - Preserves data integrity where possible

- `repair_json(raw: str) -> dict`: Progressive repair with 5 strategies:
  1. Direct parse (if already valid)
  2. Strip markdown code fences
  3. Close unclosed structures
  4. Extract valid JSON from beginning (try all possible end points)
  5. Extract JSON-like content from mixed text

- `is_response_complete(raw: str) -> tuple[bool, list]`: Response validation
  - Checks for balanced braces/brackets
  - Detects unclosed string literals
  - Identifies common truncation patterns

- `validate_json_response()`: Wrapper for schema validation

### 2. Increased max_tokens

| File | Before | After | Rationale |
|------|--------|-------|-----------|
| `routes_deep_analysis.py` LLMConfig | 4096 | 6000 | Complex character relationships with detailed descriptions |

This prevents most truncations for typical use cases.

### 3. Response Completeness Validation

Added checks in `routes_deep_analysis.py` _llm_extract():
- Before parsing JSON, validate that response isn't truncated
- If incomplete and retries remain, retry with reduced max_tokens
- Reduced tokens = 70% of original (max 2048, min 1500)
- Log specific truncation issues for debugging

### 4. Automatic Retry on Incomplete Responses

Enhanced `_llm_extract()` retry logic:
```python
if not is_complete and attempt < max_retries:
    # Reduce max_tokens and retry
    reduced_tokens = max(int(cfg.max_tokens * 0.7), 2048)
    # Retry with exponential backoff
    await asyncio.sleep(delay)
    delay = min(delay * 1.5, 30.0)
    continue
```

### 5. Consolidated Repair Usage

Updated all 4 files to use the centralized `repair_json()`:
- `routes_deep_analysis.py`: ImportedRepair logic, enhanced _parse_json()
- `routes_audit.py`: Added wrapper _repair_json() using centralized repair
- `routes_analysis.py`: Updated _repair_json() to use centralized repair
- `main.py`: Updated _repair_json() to use centralized repair

## Files Modified

### New Files
- `python-sidecar/json_repair.py` - 180 lines, centralized repair logic

### Modified Files
- `python-sidecar/routes_deep_analysis.py`:
  - Added `from json_repair import repair_json, is_response_complete`
  - Increased LLMConfig max_tokens from 4096 → 6000
  - Updated _parse_json() to use new repair logic
  - Enhanced _llm_extract() with completeness validation and retry

- `python-sidecar/routes_audit.py`:
  - Added `from json_repair import repair_json`
  - Added _repair_json() wrapper function

- `python-sidecar/routes_analysis.py`:
  - Added `from json_repair import repair_json`
  - Updated _repair_json() to use centralized repair

- `python-sidecar/main.py`:
  - Added `from json_repair import repair_json`
  - Updated _repair_json() to use centralized repair

### Test Files
- `python-sidecar/test_json_repair.py` - Comprehensive test suite (7 test functions)

## Behavior Changes

### Before
- Truncated JSON → JSON parsing error → empty dict returned silently
- No indication that data was lost
- One-shot attempt, no retry on truncation
- Four different JSON repair implementations

### After
- Truncated JSON → Logged warning about incompleteness
- Automatic retry with reduced max_tokens
- Multi-stage repair with fallback strategies
- Single authoritative repair logic used everywhere

## Testing

Run the comprehensive test suite:
```bash
python3 test_json_repair.py
```

Tests cover:
- Truncated character relationships (the original error case) ✓
- Markdown code fence handling ✓
- Unclosed structures and strings ✓
- Complex nested JSON ✓
- Valid JSON passthrough ✓
- Response completeness detection ✓

All tests pass successfully.

## Backward Compatibility

✓ All existing code paths continue to work
✓ API signatures unchanged
✓ Default behavior preserved
✓ Only adds logging and retry, doesn't change success paths

## Monitoring & Debugging

New logging enhancements:
```
logger.warning("Response appears incomplete on attempt X/Y: [issues]")
logger.warning("LLM response incomplete on attempt X/Y: [issues]. Retrying...")
logger.warning("JSON repair failed after attempts, returning empty dict | raw_content: [sample]")
```

Key metrics to monitor:
- Count of incomplete responses detected
- Count of successful retries on truncation
- Count of empty dicts returned (indicates severe truncation)

## Future Improvements

Suggested next steps:
1. **Adaptive max_tokens**: Calculate optimal max_tokens based on content length
   ```python
   optimal_max = min(max(content_chars * 2, 6000), 8000)
   ```

2. **Streaming responses**: Use streaming + partial JSON parsing to avoid truncation entirely

3. **Schema-aware repair**: Validate against expected schema during repair
   ```python
   if expected_schema:
       validate_missing_keys(repaired, expected_schema)
   ```

4. **Cache completeness patterns**: Track which LLM models tend to truncate at specific content sizes

5. **Alternative formatters**: If JSON consistently fails, try YAML or structured text formats

## References

- Original error: Character analysis endpoint returning incomplete JSON
- Affected endpoints: `/deep-analyze/start`, `/narrative-revise`, `/import-chapters/analyze`
- Configuration: `LLMConfig.max_tokens` in routes_deep_analysis.py
