package main

func activityWidgetHTML() string {
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Tautline activity</title>
<style>
:root {
  color-scheme: light dark;
  font-family: var(--font-sans, Arial, Helvetica, system-ui, sans-serif);
  background: transparent;
  --ink: var(--color-text-primary, #171717);
  --muted: var(--color-text-secondary, #666666);
  --faint: var(--color-text-tertiary, #8a8a8a);
  --surface: var(--color-background-primary, #ffffff);
  --surface-2: var(--color-background-secondary, #f5f5f5);
  --surface-3: #eeeeee;
  --line: var(--color-border-primary, #dedede);
  --code: #f7f7f7;
  --accent-neutral: #71717a;
  --accent-read: #2563eb;
  --accent-write: #d97706;
  --accent-edit: #ca8a04;
  --accent-create: #16a34a;
  --accent-delete: #dc2626;
  --accent-changes: #9333ea;
  --accent-command: #0891b2;
  --accent-skill: #0d9488;
  --accent-agent: #c026d3;
  --accent-error: #dc2626;
}
@media (prefers-color-scheme: dark) {
  :root {
    --ink: var(--color-text-primary, #f3f3f3);
    --muted: var(--color-text-secondary, #b4b4b4);
    --faint: var(--color-text-tertiary, #888888);
    --surface: var(--color-background-primary, #181818);
    --surface-2: var(--color-background-secondary, #202020);
    --surface-3: #282828;
    --line: var(--color-border-primary, #393939);
    --code: #151515;
    --accent-neutral: #a1a1aa;
    --accent-read: #60a5fa;
    --accent-write: #f59e0b;
    --accent-edit: #facc15;
    --accent-create: #4ade80;
    --accent-delete: #f87171;
    --accent-changes: #c084fc;
    --accent-command: #22d3ee;
    --accent-skill: #2dd4bf;
    --accent-agent: #e879f9;
    --accent-error: #f87171;
  }
}
* { box-sizing: border-box; }
html, body { width: 100%; min-width: 0; margin: 0; background: transparent; color: var(--ink); overflow: hidden; }
button { font: inherit; color: inherit; }
.monitor { width: 100%; overflow: hidden; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); }
.monitor-head { min-height: 62px; display: grid; grid-template-columns: 34px minmax(0,1fr) auto; align-items: center; gap: 12px; padding: 11px 14px; border-bottom: 1px solid var(--line); }
.mark { width: 34px; height: 34px; display: grid; place-items: center; border: 1px solid var(--ink); border-radius: 8px; }
.mark svg { width: 21px; height: 21px; display: block; }
.identity { min-width: 0; }
.identity strong { display: block; overflow: hidden; font-size: 14px; font-weight: 680; letter-spacing: -.02em; text-overflow: ellipsis; white-space: nowrap; }
.identity code { display: block; margin-top: 3px; overflow: hidden; color: var(--muted); font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.live { display: inline-flex; align-items: center; gap: 7px; color: var(--muted); font-size: 11px; white-space: nowrap; }
.live-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--accent-create); box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent-create) 13%, transparent); }
.live.offline .live-dot { background: transparent; box-shadow: inset 0 0 0 1px var(--faint); }
.summary { display: grid; grid-template-columns: minmax(0,1fr) minmax(0,1fr); border-bottom: 1px solid var(--line); background: var(--surface-2); }
.summary-item { min-width: 0; padding: 8px 12px; border-right: 1px solid var(--line); }
.summary-item:last-child { border-right: 0; }
.summary-label { display: block; color: var(--faint); font-size: 9px; letter-spacing: .07em; text-transform: uppercase; }
.summary-value { display: block; margin-top: 3px; overflow: hidden; font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 11px; font-weight: 550; text-overflow: ellipsis; white-space: nowrap; }
.workspace { height: clamp(320px, 58vh, 520px); min-height: 0; display: grid; grid-template-columns: minmax(190px, 32%) minmax(0,1fr); overflow: hidden; }
.timeline { min-height: 0; overflow-y: auto; overflow-x: hidden; overscroll-behavior: contain; scrollbar-gutter: stable; border-right: 1px solid var(--line); background: var(--surface-2); }
.timeline-list { min-width: 0; }
.event { --event-accent: var(--accent-neutral); width: 100%; min-height: 58px; position: relative; display: grid; grid-template-columns: 8px minmax(0,1fr) auto; gap: 9px; align-items: start; padding: 10px 11px 10px 13px; border: 0; border-bottom: 1px solid var(--line); background: transparent; cursor: pointer; text-align: left; }
.event::before { content: ""; position: absolute; inset: 0 auto 0 0; width: 2px; background: transparent; }
.event:hover { background: color-mix(in srgb, var(--surface) 72%, var(--event-accent) 4%); }
.event.active { background: var(--surface); }
.event.active::before { background: var(--event-accent); }
.event:focus-visible { outline: 2px solid var(--event-accent); outline-offset: -2px; }
.event-mark { width: 7px; height: 7px; margin-top: 4px; border-radius: 50%; background: var(--event-accent); }
.event.error .event-mark, .event.failed .event-mark { background: transparent; box-shadow: inset 0 0 0 1px var(--accent-error); }
.event.running .event-mark, .event.queued .event-mark { border-radius: 2px; }
.event-copy { min-width: 0; }
.event-title { display: block; overflow: hidden; font-size: 11px; font-weight: 630; text-overflow: ellipsis; white-space: nowrap; }
.event-meta { display: block; margin-top: 4px; overflow: hidden; color: var(--muted); font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.event-time { color: var(--faint); font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 9px; white-space: nowrap; }
.kind-read { --event-accent: var(--accent-read); }
.kind-write { --event-accent: var(--accent-write); }
.kind-edit { --event-accent: var(--accent-edit); }
.kind-create { --event-accent: var(--accent-create); }
.kind-delete { --event-accent: var(--accent-delete); }
.kind-changes { --event-accent: var(--accent-changes); }
.kind-command { --event-accent: var(--accent-command); }
.kind-skill { --event-accent: var(--accent-skill); }
.kind-agent { --event-accent: var(--accent-agent); }
.kind-error { --event-accent: var(--accent-error); }
.inspector { --event-accent: var(--accent-neutral); min-width: 0; min-height: 0; display: flex; flex-direction: column; overflow: hidden; background: var(--surface); }
.inspector-head { flex: 0 0 auto; min-width: 0; padding: 12px 15px 11px; border-bottom: 1px solid var(--line); box-shadow: inset 3px 0 0 var(--event-accent); background: var(--surface); }
.inspector-topline { min-width: 0; display: flex; align-items: center; gap: 8px; }
.kind-badge { flex: 0 0 auto; max-width: 100px; overflow: hidden; padding: 3px 6px; border: 1px solid color-mix(in srgb, var(--event-accent) 45%, var(--line)); border-radius: 999px; background: color-mix(in srgb, var(--event-accent) 9%, transparent); color: var(--event-accent); font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 8px; font-weight: 700; letter-spacing: .05em; text-overflow: ellipsis; text-transform: uppercase; white-space: nowrap; }
.inspector-title { min-width: 0; flex: 1 1 auto; margin: 0; overflow: hidden; font-size: 14px; font-weight: 680; letter-spacing: -.02em; text-overflow: ellipsis; white-space: nowrap; }
.inspector-time { flex: 0 0 auto; color: var(--faint); font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 9px; white-space: nowrap; }
.latest { flex: 0 0 auto; padding: 4px 8px; border: 1px solid var(--line); border-radius: 7px; background: var(--surface-2); color: var(--muted); cursor: pointer; font-size: 9px; font-weight: 650; }
.latest:hover:not(:disabled), .latest:focus-visible { border-color: var(--event-accent); color: var(--event-accent); }
.latest:disabled { cursor: default; opacity: .48; }
.inspector-summary { margin: 6px 0 0; overflow: hidden; color: var(--muted); font-size: 11px; line-height: 1.45; text-overflow: ellipsis; white-space: nowrap; }
.detail-scroll { flex: 1 1 auto; min-width: 0; min-height: 0; overflow-y: auto; overflow-x: hidden; overscroll-behavior: contain; scrollbar-gutter: stable; background: var(--surface); }
.rows { display: grid; }
.row { --row-accent: var(--accent-neutral); min-height: 40px; display: grid; grid-template-columns: 4px minmax(0,1fr) auto; align-items: center; gap: 10px; padding: 8px 15px 8px 11px; border-bottom: 1px solid var(--line); }
.row:last-child { border-bottom: 0; }
.row-accent { width: 4px; height: 18px; border-radius: 4px; background: var(--row-accent); }
.row code { overflow: hidden; font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.row small { color: var(--faint); font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 9px; white-space: nowrap; }
.row-create { --row-accent: var(--accent-create); }
.row-delete { --row-accent: var(--accent-delete); }
.row-edit, .row-changes { --row-accent: var(--accent-changes); }
.row-read { --row-accent: var(--accent-read); }
.code { margin: 0; min-width: 100%; padding: 10px 0; overflow-x: auto; background: var(--code); color: var(--muted); font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 10px; line-height: 1.58; white-space: pre; tab-size: 2; }
.code-line { display: block; min-height: 1.58em; padding: 0 15px; border-left: 2px solid transparent; }
.code-line.add, .code-line.create, .code-line.success { color: var(--accent-create); background: color-mix(in srgb, var(--accent-create) 7%, transparent); border-left-color: color-mix(in srgb, var(--accent-create) 65%, transparent); }
.code-line.del, .code-line.delete, .code-line.error { color: var(--accent-delete); background: color-mix(in srgb, var(--accent-delete) 7%, transparent); border-left-color: color-mix(in srgb, var(--accent-delete) 65%, transparent); }
.code-line.edit, .code-line.rename, .code-line.meta { color: var(--accent-changes); }
.code-line.read { color: var(--accent-read); }
.code-line.command { color: var(--accent-command); }
.code-line.warning { color: var(--accent-write); }
.empty, .failure, .loading { padding: 28px 18px; color: var(--muted); font-size: 12px; line-height: 1.5; text-align: center; }
.loading::before { content: ""; width: 12px; height: 12px; display: inline-block; margin-right: 8px; vertical-align: -2px; border: 2px solid var(--line); border-top-color: var(--event-accent); border-radius: 50%; animation: spin .8s linear infinite; }
.failure { color: var(--accent-error); }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 620px) {
  .workspace { height: min(620px, 72vh); grid-template-columns: 1fr; grid-template-rows: minmax(120px, 34%) minmax(0,1fr); }
  .timeline { border-right: 0; border-bottom: 1px solid var(--line); }
  .inspector-summary { white-space: normal; }
}
@media (prefers-reduced-motion: reduce) {
  * { scroll-behavior: auto !important; }
  .loading::before { animation: none; }
}
</style>
</head>
<body>
<main id="app" class="monitor">
  <header class="monitor-head">
    <span class="mark" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none"><path d="M4 5v14M20 5v14M4 12h16" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/><circle cx="12" cy="12" r="2.3" fill="currentColor"/></svg></span>
    <div class="identity"><strong id="workspace-name">Tautline</strong><code id="workspace-path">Opening workspace…</code></div>
    <div class="live offline" id="live"><span class="live-dot"></span><span>Connecting</span></div>
  </header>
  <section class="summary">
    <div class="summary-item"><span class="summary-label">Activity</span><strong class="summary-value" id="activity-count">0 recent</strong></div>
    <div class="summary-item"><span class="summary-label">Last update</span><strong class="summary-value" id="last-update">Waiting</strong></div>
  </section>
  <section class="workspace">
    <aside class="timeline" data-role="timeline" aria-label="Tautline activity timeline"><div class="timeline-list" id="timeline-list"><div class="empty">No activity recorded yet.</div></div></aside>
    <article class="inspector" data-role="inspector" id="inspector">
      <header class="inspector-head" id="inspector-head">
        <div class="inspector-topline"><span class="kind-badge" id="kind-badge">Ready</span><h2 class="inspector-title" id="inspector-title">Waiting for activity</h2><button class="latest" id="latest" type="button" disabled>Latest</button><time class="inspector-time" id="inspector-time"></time></div>
        <p class="inspector-summary" id="inspector-summary">Tautline will show the latest workspace action here.</p>
      </header>
      <div class="detail-scroll" id="detail"><div class="empty">Waiting for the first Tautline action.</div></div>
    </article>
  </section>
</main>
<script>
"use strict";
const app = document.getElementById("app");
const timeline = document.querySelector('[data-role="timeline"]');
const timelineList = document.getElementById("timeline-list");
const inspector = document.querySelector('[data-role="inspector"]');
const detail = document.getElementById("detail");
const workspaceName = document.getElementById("workspace-name");
const workspacePathNode = document.getElementById("workspace-path");
const activityCount = document.getElementById("activity-count");
const lastUpdate = document.getElementById("last-update");
const inspectorTitle = document.getElementById("inspector-title");
const inspectorSummary = document.getElementById("inspector-summary");
const inspectorTime = document.getElementById("inspector-time");
const kindBadge = document.getElementById("kind-badge");
const latestButton = document.getElementById("latest");
const pending = new Map();
const detailCache = new Map();
const restoredState = window.openai && window.openai.widgetState && typeof window.openai.widgetState === "object" ? window.openai.widgetState : {};
let requestID = 0;
let monitorID = String(restoredState.monitorId || "");
let monitorActive = restoredState.active !== false;
let promptMonitorBound = false;
let workspaceID = "";
let workspacePath = "";
let selectedID = String(restoredState.selectedId || "");
let pinned = Boolean(restoredState.pinned);
let snapshot = null;
let polling = false;
let queuedRefresh = false;
let pollTimer = 0;
let pollDelay = 1400;
let lastSequence = -1;
let lastTimelineSignature = "";
let lastDetailSignature = "";
let lastHeight = -1;

function esc(value) {
  return String(value == null ? "" : value).replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;").replace(/"/g,"&quot;").replace(/'/g,"&#039;");
}
function num(value) { const parsed = Number(value); return Number.isFinite(parsed) ? parsed : 0; }
function baseName(path) { return String(path || "Workspace").replace(/[\\/]+$/, "").split(/[\\/]/).pop() || path || "Workspace"; }
function timeLabel(value) {
  if (!value) return "now";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "now";
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}
function statusClass(value) {
  value = String(value || "complete").toLowerCase();
  return ["error","failed","cancelled","running","queued"].includes(value) ? value : "complete";
}
function eventTheme(event) {
  const tool = String(event && event.tool || "").toLowerCase();
  const kind = String(event && event.kind || "").toLowerCase();
  const status = String(event && event.status || "").toLowerCase();
  if (status === "error" || status === "failed" || status === "cancelled") return "error";
  if (kind === "read" || kind === "file" || kind === "search" || tool === "read" || tool === "search" || tool === "artifact_read") return "read";
  if (kind === "create" || tool.includes("create")) return "create";
  if (kind === "delete" || tool.includes("delete")) return "delete";
  if (kind === "edit" || tool === "edit") return "edit";
  if (kind === "write" || tool === "write") return "write";
  if (kind === "show_changes" || kind === "changes" || tool === "show_changes") return "changes";
  if (kind === "command" || tool === "bash") return "command";
  if (kind.includes("skill") || tool.includes("skill")) return "skill";
  if (kind.includes("agent") || tool.includes("agent") || tool.includes("subagent")) return "agent";
  return "neutral";
}
function classifyCodeLine(line, isDiff) {
  const text = String(line || "");
  const lower = text.trimStart().toLowerCase();
  if (isDiff) {
    if (text.startsWith("+") && !text.startsWith("+++")) return "add";
    if (text.startsWith("-") && !text.startsWith("---")) return "del";
    if (text.startsWith("@@") || text.startsWith("diff ") || text.startsWith("+++") || text.startsWith("---")) return "meta";
  }
  if (/^(create mode|create |created |new file)/i.test(lower)) return "create";
  if (/^(delete mode|delete |deleted |remove |removed )/i.test(lower)) return "delete";
  if (/^(rename |renamed |move |moved |modified |update |updated )/i.test(lower)) return "rename";
  if (/^(read |loaded |found |open |opened )/i.test(lower)) return "read";
  if (/^(ok\b|pass\b|passed\b|success\b|built\b|done\b)/i.test(lower)) return "success";
  if (/^(fail\b|failed\b|fatal\b|error\b|panic\b)/i.test(lower)) return "error";
  if (/^(warn\b|warning\b|skip\b|skipped\b)/i.test(lower)) return "warning";
  if (/^(\$|>|command:|running )/i.test(lower)) return "command";
  return "";
}
function extractResult(payload) {
  if (!payload || typeof payload !== "object") return null;
  if (payload.error) throw new Error(payload.error.message || String(payload.error));
  const envelope = payload.mcp_tool_result || payload.call_tool_result || payload;
  if (envelope.isError) {
    const text = Array.isArray(envelope.content) ? envelope.content.map(item => item && item.text || "").filter(Boolean).join("\n") : "Tool call failed";
    throw new Error(text || "Tool call failed");
  }
  return envelope.structuredContent || payload.toolOutput || (typeof envelope.kind === "string" ? envelope : null);
}
function persistState() {
  try {
    if (window.openai && typeof window.openai.setWidgetState === "function") window.openai.setWidgetState({ monitorId: monitorID, selectedId: selectedID, pinned, active: monitorActive });
  } catch (_) {}
}
function applyBootstrap(data) {
  const incomingMonitor = String(data.monitorId || data.monitor_id || "");
  if (!incomingMonitor) return;
  if (promptMonitorBound && monitorID && incomingMonitor !== monitorID) return;
  const changed = monitorID !== incomingMonitor;
  monitorID = incomingMonitor;
  promptMonitorBound = true;
  const id = data.workspaceId || data.workspace_id;
  workspaceID = id ? String(id) : "";
  workspacePath = String(data.path || data.workspacePath || "");
  if (changed) {
    monitorActive = true;
    clearTimeout(pollTimer);
    snapshot = null;
    selectedID = "";
    pinned = false;
    detailCache.clear();
    pollDelay = 1400;
    lastSequence = -1;
    lastTimelineSignature = "";
    lastDetailSignature = "";
  }
  persistState();
  renderIdentity();
  renderSummary();
  renderTimeline(true);
  renderInspector();
  updateLatestButton();
  updateLive(true);
  reportSize();
  refresh(true);
}
function initialWorkspace(payload) {
  try {
    const data = extractResult(payload);
    if (!data || typeof data !== "object") return;
    if (data.kind === "activity_bootstrap") {
      applyBootstrap(data);
      return;
    }
    if (data.kind === "activity_snapshot") applySnapshot(data);
  } catch (error) {
    renderFailure(error);
  }
}
function post(message) { window.parent.postMessage(message, "*"); }
function callTool(name, args) {
  if (window.openai && typeof window.openai.callTool === "function") return window.openai.callTool(name, args).then(extractResult);
  const id = ++requestID;
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    post({ jsonrpc: "2.0", id, method: "tools/call", params: { name, arguments: args } });
    setTimeout(() => {
      if (!pending.has(id)) return;
      pending.delete(id);
      reject(new Error("Tautline activity request timed out"));
    }, 10000);
  }).then(extractResult);
}
function reportSize() {
  requestAnimationFrame(() => {
    const height = Math.max(1, Math.ceil(app.getBoundingClientRect().height));
    if (height === lastHeight) return;
    lastHeight = height;
    post({ jsonrpc: "2.0", method: "ui/notifications/size-changed", params: { height } });
    try {
      if (window.openai && typeof window.openai.notifyIntrinsicHeight === "function") window.openai.notifyIntrinsicHeight(height);
    } catch (_) {}
  });
}
function schedule() {
  clearTimeout(pollTimer);
  if (monitorActive && monitorID && !document.hidden) pollTimer = setTimeout(() => refresh(false), pollDelay);
}
async function refresh(force, eventID) {
  if (!monitorID || !force && (!monitorActive || document.hidden)) return;
  if (polling) {
    queuedRefresh = queuedRefresh || force;
    return;
  }
  polling = true;
  updateLive(true);
  try {
    const args = { monitor_id: monitorID };
    const requestedID = String(eventID || (pinned && selectedID ? selectedID : ""));
    if (requestedID) args.event_id = requestedID;
    const next = await callTool("activity_snapshot", args);
    if (next && typeof next === "object") applySnapshot(next);
  } catch (error) {
    if (force || !snapshot) renderFailure(error);
    updateLive(false);
  } finally {
    polling = false;
    if (queuedRefresh && monitorActive) {
      queuedRefresh = false;
      setTimeout(() => refresh(true), 0);
    } else {
      queuedRefresh = false;
      schedule();
    }
  }
}
function applySnapshot(next) {
  const incomingMonitor = String(next.monitorId || next.monitor_id || "");
  if (incomingMonitor && monitorID && incomingMonitor !== monitorID) return;
  if (incomingMonitor) monitorID = incomingMonitor;
  monitorActive = next.active !== false;
  const sequence = num(next.sequence);
  pollDelay = sequence === lastSequence ? Math.min(5000, pollDelay + 500) : 1400;
  lastSequence = sequence;
  snapshot = next;
  if (next.workspaceId) workspaceID = String(next.workspaceId);
  workspacePath = next.workspacePath || workspacePath;
  if (next.selected && next.selected.id) {
    detailCache.set(String(next.selected.id), next.selected);
    trimDetailCache();
  }
  if (!pinned) selectedID = next.selected && next.selected.id || next.events && next.events[0] && next.events[0].id || "";
  persistState();
  renderIdentity();
  renderSummary();
  renderTimeline(false);
  renderInspector();
  updateLatestButton();
  updateLive(true);
  reportSize();
}
function trimDetailCache() {
  while (detailCache.size > 24) detailCache.delete(detailCache.keys().next().value);
}
function updateLive(online) {
  const node = document.getElementById("live");
  if (!node) return;
  const archived = !monitorActive;
  node.classList.toggle("offline", archived || !online);
  const label = node.querySelector("span:last-child");
  if (label) label.textContent = archived ? "Archived" : online ? "Live" : "Retrying";
}
function updateLatestButton() {
  latestButton.disabled = !pinned;
  latestButton.title = pinned ? "Follow the latest activity" : "Already following the latest activity";
}
function renderIdentity() {
  const hasWorkspace = Boolean(workspaceID || workspacePath);
  workspaceName.textContent = hasWorkspace ? baseName(workspacePath) : "Tautline";
  workspacePathNode.textContent = workspacePath || workspaceID || "Waiting for an active workspace";
}
function renderSummary() {
  const events = Array.isArray(snapshot && snapshot.events) ? snapshot.events : [];
  activityCount.textContent = events.length + " recent";
  lastUpdate.textContent = events[0] ? timeLabel(events[0].occurredAt) : "Waiting";
}
function timelineSignature(events) {
  return events.map(event => [event.id,event.status,event.title,event.path,event.occurredAt].join("|")).join(";");
}
function eventRows(events) {
  if (!events.length) return '<div class="empty">No activity recorded yet.</div>';
  return events.map(event => {
    const active = event.id === selectedID ? " active" : "";
    const status = statusClass(event.status);
    const theme = eventTheme(event);
    const meta = event.tool + (event.path ? " · " + event.path : "");
    return '<button class="event kind-' + esc(theme) + ' ' + status + active + '" type="button" data-event="' + esc(event.id) + '"><span class="event-mark"></span><span class="event-copy"><span class="event-title">' + esc(event.title || event.tool) + '</span><span class="event-meta">' + esc(meta) + '</span></span><span class="event-time">' + esc(timeLabel(event.occurredAt)) + '</span></button>';
  }).join("");
}
function renderTimeline(force) {
  const events = Array.isArray(snapshot && snapshot.events) ? snapshot.events : [];
  const signature = timelineSignature(events);
  if (force || signature !== lastTimelineSignature) {
    const scrollTop = timeline.scrollTop;
    timelineList.innerHTML = eventRows(events);
    timeline.scrollTop = pinned ? scrollTop : 0;
    lastTimelineSignature = signature;
  }
  updateActiveEvent();
}
function updateActiveEvent() {
  timelineList.querySelectorAll("[data-event]").forEach(button => button.classList.toggle("active", button.dataset.event === selectedID));
}
function eventByID(id) {
  const events = Array.isArray(snapshot && snapshot.events) ? snapshot.events : [];
  return events.find(event => event.id === id) || null;
}
function setInspectorTheme(theme) {
  const classes = ["kind-read","kind-write","kind-edit","kind-create","kind-delete","kind-changes","kind-command","kind-skill","kind-agent","kind-error"];
  inspector.classList.remove.apply(inspector.classList, classes);
  if (theme !== "neutral") inspector.classList.add("kind-" + theme);
}
function renderInspector() {
  const event = eventByID(selectedID) || snapshot && snapshot.selected || null;
  if (!event) {
    setInspectorTheme("neutral");
    kindBadge.textContent = "Ready";
    inspectorTitle.textContent = "Waiting for activity";
    inspectorTime.textContent = "";
    inspectorSummary.textContent = "Tautline will show the latest workspace action here.";
    if (lastDetailSignature !== "empty") {
      detail.innerHTML = '<div class="empty">Waiting for the first Tautline action.</div>';
      lastDetailSignature = "empty";
    }
    return;
  }
  const theme = eventTheme(event);
  setInspectorTheme(theme);
  kindBadge.textContent = theme === "neutral" ? String(event.kind || event.tool || "event") : theme;
  inspectorTitle.textContent = event.title || event.tool || "Tautline activity";
  inspectorTime.textContent = timeLabel(event.occurredAt);
  inspectorSummary.textContent = event.summary || event.path || "No additional summary";
  const selection = detailCache.get(String(event.id));
  const detailSignature = String(event.id) + ":" + (selection ? String(selection.sequence || event.sequence || "ready") + ":ready" : "loading");
  if (detailSignature === lastDetailSignature) return;
  detail.innerHTML = selection ? detailBody(selection) : '<div class="loading">Loading preview…</div>';
  lastDetailSignature = detailSignature;
}
function codeLines(text, isDiff) {
  return String(text || "").split("\n").map(line => {
    const cls = classifyCodeLine(line, isDiff);
    return '<span class="code-line' + (cls ? " " + cls : "") + '">' + esc(line || " ") + '</span>';
  }).join("");
}
function rowTheme(item, fallback) {
  const status = String(item && (item.status || item.type || item.action) || fallback || "").toLowerCase();
  if (status.includes("delete") || status.includes("remove")) return "delete";
  if (status.includes("create") || status.includes("add") || status === "file") return "create";
  if (status.includes("edit") || status.includes("change") || status.includes("rename")) return "changes";
  if (status.includes("read") || status.includes("folder") || status.includes("compatible")) return "read";
  return "edit";
}
function rows(items, labelKey, meta, fallbackTheme) {
  if (!Array.isArray(items) || !items.length) return "";
  return '<div class="rows">' + items.map(item => {
    const label = item[labelKey] || item.path || item.name || "";
    const theme = rowTheme(item, fallbackTheme);
    return '<div class="row row-' + esc(theme) + '"><span class="row-accent"></span><code title="' + esc(label) + '">' + esc(label) + '</code><small>' + esc(meta(item)) + '</small></div>';
  }).join("") + '</div>';
}
function detailBody(selection) {
  const data = selection && selection.payload || {};
  const kind = selection && selection.kind || data.kind || "";
  if (data.detailOmitted) return '<div class="empty">The full payload exceeded the safe activity limit. A compact preview is shown.</div>' + rows(data.files, "path", item => "+" + num(item.added) + " −" + num(item.removed), "changes");
  if (kind === "workspace") return rows(data.files, "path", item => item.type === "dir" ? "folder" : num(item.size) + " B", "read") || '<div class="empty">Workspace opened.</div>';
  if (kind === "search") return rows(data.matches, "path", item => "line " + num(item.line), "read") || '<div class="empty">No matches.</div>';
  if (kind === "file" || kind === "skill_file") return '<pre class="code">' + codeLines(data.content || "", false) + '</pre>';
  if (kind === "write" || kind === "edit") return '<pre class="code">' + codeLines(data.diff || "", true) + '</pre>';
  if (kind === "show_changes") return rows(data.files, "path", item => "+" + num(item.added) + " −" + num(item.removed), "changes") + (data.diff ? '<pre class="code">' + codeLines(data.diff, true) + '</pre>' : '');
  if (kind === "command") return '<pre class="code">' + codeLines(data.output || "", false) + '</pre>';
  if (kind === "artifact") return '<pre class="code">' + codeLines(data.content || data.output || "", false) + '</pre>';
  if (kind === "skills_search") return rows(data.results, "name", item => item.compatible === false ? "incompatible" : "compatible", "read");
  if (kind === "agent_run" && data.run) return '<pre class="code">' + codeLines(JSON.stringify(data.run, null, 2), false) + '</pre>';
  return '<pre class="code">' + codeLines(JSON.stringify(data, null, 2), false) + '</pre>';
}
function handleTimelineClick(event) {
  const button = event.target.closest("[data-event]");
  if (!button || !timelineList.contains(button)) return;
  const id = String(button.dataset.event || "");
  if (!id) return;
  selectedID = id;
  pinned = true;
  persistState();
  updateLatestButton();
  updateActiveEvent();
  renderInspector();
  if (!detailCache.has(id)) refresh(true, id);
}
function followLatest() {
  pinned = false;
  const events = Array.isArray(snapshot && snapshot.events) ? snapshot.events : [];
  selectedID = events[0] && events[0].id || "";
  lastDetailSignature = "";
  persistState();
  updateLatestButton();
  updateActiveEvent();
  renderInspector();
  refresh(true);
}
timeline.addEventListener("click", handleTimelineClick);
latestButton.addEventListener("click", followLatest);
function renderFailure(error) {
  const message = error && error.message ? error.message : String(error || "Unknown activity error");
  setInspectorTheme("error");
  kindBadge.textContent = "Error";
  inspectorTitle.textContent = "Tautline activity is unavailable";
  inspectorTime.textContent = "";
  inspectorSummary.textContent = "The monitor will retry automatically.";
  detail.innerHTML = '<section class="failure">' + esc(message) + '</section>';
  lastDetailSignature = "error:" + message;
  reportSize();
}
function receiveMessage(message) {
  if (!message || typeof message !== "object") return;
  if (message.id && pending.has(message.id)) {
    const request = pending.get(message.id);
    pending.delete(message.id);
    if (message.error) request.reject(new Error(message.error.message || "Tool call failed"));
    else request.resolve(message.result);
    return;
  }
  if (message.jsonrpc === "2.0" && message.method === "ui/notifications/tool-result") initialWorkspace(message.params);
}
window.addEventListener("message", event => {
  if (event.source !== window.parent) return;
  receiveMessage(event.data);
}, { passive: true });
window.addEventListener("openai:set_globals", event => {
  const globals = event.detail && event.detail.globals;
  if (!globals) return;
  initialWorkspace(globals.toolOutput);
  initialWorkspace(globals.toolResponseMetadata);
}, { passive: true });
document.addEventListener("visibilitychange", () => {
  if (document.hidden) clearTimeout(pollTimer);
  else if (monitorActive) refresh(true);
});
if (window.openai) {
  initialWorkspace(window.openai.toolOutput);
  initialWorkspace(window.openai.toolResponseMetadata);
}
setTimeout(() => { if (!snapshot && monitorID) refresh(true); }, 0);
updateLatestButton();
if (typeof ResizeObserver === "function") new ResizeObserver(reportSize).observe(app);
reportSize();
</script>
</body>
</html>`
}
