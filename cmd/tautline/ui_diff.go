package main

func diffWidgetHTML() string {
	return widgetDocument("Tautline diff viewer", diffRendererJS)
}

const diffRendererJS = `
function renderDiffLines(text) {
  return String(text || "").split("\n").map((line) => {
    let className = "code-line";
    if (line.startsWith("+") && !line.startsWith("+++")) className += " diff-add";
    else if (line.startsWith("-") && !line.startsWith("---")) className += " diff-del";
    else if (line.startsWith("@@")) className += " diff-hunk";
    else if (line.startsWith("---") || line.startsWith("+++")) className += " diff-meta";
    return '<span class="' + className + '">' + esc(line || " ") + '</span>';
  }).join("");
}

function renderDiff(data) {
  const stats = data.stats && typeof data.stats === "object" ? data.stats : {};
  const preview = lineWindow(data.diff || "", 120, 120, 0);
  const operation = data.operation === "write" ? "Write" : "Edit";
  const notices = [];
  if (preview.hidden > 0) notices.push(preview.hidden + " additional diff lines are available in fullscreen.");
  if (data.truncated) notices.push("The diff was truncated by the server safety limit.");

  app.innerHTML =
    '<div class="hero">' +
      '<div class="hero-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M8 7h8M8 17h8M5 4v6M2 7h6M19 14v6M16 17h6"/></svg></div>' +
      '<div class="hero-copy"><div class="eyebrow">' + esc(operation) + ' review</div><h2 class="title">' + esc(data.title || "Changes applied") + '</h2><div class="summary">' + esc(data.summary || "Unified diff") + '</div></div>' +
    '</div>' +
    '<div class="path">' + esc(data.path || "") + '</div>' +
    '<div class="badges"><span class="badge success">+' + esc(stats.added || 0) + ' added</span><span class="badge danger">−' + esc(stats.removed || 0) + ' removed</span><span class="badge">' + esc(stats.lines || 0) + ' lines total</span>' + (data.truncated ? '<span class="badge warning">truncated</span>' : '') + '</div>' +
    '<div class="toolbar"><div class="section-title">Unified diff</div>' + actionsHTML(true) + '</div>' +
    '<pre class="code" aria-label="Unified diff">' + renderDiffLines(preview.text) + '</pre>' +
    notices.map((notice) => '<div class="notice">' + esc(notice) + '</div>').join("");

  bindActions(data.diff || "");
}

mount(renderDiff);
`
