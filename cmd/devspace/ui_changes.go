package main

func changesWidgetHTML() string {
	return widgetDocument("DevSpace changes", changesRendererJS)
}

const changesRendererJS = `
function renderAggregateDiff(text) {
  return String(text || "").split("\n").map((line) => {
    let className = "code-line";
    if (line.startsWith("+") && !line.startsWith("+++")) className += " diff-add";
    else if (line.startsWith("-") && !line.startsWith("---")) className += " diff-del";
    else if (line.startsWith("@@")) className += " diff-hunk";
    else if (line.startsWith("---") || line.startsWith("+++")) className += " diff-meta";
    return '<span class="' + className + '">' + esc(line || " ") + '</span>';
  }).join("");
}

function renderChanges(data) {
  const files = Array.isArray(data.files) ? data.files : [];
  const stats = data.stats && typeof data.stats === "object" ? data.stats : {};
  const rows = files.map((file) => {
    const status = file.status === "added" ? "+" : file.status === "deleted" ? "−" : "·";
    return '<div class="row"><div class="row-main"><span class="row-icon">' + status + '</span><span class="row-name">' + esc(file.path || "") + '</span></div><div class="row-meta">+' + esc(file.added || 0) + ' −' + esc(file.removed || 0) + '</div></div>';
  }).join("");

  if (data.empty) {
    app.innerHTML = '<div class="hero"><div class="hero-copy"><div class="eyebrow">Review</div><h2 class="title">No pending changes</h2></div></div>';
    return;
  }

  app.innerHTML =
    '<div class="hero">' +
      '<div class="hero-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M5 7h14M5 12h14M5 17h14"/></svg></div>' +
      '<div class="hero-copy"><div class="eyebrow">Change review</div><h2 class="title">' + esc(data.summary || "Changes ready") + '</h2></div>' +
    '</div>' +
    '<div class="list">' + rows + '</div>' +
    (isFullscreen() && data.diff ? '<div class="toolbar"><div class="section-title">Diff</div></div><pre class="code">' + renderAggregateDiff(data.diff) + '</pre>' : '') +
    (!isFullscreen() ? '<div class="toolbar"><div class="section-title">Ready to review</div>' + actionsHTML(false) + '</div>' : '') +
    (data.truncated ? '<div class="notice warning">Review output was truncated by the safety limit.</div>' : '');

  bindActions("");
}

mount(renderChanges);
`
