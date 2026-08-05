(() => {
  const sleep = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));

  async function waitFor(find, timeout = 30000) {
    const deadline = Date.now() + timeout;
    while (Date.now() < deadline) {
      const value = find();
      if (value) return value;
      await sleep(150);
    }
    throw new Error("ChatGPT composer did not become ready");
  }

  function composer() {
    return document.querySelector("#prompt-textarea") ||
      document.querySelector('textarea[placeholder]') ||
      document.querySelector('[contenteditable="true"][data-lexical-editor="true"]') ||
      document.querySelector('main [contenteditable="true"]');
  }

  function clearComposer(node) {
    node.focus();
    if (node instanceof HTMLTextAreaElement || node instanceof HTMLInputElement) {
      const setter = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(node), "value")?.set;
      if (setter) setter.call(node, "");
      else node.value = "";
      node.dispatchEvent(new Event("input", { bubbles: true }));
      return;
    }
    document.execCommand("selectAll", false);
    document.execCommand("delete", false);
    node.dispatchEvent(new InputEvent("input", { bubbles: true, inputType: "deleteContentBackward" }));
  }

  function insertText(node, text) {
    node.focus();
    if (node instanceof HTMLTextAreaElement || node instanceof HTMLInputElement) {
      const prototype = Object.getPrototypeOf(node);
      const setter = Object.getOwnPropertyDescriptor(prototype, "value")?.set;
      const next = `${node.value || ""}${text}`;
      if (setter) setter.call(node, next);
      else node.value = next;
      node.dispatchEvent(new Event("input", { bubbles: true }));
      return;
    }
    if (!document.execCommand("insertText", false, text)) {
      node.textContent = `${node.textContent || ""}${text}`;
      node.dispatchEvent(new InputEvent("input", { bubbles: true, data: text, inputType: "insertText" }));
    }
  }

  async function insertPrompt(node, prompt) {
    clearComposer(node);
    const prefix = "@Tautline";
    if (!prompt.startsWith(prefix)) {
      insertText(node, prompt);
      return;
    }
    for (const character of prefix) {
      insertText(node, character);
      await sleep(35);
    }
    let option = null;
    for (let attempt = 0; attempt < 12 && !option; attempt += 1) {
      option = Array.from(document.querySelectorAll('[role="option"], [role="menuitem"], [role="listbox"] button, [role="dialog"] button'))
        .find(candidate => candidate.textContent?.toLowerCase().includes("tautline"));
      if (!option) await sleep(150);
    }
    if (option) option.click();
    insertText(node, prompt.slice(prefix.length));
  }

  async function sendPrompt(prompt) {
    const node = await waitFor(composer);
    await insertPrompt(node, prompt);
    const button = await waitFor(() => {
      const candidates = [
        document.querySelector('button[data-testid="send-button"]'),
        document.querySelector('button[aria-label*="Send" i]'),
        node.closest("form")?.querySelector('button[type="submit"]')
      ];
      return candidates.find(candidate => candidate && !candidate.disabled);
    }, 15000);
    button.click();
  }

  async function acceptPending() {
    let pending = null;
    for (let attempt = 0; attempt < 20 && !pending?.prompt; attempt += 1) {
      const response = await chrome.runtime.sendMessage({ type: "tautline-content-ready" });
      pending = response?.pending || null;
      if (!pending?.prompt) await sleep(250);
    }
    if (!pending?.prompt) return;
    try {
      if (pending.expiresAt && Date.now() >= Date.parse(pending.expiresAt)) {
        throw new Error("Relay worker prompt expired before ChatGPT became ready");
      }
      await sendPrompt(pending.prompt);
      await chrome.runtime.sendMessage({ type: "tautline-dispatch-result", ok: true });
    } catch (error) {
      await chrome.runtime.sendMessage({
        type: "tautline-dispatch-result",
        ok: false,
        error: String(error?.message || error).slice(0, 512)
      });
    }
  }

  void chrome.runtime.sendMessage({ type: "tautline-wake" });
  void acceptPending();
  setInterval(() => void chrome.runtime.sendMessage({ type: "tautline-wake" }), 3000);
})();
