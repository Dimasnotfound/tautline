package main

func fileWidgetHTML() string {
	return widgetDocument("DevSpace file viewer", fileRendererJS)
}

const fileRendererJS = `
function renderNumberedCode(text) {
  return String(text || "").split("\n").map((line) => {
    const match = line.match(/^\s*(\d+)\s{2}(.*)$/);
    if (!match) return '<span class="code-line">' + esc(line || " ") + '</span>';
    return '<span class="code-line"><span class="code-number">' + esc(match[1]) + '</span>' + esc(match[2] || " ") + '</span>';
  }).join("");
}

function renderFile(data) {
  const stats = data.stats && typeof data.stats === "object" ? data.stats : {};
  const preview = lineWindow(data.content || "", 70, 70, 0);
  const language = data.language || "text";
  const notices = [];
  if (preview.hidden > 0) notices.push(preview.hidden + " additional lines are available in fullscreen.");
  if (data.truncated) notices.push("The server response reached the configured read limit.");

  app.innerHTML =
    '<div class="hero">' +
      '<div class="hero-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M6 3.5h8l4 4V20a1.5 1.5 0 0 1-1.5 1.5h-10A1.5 1.5 0 0 1 5 20V5a1.5 1.5 0 0 1 1-1.5Z"/><path d="M14 3.5V8h4.5"/><path d="m9 12-2 2 2 2M15 12l2 2-2 2"/></svg></div>' +
      '<div class="hero-copy"><div class="eyebrow">File viewer</div><h2 class="title">' + esc(data.title || "File") + '</h2><div class="summary">' + esc(data.summary || "UTF-8 text preview") + '</div></div>' +
    '</div>' +
    '<div class="path">' + esc(data.path || "") + '</div>' +
    '<div class="badges"><span class="badge">' + esc(language) + '</span><span class="badge">' + esc(stats.lines || 0) + ' lines</span><span class="badge">' + formatSize(stats.bytes || 0) + '</span>' + (data.truncated ? '<span class="badge warning">truncated</span>' : '') + '</div>' +
    '<div class="toolbar"><div class="section-title">Content</div>' + actionsHTML(true) + '</div>' +
    '<pre class="code" aria-label="File content">' + renderNumberedCode(preview.text) + '</pre>' +
    notices.map((notice) => '<div class="notice">' + esc(notice) + '</div>').join("");

  bindActions(data.content || "");
}

mount(renderFile);
`
