#!/usr/bin/env python3
"""Reproduce the DevSpace workspace payload benchmark.

The byte comparison uses only the Python standard library. If `tiktoken` is
installed, the script also reports o200k_base token counts.
"""

from __future__ import annotations

import json
from typing import Any


def make_files(count: int = 350) -> list[dict[str, Any]]:
    files: list[dict[str, Any]] = []
    for index in range(count):
        is_directory = index % 7 == 0
        item: dict[str, Any] = {
            "name": f"item-{index:03d}" + ("" if is_directory else ".go"),
            "path": f"internal/module-{index // 25:02d}/item-{index:03d}"
            + ("" if is_directory else ".go"),
            "type": "dir" if is_directory else "file",
        }
        if not is_directory:
            item["size"] = 1200 + index
        files.append(item)
    return files


def compact_json(value: object) -> str:
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"))


def reduction(full: int, compact: int) -> float:
    return (1 - compact / full) * 100


def main() -> None:
    files = make_files()
    stats = {
        "entries": 350,
        "directories": 50,
        "files": 300,
        "bytes": 412_350,
    }
    full_view = {
        "kind": "workspace",
        "title": "sample-repository",
        "summary": "300 files and 50 directories",
        "path": "C:/projects/sample-repository",
        "files": files,
        "stats": stats,
        "truncated": False,
    }
    model_view = {**full_view, "files": files[:20], "truncated": True}

    full_json = compact_json(full_view)
    model_json = compact_json(model_view)
    print(f"Full workspace JSON:    {len(full_json):,} bytes")
    print(f"Model-visible JSON:     {len(model_json):,} bytes")
    print(f"Payload reduction:      {reduction(len(full_json), len(model_json)):.1f}%")

    try:
        import tiktoken  # type: ignore
    except ImportError:
        print("Token measurement:      skipped (install tiktoken to enable)")
        return

    encoding = tiktoken.get_encoding("o200k_base")
    full_tokens = len(encoding.encode(full_json))
    model_tokens = len(encoding.encode(model_json))
    print(f"Full workspace tokens:  {full_tokens:,}")
    print(f"Model-visible tokens:   {model_tokens:,}")
    print(f"Token reduction:        {reduction(full_tokens, model_tokens):.1f}%")


if __name__ == "__main__":
    main()
