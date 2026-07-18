package main

func workspaceWidgetHTML() string {
	return widgetDocument("DevSpace workspace", workspaceRendererJS)
}

const workspaceRendererJS = `
function renderWorkspace(data) {
  const files = Array.isArray(data.files) ? data.files : [];
  const stats = data.stats && typeof data.stats === "object" ? data.stats : {};
  const visible = isFullscreen() ? files : files.slice(0, 14);
  const remaining = Math.max(0, files.length - visible.length);

  const rows = visible.map((file) => {
    const path = String(file.path || file.name || "");
    const depth = Math.min(4, Math.max(0, path.split("/").length - 1));
    const marker = file.type === "dir" ? "▸" : "·";
    const meta = file.type === "dir" ? "folder" : formatSize(file.size || 0);
    return '<div class="row">' +
      '<div class="row-main" style="padding-left:' + (depth * 10) + 'px">' +
        '<span class="row-icon" aria-hidden="true">' + marker + '</span>' +
        '<span class="row-name">' + esc(path) + '</span>' +
      '</div>' +
      '<div class="row-meta">' + esc(meta) + '</div>' +
    '</div>';
  }).join("");

  let notice = "";
  if (remaining > 0) {
    notice = '<div class="notice">' + remaining + ' more entries are available in fullscreen.</div>';
  } else if (data.truncated) {
    notice = '<div class="notice warning">Repository scan reached its configured entry or depth limit.</div>';
  }

  app.innerHTML =
    '<div class="hero">' +
      '<div class="hero-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M3.5 6.5h6l2 2h9v9.5a2 2 0 0 1-2 2h-15a2 2 0 0 1-2-2V8.5a2 2 0 0 1 2-2Z"/><path d="M3.5 6.5V5a2 2 0 0 1 2-2h4l2 2h7a2 2 0 0 1 2 2v1.5"/></svg></div>' +
      '<div class="hero-copy"><div class="eyebrow">Workspace</div><h2 class="title">' + esc(data.title || "Repository") + '</h2><div class="summary">' + esc(data.summary || "Repository overview") + '</div></div>' +
    '</div>' +
    '<div class="path">' + esc(data.path || "") + '</div>' +
    '<div class="stat-grid">' +
      '<div class="stat"><div class="stat-value">' + esc(stats.files || 0) + '</div><div class="stat-label">Files</div></div>' +
      '<div class="stat"><div class="stat-value">' + esc(stats.directories || 0) + '</div><div class="stat-label">Folders</div></div>' +
      '<div class="stat"><div class="stat-value">' + formatSize(stats.bytes || 0) + '</div><div class="stat-label">Size</div></div>' +
    '</div>' +
    '<div class="toolbar"><div class="section-title">Repository tree</div>' + actionsHTML(false) + '</div>' +
    (rows ? '<div class="list">' + rows + '</div>' : '<div class="empty">No visible repository entries.</div>') +
    notice;

  bindActions("");
}

mount(renderWorkspace);
`
