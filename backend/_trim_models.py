#!/usr/bin/env python3
"""Trim models.go to keep only core story entity types.

Types now split into separate files:
  - models_llm.go    : LLM profiles, RAG, references, quality, narrative layers
  - models_agent.go  : agent review, graph, vector, routing, audit, book rules, creative brief, anti-detect, genre templates
  - models_config.go : change propagation, prompt presets, glossary, tasks, resources, vocab, webhooks
"""
import pathlib

path = pathlib.Path("internal/models/models.go")
lines = path.read_text().splitlines(keepends=True)
total = len(lines)
print(f"models.go has {total} lines")

# Keep ranges (1-indexed, inclusive)
keep_ranges = [
    (1,   20),   # package + imports + Project struct
    (129, 367),  # WorldBible → VectorStoreEntry + "// Request/Response types" + CreateProjectRequest → MigrationConfigRequest
    (885, 887),  # RestoreChapterSnapshotRequest
    (928, 953),  # // Chapter Import section comment + ChapterImport + CreateImportRequest
    (976, 991),  # ProjectFull + UpdateProjectFanficRequest + AutoWriteRequest
]

result = []
for start, end in keep_ranges:
    seg = lines[start - 1 : end]  # convert to 0-indexed
    result.extend(seg)
    # Ensure a blank line between segments
    if result and result[-1].strip():
        result.append("\n")

# Ensure file ends with a newline
if result and result[-1] != "\n":
    result.append("\n")

path.write_text("".join(result))
print(f"Trimmed to {len(result)} lines.")
