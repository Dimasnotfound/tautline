#!/usr/bin/env python3
"""Read-only bridge between DevSpace and the installed Hermes skill loader.

The bridge intentionally imports Hermes' own discovery, compatibility, and
skill-view code. Runtime hooks that can persist state, capture secrets, or
register environment/credential passthrough are replaced with read-only
implementations before a skill is loaded.
"""

from __future__ import annotations

import hashlib
import json
import logging
import os
import re
import sys
from pathlib import Path
from typing import Any, Iterable

MAX_CONTENT_BYTES = 512 * 1024
SENSITIVE_KEY_RE = re.compile(
    r"(?:token|secret|password|passwd|api[_-]?key|access[_-]?key|private[_-]?key|"
    r"cookie|credential|authorization|client[_-]?secret)",
    re.IGNORECASE,
)
ASSIGNMENT_SECRET_RE = re.compile(
    r"(?i)(\b(?:token|secret|password|passwd|api[_-]?key|access[_-]?key|"
    r"private[_-]?key|cookie|credential|authorization|client[_-]?secret)\b"
    r"\s*[:=]\s*)(Bearer\s+[^\s,;]+|[^\s,;]+|\"[^\"]*\"|'[^']*')"
)
BEARER_RE = re.compile(r"(?i)(\bBearer\s+)[A-Za-z0-9._~+/=-]{12,}")
URL_SECRET_RE = re.compile(
    r"(?i)([?&](?:token|secret|password|api[_-]?key|access[_-]?key)\s*=)[^&#\s]+"
)


def _json_out(payload: dict[str, Any]) -> None:
    sys.stdout.write(json.dumps(payload, ensure_ascii=False, separators=(",", ":")))
    sys.stdout.flush()


def _error(message: str, **extra: Any) -> dict[str, Any]:
    return {"success": False, "error": message, **extra}


def _hermes_home() -> Path:
    configured = os.getenv("DEVSPACE_HERMES_HOME") or os.getenv("HERMES_HOME")
    if configured:
        return Path(configured).expanduser().resolve()
    local = os.getenv("LOCALAPPDATA")
    if local:
        return (Path(local) / "hermes").resolve()
    return (Path.home() / ".hermes").resolve()


def _agent_dir(home: Path) -> Path:
    configured = os.getenv("DEVSPACE_HERMES_AGENT_DIR")
    if configured:
        return Path(configured).expanduser().resolve()
    return (home / "hermes-agent").resolve()


def _configure_imports(home: Path, agent_dir: Path) -> None:
    os.environ.setdefault("HERMES_HOME", str(home))
    os.environ.setdefault("PYTHONUTF8", "1")
    os.environ.setdefault("PYTHONIOENCODING", "utf-8")
    os.environ["DEVSPACE_HERMES_READ_ONLY"] = "1"
    if str(agent_dir) not in sys.path:
        sys.path.insert(0, str(agent_dir))
    logging.disable(logging.CRITICAL)


def _snapshot_path(home: Path) -> Path:
    configured = os.getenv("DEVSPACE_HERMES_SKILLS_SNAPSHOT")
    if configured:
        return Path(configured).expanduser().resolve()
    return home / ".skills_prompt_snapshot.json"


def _snapshot_info(home: Path) -> dict[str, Any]:
    path = _snapshot_path(home)
    if not path.is_file():
        return {
            "path": str(path),
            "exists": False,
            "fingerprint": "missing",
            "bytes": 0,
            "mtime_ns": 0,
        }
    data = path.read_bytes()
    stat = path.stat()
    digest = hashlib.sha256(data).hexdigest()
    return {
        "path": str(path),
        "exists": True,
        "fingerprint": f"{stat.st_mtime_ns}:{stat.st_size}:{digest}",
        "sha256": digest,
        "bytes": stat.st_size,
        "mtime_ns": stat.st_mtime_ns,
    }


def _load_snapshot(home: Path) -> dict[str, Any]:
    path = _snapshot_path(home)
    if not path.is_file():
        return {"version": 0, "skills": [], "category_descriptions": {}}
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except Exception as exc:
        raise RuntimeError(f"invalid Hermes skill snapshot: {exc}") from exc
    if not isinstance(payload, dict):
        raise RuntimeError("Hermes skill snapshot must contain a JSON object")
    return payload


def _install_read_only_hooks(skills_tool: Any) -> None:
    def no_capture(_skill_name: str, missing_entries: list[dict[str, Any]]) -> dict[str, Any]:
        return {
            "missing_names": [str(item.get("name", "")) for item in missing_entries if item.get("name")],
            "setup_skipped": bool(missing_entries),
            "gateway_setup_hint": None,
        }

    skills_tool._capture_required_environment_variables = no_capture

    try:
        import tools.skill_manager_tool as manager

        manager.mark_background_review_skill_read = lambda _path: None
    except Exception:
        pass
    try:
        import tools.env_passthrough as passthrough

        passthrough.register_env_passthrough = lambda _names: None
    except Exception:
        pass
    try:
        import tools.credential_files as credential_files

        def inspect_only(paths: Iterable[Any]) -> list[str]:
            missing: list[str] = []
            for raw in paths:
                path = Path(os.path.expandvars(os.path.expanduser(str(raw))))
                if not path.exists():
                    missing.append(str(raw))
            return missing

        credential_files.register_credential_files = inspect_only
    except Exception:
        pass


def _imports(home: Path, agent_dir: Path) -> dict[str, Any]:
    _configure_imports(home, agent_dir)
    import tools.skills_tool as skills_tool
    from agent.skill_utils import (
        extract_skill_conditions,
        extract_skill_config_vars,
        get_all_skills_dirs,
        iter_skill_index_files,
        parse_frontmatter,
        resolve_skill_config_values,
        skill_matches_environment,
        skill_matches_platform,
        skill_matches_platform_list,
    )

    _install_read_only_hooks(skills_tool)
    return {
        "skills_tool": skills_tool,
        "extract_skill_conditions": extract_skill_conditions,
        "extract_skill_config_vars": extract_skill_config_vars,
        "get_all_skills_dirs": get_all_skills_dirs,
        "iter_skill_index_files": iter_skill_index_files,
        "parse_frontmatter": parse_frontmatter,
        "resolve_skill_config_values": resolve_skill_config_values,
        "skill_matches_environment": skill_matches_environment,
        "skill_matches_platform": skill_matches_platform,
        "skill_matches_platform_list": skill_matches_platform_list,
    }


def _as_string_list(value: Any) -> list[str]:
    if value is None:
        return []
    if not isinstance(value, list):
        value = [value]
    result: list[str] = []
    for item in value:
        text = str(item).strip()
        if text and text not in result:
            result.append(text)
    return result


def _metadata_hermes(frontmatter: dict[str, Any]) -> dict[str, Any]:
    metadata = frontmatter.get("metadata")
    if not isinstance(metadata, dict):
        return {}
    hermes = metadata.get("hermes")
    return hermes if isinstance(hermes, dict) else {}


def _description(frontmatter: dict[str, Any], body: str) -> str:
    raw = frontmatter.get("description")
    if raw:
        return str(raw).strip()
    for line in body.splitlines():
        value = line.strip()
        if value and not value.startswith("#"):
            return value
    return ""


def _scan_skill_records(api: dict[str, Any]) -> list[dict[str, Any]]:
    records: list[dict[str, Any]] = []
    seen_names: set[str] = set()
    roots: list[Path] = [Path(path).resolve() for path in api["get_all_skills_dirs"]()]
    for root_index, root in enumerate(roots):
        if not root.is_dir():
            continue
        for skill_file in api["iter_skill_index_files"](root, "SKILL.md"):
            try:
                raw = skill_file.read_text(encoding="utf-8")
                frontmatter, body = api["parse_frontmatter"](raw)
            except Exception:
                continue
            name = str(frontmatter.get("name") or skill_file.parent.name).strip()
            if not name or name in seen_names:
                continue
            try:
                relative = skill_file.relative_to(root)
                parent_parts = relative.parts[:-1]
                identifier = "/".join(parent_parts)
                category = parent_parts[0] if len(parent_parts) >= 2 else "general"
            except ValueError:
                identifier = skill_file.parent.name
                category = "general"
            hermes_meta = _metadata_hermes(frontmatter)
            tags = _as_string_list(hermes_meta.get("tags") or frontmatter.get("tags"))
            related = _as_string_list(
                hermes_meta.get("related_skills") or frontmatter.get("related_skills")
            )
            record = {
                "name": name,
                "directory_name": skill_file.parent.name,
                "identifier": identifier or name,
                "category": category,
                "description": _description(frontmatter, body),
                "platforms": _as_string_list(frontmatter.get("platforms")),
                "environments": _as_string_list(frontmatter.get("environments")),
                "conditions": api["extract_skill_conditions"](frontmatter),
                "tags": tags,
                "related_skills": related,
                "skill_path": str(skill_file),
                "skill_dir": str(skill_file.parent),
                "root_index": root_index,
                "platform_compatible": bool(api["skill_matches_platform"](frontmatter)),
                "environment_compatible": bool(api["skill_matches_environment"](frontmatter)),
            }
            seen_names.add(name)
            records.append(record)
    return records


def _condition_compatibility(
    conditions: dict[str, Any], available_tools: set[str], available_toolsets: set[str]
) -> tuple[bool, list[str]]:
    reasons: list[str] = []
    for toolset in _as_string_list(conditions.get("fallback_for_toolsets")):
        if toolset in available_toolsets:
            reasons.append(f"fallback hidden because toolset '{toolset}' is available")
    for tool in _as_string_list(conditions.get("fallback_for_tools")):
        if tool in available_tools:
            reasons.append(f"fallback hidden because tool '{tool}' is available")
    for toolset in _as_string_list(conditions.get("requires_toolsets")):
        if toolset not in available_toolsets:
            reasons.append(f"requires unavailable toolset '{toolset}'")
    for tool in _as_string_list(conditions.get("requires_tools")):
        if tool not in available_tools:
            reasons.append(f"requires unavailable tool '{tool}'")
    return not reasons, reasons


def _build_index(
    home: Path,
    api: dict[str, Any],
    available_tools: set[str],
    available_toolsets: set[str],
) -> dict[str, Any]:
    snapshot = _load_snapshot(home)
    snapshot_entries = snapshot.get("skills") if isinstance(snapshot.get("skills"), list) else []
    records = _scan_skill_records(api)
    by_name = {str(item["name"]): item for item in records}
    by_identifier = {str(item["identifier"]): item for item in records}
    by_category_dir = {
        f"{item['category']}/{item['directory_name']}": item for item in records
    }

    active_raw = json.loads(api["skills_tool"].skills_list())
    if not active_raw.get("success"):
        raise RuntimeError(str(active_raw.get("error") or "Hermes skills_list failed"))
    active_entries = active_raw.get("skills") or []
    active_names = {str(item.get("name", "")) for item in active_entries}
    active_by_name = {str(item.get("name", "")): item for item in active_entries}

    result: list[dict[str, Any]] = []
    used_names: set[str] = set()
    for snapshot_entry in snapshot_entries:
        if not isinstance(snapshot_entry, dict):
            continue
        snapshot_name = str(
            snapshot_entry.get("frontmatter_name")
            or snapshot_entry.get("skill_name")
            or ""
        ).strip()
        skill_name = str(snapshot_entry.get("skill_name") or snapshot_name).strip()
        category = str(snapshot_entry.get("category") or "general").strip() or "general"
        record = (
            by_name.get(snapshot_name)
            or by_identifier.get(f"{category}/{skill_name}")
            or by_category_dir.get(f"{category}/{skill_name}")
        )
        conditions = snapshot_entry.get("conditions") or (record or {}).get("conditions") or {}
        tool_ok, tool_reasons = _condition_compatibility(
            conditions, available_tools, available_toolsets
        )
        platforms = _as_string_list(
            snapshot_entry.get("platforms") or (record or {}).get("platforms")
        )
        platform_ok = bool(api["skill_matches_platform_list"](platforms))
        active = snapshot_name in active_names or bool(record and record["name"] in active_names)
        environment_ok = bool((record or {}).get("environment_compatible", active))
        reasons: list[str] = []
        if not platform_ok:
            reasons.append("platform is not compatible with this Windows runtime")
        if platform_ok and not environment_ok:
            reasons.append("required runtime environment is not active")
        if platform_ok and environment_ok and not active:
            reasons.append("skill is disabled or excluded by the Hermes loader")
        reasons.extend(tool_reasons)
        active_meta = active_by_name.get(snapshot_name) or active_by_name.get(
            str((record or {}).get("name", ""))
        )
        description = str(
            snapshot_entry.get("description")
            or (record or {}).get("description")
            or (active_meta or {}).get("description")
            or ""
        ).strip()
        name = str((record or {}).get("name") or snapshot_name or skill_name)
        used_names.add(name)
        result.append(
            {
                "name": name,
                "identifier": str((record or {}).get("identifier") or f"{category}/{skill_name}"),
                "category": str((record or {}).get("category") or category),
                "description": description,
                "tags": list((record or {}).get("tags") or []),
                "related_skills": list((record or {}).get("related_skills") or []),
                "platforms": platforms,
                "conditions": conditions,
                "active": active,
                "platform_compatible": platform_ok,
                "environment_compatible": environment_ok,
                "tool_compatible": tool_ok,
                "compatible": active and platform_ok and environment_ok and tool_ok,
                "compatibility_reasons": reasons,
                "skill_dir": str((record or {}).get("skill_dir") or ""),
            }
        )

    for active_entry in active_entries:
        name = str(active_entry.get("name", "")).strip()
        if not name or name in used_names:
            continue
        record = by_name.get(name)
        conditions = (record or {}).get("conditions") or {}
        tool_ok, reasons = _condition_compatibility(
            conditions, available_tools, available_toolsets
        )
        result.append(
            {
                "name": name,
                "identifier": str((record or {}).get("identifier") or name),
                "category": str(
                    active_entry.get("category") or (record or {}).get("category") or "general"
                ),
                "description": str(
                    active_entry.get("description") or (record or {}).get("description") or ""
                ).strip(),
                "tags": list((record or {}).get("tags") or []),
                "related_skills": list((record or {}).get("related_skills") or []),
                "platforms": list((record or {}).get("platforms") or []),
                "conditions": conditions,
                "active": True,
                "platform_compatible": True,
                "environment_compatible": True,
                "tool_compatible": tool_ok,
                "compatible": tool_ok,
                "compatibility_reasons": reasons,
                "skill_dir": str((record or {}).get("skill_dir") or ""),
            }
        )

    result.sort(key=lambda item: (str(item["category"]), str(item["name"])))
    compatible_count = sum(1 for item in result if item["compatible"])
    return {
        "success": True,
        "skills": result,
        "stats": {
            "installed": len(snapshot_entries),
            "discovered": len(records),
            "active": len(active_entries),
            "compatible": compatible_count,
            "categories": len({str(item["category"]) for item in result}),
        },
        "snapshot": _snapshot_info(home),
        "source": "Hermes read-only skill loader",
    }


def _sensitive_key(value: str) -> bool:
    return bool(SENSITIVE_KEY_RE.search(value or ""))


def _redact_text(text: str, known_values: Iterable[str]) -> tuple[str, bool]:
    redacted = str(text or "")
    original = redacted
    values = sorted(
        {str(value) for value in known_values if isinstance(value, str) and len(str(value)) >= 6},
        key=len,
        reverse=True,
    )
    for value in values:
        redacted = redacted.replace(value, "[REDACTED]")
    redacted = ASSIGNMENT_SECRET_RE.sub(lambda match: match.group(1) + "[REDACTED]", redacted)
    redacted = BEARER_RE.sub(lambda match: match.group(1) + "[REDACTED]", redacted)
    redacted = URL_SECRET_RE.sub(lambda match: match.group(1) + "[REDACTED]", redacted)
    return redacted, redacted != original


def _config_status(api: dict[str, Any], content: str) -> tuple[list[dict[str, Any]], list[str]]:
    try:
        frontmatter, _ = api["parse_frontmatter"](content)
        declarations = api["extract_skill_config_vars"](frontmatter)
        values = api["resolve_skill_config_values"](declarations)
    except Exception:
        return [], []
    statuses: list[dict[str, Any]] = []
    secrets: list[str] = []
    for declaration in declarations:
        key = str(declaration.get("key", ""))
        value = values.get(key)
        present = value is not None and (not isinstance(value, str) or bool(value.strip()))
        sensitive = _sensitive_key(key) or _sensitive_key(str(declaration.get("description", "")))
        statuses.append(
            {
                "key": key,
                "description": str(declaration.get("description", "")),
                "configured": bool(present),
                "sensitive": sensitive,
                "value": "[REDACTED]" if present and sensitive else "[AVAILABLE]" if present else "[NOT SET]",
            }
        )
        if sensitive and present:
            secrets.append(str(value))
    return statuses, secrets


def _required_env_secrets(payload: dict[str, Any]) -> list[str]:
    values: list[str] = []
    for item in payload.get("required_environment_variables") or []:
        if not isinstance(item, dict):
            continue
        name = str(item.get("name", ""))
        if not name:
            continue
        current = os.getenv(name)
        if current:
            values.append(current)
    return values


def _skill_secret_context(
    api: dict[str, Any], match: dict[str, Any]
) -> tuple[list[dict[str, Any]], list[str]]:
    skill_dir = Path(str(match.get("skill_dir") or ""))
    skill_md = skill_dir / "SKILL.md"
    if not skill_md.is_file():
        return [], []
    try:
        content = skill_md.read_text(encoding="utf-8")
        config, secrets = _config_status(api, content)
        frontmatter, _ = api["parse_frontmatter"](content)
        legacy, _ = api["skills_tool"]._collect_prerequisite_values(frontmatter)
        required = api["skills_tool"]._get_required_environment_variables(
            frontmatter, legacy
        )
        for item in required:
            if not isinstance(item, dict):
                continue
            name = str(item.get("name", ""))
            current = os.getenv(name) if name else None
            if current:
                secrets.append(current)
        return config, secrets
    except Exception:
        return [], []


def _bounded_content(content: str, requested: Any) -> tuple[str, bool, int]:
    try:
        limit = int(requested or 128 * 1024)
    except Exception:
        limit = 128 * 1024
    limit = max(1024, min(limit, MAX_CONTENT_BYTES))
    data = content.encode("utf-8")
    if len(data) <= limit:
        return content, False, len(data)
    clipped = data[:limit]
    while clipped:
        try:
            return clipped.decode("utf-8"), True, len(data)
        except UnicodeDecodeError:
            clipped = clipped[:-1]
    return "", True, len(data)


def _find_index_skill(index: dict[str, Any], name: str) -> dict[str, Any] | None:
    normalized = name.strip().replace("\\", "/")
    for item in index.get("skills") or []:
        if normalized in {
            str(item.get("name", "")),
            str(item.get("identifier", "")),
            f"{item.get('category', '')}/{item.get('name', '')}",
        }:
            return item
    return None


def _safe_view(
    api: dict[str, Any],
    index: dict[str, Any],
    name: str,
    file_path: str | None,
    max_bytes: Any,
    allow_incompatible: bool,
) -> dict[str, Any]:
    match = _find_index_skill(index, name)
    if match is None:
        return _error(f"skill '{name}' was not found in the Hermes index")
    if not match.get("compatible") and not allow_incompatible:
        return _error(
            f"skill '{name}' is not compatible with the current platform or DevSpace toolset",
            compatibility=match,
        )
    identifier = str(match.get("identifier") or match.get("name") or name)
    raw = api["skills_tool"].skill_view(
        identifier,
        file_path=file_path,
        preprocess=False,
    )
    payload = json.loads(raw)
    if not payload.get("success"):
        return payload
    content = str(payload.get("content") or "")
    config, context_secrets = _skill_secret_context(api, match)
    known_secrets = context_secrets + _required_env_secrets(payload)
    content, redacted = _redact_text(content, known_secrets)
    content, truncated, total_bytes = _bounded_content(content, max_bytes)
    if file_path:
        return {
            "success": True,
            "name": str(match.get("name") or payload.get("name") or name),
            "identifier": identifier,
            "category": str(match.get("category") or "general"),
            "file": str(payload.get("file") or file_path),
            "content": content,
            "file_type": str(payload.get("file_type") or Path(file_path).suffix),
            "is_binary": bool(payload.get("is_binary")),
            "bytes": total_bytes,
            "truncated": truncated,
            "redacted": redacted,
            "compatibility": match,
            "source": "Hermes read-only skill loader",
        }
    linked = payload.get("linked_files") if isinstance(payload.get("linked_files"), dict) else {}
    return {
        "success": True,
        "name": str(match.get("name") or payload.get("name") or name),
        "identifier": identifier,
        "category": str(match.get("category") or "general"),
        "description": str(payload.get("description") or match.get("description") or ""),
        "tags": payload.get("tags") or match.get("tags") or [],
        "related_skills": payload.get("related_skills") or match.get("related_skills") or [],
        "content": content,
        "linked_files": linked,
        "readiness_status": str(payload.get("readiness_status") or "available"),
        "setup_needed": bool(payload.get("setup_needed")),
        "setup_note": str(payload.get("setup_note") or ""),
        "required_environment_variables": [
            {
                "name": str(item.get("name", "")),
                "optional": bool(item.get("optional")),
                "configured": bool(os.getenv(str(item.get("name", "")))),
            }
            for item in (payload.get("required_environment_variables") or [])
            if isinstance(item, dict) and item.get("name")
        ],
        "missing_credential_files": payload.get("missing_credential_files") or [],
        "config": config,
        "bytes": total_bytes,
        "truncated": truncated,
        "redacted": redacted,
        "compatibility": match,
        "skill_dir": str(payload.get("skill_dir") or match.get("skill_dir") or ""),
        "source": "Hermes read-only skill loader",
    }


def _mutable_state(home: Path) -> dict[str, tuple[int, int] | None]:
    paths = [
        home / ".skills_prompt_snapshot.json",
        home / ".usage.json",
        home / "config.yaml",
        home / ".env",
    ]
    result: dict[str, tuple[int, int] | None] = {}
    for path in paths:
        if path.exists():
            stat = path.stat()
            result[str(path)] = (stat.st_mtime_ns, stat.st_size)
        else:
            result[str(path)] = None
    return result


def _self_test(
    home: Path,
    api: dict[str, Any],
    index: dict[str, Any],
    full_view: bool,
) -> dict[str, Any]:
    before = _mutable_state(home)
    installed = index.get("skills") or []
    failures: list[dict[str, str]] = []
    resolved = 0
    viewed = 0
    unsupported = 0
    support_files_read = 0
    for item in installed:
        identifier = str(item.get("identifier") or item.get("name") or "")
        skill_dir = Path(str(item.get("skill_dir") or ""))
        if skill_dir.is_dir() and (skill_dir / "SKILL.md").is_file():
            resolved += 1
        else:
            failures.append({"skill": identifier, "error": "SKILL.md path was not resolved"})
            continue
        if not full_view:
            continue
        raw = api["skills_tool"].skill_view(identifier, preprocess=False)
        payload = json.loads(raw)
        if payload.get("success"):
            viewed += 1
            linked = payload.get("linked_files") or {}
            candidate = None
            if isinstance(linked, dict):
                for entries in linked.values():
                    if isinstance(entries, list) and entries:
                        candidate = str(entries[0])
                        break
            if candidate:
                support = json.loads(
                    api["skills_tool"].skill_view(
                        identifier,
                        file_path=candidate,
                        preprocess=False,
                    )
                )
                if support.get("success"):
                    support_files_read += 1
                else:
                    failures.append(
                        {"skill": identifier, "error": f"support file failed: {candidate}"}
                    )
        elif str(payload.get("readiness_status")) == "unsupported" or not item.get(
            "platform_compatible", True
        ):
            unsupported += 1
        else:
            failures.append(
                {"skill": identifier, "error": str(payload.get("error") or "view failed")}
            )
    after = _mutable_state(home)
    changed = [path for path, value in before.items() if after.get(path) != value]
    if changed:
        failures.append({"skill": "bridge", "error": "read-only state changed: " + ", ".join(changed)})
    return {
        "success": not failures,
        "installed": len(installed),
        "resolved": resolved,
        "viewed": viewed,
        "expected_unsupported": unsupported,
        "support_files_read": support_files_read,
        "read_only_verified": not changed,
        "state_changes": changed,
        "failures": failures[:50],
        "snapshot": index.get("snapshot"),
    }


def main() -> None:
    try:
        request = json.loads(sys.stdin.read() or "{}")
        if not isinstance(request, dict):
            _json_out(_error("request must be a JSON object"))
            return
        home = _hermes_home()
        agent_dir = _agent_dir(home)
        if not agent_dir.is_dir():
            _json_out(_error(f"Hermes Agent directory not found: {agent_dir}"))
            return
        api = _imports(home, agent_dir)
        action = str(request.get("action") or "status")
        available_tools = set(
            _as_string_list(
                request.get("available_tools")
                or [
                    "open_workspace",
                    "read",
                    "write",
                    "edit",
                    "bash",
                    "show_changes",
                    "skills_search",
                    "skill_view",
                    "skill_read_file",
                ]
            )
        )
        available_toolsets = set(
            _as_string_list(request.get("available_toolsets") or ["terminal", "files"])
        )
        index = _build_index(home, api, available_tools, available_toolsets)
        if action in {"status", "list"}:
            if action == "status":
                index = {
                    "success": True,
                    "home": str(home),
                    "agent_dir": str(agent_dir),
                    "python": sys.executable,
                    "stats": index.get("stats"),
                    "snapshot": index.get("snapshot"),
                    "source": index.get("source"),
                }
            _json_out(index)
            return
        if action == "view":
            _json_out(
                _safe_view(
                    api,
                    index,
                    str(request.get("name") or ""),
                    None,
                    request.get("max_bytes"),
                    bool(request.get("allow_incompatible")),
                )
            )
            return
        if action == "read_file":
            _json_out(
                _safe_view(
                    api,
                    index,
                    str(request.get("name") or ""),
                    str(request.get("file_path") or ""),
                    request.get("max_bytes"),
                    bool(request.get("allow_incompatible")),
                )
            )
            return
        if action == "self_test":
            _json_out(
                _self_test(
                    home,
                    api,
                    index,
                    full_view=bool(request.get("full_view", True)),
                )
            )
            return
        _json_out(_error(f"unsupported bridge action: {action}"))
    except Exception as exc:
        _json_out(_error(f"Hermes skill bridge failed: {exc}"))


if __name__ == "__main__":
    main()
