from __future__ import annotations

import os
import re
import sqlite3
import uuid
from typing import Iterable


_PG_PLACEHOLDER_RE = re.compile(r"%s")


class SQLiteCompatConnection:
    def __init__(self, path: str):
        directory = os.path.dirname(os.path.abspath(path))
        if directory:
            os.makedirs(directory, exist_ok=True)
        self._conn = sqlite3.connect(path, check_same_thread=False)
        self._conn.row_factory = sqlite3.Row

    def cursor(self, cursor_factory=None):
        return SQLiteCompatCursor(self._conn.cursor())

    def commit(self):
        self._conn.commit()

    def rollback(self):
        self._conn.rollback()

    def close(self):
        self._conn.close()


class SQLiteCompatCursor:
    def __init__(self, cursor: sqlite3.Cursor):
        self._cursor = cursor

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc, tb):
        self.close()
        return False

    def execute(self, query: str, params: Iterable | None = None):
        sql, args = normalize_sqlite(query, tuple(params or ()))
        self._cursor.execute(sql, args)
        return self

    def fetchone(self):
        row = self._cursor.fetchone()
        return dict(row) if isinstance(row, sqlite3.Row) else row

    def fetchall(self):
        return [dict(row) if isinstance(row, sqlite3.Row) else row for row in self._cursor.fetchall()]

    def close(self):
        self._cursor.close()


def normalize_sqlite(query: str, params: tuple) -> tuple[str, tuple]:
    sql = query
    args = list(params)

    if "gen_random_uuid()" in sql:
        sql = sql.replace("gen_random_uuid()", "?")
        args.insert(0, str(uuid.uuid4()))

    sql = sql.replace("quarantine_zone.", "")
    sql = sql.replace("profile->>'core_traits'", "json_extract(profile, '$.core_traits')")
    sql = _replace_world_rules_query(sql)
    sql = _PG_PLACEHOLDER_RE.sub("?", sql)
    return sql, tuple(args)


def _replace_world_rules_query(sql: str) -> str:
    if "jsonb_array_elements(immutable_rules)" in sql:
        return """
            SELECT CASE
                     WHEN json_type(elem.value) = 'object' THEN COALESCE(json_extract(elem.value, '$.rule'), '')
                     ELSE elem.value
                   END AS rule
            FROM world_bible_constitutions,
                 json_each(immutable_rules) AS elem
            WHERE project_id = ?
        """
    if "jsonb_array_elements(mutable_rules)" in sql:
        return """
            SELECT CASE
                     WHEN json_type(elem.value) = 'object' THEN COALESCE(json_extract(elem.value, '$.rule'), '')
                     ELSE elem.value
                   END AS rule
            FROM world_bible_constitutions,
                 json_each(mutable_rules) AS elem
            WHERE project_id = ?
        """
    return sql
