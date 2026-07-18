package main

func workspaceWidgetHTML() string {
	return widgetDocument("DevSpace workspace", workspaceRendererJS)
}

const workspaceRendererJS = `
function renderWorkspace(data) {
  const files = Array.isArray(data.files) ? data.files : [];
  const visible = isFullscreen() ? files : files.slice(0, 8);
  const remaining = Math.max(0, files.length - visible.length);
  const rows = visible.map((file) => {
    const path = String(file.path || file.name || "");
    const marker = file.type === "dir" ? "▸" : "·";
    return '<div class="row"><div class="row-main"><span class="row-icon">' + marker + '</span><span class="row-name">' + esc(path) + '</span></div></div>';
  }).join("");

  app.innerHTML =
    '<div class="hero">' +
      '<div class="hero-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M3.5 6.5h6l2 2h9v9.5a2 2 0 0 1-2 2h-15a2 2 0 0 1-2-2V8.5a2 2 0 0 1 2-2Z"/></svg></div>' +
      '<div class="hero-copy"><div class="eyebrow">Workspace ready</div><h2 class="title">' + esc(data.title || "Repository") + '</h2><div class="summary">' + esc(data.summary || "") + '</div></div>' +
    '</div>' +
    '<div class="path">' + esc(data.path || "") + '</div>' +
    '<div class="badges"><span class="badge">' + esc(data.workspaceId || "") + '</span></div>' +
    (isFullscreen() ? '<div class="toolbar"><div class="section-title">Files</div></div><div class="list">' + rows + '</div>' : '') +
    (!isFullscreen() && remaining > 0 ? '<div class="toolbar"><div class="section-title">' + remaining + ' more entries</div>' + actionsHTML(false) + '</div>' : '');

  bindActions("");
}

mount(renderWorkspace);
`
