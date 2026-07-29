package main

import "strings"

func widgetDocument(title, renderer string) string {
	return strings.Join([]string{
		"<!doctype html>",
		`<html lang="en">`,
		"<head>",
		`<meta charset="utf-8">`,
		`<meta name="viewport" content="width=device-width,initial-scale=1">`,
		"<title>" + title + "</title>",
		"<style>", widgetCSS, "</style>",
		"</head>",
		"<body>",
		`<main id="app" class="shell"><div class="empty">Waiting for Tautline result…</div></main>`,
		"<script>", widgetBridgeJS, renderer, "</script>",
		"</body>",
		"</html>",
	}, "\n")
}

const widgetCSS = `
:root {
  color-scheme: light dark;
  --surface: #ffffff;
  --surface-subtle: #f7f7f8;
  --surface-raised: #ffffff;
  --border: #e4e4e7;
  --border-strong: #d4d4d8;
  --text: #18181b;
  --muted: #71717a;
  --accent: #2563eb;
  --success: #15803d;
  --success-soft: #f0fdf4;
  --danger: #b91c1c;
  --danger-soft: #fef2f2;
  --warning: #a16207;
  --warning-soft: #fefce8;
  --code: #111827;
  --code-text: #e5e7eb;
  --code-muted: #94a3b8;
}
@media (prefers-color-scheme: dark) {
  :root {
    --surface: #171717;
    --surface-subtle: #202020;
    --surface-raised: #242424;
    --border: #343434;
    --border-strong: #454545;
    --text: #f4f4f5;
    --muted: #a1a1aa;
    --accent: #7aa2ff;
    --success: #4ade80;
    --success-soft: #052e16;
    --danger: #f87171;
    --danger-soft: #450a0a;
    --warning: #facc15;
    --warning-soft: #422006;
    --code: #0b0f19;
    --code-text: #e5e7eb;
    --code-muted: #94a3b8;
  }
}
* { box-sizing: border-box; }
html, body { width: 100%; height: auto; min-height: 0; }
body {
  margin: 0;
  background: transparent;
  color: var(--text);
  font: 13px/1.5 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}
button { font: inherit; }
.shell { padding: 12px; }
.hero { display: flex; align-items: flex-start; gap: 11px; }
.hero-icon {
  width: 34px; height: 34px; flex: 0 0 auto;
  display: grid; place-items: center;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--surface-subtle);
  color: var(--text);
}
.hero-icon svg {
  width: 18px; height: 18px; fill: none; stroke: currentColor;
  stroke-width: 1.8; stroke-linecap: round; stroke-linejoin: round;
}
.hero-copy { min-width: 0; flex: 1; }
.eyebrow { color: var(--muted); font-size: 10px; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
.title { margin: 1px 0 0; font-size: 15px; line-height: 1.35; font-weight: 700; overflow-wrap: anywhere; }
.summary { margin-top: 3px; color: var(--muted); overflow-wrap: anywhere; }
.path, .command {
  margin-top: 10px; padding: 8px 10px;
  border: 1px solid var(--border); border-radius: 9px;
  background: var(--surface-subtle);
  font: 12px/1.45 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  overflow-wrap: anywhere;
}
.stat-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 7px; margin-top: 10px; }
.stat { padding: 8px 9px; border: 1px solid var(--border); border-radius: 9px; background: var(--surface-raised); }
.stat-value { font-size: 14px; line-height: 1.2; font-weight: 700; }
.stat-label { margin-top: 2px; color: var(--muted); font-size: 10px; text-transform: uppercase; letter-spacing: .04em; }
.toolbar { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-top: 11px; }
.section-title { color: var(--muted); font-size: 10px; font-weight: 700; letter-spacing: .07em; text-transform: uppercase; }
.actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 6px; }
.btn {
  min-height: 30px; padding: 5px 10px;
  border: 1px solid var(--border-strong); border-radius: 8px;
  background: var(--surface-raised); color: var(--text);
  cursor: pointer; font-size: 12px; font-weight: 600;
}
.btn:hover { background: var(--surface-subtle); }
.btn.primary { border-color: var(--accent); background: var(--accent); color: #fff; }
.btn.primary:hover { filter: brightness(.96); }
.badges { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 9px; }
.badge {
  display: inline-flex; align-items: center; gap: 5px;
  padding: 3px 7px; border: 1px solid var(--border); border-radius: 999px;
  color: var(--muted); background: var(--surface-subtle); font-size: 11px;
}
.badge.success { color: var(--success); background: var(--success-soft); }
.badge.danger { color: var(--danger); background: var(--danger-soft); }
.badge.warning { color: var(--warning); background: var(--warning-soft); }
.list { margin-top: 7px; border: 1px solid var(--border); border-radius: 10px; overflow: hidden; }
.row {
  display: grid; grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px; align-items: center; min-height: 34px;
  padding: 7px 9px; border-bottom: 1px solid var(--border);
}
.row:last-child { border-bottom: 0; }
.row-main { min-width: 0; display: flex; align-items: center; gap: 7px; }
.row-icon { width: 15px; color: var(--muted); flex: 0 0 auto; }
.row-name { min-width: 0; font: 12px/1.4 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; overflow-wrap: anywhere; }
.row-meta { color: var(--muted); white-space: nowrap; font-size: 11px; }
.code {
  margin: 7px 0 0; padding: 10px 0;
  border: 1px solid var(--border); border-radius: 10px;
  background: var(--code); color: var(--code-text);
  font: 12px/1.55 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  white-space: pre; overflow-x: auto; tab-size: 2;
}
.code-line { display: block; min-height: 1.55em; padding: 0 10px; }
.code-number { display: inline-block; width: 4.5em; margin-right: 10px; color: var(--code-muted); text-align: right; user-select: none; }
.diff-add { color: var(--success); background: color-mix(in srgb, var(--success) 8%, transparent); }
.diff-del { color: var(--danger); background: color-mix(in srgb, var(--danger) 8%, transparent); }
.diff-hunk { color: var(--accent); }
.diff-meta { color: var(--code-muted); }
.notice { margin-top: 9px; padding: 8px 10px; border: 1px solid var(--border); border-radius: 9px; color: var(--muted); background: var(--surface-subtle); }
.notice.warning { color: var(--warning); background: var(--warning-soft); }
.empty { padding: 20px 12px; border: 1px dashed var(--border); border-radius: 10px; color: var(--muted); text-align: center; }
html[data-mode="fullscreen"] .shell { max-width: 1120px; margin: 0 auto; padding: 18px; }
html[data-mode="fullscreen"] .title { font-size: 17px; }
html[data-mode="fullscreen"] .row { min-height: 38px; }
@media (max-width: 560px) {
  .shell { padding: 10px; }
  .stat-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .row { grid-template-columns: 1fr; gap: 2px; }
  .row-meta { padding-left: 22px; white-space: normal; }
  .toolbar { align-items: flex-start; flex-direction: column; }
  .actions { justify-content: flex-start; }
}
`

const widgetBridgeJS = `
const app = document.getElementById("app");
let latestData = null;
let renderer = null;
let displayMode = window.openai?.displayMode || "inline";
document.documentElement.dataset.mode = displayMode;

function esc(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/\"/g, "&quot;")
    .replace(/'/g, "&#039;");
}

function formatSize(bytes) {
  const n = Number(bytes || 0);
  if (n < 1024) return n + " B";
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + " KB";
  return (n / 1024 / 1024).toFixed(1) + " MB";
}

function formatDuration(milliseconds) {
  const n = Number(milliseconds || 0);
  return n < 1000 ? n + " ms" : (n / 1000).toFixed(n < 10000 ? 1 : 0) + " s";
}

function isFullscreen() {
  return displayMode === "fullscreen";
}

function lineWindow(text, inlineLimit, headCount, tailCount) {
  const lines = String(text || "").split("\n");
  if (isFullscreen() || lines.length <= inlineLimit) return { text: lines.join("\n"), hidden: 0 };
  if (tailCount > 0) {
    const hidden = Math.max(0, lines.length - headCount - tailCount);
    return {
      text: lines.slice(0, headCount).join("\n") + "\n… " + hidden + " lines hidden in inline view …\n" + lines.slice(-tailCount).join("\n"),
      hidden
    };
  }
  return { text: lines.slice(0, inlineLimit).join("\n"), hidden: lines.length - inlineLimit };
}

function actionsHTML(copyEnabled) {
  const copy = copyEnabled ? '<button class="btn" type="button" data-action="copy">Copy</button>' : "";
  const full = isFullscreen() ? "" : '<button class="btn primary" type="button" data-action="fullscreen">Open fullscreen</button>';
  if (!copy && !full) return "";
  return '<div class="actions">' + copy + full + "</div>";
}

function bindActions(copyValue) {
  const fullButton = app.querySelector('[data-action="fullscreen"]');
  if (fullButton) {
    fullButton.addEventListener("click", async () => {
      try {
        if (window.openai?.requestDisplayMode) {
          const result = await window.openai.requestDisplayMode({ mode: "fullscreen" });
          displayMode = result?.mode || "fullscreen";
          document.documentElement.dataset.mode = displayMode;
          renderer?.(latestData);
        }
      } catch (error) {
        console.error("Tautline fullscreen request failed", error);
      }
    });
  }

  const copyButton = app.querySelector('[data-action="copy"]');
  if (copyButton) {
    copyButton.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(String(copyValue || ""));
        copyButton.textContent = "Copied";
        setTimeout(() => { copyButton.textContent = "Copy"; }, 1200);
      } catch (error) {
        console.error("Tautline copy failed", error);
      }
    });
  }
}

function textFromContent(content) {
  if (!Array.isArray(content)) return "";
  return content.map((item) => item?.type === "text" ? String(item.text || "") : "").filter(Boolean).join("\n");
}

function extractWidgetData(payload) {
  if (!payload || typeof payload !== "object") return null;
  const envelope = payload.mcp_tool_result || payload.call_tool_result || payload;
  if (envelope?.isError) {
    const message = textFromContent(envelope.content) || "The tool returned an error without details.";
    return { kind: "error", title: "Tautline tool error", summary: message, content: message };
  }
  const data = envelope?._meta?.["devspace/widgetData"] ||
    envelope?.["devspace/widgetData"] ||
    payload["devspace/widgetData"] ||
    envelope?.structuredContent ||
    payload.toolOutput ||
    (typeof envelope?.kind === "string" ? envelope : null);
  if (!data && payload.status) return null;
  return data && typeof data === "object" ? data : null;
}

function reportSize() {
  requestAnimationFrame(() => {
    const content = app.firstElementChild || app;
    const rect = content.getBoundingClientRect();
    const width = Math.max(1, Math.ceil(rect.width || app.getBoundingClientRect().width || 1));
    const height = Math.max(1, Math.ceil(rect.height || 1));
    try {
      window.parent.postMessage({ jsonrpc: "2.0", method: "ui/notifications/size-changed", params: { width, height } }, "*");
    } catch (_) {}
  });
}

function renderSafely() {
  if (!renderer || !latestData) return;
  try {
    if (latestData.kind === "error") {
      app.innerHTML = '<div class="notice warning"><strong>Tautline tool error</strong><br>' + esc(latestData.content || latestData.summary || "No details available.") + '</div>';
    } else {
      renderer(latestData);
    }
    reportSize();
  } catch (error) {
    app.innerHTML = '<div class="notice warning"><strong>Template render error</strong><br>' + esc(error?.message || error || "Unknown error") + '</div>';
    reportSize();
  }
}

function receive(payload) {
  const data = extractWidgetData(payload);
  if (!data) return;
  latestData = data;
  renderSafely();
}

function syncGlobals(globals) {
  if (!globals || typeof globals !== "object") return;
  if (globals.displayMode) {
    displayMode = globals.displayMode;
    document.documentElement.dataset.mode = displayMode;
  }
  if (globals.toolResponseMetadata) receive(globals.toolResponseMetadata);
  else if (globals.toolOutput) receive(globals.toolOutput);
  else renderSafely();
}

function mount(nextRenderer) {
  renderer = nextRenderer;
  if (window.openai?.toolResponseMetadata) receive(window.openai.toolResponseMetadata);
  else receive(window.openai?.toolOutput);
  window.addEventListener("openai:set_globals", (event) => syncGlobals(event.detail?.globals), { passive: true });
  window.addEventListener("message", (event) => {
    if (event.source !== window.parent) return;
    const message = event.data;
    if (message?.jsonrpc === "2.0" && message?.method === "ui/notifications/tool-result") receive(message.params);
  }, { passive: true });
  if (typeof ResizeObserver === "function") new ResizeObserver(reportSize).observe(app);
  reportSize();
}
`
