"use strict";

const ui = {
  state: null,
  csrf: "",
  timer: null,
  noticeTimer: null,
  tokenRevealed: false,
  routerDirty: false,
};

const $ = (id) => document.getElementById(id);
const esc = (value) => String(value ?? "")
  .replaceAll("&", "&amp;")
  .replaceAll("<", "&lt;")
  .replaceAll(">", "&gt;")
  .replaceAll('"', "&quot;")
  .replaceAll("'", "&#039;");

async function api(path, options = {}) {
  const request = { credentials: "same-origin", ...options };
  request.headers = { Accept: "application/json", ...(options.headers || {}) };
  if (request.method && request.method !== "GET") {
    request.headers["X-Tautline-CSRF"] = ui.csrf;
    if (request.body && typeof request.body !== "string") {
      request.headers["Content-Type"] = "application/json";
      request.body = JSON.stringify(request.body);
    }
  }
  const response = await fetch(path, request);
  const contentType = response.headers.get("content-type") || "";
  const payload = contentType.includes("application/json") ? await response.json() : await response.text();
  if (!response.ok) {
    throw new Error(typeof payload === "string" ? payload.trim() : payload.error || response.statusText);
  }
  return payload;
}

function notify(message, error = false) {
  const node = $("notice");
  node.textContent = message;
  node.classList.toggle("error", error);
  node.classList.remove("hidden");
  clearTimeout(ui.noticeTimer);
  ui.noticeTimer = setTimeout(() => node.classList.add("hidden"), error ? 7000 : 3200);
}

function badge(node, label, state) {
  node.textContent = label;
  node.className = "badge" + (state ? ` ${state}` : "");
}

function formatDuration(seconds) {
  const value = Math.max(0, Number(seconds) || 0);
  if (value < 60) return `${Math.floor(value)}s`;
  if (value < 3600) return `${Math.floor(value / 60)}m ${Math.floor(value % 60)}s`;
  return `${Math.floor(value / 3600)}h ${Math.floor((value % 3600) / 60)}m`;
}

function setInput(id, value) {
  const node = $(id);
  if (node && document.activeElement !== node) node.value = value ?? "";
}

function stateClass(status) {
  if (["completed", "running", "ready", "online", "connected"].includes(status)) return "ok";
  if (["failed", "cancelled", "timed_out", "offline", "missing", "error"].includes(status)) return "bad";
  return "warn";
}

function selectTab(name, updateHash = true) {
  const panel = document.getElementById(name) || $("overview");
  document.querySelectorAll(".view").forEach((view) => { view.hidden = view !== panel; });
  document.querySelectorAll(".nav-tab").forEach((tab) => {
    const active = tab.dataset.tab === panel.id;
    tab.classList.toggle("active", active);
    tab.setAttribute("aria-selected", String(active));
  });
  $("page-title").textContent = panel.dataset.title || "Tautline";
  $("page-description").textContent = panel.dataset.description || "";
  if (updateHash && location.hash !== `#${panel.id}`) history.replaceState(null, "", `#${panel.id}`);
}

function render(state) {
  ui.state = state;
  ui.csrf = state.csrf;
  $("version").textContent = `v${state.version}`;
  $("service-health").textContent = `${state.service} is running`;
  $("service-dot").className = "status-dot ok";
  $("uptime").textContent = `Uptime ${formatDuration(state.uptime_seconds)}`;
  $("metric-service").textContent = "Online";
  $("metric-service-detail").textContent = `v${state.version} · ${formatDuration(state.uptime_seconds)}`;
  $("local-mcp").textContent = state.mcp_local_url;
  $("public-mcp").textContent = state.mcp_public_url || "Not configured";
  if (!ui.tokenRevealed) $("owner-token").textContent = state.owner_token;
  const roots = state.allowed_roots || [];
  $("root-count").textContent = String(roots.length);
  $("allowed-roots").innerHTML = roots.length ? roots.map((root) => `<code>${esc(root)}</code>`).join("") : '<div class="empty">No allowed roots configured.</div>';

  renderRouter(state);
  renderRelayBridge(state.relay_bridge || {});
  renderMCP(state.mcp_servers || []);
  renderTunnel(state);
  renderBrowser(state);
  renderAgents(state, state.slots || []);
  renderRuns(state.runs || []);
}

function uniqueModels(values) {
  return [...new Set(values.map((value) => String(value || "").trim()).filter(Boolean))];
}

function selectedRouterModels() {
  return [...document.querySelectorAll(".router-model-option:checked")].map((node) => node.value);
}

function syncRouterDefaultOptions(preferred = "") {
  const select = $("router-model");
  const models = selectedRouterModels();
  const current = preferred || select.value;
  if (!models.length) {
    select.innerHTML = '<option value="">Select a model</option>';
    select.disabled = true;
    return;
  }
  select.disabled = false;
  select.innerHTML = models.map((model) => `<option value="${esc(model)}" ${model === current ? "selected" : ""}>${esc(model)}</option>`).join("");
  if (!models.includes(select.value)) select.value = models[0];
}

function renderRouterModelControls(config, router) {
  const allowed = uniqueModels(config.allowed_models || []);
  const available = (router.models || []).map((model) => model.id);
  const models = uniqueModels([...allowed, ...available]);
  const support = new Map((router.models || []).map((model) => [model.id, model.image_support || "unknown"]));
  $("model-list").innerHTML = models.length
    ? models.map((model) => `<label class="model-option"><input class="router-model-option" type="checkbox" value="${esc(model)}" ${allowed.includes(model) ? "checked" : ""}><span><strong title="${esc(model)}">${esc(model)}</strong><small>Image: ${esc(support.get(model) || "unknown")}</small></span></label>`).join("")
    : `<span class="empty">${esc(router.error || "Check models to load the list.")}</span>`;
  syncRouterDefaultOptions(config.default_model || "");
}

function syncAgentBackendFields(backend = $("agent-backend").value) {
  const legacy = backend === "9router";
  document.querySelectorAll(".legacy-router-field").forEach((node) => node.classList.toggle("hidden", !legacy));
  $("relay-help").classList.toggle("hidden", legacy);
}

function renderRouter(state) {
  const router = state.router || {};
  const config = state.config.router || {};
  const backend = state.config.agent_backend || "chatgpt-relay";
  setInput("agent-backend", backend);
  syncAgentBackendFields(backend);
  if (backend === "chatgpt-relay") {
    $("metric-router").textContent = "ChatGPT Relay";
    $("metric-router-detail").textContent = "ordinary ChatGPT worker chats";
    badge($("router-badge"), "No router needed", "ok");
  } else {
    $("metric-router").textContent = router.reachable ? "9Router connected" : "9Router offline";
    $("metric-router-detail").textContent = router.reachable ? `${(router.models || []).length} models` : (router.error || router.base_url || "Unavailable");
    badge($("router-badge"), router.reachable ? "Connected" : "Offline", router.reachable ? "ok" : "bad");
  }
  setInput("router-url", config.base_url || "");
  if (!ui.routerDirty) renderRouterModelControls(config, router);
}

function renderRelayBridge(bridge) {
  const connected = Boolean(bridge.connected);
  $("metric-relay").textContent = connected ? "Connected" : "Manual fallback";
  $("metric-relay-detail").textContent = connected
    ? `${bridge.clients || 0} Laju client${bridge.clients === 1 ? "" : "s"} · ${bridge.queued || 0} queued`
    : "Install or restart the Laju Relay Bridge";
}

function renderMCP(servers) {
  const connected = servers.filter((server) => server.connected).length;
  const tools = servers.reduce((total, server) => total + (Number(server.tool_count) || 0), 0);
  $("metric-mcp").textContent = `${connected} connected`;
  $("metric-mcp-detail").textContent = `${servers.length} configured · ${tools} tools published`;
  const list = $("mcp-list");
  if (!servers.length) {
    list.innerHTML = '<div class="empty">No integrations yet. Add an MCP server when a capability is needed.</div>';
    return;
  }
  list.innerHTML = servers.map((server) => {
    const label = server.connected ? "Connected" : server.enabled ? "Error" : "Disabled";
    const status = server.connected ? "ok" : server.enabled ? "bad" : "warn";
    const secrets = [
      ...(server.environment_keys || []).map((key) => `env:${key}`),
      ...(server.header_keys || []).map((key) => `header:${key}`),
    ];
    const transport = server.active_transport && server.active_transport !== server.transport ? `${server.transport} → ${server.active_transport}` : server.transport;
    const detail = [transport, `${server.tool_count || 0} tools`, secrets.length ? `${secrets.length} protected values` : "no protected values"].join(" · ");
    return `<article class="integration-card" data-mcp="${esc(server.id)}">
      <div class="integration-name"><strong title="${esc(server.name)}">${esc(server.name)}</strong><small>${esc(server.prefix)}_* · ${esc(server.server_name || "not initialized")}</small></div>
      <div class="integration-detail"><code title="${esc(server.endpoint)}">${esc(server.endpoint || "Not configured")}</code><small>${esc(detail)}</small>${server.last_error ? `<div class="integration-error" title="${esc(server.last_error)}">${esc(server.last_error)}</div>` : ""}</div>
      <div class="integration-actions"><span class="badge ${status}">${label}</span><button class="button compact edit-mcp" type="button" data-mcp="${esc(server.id)}">Edit</button><button class="button compact toggle-mcp" type="button" data-mcp="${esc(server.id)}" data-action="${server.connected ? "disconnect" : "connect"}">${server.connected ? "Disconnect" : "Connect"}</button><button class="button compact danger remove-mcp" type="button" data-mcp="${esc(server.id)}">Remove</button></div>
    </article>`;
  }).join("");
}

function renderTunnel(state) {
  const tunnel = state.tunnel || {};
  const config = state.config.tunnel || {};
  const label = tunnel.running ? "Running" : tunnel.available ? "Stopped" : "Unavailable";
  $("metric-tunnel").textContent = label;
  $("metric-tunnel-detail").textContent = tunnel.origin_url ? `${tunnel.public_url || tunnel.name || "Tunnel"} → ${tunnel.origin_url}` : (tunnel.public_url || tunnel.last_error || "No public endpoint");
  badge($("tunnel-badge"), label, tunnel.running ? "ok" : tunnel.available ? "warn" : "bad");
  setInput("tunnel-mode", config.mode || "off");
  setInput("tunnel-name", config.name || "");
  setInput("tunnel-domain", config.custom_domain || "");
  setInput("tunnel-protocol", config.protocol || "http2");
  if (tunnel.tunnel_id || tunnel.dns_target) {
    const dns = $("dns-result");
    dns.textContent = `Tunnel ID: ${tunnel.tunnel_id || "unknown"}\nCNAME target: ${tunnel.dns_target || "not resolved"}\nPublic URL: ${tunnel.public_url || "not active"}\nOrigin: ${tunnel.origin_url || "not detected"}${tunnel.detected_externally ? " (detected process)" : ""}`;
    dns.classList.remove("hidden");
  }
}

function renderBrowser(state) {
  const browser = state.lightpanda || {};
  const config = state.config.lightpanda || {};
  const label = browser.running ? "Running" : browser.starting ? "Starting" : browser.detecting ? "Detecting" : browser.available ? "Installed" : "Unavailable";
  $("metric-browser").textContent = label;
  $("metric-browser-detail").textContent = browser.endpoint || "Port not configured";
  badge($("browser-badge"), label, browser.running ? "ok" : (browser.available || browser.starting || browser.detecting) ? "warn" : "bad");
  setInput("browser-path", config.executable || "");
  setInput("browser-port", config.port || 9223);
  if (document.activeElement !== $("browser-robots")) $("browser-robots").checked = Boolean(config.obey_robots);
  $("browser-endpoint").textContent = browser.endpoint || "";
  $("browser-error").textContent = browser.last_error || "";
}

function renderAgents(state, slots) {
  const globallyEnabled = Boolean(state.config?.agent_enabled);
  const busy = slots.filter((slot) => slot.busy).length;
  const enabled = slots.filter((slot) => slot.enabled).length;
  if (document.activeElement !== $("agents-enabled")) $("agents-enabled").checked = globallyEnabled;
  $("agents-enabled-label").textContent = globallyEnabled ? "Enabled" : "Disabled";
  $("metric-agents").textContent = globallyEnabled ? `${enabled} available` : "Disabled";
  const backend = state.config?.agent_backend || "chatgpt-relay";
  $("metric-agents-detail").textContent = globallyEnabled ? `${busy} busy · ${slots.length} total · ${backend}` : "New delegation is blocked";
  $("agent-slots").classList.toggle("paused", !globallyEnabled);
  $("agent-slots").innerHTML = slots.map((slot, index) => {
    const status = slot.busy ? "Busy" : !globallyEnabled ? "Paused" : slot.enabled ? "Ready" : "Off";
    const statusClass = slot.busy || (globallyEnabled && slot.enabled) ? "ok" : "warn";
    const detail = slot.busy ? `working on ${esc(slot.active_run_id)}` : globallyEnabled ? "available" : "delegation paused";
    return `<article class="agent-card" data-slot="${esc(slot.id)}"><div class="agent-head"><div class="agent-name"><span class="agent-icon">${String(index + 1).padStart(2, "0")}</span><span><strong>Sub-agent slot</strong><small>${esc(slot.id)} · ${detail}</small></span></div><span class="badge ${statusClass}">${status}</span></div><div class="toggle-list">${toggleHTML(slot.id, "enabled", "Slot enabled", slot.enabled, slot.busy)}${toggleHTML(slot.id, "allow_images", "Images", slot.allow_images, false)}${toggleHTML(slot.id, "rtk", "RTK", slot.rtk, false)}${toggleHTML(slot.id, "caveman", "Caveman", slot.caveman, false)}</div><div class="agent-actions"><button class="button compact danger remove-agent" type="button" data-slot="${esc(slot.id)}" ${slot.busy || slots.length <= 1 ? "disabled" : ""}>Remove</button></div></article>`;
  }).join("");
}

function toggleHTML(slot, field, label, checked, disabled) {
  return `<label class="toggle-row"><input class="slot-toggle" data-slot="${esc(slot)}" data-field="${esc(field)}" type="checkbox" ${checked ? "checked" : ""} ${disabled ? "disabled" : ""}><span class="toggle"></span><span>${esc(label)}</span></label>`;
}

function renderRuns(runs) {
  const node = $("run-list");
  if (!runs.length) {
    node.innerHTML = '<div class="empty">No delegated runs yet.</div>';
    return;
  }
  node.innerHTML = runs.map((run) => {
    const identity = run.name || run.agent_id || "Temporary sub-agent";
    const role = run.role || "Role assigned by ChatGPT";
    const active = ["queued", "waiting_worker", "running", "finishing"].includes(run.status);
    const detail = [run.slot_id, run.provider, run.model, run.requires_images ? "image verified" : "text", run.rtk ? "RTK" : "", run.caveman ? "Caveman" : ""].filter(Boolean).join(" · ");
    const output = run.error ? `Error: ${run.error}` : run.output || "";
    return `<article class="run-card"><div class="run-head"><div class="run-identity"><strong title="${esc(identity)}">${esc(identity)}</strong><small>${esc(role)}</small></div><div class="run-copy"><p title="${esc(run.task)}">${esc(run.task)}</p><small>${esc(run.activity || run.phase || "")}</small></div><div class="run-meta"><span class="badge ${stateClass(run.status)}">${esc(run.status)}</span>${active ? `<button class="button compact danger cancel-run" data-run="${esc(run.id)}" type="button">Cancel</button>` : ""}</div></div><div class="detail-strip run-detail"><code>${esc(detail)}</code><span>${esc(run.id)}</span></div>${output ? `<pre class="run-output">${esc(output)}</pre>` : ""}</article>`;
  }).join("");
}

async function refresh(silent = true) {
  try {
    render(await api("/api/state"));
  } catch (error) {
    $("service-dot").className = "status-dot bad";
    $("service-health").textContent = "Dashboard disconnected";
    $("metric-service").textContent = "Offline";
    if (!silent) notify(error.message, true);
  }
}

async function mutate(path, options, success) {
  try {
    const result = await api(path, options);
    if (success) notify(success);
    await refresh();
    return result;
  } catch (error) {
    await refresh();
    notify(error.message, true);
    throw error;
  }
}

function splitLines(value) {
  return value.split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
}

function parseKeyValues(value) {
  const trimmed = value.trim();
  if (!trimmed) return null;
  if (trimmed === "-") return {};
  const entries = {};
  for (const line of splitLines(trimmed)) {
    const separator = line.indexOf("=");
    if (separator < 1) throw new Error(`Expected KEY=value: ${line}`);
    entries[line.slice(0, separator).trim()] = line.slice(separator + 1);
  }
  return entries;
}

function parseOptionalLines(value) {
  const trimmed = value.trim();
  if (!trimmed) return null;
  return trimmed === "-" ? [] : splitLines(trimmed);
}

function syncMCPTransport() {
  const urlTransport = $("mcp-transport").value !== "stdio";
  $("mcp-http-fields").classList.toggle("hidden", !urlTransport);
  $("mcp-stdio-fields").classList.toggle("hidden", urlTransport);
  $("mcp-url").required = urlTransport;
  $("mcp-command").required = !urlTransport;
}

function openMCPDialog(server = null) {
  $("mcp-form").reset();
  $("mcp-id").value = server?.id || "";
  $("mcp-form-title").textContent = server ? `Edit ${server.name}` : "Add integration";
  $("mcp-name").value = server?.name || "";
  $("mcp-prefix").value = server?.prefix || "";
  const configuredTransport = server?.transport === "http" ? "streamable-http" : (server?.transport || "stdio");
  $("mcp-transport").value = configuredTransport;
  $("mcp-timeout").value = server?.timeout_seconds || 30;
  $("mcp-command").value = server?.command || "";
  $("mcp-args").value = "";
  $("mcp-args").placeholder = server?.argument_count ? `${server.argument_count} arguments configured\nLeave blank to keep them` : "-y\n@example/google-docs-mcp";
  $("mcp-working-directory").value = server?.working_directory || "";
  $("mcp-url").value = server?.url || "";
  $("mcp-environment").value = "";
  $("mcp-headers").value = "";
  $("mcp-environment").placeholder = server?.environment_keys?.length ? `Configured: ${server.environment_keys.join(", ")}\nLeave blank to keep them` : "GOOGLE_CLIENT_ID=${GOOGLE_CLIENT_ID}";
  $("mcp-headers").placeholder = server?.header_keys?.length ? `Configured: ${server.header_keys.join(", ")}\nLeave blank to keep them` : "Authorization=Bearer ${GOOGLE_MCP_TOKEN}";
  $("mcp-enabled").checked = Boolean(server?.enabled);
  syncMCPTransport();
  $("mcp-dialog").showModal();
}

function closeMCPDialog() {
  $("mcp-dialog").close();
}

document.querySelectorAll(".nav-tab").forEach((tab) => tab.addEventListener("click", () => selectTab(tab.dataset.tab)));
window.addEventListener("hashchange", () => selectTab(location.hash.slice(1) || "overview", false));

$("refresh-all").addEventListener("click", async () => {
  await refresh(false);
  notify("Dashboard refreshed.");
});

$("reveal-token").addEventListener("click", async () => {
  try {
    const result = await api("/api/token/reveal", { method: "POST" });
    $("owner-token").textContent = result.token;
    ui.tokenRevealed = true;
    $("reveal-token").textContent = "Revealed";
  } catch (error) {
    notify(error.message, true);
  }
});

$("add-mcp").addEventListener("click", () => openMCPDialog());
$("close-mcp-dialog").addEventListener("click", closeMCPDialog);
$("cancel-mcp").addEventListener("click", closeMCPDialog);
$("mcp-transport").addEventListener("change", syncMCPTransport);

$("mcp-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  try {
    const id = $("mcp-id").value;
    const transport = $("mcp-transport").value;
    const args = parseOptionalLines($("mcp-args").value);
    const environment = parseKeyValues($("mcp-environment").value);
    const headers = parseKeyValues($("mcp-headers").value);
    const body = {
      name: $("mcp-name").value.trim(),
      prefix: $("mcp-prefix").value.trim(),
      transport,
      enabled: $("mcp-enabled").checked,
      timeout_seconds: Number($("mcp-timeout").value),
      command: transport === "stdio" ? $("mcp-command").value.trim() : "",
      working_directory: transport === "stdio" ? $("mcp-working-directory").value.trim() : "",
      url: transport !== "stdio" ? $("mcp-url").value.trim() : "",
    };
    if (!id || args !== null || transport !== "stdio") body.args = transport === "stdio" ? (args || []) : [];
    if (!id || environment !== null) body.environment = environment || {};
    if (!id || headers !== null) body.headers = headers || {};
    await mutate(id ? `/api/mcp/${encodeURIComponent(id)}` : "/api/mcp", { method: id ? "PATCH" : "POST", body }, id ? "Integration updated." : "Integration added.");
    closeMCPDialog();
  } catch (error) {
    if (!error.message.startsWith("Expected KEY=value")) return;
    notify(error.message, true);
  }
});

document.addEventListener("click", async (event) => {
  const copy = event.target.closest(".copy");
  if (copy) {
    const value = $(copy.dataset.copyTarget)?.textContent || "";
    try { await navigator.clipboard.writeText(value); notify("Copied to clipboard."); } catch { notify("Clipboard access was blocked.", true); }
    return;
  }
  const editMCP = event.target.closest(".edit-mcp");
  if (editMCP) {
    const server = (ui.state?.mcp_servers || []).find((candidate) => candidate.id === editMCP.dataset.mcp);
    if (server) openMCPDialog(server);
    return;
  }
  const toggleMCP = event.target.closest(".toggle-mcp");
  if (toggleMCP) {
    try { await mutate(`/api/mcp/${encodeURIComponent(toggleMCP.dataset.mcp)}/${toggleMCP.dataset.action}`, { method: "POST", body: {} }, toggleMCP.dataset.action === "connect" ? "Integration connected." : "Integration disconnected."); } catch {}
    return;
  }
  const removeMCP = event.target.closest(".remove-mcp");
  if (removeMCP) {
    if (!window.confirm("Remove this MCP integration and its published tools?")) return;
    try { await mutate(`/api/mcp/${encodeURIComponent(removeMCP.dataset.mcp)}`, { method: "DELETE" }, "Integration removed."); } catch {}
    return;
  }
  const removeAgent = event.target.closest(".remove-agent");
  if (removeAgent) {
    try { await mutate(`/api/agents/${encodeURIComponent(removeAgent.dataset.slot)}`, { method: "DELETE" }, "Sub-agent removed."); } catch {}
    return;
  }
  const cancel = event.target.closest(".cancel-run");
  if (cancel) {
    try { await mutate(`/api/runs/${encodeURIComponent(cancel.dataset.run)}/cancel`, { method: "POST" }, "Agent run cancelled."); } catch {}
  }
});

document.addEventListener("change", async (event) => {
  const toggle = event.target.closest(".slot-toggle");
  if (!toggle) return;
  const body = { [toggle.dataset.field]: toggle.checked };
  try {
    await mutate(`/api/agents/${encodeURIComponent(toggle.dataset.slot)}`, { method: "PATCH", body }, "Sub-agent updated.");
  } catch {
    toggle.checked = !toggle.checked;
  }
});

$("agents-enabled").addEventListener("change", async (event) => {
  const enabled = event.target.checked;
  try {
    await mutate("/api/settings", { method: "PATCH", body: { agent_enabled: enabled } }, enabled ? "Sub-agents enabled." : "Sub-agents disabled.");
  } catch {
    event.target.checked = !enabled;
  }
});

$("add-agent").addEventListener("click", async () => {
  try { await mutate("/api/agents", { method: "POST", body: {} }, "Sub-agent slot added."); } catch {}
});

$("router-form").addEventListener("input", () => { ui.routerDirty = true; });
$("router-form").addEventListener("change", (event) => {
  ui.routerDirty = true;
  if (event.target.id === "agent-backend") syncAgentBackendFields(event.target.value);
  if (event.target.classList.contains("router-model-option")) syncRouterDefaultOptions();
});

$("router-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const backend = $("agent-backend").value;
  const allowedModels = selectedRouterModels();
  if (backend === "9router" && !allowedModels.length) {
    notify("Select at least one allowed model for the legacy 9Router backend.", true);
    return;
  }
  const body = {
    agent_backend: backend,
    router_base_url: $("router-url").value.trim(),
    router_default_model: $("router-model").value,
    router_allowed_models: allowedModels,
  };
  const key = $("router-key").value.trim();
  if (key) body.router_api_key = key;
  ui.routerDirty = false;
  try {
    await mutate("/api/settings", { method: "PATCH", body }, backend === "chatgpt-relay" ? "ChatGPT relay enabled." : "Legacy 9Router settings saved.");
    $("router-key").value = "";
  } catch {
    ui.routerDirty = true;
  }
});

$("probe-router").addEventListener("click", async () => {
  try { await mutate("/api/router/refresh", { method: "POST", body: {} }, "9Router probe completed."); } catch {}
});

$("tunnel-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  try {
    await mutate("/api/settings", { method: "PATCH", body: { tunnel_mode: $("tunnel-mode").value, tunnel_name: $("tunnel-name").value.trim(), tunnel_domain: $("tunnel-domain").value.trim(), tunnel_protocol: $("tunnel-protocol").value } }, "Tunnel settings saved.");
  } catch {}
});

$("start-tunnel").addEventListener("click", async () => { try { await mutate("/api/tunnel/start", { method: "POST", body: { mode: $("tunnel-mode").value } }, "Cloudflare tunnel started."); } catch {} });
$("stop-tunnel").addEventListener("click", async () => { try { await mutate("/api/tunnel/stop", { method: "POST", body: {} }, "Cloudflare tunnel stopped."); } catch {} });
$("generate-dns").addEventListener("click", async () => {
  const domain = $("tunnel-domain").value.trim();
  try {
    const result = await mutate("/api/tunnel/dns", { method: "POST", body: { domain } }, "Cloudflare DNS route generated.");
    const dns = $("dns-result");
    dns.textContent = `Type: CNAME\nName: ${domain}\nTarget: ${result.dns_target || "created by cloudflared"}\nTunnel ID: ${result.tunnel_id || "resolved by Cloudflare"}`;
    dns.classList.remove("hidden");
  } catch {}
});

$("browser-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  try {
    await mutate("/api/settings", { method: "PATCH", body: { lightpanda_path: $("browser-path").value.trim(), lightpanda_port: Number($("browser-port").value), lightpanda_obey_robots: $("browser-robots").checked } }, "Lightpanda settings saved.");
  } catch {}
});
$("start-browser").addEventListener("click", async () => { try { await mutate("/api/lightpanda/start", { method: "POST", body: {} }, "Lightpanda started."); } catch {} });
$("stop-browser").addEventListener("click", async () => { try { await mutate("/api/lightpanda/stop", { method: "POST", body: {} }, "Lightpanda stopped."); } catch {} });

selectTab(location.hash.slice(1) || "overview", false);
refresh(false).then(() => { ui.timer = setInterval(() => refresh(true), 2000); });
