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
  if (document.activeElement !== node) node.value = value ?? "";
}

function stateClass(status) {
  if (["completed", "running", "ready", "online"].includes(status)) return "ok";
  if (["failed", "cancelled", "offline", "missing"].includes(status)) return "bad";
  return "warn";
}

function render(state) {
  ui.state = state;
  ui.csrf = state.csrf;
  $("version").textContent = `v${state.version}`;
  $("service-health").textContent = `${state.service} is running`;
  $("service-dot").className = "status-dot ok";
  $("uptime").textContent = `Uptime ${formatDuration(state.uptime_seconds)}`;
  $("local-mcp").textContent = state.mcp_local_url;
  $("public-mcp").textContent = state.mcp_public_url || "Not configured";
  if (!ui.tokenRevealed) $("owner-token").textContent = state.owner_token;

  renderRouter(state);
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
    : `<span class="quiet">${esc(router.error || "Check models to load the list.")}</span>`;
  syncRouterDefaultOptions(config.default_model || "");
}

function renderRouter(state) {
  const router = state.router || {};
  const config = state.config.router || {};
  $("metric-router").textContent = router.reachable ? "Connected" : "Offline";
  $("metric-router-detail").textContent = router.reachable ? `${(router.models || []).length} models` : (router.error || router.base_url || "Unavailable");
  badge($("router-badge"), router.reachable ? "Connected" : "Offline", router.reachable ? "ok" : "bad");
  setInput("router-url", config.base_url || "");
  if (!ui.routerDirty) renderRouterModelControls(config, router);
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
  $("metric-agents-detail").textContent = globallyEnabled ? `${busy} busy · ${slots.length} total` : "New delegation is blocked";
  $("agent-slots").classList.toggle("paused", !globallyEnabled);
  $("agent-slots").innerHTML = slots.map((slot, index) => {
    const status = slot.busy ? "Busy" : !globallyEnabled ? "Paused" : slot.enabled ? "Ready" : "Off";
    const statusClass = slot.busy ? "ok" : globallyEnabled && slot.enabled ? "warn" : "bad";
    const detail = slot.busy ? `working on ${esc(slot.active_run_id)}` : globallyEnabled ? "available" : "delegation paused";
    return `
    <article class="agent-card" data-slot="${esc(slot.id)}">
      <div class="agent-head">
        <div class="agent-name"><span class="agent-icon">${String(index + 1).padStart(2, "0")}</span><span><strong>Sub-agent slot</strong><small>${esc(slot.id)} · ${detail}</small></span></div>
        <span class="badge ${statusClass}">${status}</span>
      </div>
      <div class="toggle-list">
        ${toggleHTML(slot.id, "enabled", "Slot enabled", slot.enabled, slot.busy)}
        ${toggleHTML(slot.id, "allow_images", "Images", slot.allow_images, false)}
        ${toggleHTML(slot.id, "rtk", "RTK", slot.rtk, false)}
        ${toggleHTML(slot.id, "caveman", "Caveman", slot.caveman, false)}
      </div>
      <div class="agent-actions"><button class="button danger remove-agent" type="button" data-slot="${esc(slot.id)}" ${slot.busy || slots.length <= 1 ? "disabled" : ""}>Remove</button></div>
    </article>`;
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
    const active = run.status === "queued" || run.status === "running";
    const detail = [run.slot_id, run.provider, run.model, run.requires_images ? "image verified" : "text", run.rtk ? "RTK" : "", run.caveman ? "Caveman" : ""].filter(Boolean).join(" · ");
    const output = run.error ? `Error: ${run.error}` : run.output || "";
    return `<article class="run-card">
      <div class="run-head">
        <div class="run-identity"><strong title="${esc(identity)}">${esc(identity)}</strong><small>${esc(role)}</small></div>
        <div class="run-copy"><p title="${esc(run.task)}">${esc(run.task)}</p><small>${esc(run.activity || run.phase || "")}</small></div>
        <div class="run-meta"><span class="badge ${stateClass(run.status)}">${esc(run.status)}</span>${active ? `<button class="button danger cancel-run" data-run="${esc(run.id)}" type="button">Cancel</button>` : ""}</div>
      </div>
      <div class="detail-strip run-detail"><code>${esc(detail)}</code><span>${esc(run.id)}</span></div>
      ${output ? `<pre class="run-output">${esc(output)}</pre>` : ""}
    </article>`;
  }).join("");
}

async function refresh(silent = true) {
  try {
    const state = await api("/api/state");
    render(state);
  } catch (error) {
    $("service-dot").className = "status-dot bad";
    $("service-health").textContent = "Dashboard disconnected";
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
    notify(error.message, true);
    throw error;
  }
}

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

document.addEventListener("click", async (event) => {
  const copy = event.target.closest(".copy");
  if (copy) {
    const value = $(copy.dataset.copyTarget)?.textContent || "";
    try { await navigator.clipboard.writeText(value); notify("Copied to clipboard."); } catch { notify("Clipboard access was blocked.", true); }
    return;
  }
  const remove = event.target.closest(".remove-agent");
  if (remove) {
    await mutate(`/api/agents/${encodeURIComponent(remove.dataset.slot)}`, { method: "DELETE" }, "Sub-agent removed.");
    return;
  }
  const cancel = event.target.closest(".cancel-run");
  if (cancel) {
    await mutate(`/api/runs/${encodeURIComponent(cancel.dataset.run)}/cancel`, { method: "POST" }, "Agent run cancelled.");
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

$("add-agent").addEventListener("click", () => mutate("/api/agents", { method: "POST", body: {} }, "Sub-agent slot added."));

$("router-form").addEventListener("input", () => { ui.routerDirty = true; });
$("router-form").addEventListener("change", (event) => {
  ui.routerDirty = true;
  if (event.target.classList.contains("router-model-option")) syncRouterDefaultOptions();
});

$("router-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const allowedModels = selectedRouterModels();
  if (!allowedModels.length) {
    notify("Select at least one allowed model.", true);
    return;
  }
  const body = {
    router_base_url: $("router-url").value.trim(),
    router_default_model: $("router-model").value,
    router_allowed_models: allowedModels,
  };
  const key = $("router-key").value.trim();
  if (key) body.router_api_key = key;
  ui.routerDirty = false;
  try {
    await mutate("/api/settings", { method: "PATCH", body }, "9Router settings saved.");
    $("router-key").value = "";
  } catch {
    ui.routerDirty = true;
  }
});

$("probe-router").addEventListener("click", () => mutate("/api/router/refresh", { method: "POST", body: {} }, "9Router probe completed."));

$("tunnel-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  await mutate("/api/settings", { method: "PATCH", body: {
    tunnel_mode: $("tunnel-mode").value,
    tunnel_name: $("tunnel-name").value.trim(),
    tunnel_domain: $("tunnel-domain").value.trim(),
    tunnel_protocol: $("tunnel-protocol").value,
  } }, "Tunnel settings saved.");
});

$("start-tunnel").addEventListener("click", () => mutate("/api/tunnel/start", { method: "POST", body: { mode: $("tunnel-mode").value } }, "Cloudflare tunnel started."));
$("stop-tunnel").addEventListener("click", () => mutate("/api/tunnel/stop", { method: "POST", body: {} }, "Cloudflare tunnel stopped."));
$("generate-dns").addEventListener("click", async () => {
  const domain = $("tunnel-domain").value.trim();
  const result = await mutate("/api/tunnel/dns", { method: "POST", body: { domain } }, "Cloudflare DNS route generated.");
  const dns = $("dns-result");
  dns.textContent = `Type: CNAME\nName: ${domain}\nTarget: ${result.dns_target || "created by cloudflared"}\nTunnel ID: ${result.tunnel_id || "resolved by Cloudflare"}`;
  dns.classList.remove("hidden");
});

$("browser-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  await mutate("/api/settings", { method: "PATCH", body: {
    lightpanda_path: $("browser-path").value.trim(),
    lightpanda_port: Number($("browser-port").value),
    lightpanda_obey_robots: $("browser-robots").checked,
  } }, "Lightpanda settings saved.");
});
$("start-browser").addEventListener("click", () => mutate("/api/lightpanda/start", { method: "POST", body: {} }, "Lightpanda started."));
$("stop-browser").addEventListener("click", () => mutate("/api/lightpanda/stop", { method: "POST", body: {} }, "Lightpanda stopped."));

const observer = new IntersectionObserver((entries) => {
  const visible = entries.filter((entry) => entry.isIntersecting).sort((a, b) => b.intersectionRatio - a.intersectionRatio)[0];
  if (!visible) return;
  document.querySelectorAll(".nav-link").forEach((link) => link.classList.toggle("active", link.getAttribute("href") === `#${visible.target.id}`));
}, { rootMargin: "-20% 0px -65% 0px", threshold: [0, .2, .6] });
document.querySelectorAll("main section[id]").forEach((section) => observer.observe(section));

refresh(false).then(() => {
  ui.timer = setInterval(() => refresh(true), 1000);
});
