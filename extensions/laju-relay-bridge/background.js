importScripts("config.js");

const config = self.TAUTLINE_RELAY_CONFIG || {};
const pendingPrefix = "tautline-pending-";
let polling = false;

function bridgeHeaders(json = false) {
  const headers = {
    Authorization: `Bearer ${config.token || ""}`,
    "X-Tautline-Browser": "Laju Browser",
    "X-Tautline-Bridge-Version": chrome.runtime.getManifest().version
  };
  if (json) headers["Content-Type"] = "application/json";
  return headers;
}

async function clientID() {
  const stored = await chrome.storage.local.get("tautlineClientID");
  if (stored.tautlineClientID) return stored.tautlineClientID;
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  const value = `laju-${Array.from(bytes, byte => byte.toString(16).padStart(2, "0")).join("")}`;
  await chrome.storage.local.set({ tautlineClientID: value });
  return value;
}

async function acknowledge(id, status, error = "") {
  await fetch(`${config.endpoint}/ack`, {
    method: "POST",
    headers: bridgeHeaders(true),
    body: JSON.stringify({
      client_id: await clientID(),
      dispatch_id: id,
      status,
      error: String(error).slice(0, 512)
    })
  });
}

async function deliver(dispatch) {
  const tab = await chrome.tabs.create({ url: "https://chatgpt.com/", active: false });
  if (!tab.id) throw new Error("Laju did not return a worker tab ID");
  await chrome.storage.local.set({
    [`${pendingPrefix}${tab.id}`]: {
      dispatchID: dispatch.dispatch_id,
      runID: dispatch.run_id,
      prompt: dispatch.prompt,
      expiresAt: dispatch.expires_at
    }
  });
}

async function pollOnce() {
  if (polling || !config.endpoint || !config.token) return;
  polling = true;
  try {
    const id = await clientID();
    const response = await fetch(`${config.endpoint}/next?client_id=${encodeURIComponent(id)}&wait_seconds=15`, {
      headers: bridgeHeaders()
    });
    if (response.status === 204) return;
    if (!response.ok) throw new Error(`Tautline relay returned HTTP ${response.status}`);
    const dispatch = await response.json();
    try {
      await deliver(dispatch);
    } catch (error) {
      await acknowledge(dispatch.dispatch_id, "failed", error?.message || error);
    }
  } catch {
    // Tautline may be stopped or switching. The next wake retries without exposing credentials.
  } finally {
    polling = false;
  }
}

async function pendingForTab(tabID) {
  const key = `${pendingPrefix}${tabID}`;
  const stored = await chrome.storage.local.get(key);
  return stored[key] || null;
}

async function clearPending(tabID) {
  await chrome.storage.local.remove(`${pendingPrefix}${tabID}`);
}

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message?.type === "tautline-wake") {
    void pollOnce();
    sendResponse({ ok: true });
    return false;
  }
  if (message?.type === "tautline-content-ready" && sender.tab?.id) {
    void pendingForTab(sender.tab.id).then(pending => sendResponse({ pending }));
    return true;
  }
  if (message?.type === "tautline-dispatch-result" && sender.tab?.id) {
    void (async () => {
      const pending = await pendingForTab(sender.tab.id);
      if (!pending) return;
      await clearPending(sender.tab.id);
      await acknowledge(pending.dispatchID, message.ok ? "sent" : "failed", message.error || "");
    })();
    sendResponse({ ok: true });
    return false;
  }
  return false;
});

chrome.tabs.onRemoved.addListener(tabID => {
  void pendingForTab(tabID).then(async pending => {
    if (!pending) return;
    await clearPending(tabID);
    await acknowledge(pending.dispatchID, "failed", "Worker tab was closed before the prompt was sent");
  });
});

chrome.runtime.onInstalled.addListener(() => {
  chrome.alarms.create("tautline-relay-poll", { periodInMinutes: 1 });
  void pollOnce();
});
chrome.runtime.onStartup.addListener(() => void pollOnce());
chrome.alarms.onAlarm.addListener(alarm => {
  if (alarm.name === "tautline-relay-poll") void pollOnce();
});
void pollOnce();
