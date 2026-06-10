document.addEventListener("DOMContentLoaded", () => {
  const currentID = location.pathname.startsWith("/messages/") ? location.pathname.split("/")[2] : "";
  const list = document.querySelector(".list");
  const inboxCount = document.querySelector(".inbox-count");
  const settingsModal = document.querySelector(".settings-modal");
  const unsubscribeModal = document.querySelector(".unsubscribe-modal");
  const clearForm = document.querySelector(".clear-form");
  const clearConfirm = document.querySelector("[data-clear-confirm]");
  const themeKey = "mirage-theme";
  const timezoneKey = "mirage-timezone";
  const time24Key = "mirage-time-24";
  const dateFormatKey = "mirage-date-format";
  const previewWidthKey = "mirage-preview-width";
  const systemTheme = matchMedia("(prefers-color-scheme: dark)");
  const themeInputs = Array.from(document.querySelectorAll('input[name="theme"]'));
  const timezoneSelect = document.querySelector("[data-timezone-select]");
  const time24Toggle = document.querySelector("[data-time-24-toggle]");
  const dateFormatSelect = document.querySelector("[data-date-format-select]");
  const previewCanvas = document.querySelector("[data-preview-canvas]");
  const previewButtons = Array.from(document.querySelectorAll("[data-preview-mode]"));
  const previewWidthControl = document.querySelector("[data-preview-width]");
  const tabDownload = document.querySelector("[data-tab-download]");
  const unsubscribeOpen = document.querySelector("[data-unsubscribe-open]");
  const unsubscribeSend = document.querySelector("[data-unsubscribe-send]");
  const unsubscribeCopyCurl = document.querySelector("[data-unsubscribe-copy-curl]");

  function themeChoice() {
    const stored = localStorage.getItem(themeKey);
    return stored === "light" || stored === "dark" || stored === "auto" ? stored : "auto";
  }

  function applyTheme(choice) {
    const resolved = choice === "auto" ? (systemTheme.matches ? "dark" : "light") : choice;
    document.documentElement.dataset.themeChoice = choice;
    document.documentElement.dataset.theme = resolved;
    themeInputs.forEach((input) => {
      input.checked = input.value === choice;
    });
  }

  applyTheme(themeChoice());
  themeInputs.forEach((input) => {
    input.addEventListener("change", () => {
      if (!input.checked) return;
      localStorage.setItem(themeKey, input.value);
      applyTheme(input.value);
    });
  });
  systemTheme.addEventListener("change", () => {
    if (themeChoice() === "auto") applyTheme("auto");
  });

  function supportedTimeZones() {
    if (typeof Intl.supportedValuesOf !== "function") return [];
    try {
      return Intl.supportedValuesOf("timeZone");
    } catch {
      return [];
    }
  }

  function timezoneChoice() {
    const stored = localStorage.getItem(timezoneKey);
    if (!stored || stored === "local" || stored === "UTC") return stored || "local";
    try {
      new Intl.DateTimeFormat([], { timeZone: stored }).format(new Date());
      return stored;
    } catch {
      return "local";
    }
  }

  function time24Choice() {
    const stored = localStorage.getItem(time24Key);
    return stored === "12" ? false : true;
  }

  function dateFormatChoice() {
    const stored = localStorage.getItem(dateFormatKey);
    return stored === "us" || stored === "eu" || stored === "iso" ? stored : "iso";
  }

  function populateTimezones() {
    if (!timezoneSelect) return;
    const current = timezoneChoice();
    for (const zone of supportedTimeZones()) {
      if (zone === "UTC") continue;
      const option = document.createElement("option");
      option.value = zone;
      option.textContent = zone.replace(/_/g, " ");
      timezoneSelect.append(option);
    }
    if (!Array.from(timezoneSelect.options).some((option) => option.value === current)) {
      const option = document.createElement("option");
      option.value = current;
      option.textContent = current.replace(/_/g, " ");
      timezoneSelect.append(option);
    }
    timezoneSelect.value = Array.from(timezoneSelect.options).some((option) => option.value === current) ? current : "local";
  }

  function activeTimeZone() {
    const choice = timezoneChoice();
    return choice === "local" ? undefined : choice;
  }

  function timezoneFormatOptions(mode) {
    const options = mode === "datetime"
      ? { weekday: "short", year: "numeric", month: "short", day: "numeric", hour: "numeric", minute: "2-digit" }
      : { hour: "2-digit", minute: "2-digit" };
    options.hour12 = !time24Choice();
    const zone = activeTimeZone();
    if (zone) options.timeZone = zone;
    return options;
  }

  function dateParts(date) {
    const options = { year: "numeric", month: "2-digit", day: "2-digit" };
    const zone = activeTimeZone();
    if (zone) options.timeZone = zone;
    try {
      const parts = new Intl.DateTimeFormat("en-US", options).formatToParts(date);
      return {
        year: parts.find((part) => part.type === "year")?.value || "0000",
        month: parts.find((part) => part.type === "month")?.value || "00",
        day: parts.find((part) => part.type === "day")?.value || "00",
      };
    } catch {
      const parts = new Intl.DateTimeFormat("en-US", { year: "numeric", month: "2-digit", day: "2-digit", timeZone: "UTC" }).formatToParts(date);
      return {
        year: parts.find((part) => part.type === "year")?.value || "0000",
        month: parts.find((part) => part.type === "month")?.value || "00",
        day: parts.find((part) => part.type === "day")?.value || "00",
      };
    }
  }

  function formatDateOnly(date) {
    const parts = dateParts(date);
    switch (dateFormatChoice()) {
      case "us":
        return parts.month + "/" + parts.day + "/" + parts.year;
      case "eu":
        return parts.day + "/" + parts.month + "/" + parts.year;
      default:
        return parts.year + "-" + parts.month + "-" + parts.day;
    }
  }

  function isWithinLast24Hours(date) {
    const age = Date.now() - date.getTime();
    return age >= 0 && age < 24 * 60 * 60 * 1000;
  }

  function formatTimestamp(value, mode = "time") {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "";
    if (mode === "inbox" && !isWithinLast24Hours(date)) return formatDateOnly(date);
    try {
      return mode === "datetime" ? date.toLocaleString([], timezoneFormatOptions(mode)) : date.toLocaleTimeString([], timezoneFormatOptions(mode));
    } catch {
      return mode === "datetime"
        ? date.toLocaleString([], { weekday: "short", year: "numeric", month: "short", day: "numeric", hour: "numeric", minute: "2-digit", hour12: !time24Choice() })
        : date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: !time24Choice() });
    }
  }

  function applyDisplayTimezone() {
    document.querySelectorAll("time[data-timestamp]").forEach((node) => {
      node.textContent = formatTimestamp(node.dataset.timestamp, node.dataset.timestampFormat || "time");
    });
  }

  populateTimezones();
  if (time24Toggle) time24Toggle.checked = time24Choice();
  if (dateFormatSelect) dateFormatSelect.value = dateFormatChoice();
  applyDisplayTimezone();
  timezoneSelect?.addEventListener("change", () => {
    localStorage.setItem(timezoneKey, timezoneSelect.value || "local");
    applyDisplayTimezone();
  });
  time24Toggle?.addEventListener("change", () => {
    localStorage.setItem(time24Key, time24Toggle.checked ? "24" : "12");
    applyDisplayTimezone();
  });
  dateFormatSelect?.addEventListener("change", () => {
    localStorage.setItem(dateFormatKey, dateFormatSelect.value || "iso");
    applyDisplayTimezone();
  });

  function previewWidthChoice() {
    const stored = localStorage.getItem(previewWidthKey);
    return stored === "mobile" || stored === "desktop" || stored === "full" ? stored : "desktop";
  }

  function applyPreviewWidth(choice) {
    if (!previewCanvas) return;
    previewCanvas.dataset.previewCurrent = choice;
    previewButtons.forEach((button) => {
      const active = button.dataset.previewMode === choice;
      button.classList.toggle("active", active);
      button.setAttribute("aria-pressed", active ? "true" : "false");
    });
  }

  applyPreviewWidth(previewWidthChoice());
  previewButtons.forEach((button) => {
    button.addEventListener("click", () => {
      const choice = button.dataset.previewMode || "desktop";
      localStorage.setItem(previewWidthKey, choice);
      applyPreviewWidth(choice);
    });
  });

  document.querySelector("[data-settings-open]")?.addEventListener("click", () => {
    if (typeof settingsModal?.showModal === "function") {
      settingsModal.showModal();
    } else {
      settingsModal?.setAttribute("open", "");
    }
  });
  document.querySelector("[data-settings-close]")?.addEventListener("click", () => settingsModal?.close());
  settingsModal?.addEventListener("click", (event) => {
    if (event.target === settingsModal) settingsModal.close();
  });
  document.querySelector("[data-unsubscribe-close]")?.addEventListener("click", () => unsubscribeModal?.close());
  unsubscribeModal?.addEventListener("click", (event) => {
    if (event.target === unsubscribeModal) unsubscribeModal.close();
  });
  document.querySelector("[data-clear-open]")?.addEventListener("click", () => {
    if (clearConfirm) clearConfirm.hidden = false;
  });
  document.querySelector("[data-clear-cancel]")?.addEventListener("click", () => {
    if (clearConfirm) clearConfirm.hidden = true;
  });
  document.addEventListener("click", (event) => {
    if (!clearConfirm || clearConfirm.hidden) return;
    if (event.target instanceof Node && clearForm?.contains(event.target)) return;
    clearConfirm.hidden = true;
  });
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape" && clearConfirm) clearConfirm.hidden = true;
  });
  document.querySelector("[data-copy-message-id]")?.addEventListener("click", async (event) => {
    const button = event.currentTarget;
    await copyFromButton(button, button.dataset.copyMessageId || "");
  });
  document.querySelector("[data-copy-curl]")?.addEventListener("click", async (event) => {
    const button = event.currentTarget;
    const id = button.dataset.copyCurl || currentID;
    const url = location.origin + "/api/v1/message/" + encodeURIComponent(id);
    await copyFromButton(button, "curl -fsS " + shellQuote(url));
  });

  unsubscribeOpen?.addEventListener("click", () => {
    openUnsubscribeInspector(unsubscribeOpen.dataset.unsubscribeAction || "", unsubscribeOpen.dataset.unsubscribeTargetUrl || "");
  });
  unsubscribeCopyCurl?.addEventListener("click", async (event) => {
    const button = event.currentTarget;
    const targetURL = unsubscribeModal?.dataset.unsubscribeUrl || "";
    if (!targetURL) return;
    await copyFromButton(button, unsubscribeCurlCommand(targetURL));
  });
  unsubscribeSend?.addEventListener("click", async () => {
    const action = unsubscribeModal?.dataset.unsubscribeAction || "";
    if (!action || !unsubscribeSend) return;
    unsubscribeSend.disabled = true;
    renderUnsubscribePending();
    try {
      const response = await fetch(action, {
        method: "POST",
        headers: { Accept: "application/json" },
      });
      const result = response.headers.get("Content-Type")?.includes("application/json")
        ? await response.json()
        : {
            url: unsubscribeModal?.dataset.unsubscribeUrl || "",
            statusCode: response.status,
            status: response.statusText || String(response.status),
            error: "Mirage returned a non-JSON unsubscribe response.",
          };
      renderUnsubscribeResponse(result);
    } catch (error) {
      renderUnsubscribeResponse({
        url: unsubscribeModal?.dataset.unsubscribeUrl || "",
        statusCode: 0,
        status: "Request failed",
        error: error instanceof Error ? error.message : String(error),
      });
    } finally {
      unsubscribeSend.disabled = false;
    }
  });

  function updateTabActions(tabName) {
    if (previewWidthControl) previewWidthControl.hidden = tabName !== "html";
    if (!tabDownload) return;

    const downloads = {
      source: {
        part: "html",
        filename: "message-" + currentID + ".html",
        label: "Download HTML source",
      },
      text: {
        part: "text",
        filename: "message-" + currentID + ".txt",
        label: "Download text",
      },
      raw: {
        part: "raw",
        filename: "message-" + currentID + ".eml",
        label: "Download raw email",
      },
    };
    const download = downloads[tabName];
    tabDownload.hidden = !download;
    if (!download) return;

    tabDownload.href = "/api/v1/message/" + encodeURIComponent(currentID) + "/body/" + download.part + "?download=1";
    tabDownload.download = download.filename;
    tabDownload.title = download.label;
    tabDownload.setAttribute("aria-label", download.label);
  }

  document.querySelectorAll(".tab").forEach((tab) => {
    tab.addEventListener("click", () => {
      document.querySelectorAll(".tab").forEach((item) => item.classList.remove("active"));
      document.querySelectorAll(".tab-panel").forEach((panel) => panel.classList.remove("active"));
      tab.classList.add("active");
      document.getElementById("tab-" + tab.dataset.tab)?.classList.add("active");
      updateTabActions(tab.dataset.tab || "");
    });
  });
  updateTabActions(document.querySelector(".tab.active")?.dataset.tab || "");

  if (!list) return;

  let knownSignature = Array.from(document.querySelectorAll(".message")).map((item) => item.dataset.id + ":" + item.classList.contains("unread")).join("|");
  async function refreshMessages() {
    const response = await fetch("/api/v1/inbox?limit=50&offset=0", { cache: "no-store" });
    if (!response.ok) return;
    const inbox = await response.json();
    const messages = Array.isArray(inbox.messages) ? inbox.messages : [];
    const nextSignature = messages.map((message) => message.id + ":" + !message.viewed).join("|") + "|total:" + inbox.total + "|unread:" + inbox.unreadTotal;
    if (nextSignature === knownSignature) return;
    knownSignature = nextSignature;
    renderList(messages, inbox);
  }

  function renderList(messages, inbox) {
    const entries = list.querySelectorAll(".message-row, .empty");
    entries.forEach((entry) => entry.remove());
    if (inboxCount) inboxCount.textContent = mailboxLine(inbox);
    if (messages.length === 0) {
      list.insertAdjacentHTML("beforeend", '<div class="empty">No captured emails yet.</div>');
      return;
    }
    for (const message of messages) {
      const active = message.id === currentID ? " active" : "";
      const unread = message.viewed ? "" : " unread";
      const to = Array.isArray(message.to) ? message.to.join(", ") : "";
      const from = message.from || "";
      const timestamp = message.createdAt || "";
      const time = formatTimestamp(timestamp, "inbox");
      list.insertAdjacentHTML("beforeend", '<div class="message-row"><a class="message' + active + unread + '" data-id="' + escapeAttr(message.id) + '" href="/messages/' + encodeURIComponent(message.id) + '"><span class="sender-name">' + escapeHTML(senderName(from)) + '</span><time datetime="' + escapeAttr(timestamp) + '" data-timestamp="' + escapeAttr(timestamp) + '" data-timestamp-format="inbox">' + escapeHTML(time) + '</time><span class="to-line">' + escapeHTML("To: " + to) + '</span><span class="subject">' + escapeHTML(message.subject || "(no subject)") + '</span></a></div>');
    }
  }

  function mailboxLine(inbox) {
    const count = Number(inbox?.total) || 0;
    const unread = Number(inbox?.unreadTotal) || 0;
    return count + " " + (count === 1 ? "mail" : "mails") + ", " + unread + " unread";
  }

  function openUnsubscribeInspector(action, targetURL) {
    if (!unsubscribeModal) return;
    unsubscribeModal.dataset.unsubscribeAction = action;
    unsubscribeModal.dataset.unsubscribeUrl = targetURL;
    const url = unsubscribeModal.querySelector("[data-unsubscribe-url]");
    const response = unsubscribeModal.querySelector("[data-unsubscribe-response]");
    const output = unsubscribeModal.querySelector("[data-unsubscribe-output]");
    const status = unsubscribeModal.querySelector("[data-unsubscribe-status]");
    const time = unsubscribeModal.querySelector("[data-unsubscribe-time]");
    const size = unsubscribeModal.querySelector("[data-unsubscribe-size]");
    if (url) url.textContent = targetURL || "(unavailable)";
    if (response) response.hidden = true;
    if (output) output.textContent = "";
    if (status) {
      status.textContent = "Not sent";
      status.className = "status-pill";
    }
    if (time) time.textContent = "";
    if (size) size.textContent = "";
    if (typeof unsubscribeModal.showModal === "function") {
      unsubscribeModal.showModal();
    } else {
      unsubscribeModal.setAttribute("open", "");
    }
  }

  function renderUnsubscribePending() {
    if (!unsubscribeModal) return;
    const response = unsubscribeModal.querySelector("[data-unsubscribe-response]");
    const output = unsubscribeModal.querySelector("[data-unsubscribe-output]");
    const status = unsubscribeModal.querySelector("[data-unsubscribe-status]");
    const time = unsubscribeModal.querySelector("[data-unsubscribe-time]");
    const size = unsubscribeModal.querySelector("[data-unsubscribe-size]");
    if (response) response.hidden = false;
    if (status) {
      status.textContent = "Sending";
      status.className = "status-pill pending";
    }
    if (time) time.textContent = "";
    if (size) size.textContent = "";
    if (output) output.textContent = "POST request in flight...";
  }

  function renderUnsubscribeResponse(result) {
    if (!unsubscribeModal) return;
    const response = unsubscribeModal.querySelector("[data-unsubscribe-response]");
    const output = unsubscribeModal.querySelector("[data-unsubscribe-output]");
    const status = unsubscribeModal.querySelector("[data-unsubscribe-status]");
    const time = unsubscribeModal.querySelector("[data-unsubscribe-time]");
    const size = unsubscribeModal.querySelector("[data-unsubscribe-size]");
    const statusCode = Number(result.statusCode) || 0;
    if (response) response.hidden = false;
    if (status) {
      status.textContent = statusCode > 0 ? statusCode + " " + (result.status || "") : (result.status || "Request failed");
      status.className = "status-pill " + statusClass(statusCode, result.error);
    }
    if (time) time.textContent = result.durationMs !== undefined ? String(result.durationMs) + " ms" : "";
    if (size) size.textContent = result.responseBodySize !== undefined ? formatBytes(result.responseBodySize) : "";
    if (output) output.textContent = unsubscribeResponseText(result);
  }

  function statusClass(statusCode, error) {
    if (error || statusCode === 0) return "error";
    if (statusCode >= 200 && statusCode < 300) return "success";
    if (statusCode >= 300 && statusCode < 500) return "warning";
    return "error";
  }

  function unsubscribeResponseText(result) {
    const lines = [];
    if (result.error) {
      lines.push("Error: " + result.error, "");
    }
    lines.push("Request");
    lines.push((result.requestMethod || "POST") + " " + (result.url || unsubscribeModal?.dataset.unsubscribeUrl || ""));
    for (const [key, value] of Object.entries(result.requestHeaders || { "Content-Type": "application/x-www-form-urlencoded" }).sort()) {
      lines.push(key + ": " + value);
    }
    lines.push("");
    lines.push(result.requestBody || "List-Unsubscribe=One-Click");
    lines.push("");
    lines.push("Response");
    if (Number(result.statusCode) > 0) {
      lines.push(String(result.statusCode) + " " + (result.status || ""));
    }
    for (const [key, value] of Object.entries(result.responseHeaders || {}).sort()) {
      lines.push(key + ": " + value);
    }
    if (result.responseBody) {
      lines.push("");
      lines.push(result.responseBody);
    }
    return lines.join("\n");
  }

  function formatBytes(size) {
    const value = Number(size) || 0;
    if (value < 1024) return value + " B";
    return (value / 1024).toFixed(1) + " kB";
  }

  function unsubscribeCurlCommand(targetURL) {
    return "curl -X POST -H " + bashDoubleQuote("Content-Type: application/x-www-form-urlencoded") + " --data-urlencode " + bashDoubleQuote("List-Unsubscribe=One-Click") + " " + bashDoubleQuote(targetURL);
  }

  function escapeHTML(value) {
    return String(value).replace(/[&<>"']/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[char]));
  }
  function escapeAttr(value) {
    return escapeHTML(value);
  }
  function senderName(value) {
    const text = String(value || "").trim();
    const match = text.match(/^"?([^"<]+?)"?\s*<[^>]+>$/);
    if (match?.[1]?.trim()) return match[1].trim();
    const at = text.indexOf("@");
    return at > 0 ? text.slice(0, at) : text;
  }
  async function copyFromButton(button, value) {
    if (!value) return;
    await copyText(value);
    const originalTitle = button.getAttribute("title") || "";
    button.setAttribute("title", "Copied");
    button.dataset.copied = "true";
    setTimeout(() => {
      button.setAttribute("title", originalTitle);
      delete button.dataset.copied;
    }, 1200);
  }
  async function copyText(value) {
    if (navigator.clipboard?.writeText) {
      try {
        await navigator.clipboard.writeText(value);
        return;
      } catch {
        // Fall back for browser contexts that expose Clipboard API but reject writes.
      }
    }
    const textarea = document.createElement("textarea");
    textarea.value = value;
    textarea.setAttribute("readonly", "");
    textarea.style.position = "fixed";
    textarea.style.left = "-9999px";
    document.body.append(textarea);
    textarea.select();
    document.execCommand("copy");
    textarea.remove();
  }
  function shellQuote(value) {
    return "'" + String(value).replace(/'/g, "'\\''") + "'";
  }
  function bashDoubleQuote(value) {
    return '"' + String(value).replace(/(["\\$`])/g, "\\$1") + '"';
  }

  setInterval(() => refreshMessages().catch(() => {}), 2000);
});
