package main

func commandWidgetHTML() string {
	return widgetDocument("Tautline command result", commandRendererJS)
}

const commandRendererJS = `
function renderPlainLines(text) {
  return String(text || "").split("\n").map((line) => '<span class="code-line">' + esc(line || " ") + '</span>').join("");
}

function renderCommand(data) {
  const stats = data.stats && typeof data.stats === "object" ? data.stats : {};
  const preview = lineWindow(data.output || "", 120, 80, 30);
  const exitCode = stats.exitCode ?? (data.success ? 0 : "unknown");
  const statusClass = data.success ? "success" : "danger";
  const statusText = data.success ? "Succeeded" : "Failed";
  const notices = [];
  if (preview.hidden > 0) notices.push(preview.hidden + " output lines are hidden in inline view.");
  if (data.truncated) notices.push("Command output reached the configured 128 KiB safety limit.");

  app.innerHTML =
    '<div class="hero">' +
      '<div class="hero-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="m5 7 4 4-4 4M11 17h7"/><rect x="2.5" y="3.5" width="19" height="17" rx="2"/></svg></div>' +
      '<div class="hero-copy"><div class="eyebrow">Command result</div><h2 class="title">' + esc(data.title || "Command finished") + '</h2><div class="summary">' + esc(data.summary || "Shell execution result") + '</div></div>' +
    '</div>' +
    '<div class="badges"><span class="badge ' + statusClass + '">' + statusText + '</span><span class="badge">exit ' + esc(exitCode) + '</span><span class="badge">' + formatDuration(stats.durationMs || 0) + '</span><span class="badge">' + formatSize(stats.bytes || 0) + '</span>' + (data.truncated ? '<span class="badge warning">truncated</span>' : '') + '</div>' +
    '<div class="command">' + esc(data.command || "") + '</div>' +
    '<div class="path">cwd: ' + esc(data.path || "") + '</div>' +
    '<div class="toolbar"><div class="section-title">Output</div>' + actionsHTML(true) + '</div>' +
    '<pre class="code" aria-label="Command output">' + renderPlainLines(preview.text || "(no output)") + '</pre>' +
    notices.map((notice) => '<div class="notice">' + esc(notice) + '</div>').join("");

  bindActions(data.output || "");
}

mount(renderCommand);
`
