from __future__ import annotations

import os


def parse_allowed_origins() -> list[str]:
    raw = os.getenv(
        "ALLOWED_ORIGINS",
        "http://localhost:5173,http://127.0.0.1:5173,http://localhost:4173,http://127.0.0.1:4173,http://localhost:8080,http://127.0.0.1:8080,http://localhost:3000,http://127.0.0.1:3000",
    ).strip()
    if raw == "*":
        return ["*"]

    origins: list[str] = []
    for item in raw.split(","):
        origin = item.strip()
        if origin and origin not in origins:
            origins.append(origin)
    return origins or ["http://localhost:5173", "http://127.0.0.1:5173"]
