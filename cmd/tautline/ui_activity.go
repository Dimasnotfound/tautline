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
  --muted: var(--color-text-secondary, #686868);
  --faint: var(--color-text-tertiary, #8a8a8a);
  --surface: var(--color-background-primary, #ffffff);
  --surface-2: var(--color-background-secondary, #f6f6f6);
  --line: var(--color-border-primary, #dedede);
  --code: var(--color-background-secondary, #f7f7f7);
}
@media (prefers-color-scheme: dark) {
  :root {
    --ink: var(--color-text-primary, #f3f3f3);
    --muted: var(--color-text-secondary, #b4b4b4);
    --faint: var(--color-text-tertiary, #888888);
    --surface: var(--color-background-primary, #181818);
    --surface-2: var(--color-background-secondary, #222222);
    --line: var(--color-border-primary, #3a3a3a);
    --code: var(--color-background-secondary, #202020);
  }
}
* { box-sizing: border-box; }
html, body { width: 100%; min-width: 0; margin: 0; background: transparent; color: var(--ink); overflow: hidden; }
button { font: inherit; color: inherit; }
.monitor { width: 100%; overflow: hidden; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); }
.monitor-head { min-height: 64px; display: grid; grid-template-columns: 34px minmax(0,1fr) auto; align-items: center; gap: 12px; padding: 12px 14px; border-bottom: 1px solid var(--line); }
.mark { width: 34px; height: 34px; display: grid; place-items: center; border: 1px solid var(--ink); border-radius: 8px; }
.mark svg { width: 21px; height: 21px; display: block; }
.identity { min-width: 0; }
.identity strong { display: block; overflow: hidden; font-size: 14px; font-weight: 650; letter-spacing: -.02em; text-overflow: ellipsis; white-space: nowrap; }
.identity code { display: block; margin-top: 3px; overflow: hidden; color: var(--muted); font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.live { display: inline-flex; align-items: center; gap: 7px; color: var(--muted); font-size: 11px; white-space: nowrap; }
.live-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--ink); }
.live.offline .live-dot { background: transparent; box-shadow: inset 0 0 0 1px var(--faint); }
.summary { display: grid; grid-template-columns: repeat(3,minmax(0,1fr)); border-bottom: 1px solid var(--line); background: var(--surface-2); }
.summary div { min-width: 0; padding: 9px 12px; border-right: 1px solid var(--line); }
.summary div:last-child { border-right: 0; }
.summary span { display: block; color: var(--faint); font-size: 9px; letter-spacing: .07em; text-transform: uppercase; }
.summary strong { display: block; margin-top: 3px; overflow: hidden; font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 11px; font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
.workspace { display: grid; grid-template-columns: minmax(190px, 32%) minmax(0,1fr); min-height: 260px; max-height: 520px; }
.timeline { overflow: auto; border-right: 1px solid var(--line); background: var(--surface-2); }
.event { width: 100%; min-height: 58px; display: grid; grid-template-columns: 8px minmax(0,1fr) auto; gap: 9px; align-items: start; padding: 10px 11px; border: 0; border-bottom: 1px solid var(--line); background: transparent; cursor: pointer; text-align: left; }
.event:hover, .event.active { background: var(--surface); }
.event:focus-visible, .control:focus-visible { outline: 2px solid var(--ink); outline-offset: -2px; }
.event-mark { width: 7px; height: 7px; margin-top: 4px; border-radius: 50%; background: var(--ink); }
.event.error .event-mark, .event.failed .event-mark { background: transparent; box-shadow: inset 0 0 0 1px var(--ink); }
.event.running .event-mark, .event.queued .event-mark { border-radius: 2px; }
.event-copy { min-width: 0; }
.event-title { display: block; overflow: hidden; font-size: 11px; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.event-meta { display: block; margin-top: 4px; overflow: hidden; color: var(--muted); font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.event-time { color: var(--faint); font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 9px; white-space: nowrap; }
.inspector { min-width: 0; overflow: auto; background: var(--surface); }
.inspector-head { position: sticky; top: 0; z-index: 2; display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; padding: 13px 15px; border-bottom: 1px solid var(--line); background: var(--surface); }
.inspector-copy { min-width: 0; }
.inspector-copy h2 { margin: 0; font-size: 14px; font-weight: 650; letter-spacing: -.02em; }
.inspector-copy p { margin: 5px 0 0; color: var(--muted); font-size: 11px; line-height: 1.45; overflow-wrap: anywhere; }
.controls { display: flex; gap: 6px; }
.control { min-height: 30px; padding: 6px 9px; border: 1px solid var(--line); border-radius: 6px; background: var(--surface); cursor: pointer; font-size: 10px; }
.control:hover { background: var(--surface-2); }
.detail { min-width: 0; }
.facts { display: flex; flex-wrap: wrap; gap: 6px; padding: 10px 15px; border-bottom: 1px solid var(--line); }
.fact { padding: 4px 7px; border: 1px solid var(--line); border-radius: 999px; color: var(--muted); font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 9px; }
.rows { display: grid; }
.row { min-height: 38px; display: grid; grid-template-columns: minmax(0,1fr) auto; align-items: center; gap: 12px; padding: 8px 15px; border-bottom: 1px solid var(--line); }
.row:last-child { border-bottom: 0; }
.row code { overflow: hidden; font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.row small { color: var(--faint); font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 9px; white-space: nowrap; }
.code { margin: 0; padding: 11px 0; overflow: auto; background: var(--code); color: var(--muted); font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace); font-size: 10px; line-height: 1.55; white-space: pre; tab-size: 2; }
.code-line { display: block; min-height: 1.55em; padding: 0 15px; }
.code-line.add { color: var(--ink); font-weight: 600; background: color-mix(in srgb, var(--ink) 5%, transparent); }
.code-line.del { color: var(--faint); text-decoration: line-through; }
.code-line.meta { color: var(--faint); }
.empty, .failure { padding: 28px 18px; color: var(--muted); font-size: 12px; line-height: 1.5; text-align: center; }
.failure { color: var(--ink); }
@media (max-width: 620px) {
  .workspace { grid-template-columns: 1fr; max-height: 620px; }
  .timeline { max-height: 190px; border-right: 0; border-bottom: 1px solid var(--line); }
  .summary { grid-template-columns: 1fr 1fr; }
  .summary div:nth-child(2) { border-right: 0; }
  .summary div:last-child { grid-column: span 2; border-top: 1px solid var(--line); }
  .inspector-head { position: static; }
}
@media (prefers-reduced-motion: reduce) { * { scroll-behavior: auto !important; } }
</style>
</head>
<body>
<main id="app" class="monitor"><section class="empty">Opening Tautline activity…</section></main>
<script>
"use strict";
const app = document.getElementById("app");
const pending = new Map();
let requestID = 0;
let workspaceID = "";
let workspacePath = "";
let selectedID = "";
let pinned = false;
let snapshot = null;
let polling = false;
let pollTimer = 0;
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
function initialWorkspace(payload) {
  try {
    const data = extractResult(payload);
    if (!data || typeof data !== "object") return;
    const id = data.workspaceId || data.workspace_id;
    if (!id) return;
    workspaceID = String(id);
    workspacePath = String(data.path || workspacePath || "");
    refresh(true);
  } catch (error) {
    renderFailure(error);
  }
}
function post(message) { window.parent.postMessage(message, "*"); }
function callTool(name, args) {
  if (window.openai && typeof window.openai.callTool === "function") {
    return window.openai.callTool(name, args).then(extractResult);
  }
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
  if (!document.hidden) pollTimer = setTimeout(() => refresh(false), 1800);
}
async function refresh(force) {
  if (!workspaceID || polling || (document.hidden && !force)) return;
  polling = true;
  updateLive(true);
  try {
    const args = { workspace_id: workspaceID };
    if (pinned && selectedID) args.event_id = selectedID;
    const next = await callTool("activity_snapshot", args);
    if (next && typeof next === "object") {
      snapshot = next;
      workspacePath = next.workspacePath || workspacePath;
      if (!pinned && next.selected) selectedID = next.selected.id || "";
      render();
    }
  } catch (error) {
    if (force || !snapshot) renderFailure(error);
    updateLive(false);
  } finally {
    polling = false;
    schedule();
  }
}
function updateLive(online) {
  const node = document.getElementById("live");
  if (!node) return;
  node.classList.toggle("offline", !online);
  const label = node.querySelector("span:last-child");
  if (label) label.textContent = online ? "Live" : "Retrying";
}
function eventRows(events) {
  if (!events.length) return '<div class="empty">No activity recorded yet.</div>';
  return events.map(event => {
    const active = event.id === selectedID ? " active" : "";
    const status = statusClass(event.status);
    return '<button class="event ' + status + active + '" type="button" data-event="' + esc(event.id) + '"><span class="event-mark"></span><span class="event-copy"><span class="event-title">' + esc(event.title || event.tool) + '</span><span class="event-meta">' + esc(event.tool) + (event.path ? ' · ' + esc(event.path) : '') + '</span></span><span class="event-time">' + esc(timeLabel(event.occurredAt)) + '</span></button>';
  }).join("");
}
function facts(event) {
  const values = [event.tool, event.status, event.kind];
  const stats = event.stats || {};
  Object.keys(stats).slice(0, 5).forEach(key => values.push(key + " " + stats[key]));
  return '<div class="facts">' + values.filter(Boolean).map(value => '<span class="fact">' + esc(value) + '</span>').join("") + '</div>';
}
function codeLines(text, isDiff) {
  return String(text || "").split("\n").map(line => {
    let cls = "code-line";
    if (isDiff) {
      if (line.startsWith("+") && !line.startsWith("+++")) cls += " add";
      else if (line.startsWith("-") && !line.startsWith("---")) cls += " del";
      else if (line.startsWith("@@") || line.startsWith("diff ") || line.startsWith("+++") || line.startsWith("---")) cls += " meta";
    }
    return '<span class="' + cls + '">' + esc(line || " ") + '</span>';
  }).join("");
}
function rows(items, labelKey, meta) {
  if (!Array.isArray(items) || !items.length) return "";
  return '<div class="rows">' + items.map(item => '<div class="row"><code title="' + esc(item[labelKey] || item.path || item.name || "") + '">' + esc(item[labelKey] || item.path || item.name || "") + '</code><small>' + esc(meta(item)) + '</small></div>').join("") + '</div>';
}
function detailBody(selection) {
  const data = selection && selection.payload || {};
  const kind = selection && selection.kind || data.kind || "";
  if (data.detailOmitted) return '<div class="empty">The full payload was larger than the activity monitor limit. The safe summary remains available.</div>' + rows(data.files, "path", item => "+" + num(item.added) + " −" + num(item.removed));
  if (kind === "workspace") return rows(data.files, "path", item => item.type === "dir" ? "folder" : num(item.size) + " B") || '<div class="empty">Workspace opened.</div>';
  if (kind === "search") return rows(data.matches, "path", item => "line " + num(item.line)) || '<div class="empty">No matches.</div>';
  if (kind === "file" || kind === "skill_file") return '<pre class="code">' + codeLines(data.content || "", false) + '</pre>';
  if (kind === "write" || kind === "edit") return '<pre class="code">' + codeLines(data.diff || "", true) + '</pre>';
  if (kind === "show_changes") return rows(data.files, "path", item => "+" + num(item.added) + " −" + num(item.removed)) + (data.diff ? '<pre class="code">' + codeLines(data.diff, true) + '</pre>' : '');
  if (kind === "command") return '<pre class="code">' + codeLines(data.output || "", false) + '</pre>';
  if (kind === "artifact") return '<pre class="code">' + codeLines(data.content || data.output || "", false) + '</pre>';
  if (kind === "skills_search") return rows(data.results, "name", item => item.compatible === false ? "incompatible" : "compatible");
  if (kind === "agent_run" && data.run) return '<pre class="code">' + codeLines(JSON.stringify(data.run, null, 2), false) + '</pre>';
  return '<pre class="code">' + codeLines(JSON.stringify(data, null, 2), false) + '</pre>';
}
function render() {
  const events = Array.isArray(snapshot && snapshot.events) ? snapshot.events : [];
  const selected = snapshot && snapshot.selected || null;
  const path = snapshot && snapshot.workspacePath || workspacePath;
  const latest = events[0] || null;
  const changeStats = selected && selected.kind === "show_changes" ? selected.stats || {} : {};
  app.innerHTML = '<header class="monitor-head"><span class="mark" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none"><path d="M4 5v14M20 5v14M4 12h16" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/><circle cx="12" cy="12" r="2.3" fill="currentColor"/></svg></span><div class="identity"><strong>' + esc(baseName(path)) + '</strong><code>' + esc(path || workspaceID) + '</code></div><div class="live" id="live"><span class="live-dot"></span><span>Live</span></div></header>' +
    '<section class="summary"><div><span>Activity</span><strong>' + events.length + ' recent</strong></div><div><span>Changes</span><strong>' + (changeStats.files ? changeStats.files + ' files · +' + num(changeStats.added) + ' −' + num(changeStats.removed) : 'No review selected') + '</strong></div><div><span>Last update</span><strong>' + esc(latest ? timeLabel(latest.occurredAt) : 'Waiting') + '</strong></div></section>' +
    '<section class="workspace"><aside class="timeline" aria-label="Tautline activity timeline">' + eventRows(events) + '</aside><article class="inspector">' + (selected ? '<header class="inspector-head"><div class="inspector-copy"><h2>' + esc(selected.title || selected.tool) + '</h2><p>' + esc(selected.summary || selected.path || "No additional summary") + '</p></div><div class="controls"><button class="control" id="latest" type="button">Latest</button><button class="control" id="refresh" type="button">Refresh</button></div></header>' + facts(selected) + '<div class="detail">' + detailBody(selected) + '</div>' : '<div class="empty">Waiting for the first Tautline action.</div>') + '</article></section>';
  app.querySelectorAll("[data-event]").forEach(button => button.addEventListener("click", () => {
    selectedID = button.dataset.event || "";
    pinned = true;
    refresh(true);
  }));
  const latestButton = document.getElementById("latest");
  if (latestButton) latestButton.addEventListener("click", () => { pinned = false; selectedID = ""; refresh(true); });
  const refreshButton = document.getElementById("refresh");
  if (refreshButton) refreshButton.addEventListener("click", () => refresh(true));
  reportSize();
}
function renderFailure(error) {
  const message = error && error.message ? error.message : String(error || "Unknown activity error");
  app.innerHTML = '<section class="failure"><strong>Tautline activity is unavailable.</strong><br>' + esc(message) + '</section>';
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
  if (globals) initialWorkspace(globals.toolResponseMetadata || globals.toolOutput);
}, { passive: true });
document.addEventListener("visibilitychange", () => {
  if (document.hidden) clearTimeout(pollTimer);
  else refresh(true);
});
if (window.openai) initialWorkspace(window.openai.toolResponseMetadata || window.openai.toolOutput);
if (typeof ResizeObserver === "function") new ResizeObserver(reportSize).observe(app);
reportSize();
</script>
</body>
</html>`
}
