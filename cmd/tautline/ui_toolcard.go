package main

// The visual structure and spacing in this widget are adapted from
// Waishnav/devspace (MIT License, Copyright (c) 2026 Waishnav).
// This implementation is an independent, dependency-free Go/HTML renderer.
func toolCardWidgetHTML() string {
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Tautline tool card</title>
<style>
:root {
  color-scheme: light dark;
  font-family: var(--font-sans, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif);
  background: transparent;
  color: var(--color-text-primary, #f5f5f6);
  --ds-text: var(--color-text-primary, #f5f5f6);
  --ds-text-secondary: var(--color-text-secondary, #d6d6dc);
  --ds-text-tertiary: var(--color-text-tertiary, #a3a3aa);
  --ds-border: color-mix(in srgb, var(--color-border-primary, #414141) 92%, transparent);
  --ds-header: color-mix(in srgb, var(--color-background-secondary, #272727) 96%, transparent);
  --ds-body: color-mix(in srgb, var(--color-background-primary, #181818) 98%, transparent);
  --ds-hover: color-mix(in srgb, var(--color-background-tertiary, #343434) 62%, transparent);
  --ds-icon: color-mix(in srgb, var(--color-background-primary, #0d0d0d) 98%, transparent);
  --ds-divider: color-mix(in srgb, var(--color-border-primary, #414141) 82%, transparent);
  --ds-code: var(--color-background-primary, #101114);
  --ds-success: var(--color-success-text, #6fda83);
  --ds-danger: var(--color-danger-text, #ee7676);
}
@media (prefers-color-scheme: light) {
  :root {
    --ds-text: var(--color-text-primary, #18181b);
    --ds-text-secondary: var(--color-text-secondary, #52525b);
    --ds-text-tertiary: var(--color-text-tertiary, #71717a);
    --ds-border: color-mix(in srgb, var(--color-border-primary, #d4d4d8) 92%, transparent);
    --ds-header: color-mix(in srgb, var(--color-background-secondary, #f4f4f5) 96%, transparent);
    --ds-body: color-mix(in srgb, var(--color-background-primary, #ffffff) 98%, transparent);
    --ds-hover: color-mix(in srgb, var(--color-background-tertiary, #e4e4e7) 62%, transparent);
    --ds-icon: color-mix(in srgb, var(--color-background-primary, #ffffff) 98%, transparent);
    --ds-divider: color-mix(in srgb, var(--color-border-primary, #d4d4d8) 82%, transparent);
    --ds-code: var(--color-background-primary, #fafafa);
    --ds-success: var(--color-success-text, #16803c);
    --ds-danger: var(--color-danger-text, #dc2626);
  }
}
* { box-sizing: border-box; }
html, body { width: 100%; height: auto; min-height: 0; margin: 0; background: transparent; overflow: hidden; }
body { color: var(--ds-text); }
button { font: inherit; }
.shell { width: 100%; padding: 0; overflow: hidden; }
.empty-state, .tool-card {
  width: 100%; overflow: hidden; border: 1px solid var(--ds-border);
  border-radius: 14px; background: var(--ds-header); box-shadow: none; color: var(--ds-text);
}
.empty-state { padding: 14px 16px; color: var(--ds-text-secondary); font-size: 13px; }
.empty-state.error-state { border-color: color-mix(in srgb, var(--ds-danger) 45%, var(--ds-border)); background: color-mix(in srgb, var(--ds-danger) 7%, var(--ds-header)); }
.error-title { color: var(--ds-danger); font-weight: 650; }
.error-detail { margin-top: 5px; color: var(--ds-text-secondary); overflow-wrap: anywhere; }
.tool-header {
  display: grid; grid-template-columns: 54px minmax(0, 1fr) auto 24px;
  align-items: center; gap: 14px; width: 100%; min-height: 82px; padding: 12px 16px;
  border: 0; border-radius: 13px; background: transparent; color: inherit;
  cursor: pointer; text-align: left;
}
.tool-header:hover:not(:disabled) { background: var(--ds-hover); }
.tool-header:disabled { cursor: default; }
.tool-icon {
  display: grid; width: 54px; height: 54px; place-items: center;
  border: 1px solid color-mix(in srgb, var(--ds-border) 72%, transparent);
  border-radius: 13px; background: var(--ds-icon); color: var(--ds-text);
}
.icon-svg { width: 22px; height: 22px; display: block; fill: none; stroke: currentColor; stroke-width: 1.8; stroke-linecap: round; stroke-linejoin: round; }
.tool-main { display: grid; min-width: 0; gap: 4px; }
.tool-title { color: var(--ds-text); font-size: 16px; font-weight: 500; line-height: 1.25; }
.tool-label {
  overflow: hidden; color: var(--ds-text-secondary);
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace);
  font-size: 13px; line-height: 1.35; text-overflow: ellipsis; white-space: nowrap;
}
.header-meta, .stats {
  color: var(--ds-text-secondary); font-size: 13px; line-height: 1.35;
  font-variant-numeric: tabular-nums; white-space: nowrap;
}
.header-meta { text-align: right; }
.header-meta.empty { width: 0; }
.stats { display: inline-flex; gap: 6px; align-items: center; font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); }
.add { color: var(--ds-success); }
.remove { color: var(--ds-danger); }
.chevron {
  display: grid; width: 24px; height: 24px; place-items: center; border-radius: 7px;
  color: var(--ds-text-tertiary); transition: background 140ms ease, color 140ms ease, transform 140ms ease;
}
.tool-header:hover:not(:disabled) .chevron { color: var(--ds-text); }
.chevron .icon-svg { width: 16px; height: 16px; }
.chevron.expanded { transform: rotate(180deg); }
.tool-body { border-top: 1px solid var(--ds-divider); background: var(--ds-body); }
.payload { max-height: 420px; overflow: auto; }
.code {
  margin: 0; padding: 12px 0; color: var(--ds-text-secondary); background: var(--ds-code);
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace);
  font-size: 12px; line-height: 1.55; white-space: pre; overflow-x: auto; tab-size: 2;
}
.code-line { display: block; min-height: 1.55em; padding: 0 14px; }
.code-line.addition { color: var(--ds-success); background: color-mix(in srgb, var(--ds-success) 9%, transparent); }
.code-line.deletion { color: var(--ds-danger); background: color-mix(in srgb, var(--ds-danger) 9%, transparent); }
.code-line.hunk { color: #7aa2ff; }
.code-line.meta { color: var(--ds-text-tertiary); }
.tree, .change-list { display: grid; }
.tree-row, .change-row {
  display: grid; grid-template-columns: 20px minmax(0, 1fr) auto; align-items: center;
  gap: 9px; min-height: 42px; padding: 0 16px; border-bottom: 1px solid var(--ds-divider);
}
.tree-row:last-child, .change-row:last-child { border-bottom: 0; }
.tree-icon { display: grid; place-items: center; color: var(--ds-text-tertiary); }
.tree-icon .icon-svg { width: 16px; height: 16px; }
.tree-path, .change-path {
  min-width: 0; overflow: hidden; color: var(--ds-text-secondary);
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace);
  font-size: 12px; text-overflow: ellipsis; white-space: nowrap;
}
.tree-size, .change-stats { color: var(--ds-text-tertiary); font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 12px; white-space: nowrap; }
.change-list + .code { border-top: 1px solid var(--ds-divider); }
.skill-list, .skill-sections { display: grid; }
.skill-row {
  display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 12px;
  padding: 13px 16px; border-bottom: 1px solid var(--ds-divider);
}
.skill-row:last-child { border-bottom: 0; }
.skill-copy { display: grid; min-width: 0; gap: 4px; }
.skill-name { color: var(--ds-text); font-size: 13px; font-weight: 600; line-height: 1.3; }
.skill-category, .skill-description, .skill-match, .skill-note {
  color: var(--ds-text-secondary); font-size: 12px; line-height: 1.45;
}
.skill-category, .skill-match {
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace);
}
.skill-description { overflow-wrap: anywhere; }
.skill-score { color: var(--ds-text-tertiary); font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 12px; white-space: nowrap; }
.skill-section { padding: 13px 16px; border-bottom: 1px solid var(--ds-divider); }
.skill-section:last-child { border-bottom: 0; }
.skill-section-title { margin-bottom: 7px; color: var(--ds-text); font-size: 12px; font-weight: 600; text-transform: uppercase; letter-spacing: .04em; }
.skill-tags { display: flex; flex-wrap: wrap; gap: 6px; }
.skill-badge {
  display: inline-flex; align-items: center; min-height: 22px; padding: 2px 7px;
  border: 1px solid var(--ds-divider); border-radius: 999px; color: var(--ds-text-secondary);
  background: var(--ds-header); font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 11px;
}
.skill-badge.good { color: var(--ds-success); }
.skill-badge.bad { color: var(--ds-danger); }
.skill-file-row, .skill-config-row {
  display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 10px;
  padding: 6px 0; color: var(--ds-text-secondary); font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 12px;
}
.skill-file-row + .skill-file-row, .skill-config-row + .skill-config-row { border-top: 1px solid color-mix(in srgb, var(--ds-divider) 65%, transparent); }
.skill-content { border-top: 1px solid var(--ds-divider); }
.notice { padding: 13px 16px; color: var(--ds-text-secondary); font-size: 13px; }
@media (max-width: 520px) {
  .tool-header { grid-template-columns: 42px minmax(0, 1fr) auto 18px; gap: 10px; min-height: 68px; padding: 10px 12px; }
  .tool-icon { width: 42px; height: 42px; border-radius: 10px; }
  .tool-title { font-size: 14px; }
  .header-meta { max-width: 120px; overflow: hidden; text-overflow: ellipsis; }
  .chevron { width: 18px; height: 18px; }
  .tree-row, .change-row { padding: 0 12px; }
}
@media (prefers-reduced-motion: reduce) {
  .chevron { transition: none; }
}
</style>
</head>
<body>
<main id="app" class="shell"><section class="empty-state">Waiting for Tautline result...</section></main>
<script>
const app = document.getElementById("app");
let latestData = null;
let latestIdentity = "";
let expanded = false;
let resizeFrame = 0;
let lastReportedWidth = -1;
let lastReportedHeight = -1;

function esc(value) {
  return String(value == null ? "" : value)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/\"/g, "&quot;")
    .replace(/'/g, "&#039;");
}
function number(value) { const n = Number(value); return Number.isFinite(n) ? n : 0; }
function textFromContent(content) {
  if (!Array.isArray(content)) return "";
  return content.map(function(item) {
    if (!item || typeof item !== "object") return "";
    if (item.type === "text") return String(item.text || "");
    return item.mimeType ? "[" + String(item.mimeType) + " content]" : "";
  }).filter(Boolean).join("\n");
}
function errorData(result, fallback) {
  const message = textFromContent(result && result.content) || String(fallback || "The tool returned an error without details.");
  return { kind: "error", title: "Tautline tool error", summary: message, content: message };
}
function contentBounds() {
  const content = app.firstElementChild || app;
  const rect = content.getBoundingClientRect();
  return {
    width: Math.max(1, Math.ceil(rect.width || app.getBoundingClientRect().width || 1)),
    height: Math.max(1, Math.ceil(rect.height || 1))
  };
}
function reportIntrinsicSize() {
  if (resizeFrame) cancelAnimationFrame(resizeFrame);
  resizeFrame = requestAnimationFrame(function() {
    resizeFrame = 0;
    const bounds = contentBounds();
    const width = bounds.width;
    const height = bounds.height;
    if (width === lastReportedWidth && height === lastReportedHeight) return;
    lastReportedWidth = width;
    lastReportedHeight = height;
    try {
      window.parent.postMessage({
        jsonrpc: "2.0",
        method: "ui/notifications/size-changed",
        params: { width: width, height: height }
      }, "*");
    } catch (_) {}
    try {
      if (window.openai && typeof window.openai.notifyIntrinsicHeight === "function") {
        const notified = window.openai.notifyIntrinsicHeight(height);
        if (notified && typeof notified.catch === "function") notified.catch(function() {});
      }
    } catch (_) {}
  });
}
function formatSize(bytes) {
  const n = number(bytes);
  if (n < 1024) return n + " B";
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + " KB";
  return (n / 1024 / 1024).toFixed(1) + " MB";
}
function formatDuration(milliseconds) {
  const n = number(milliseconds);
  if (n < 1000) return Math.round(n) + "ms";
  return (n / 1000).toFixed(n < 10000 ? 1 : 0) + "s";
}
function lineCount(text) { const value = String(text || ""); return value ? value.split("\n").length : 0; }
function svg(paths) {
  return '<svg class="icon-svg" viewBox="0 0 24 24" aria-hidden="true">' + paths + '</svg>';
}
const icons = {
  folder: svg('<path d="M3 6.5A2.5 2.5 0 0 1 5.5 4H9l2 2h7.5A2.5 2.5 0 0 1 21 8.5v8A2.5 2.5 0 0 1 18.5 19h-13A2.5 2.5 0 0 1 3 16.5z"/><path d="M3 9h18"/>'),
  file: svg('<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6M8 13h8M8 17h6"/>'),
  write: svg('<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6M12 12v6M9 15h6"/>'),
  edit: svg('<path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L8 18l-4 1 1-4z"/>'),
  terminal: svg('<rect x="3" y="4" width="18" height="16" rx="2"/><path d="m7 9 3 3-3 3M13 15h4"/>'),
  search: svg('<circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/>'),
  artifact: svg('<path d="M4 7h16v13H4z"/><path d="M2 3h20v4H2zM9 11h6"/>'),
  error: svg('<circle cx="12" cy="12" r="9"/><path d="M12 7v6M12 17h.01"/>'),
  diff: svg('<path d="M6 3v12M18 9v12M3 6h6M15 18h6"/><path d="m15 6 3-3 3 3M3 18l3 3 3-3"/>'),
  skills: svg('<path d="m12 3 1.2 3.8L17 8l-3.8 1.2L12 13l-1.2-3.8L7 8l3.8-1.2z"/><path d="m18 14 .8 2.2L21 17l-2.2.8L18 20l-.8-2.2L15 17l2.2-.8z"/><path d="M5 14v7M2 17.5h6"/>'),
  skill: svg('<path d="M4 5.5A2.5 2.5 0 0 1 6.5 3H20v16H6.5A2.5 2.5 0 0 0 4 21.5z"/><path d="M4 5.5v16M8 7h8M8 11h6"/>'),
  skillFile: svg('<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6M8 13h8M8 17h4"/><path d="m16.5 15.5 1 1 2-2"/>'),
  folderSmall: svg('<path d="M3 7h6l2 2h10v9H3z"/>'),
  fileSmall: svg('<path d="M6 2h8l4 4v16H6zM14 2v5h5"/>'),
  chevron: svg('<path d="m6 9 6 6 6-6"/>')
};
function descriptor(data) {
  const stats = data.stats || {};
  if (data.kind === "error") return {
    icon: icons.error, title: data.title || "Tautline tool error", label: data.summary || "The tool could not complete.",
    meta: "error", expandable: Boolean(data.content), body: "error"
  };
  if (data.kind === "workspace") return {
    icon: icons.folder, title: "Opened workspace", label: data.path || data.title || "Workspace",
    meta: number(stats.files) + " files · " + number(stats.directories) + " folders",
    expandable: Array.isArray(data.files) && data.files.length > 0, body: "workspace"
  };
  if (data.kind === "search") {
    const matches = Array.isArray(data.matches) ? data.matches : [];
    const searchStats = data.stats || {};
    return {
      icon: icons.search, title: "Searched workspace", label: data.query || "Search",
      meta: matches.length + " matches · " + number(searchStats.filesScanned) + " files",
      expandable: matches.length > 0, body: "search"
    };
  }
  if (data.kind === "file") return {
    icon: icons.file, title: "Read file", label: data.path || data.title || "File",
    meta: number(stats.lines) + " lines", expandable: Boolean(data.content), body: "file"
  };
  if (data.kind === "write") return {
    icon: icons.write, title: "Wrote file", label: data.path || data.title || "File",
    additions: number(stats.added), removals: number(stats.removed), expandable: Boolean(data.diff), body: "diff"
  };
  if (data.kind === "edit") return {
    icon: icons.edit, title: "Edited file", label: data.path || data.title || "File",
    additions: number(stats.added), removals: number(stats.removed), expandable: Boolean(data.diff), body: "diff"
  };
  if (data.kind === "command") {
    const lines = number(stats.lines) || lineCount(data.output);
    const failed = data.success === false;
    const stored = data.artifact && data.artifact.id ? " · artifact" : "";
    return {
      icon: icons.terminal, title: failed ? "Command failed" : "Ran command",
      label: data.command || data.path || "Command",
      meta: lines + " lines · " + formatDuration(stats.durationMs) + stored,
      expandable: Boolean(data.output) || Boolean(data.artifact), body: "command"
    };
  }
  if (data.kind === "artifact") {
    const matches = Array.isArray(data.matches) ? data.matches : [];
    const artifact = data.artifact || {};
    return {
      icon: icons.artifact, title: matches.length ? "Searched artifact" : "Read artifact",
      label: artifact.id || data.sourceLabel || "Artifact",
      meta: matches.length ? matches.length + " matches" : number(data.totalLines) + " lines",
      expandable: Boolean(data.content) || matches.length > 0, body: "artifact"
    };
  }
  if (data.kind === "show_changes") {
    const files = number((data.stats || {}).files);
    return {
      icon: icons.diff, title: data.empty ? "No changes" : "Changed " + files + (files === 1 ? " file" : " files"),
      label: data.path || "", additions: number((data.stats || {}).added), removals: number((data.stats || {}).removed),
      expandable: Boolean(data.diff) || (Array.isArray(data.files) && data.files.length > 0), body: "changes"
    };
  }
  if (data.kind === "skills_search") {
    const results = Array.isArray(data.results) ? data.results : [];
    const stats = data.stats || {};
    return {
      icon: icons.skills, title: data.title || "Matched Hermes skills", label: data.query || "Hermes skills",
      meta: results.length + " matches · " + number(stats.compatible) + "/" + number(stats.installed) + " compatible",
      expandable: results.length > 0, body: "skills_search"
    };
  }
  if (data.kind === "skill") {
    const stats = data.stats || {};
    return {
      icon: icons.skill, title: "Loaded skill", label: (data.category ? data.category + "/" : "") + (data.name || data.identifier || "Hermes skill"),
      meta: (data.readinessStatus || "available") + " · " + number(stats.lines) + " lines",
      expandable: Boolean(data.content) || Boolean(data.description), body: "skill"
    };
  }
  if (data.kind === "skill_file") {
    const stats = data.stats || {};
    return {
      icon: icons.skillFile, title: "Read skill file", label: (data.name || "skill") + " › " + (data.file || "file"),
      meta: number(stats.lines) + " lines", expandable: Boolean(data.content), body: "skill_file"
    };
  }
  if (data.kind === "subagents") {
    const slots = Array.isArray(data.slots) ? data.slots : [];
    const busy = slots.filter(function(slot) { return Boolean(slot.busy); }).length;
    return { icon: icons.skills, title: data.title || "Tautline sub-agents", label: data.summary || "Generic sub-agent capacity", meta: busy + " busy · " + slots.length + " total", expandable: true, body: "generic" };
  }
  if (data.kind === "agent_run") {
    const run = data.run || {};
    return { icon: icons.terminal, title: run.status === "completed" ? "Sub-agent completed" : "Sub-agent " + (run.status || "queued"), label: run.name || run.agent_id || run.id || "Temporary sub-agent", meta: run.activity || run.model || "9Router", expandable: true, body: "generic" };
  }
  if (data.kind === "browser") {
    return { icon: icons.terminal, title: data.title || "Lightpanda result", label: data.url || "Lightpanda", meta: data.summary || "", expandable: true, body: "generic" };
  }
  return { icon: icons.file, title: data.title || "Tautline", label: data.path || data.summary || "", meta: data.summary || "", expandable: true, body: "generic" };
}
function headerSummary(display) {
  if (display.additions != null || display.removals != null) {
    return '<span class="stats" aria-label="Diff statistics"><span class="add">+' + number(display.additions) + '</span><span class="remove">-' + number(display.removals) + '</span></span>';
  }
  return '<span class="header-meta' + (display.meta ? '' : ' empty') + '">' + esc(display.meta || '') + '</span>';
}
function codeLines(text, diff) {
  return String(text || "").split("\n").map(function(line) {
    let cls = "code-line";
    if (diff) {
      if (line.startsWith("+") && !line.startsWith("+++")) cls += " addition";
      else if (line.startsWith("-") && !line.startsWith("---")) cls += " deletion";
      else if (line.startsWith("@@")) cls += " hunk";
      else if (line.startsWith("diff ") || line.startsWith("---") || line.startsWith("+++")) cls += " meta";
    }
    return '<span class="' + cls + '">' + esc(line || " ") + '</span>';
  }).join("");
}
function workspaceBody(data) {
  const files = Array.isArray(data.files) ? data.files : [];
  if (!files.length) return '<div class="notice">No workspace entries available.</div>';
  return '<div class="payload tree">' + files.map(function(item) {
    const isDir = item.type === "dir";
    return '<div class="tree-row"><span class="tree-icon">' + (isDir ? icons.folderSmall : icons.fileSmall) + '</span><span class="tree-path" title="' + esc(item.path || item.name || '') + '">' + esc(item.path || item.name || '') + '</span><span class="tree-size">' + (isDir ? '' : formatSize(item.size)) + '</span></div>';
  }).join("") + '</div>';
}
function searchBody(data) {
  const matches = Array.isArray(data.matches) ? data.matches : [];
  if (!matches.length) return '<div class="notice">No matches were returned.</div>';
  return '<div class="payload skill-list">' + matches.map(function(match) {
    const location = (match.path || '') + ':' + number(match.line) + (match.column ? ':' + number(match.column) : '');
    return '<div class="skill-row"><div class="skill-copy"><div class="skill-name">' + esc(location) + '</div><div class="skill-category">sha256 ' + esc(String(match.sha256 || '').slice(0, 12)) + '</div><pre class="code">' + codeLines(match.excerpt || '', false) + '</pre></div></div>';
  }).join('') + '</div>';
}
function fileBody(data) {
  const provenance = data.sha256 ? '<div class="notice">sha256 ' + esc(data.sha256) + (data.nextCursor ? '<br>next: ' + esc(data.nextCursor) : '') + '</div>' : '';
  return '<div class="payload"><pre class="code">' + codeLines(data.content, false) + '</pre>' + provenance + '</div>';
}
function diffBody(data) { return '<div class="payload"><pre class="code">' + codeLines(data.diff, true) + '</pre></div>'; }
function commandBody(data) {
  const artifact = data.artifact || null;
  const notice = artifact ? '<div class="notice">Full redacted output: ' + esc(artifact.id || '') + ' · ' + formatSize(artifact.storedBytes || 0) + ' · ' + number(data.omittedLines || artifact.omittedLines) + ' omitted lines</div>' : (data.storageUnavailable ? '<div class="notice">Artifact storage was unavailable; inline fallback was used.</div>' : '');
  return '<div class="payload"><pre class="code">' + codeLines(data.output, false) + '</pre>' + notice + '</div>';
}
function artifactBody(data) {
  const matches = Array.isArray(data.matches) ? data.matches : [];
  if (matches.length) return searchBody(data);
  return '<div class="payload"><pre class="code">' + codeLines(data.content || '', false) + '</pre></div>';
}
function changesBody(data) {
  const files = Array.isArray(data.files) ? data.files : [];
  const list = files.length ? '<div class="change-list">' + files.map(function(file) {
    return '<div class="change-row"><span class="tree-icon">' + icons.fileSmall + '</span><span class="change-path" title="' + esc(file.path || '') + '">' + esc(file.path || '') + '</span><span class="change-stats"><span class="add">+' + number(file.added) + '</span> <span class="remove">-' + number(file.removed) + '</span></span></div>';
  }).join("") + '</div>' : '';
  const diff = data.diff ? '<pre class="code">' + codeLines(data.diff, true) + '</pre>' : '';
  return '<div class="payload">' + list + diff + (list || diff ? '' : '<div class="notice">No pending changes.</div>') + '</div>';
}
function badge(text, className) {
  return '<span class="skill-badge' + (className ? ' ' + className : '') + '">' + esc(text) + '</span>';
}
function skillSearchBody(data) {
  const results = Array.isArray(data.results) ? data.results : [];
  if (!results.length) return '<div class="notice">No compatible Hermes skills matched this task.</div>';
  return '<div class="payload skill-list">' + results.map(function(item) {
    const category = item.category || 'general';
    const matched = Array.isArray(item.matchedOn) ? item.matchedOn.join(', ') : '';
    const compatibility = item.compatible === false ? badge('incompatible', 'bad') : badge('compatible', 'good');
    return '<div class="skill-row"><div class="skill-copy"><div class="skill-name">' + esc(item.name || item.identifier || 'Skill') + '</div><div class="skill-category">' + esc(category + ' · ' + (item.identifier || item.name || '')) + '</div>' + (item.description ? '<div class="skill-description">' + esc(item.description) + '</div>' : '') + (matched ? '<div class="skill-match">matched: ' + esc(matched) + '</div>' : '') + '</div><div class="skill-score">' + compatibility + '<br>score ' + number(item.score) + '</div></div>';
  }).join('') + '</div>';
}
function skillFilesSection(linkedFiles) {
  if (!linkedFiles || typeof linkedFiles !== 'object') return '';
  const rows = [];
  Object.keys(linkedFiles).sort().forEach(function(group) {
    const files = Array.isArray(linkedFiles[group]) ? linkedFiles[group] : [];
    files.forEach(function(file) {
      rows.push('<div class="skill-file-row"><span>' + esc(file) + '</span><span>' + esc(group) + '</span></div>');
    });
  });
  if (!rows.length) return '';
  return '<section class="skill-section"><div class="skill-section-title">Supporting files</div>' + rows.join('') + '</section>';
}
function skillConfigSection(config) {
  const values = Array.isArray(config) ? config : [];
  if (!values.length) return '';
  return '<section class="skill-section"><div class="skill-section-title">Skill configuration</div>' + values.map(function(item) {
    const state = item.configured ? (item.sensitive ? '[REDACTED]' : '[AVAILABLE]') : '[NOT SET]';
    return '<div class="skill-config-row"><span title="' + esc(item.description || '') + '">' + esc(item.key || 'config') + '</span><span class="' + (item.configured ? 'add' : 'remove') + '">' + esc(state) + '</span></div>';
  }).join('') + '</section>';
}
function errorBody(data) {
  return '<div class="notice"><div class="error-title">Tool execution failed</div><div class="error-detail">' + esc(data.content || data.summary || "No error details were provided.") + '</div></div>';
}
function skillBody(data) {
  const tags = Array.isArray(data.tags) ? data.tags : [];
  const related = Array.isArray(data.relatedSkills) ? data.relatedSkills : [];
  const environments = Array.isArray(data.requiredEnvironmentVariables) ? data.requiredEnvironmentVariables : [];
  let overview = '<section class="skill-section"><div class="skill-section-title">Hermes skill</div>';
  if (data.description) overview += '<div class="skill-description">' + esc(data.description) + '</div>';
  overview += '<div class="skill-tags">' + badge(data.readinessStatus || 'available', data.setupNeeded ? 'bad' : 'good') + badge(data.category || 'general') + (data.redacted ? badge('secrets redacted', 'good') : '') + tags.map(function(tag) { return badge(tag); }).join('') + '</div>';
  if (data.setupNote) overview += '<div class="skill-note">' + esc(data.setupNote) + '</div>';
  overview += '</section>';
  let environmentSection = '';
  if (environments.length) {
    environmentSection = '<section class="skill-section"><div class="skill-section-title">Required environment</div>' + environments.map(function(item) {
      return '<div class="skill-config-row"><span>' + esc(item.name || '') + (item.optional ? ' (optional)' : '') + '</span><span class="' + (item.configured ? 'add' : 'remove') + '">' + (item.configured ? 'AVAILABLE' : 'NOT SET') + '</span></div>';
    }).join('') + '</section>';
  }
  const relatedSection = related.length ? '<section class="skill-section"><div class="skill-section-title">Related skills</div><div class="skill-tags">' + related.map(function(name) { return badge(name); }).join('') + '</div></section>' : '';
  const content = data.content ? '<div class="skill-content"><pre class="code">' + codeLines(data.content, false) + '</pre></div>' : '';
  return '<div class="payload skill-sections">' + overview + skillConfigSection(data.config) + environmentSection + skillFilesSection(data.linkedFiles) + relatedSection + content + '</div>';
}
function bodyHTML(data, display) {
  if (display.body === "error") return errorBody(data);
  if (display.body === "workspace") return workspaceBody(data);
  if (display.body === "search") return searchBody(data);
  if (display.body === "file") return fileBody(data);
  if (display.body === "diff") return diffBody(data);
  if (display.body === "command") return commandBody(data);
  if (display.body === "artifact") return artifactBody(data);
  if (display.body === "changes") return changesBody(data);
  if (display.body === "skills_search") return skillSearchBody(data);
  if (display.body === "skill") return skillBody(data);
  if (display.body === "skill_file") return fileBody(data);
  if (display.body === "generic") return '<div class="payload"><pre class="code">' + codeLines(JSON.stringify(data, null, 2), false) + '</pre></div>';
  return '<div class="notice">No details available.</div>';
}
function renderFailure(error) {
  const message = error && error.message ? error.message : String(error || "Unknown template error");
  app.innerHTML = '<section class="empty-state error-state" role="alert"><div class="error-title">Tautline template could not render</div><div class="error-detail">' + esc(message) + '</div></section>';
  reportIntrinsicSize();
}
function render() {
  try {
    if (!latestData || typeof latestData !== "object") {
      app.innerHTML = '<section class="empty-state">Waiting for Tautline result...</section>';
      reportIntrinsicSize();
      return;
    }
    const display = descriptor(latestData);
    const chevron = display.expandable ? '<span class="chevron' + (expanded ? ' expanded' : '') + '" aria-hidden="true">' + icons.chevron + '</span>' : '<span class="chevron" aria-hidden="true"></span>';
    app.innerHTML = '<section class="tool-card"><button class="tool-header" type="button" aria-expanded="' + String(expanded) + '"' + (display.expandable ? '' : ' disabled') + '><span class="tool-icon" aria-hidden="true">' + display.icon + '</span><span class="tool-main"><span class="tool-title">' + esc(display.title) + '</span>' + (display.label ? '<span class="tool-label" title="' + esc(display.label) + '">' + esc(display.label) + '</span>' : '') + '</span>' + headerSummary(display) + chevron + '</button>' + (expanded && display.expandable ? '<div class="tool-body">' + bodyHTML(latestData, display) + '</div>' : '') + '</section>';
    const header = app.querySelector('.tool-header');
    if (header && display.expandable) header.addEventListener('click', function() { expanded = !expanded; render(); });
    reportIntrinsicSize();
  } catch (error) {
    renderFailure(error);
  }
}
function extractWidgetData(payload) {
  if (!payload || typeof payload !== "object") return null;
  if (payload.error) return errorData(null, payload.error.message || payload.error);
  const envelope = payload.mcp_tool_result || payload.call_tool_result || payload;
  if (envelope && envelope.isError) return errorData(envelope);
  const metadata = envelope && envelope._meta;
  const data = metadata && metadata["tautline/widgetData"] ||
    envelope && envelope["tautline/widgetData"] ||
    payload["tautline/widgetData"] ||
    metadata && metadata["devspace/widgetData"] ||
    envelope && envelope["devspace/widgetData"] ||
    payload["devspace/widgetData"] ||
    envelope && envelope.structuredContent ||
    payload.toolOutput ||
    envelope && typeof envelope.kind === "string" && envelope || null;
  if (data && typeof data === "object" && data.isError) return errorData(data);
  if (!data && payload.status) return null;
  return data && typeof data === "object" ? data : null;
}
function receive(payload) {
  const data = extractWidgetData(payload);
  if (!data) return;
  const identity = [data.kind, data.workspaceId, data.path, data.command, data.query, data.name, data.file, data.title, data.artifact && data.artifact.id, data.matches && data.matches.length, data.diff && data.diff.length, data.output && data.output.length, data.content && data.content.length].join('|');
  if (identity !== latestIdentity) expanded = false;
  latestIdentity = identity;
  latestData = data;
  render();
}
function syncGlobals(globals) {
  if (!globals || typeof globals !== "object") return;
  if (globals.toolResponseMetadata) receive(globals.toolResponseMetadata);
  else if (globals.toolOutput) receive(globals.toolOutput);
}
if (window.openai && window.openai.toolResponseMetadata) receive(window.openai.toolResponseMetadata);
else if (window.openai && window.openai.toolOutput) receive(window.openai.toolOutput);
window.addEventListener("openai:set_globals", function(event) { syncGlobals(event.detail && event.detail.globals); }, { passive: true });
window.addEventListener("message", function(event) {
  if (event.source !== window.parent) return;
  const message = event.data;
  if (message && message.jsonrpc === "2.0" && message.method === "ui/notifications/tool-result") receive(message.params);
}, { passive: true });
window.addEventListener("error", function(event) {
  if (!latestData) renderFailure(event.error || event.message || "Template script error");
});
window.addEventListener("unhandledrejection", function(event) {
  if (!latestData) renderFailure(event.reason || "Unhandled template promise rejection");
});
if (typeof ResizeObserver === "function") {
  const resizeObserver = new ResizeObserver(reportIntrinsicSize);
  resizeObserver.observe(app);
} else {
  window.addEventListener("resize", reportIntrinsicSize, { passive: true });
}
reportIntrinsicSize();
</script>
</body>
</html>`
}
