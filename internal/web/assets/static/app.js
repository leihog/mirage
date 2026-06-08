document.addEventListener("DOMContentLoaded", () => {
  const currentID = location.pathname.startsWith("/messages/") ? location.pathname.split("/")[2] : "";
  const list = document.querySelector(".list");
  const inboxCount = document.querySelector(".inbox-count");
  const settingsModal = document.querySelector(".settings-modal");
  const unsubscribeModal = document.querySelector(".unsubscribe-modal");
  const themeKey = "mirage-theme";
  const systemTheme = matchMedia("(prefers-color-scheme: dark)");
  const themeInputs = Array.from(document.querySelectorAll('input[name="theme"]'));

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
  document.querySelector("[data-unsubscribe-ok]")?.addEventListener("click", () => unsubscribeModal?.close());

  document.querySelector(".unsubscribe-form")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = event.currentTarget;
    const button = form.querySelector("button");
    button.disabled = true;
    try {
      const response = await fetch(form.action, {
        method: "POST",
        headers: { Accept: "application/json" },
      });
      const result = response.headers.get("Content-Type")?.includes("application/json")
        ? await response.json()
        : {
            url: "",
            success: false,
            statusCode: response.status,
            status: response.statusText || String(response.status),
            error: "Mirage returned a non-JSON unsubscribe response.",
          };
      showUnsubscribeResult(result);
    } catch (error) {
      showUnsubscribeResult({
        url: "",
        success: false,
        statusCode: 0,
        status: "Request failed",
        error: error instanceof Error ? error.message : String(error),
      });
    } finally {
      button.disabled = false;
    }
  });

  document.querySelectorAll(".tab").forEach((tab) => {
    tab.addEventListener("click", () => {
      document.querySelectorAll(".tab").forEach((item) => item.classList.remove("active"));
      document.querySelectorAll(".tab-panel").forEach((panel) => panel.classList.remove("active"));
      tab.classList.add("active");
      document.getElementById("tab-" + tab.dataset.tab)?.classList.add("active");
    });
  });

  if (!list) return;

  let knownIDs = new Set(Array.from(document.querySelectorAll(".message")).map((item) => item.dataset.id));
  async function refreshMessages() {
    const response = await fetch("/api/messages", { cache: "no-store" });
    if (!response.ok) return;
    const messages = await response.json();
    const nextIDs = new Set(messages.map((message) => message.ID));
    const changed = messages.length !== knownIDs.size || messages.some((message) => !knownIDs.has(message.ID));
    if (!changed) return;
    knownIDs = nextIDs;
    renderList(messages);
  }

  function renderList(messages) {
    const entries = list.querySelectorAll(".message-row, .empty");
    entries.forEach((entry) => entry.remove());
    if (inboxCount) inboxCount.textContent = mailboxLine(messages);
    if (messages.length === 0) {
      list.insertAdjacentHTML("beforeend", '<div class="empty">No captured emails yet.</div>');
      return;
    }
    for (const message of messages) {
      const active = message.ID === currentID ? " active" : "";
      const unread = message.Viewed ? "" : " unread";
      const to = Array.isArray(message.To) ? message.To.join(", ") : "";
      const date = new Date(message.CreatedAt);
      const time = Number.isNaN(date.getTime()) ? "" : date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
      list.insertAdjacentHTML("beforeend", '<div class="message-row"><a class="message' + active + unread + '" data-id="' + escapeAttr(message.ID) + '" href="/messages/' + encodeURIComponent(message.ID) + '"><span class="subject">' + escapeHTML(message.Subject || "(no subject)") + '</span><time>' + escapeHTML(time) + '</time><span class="meta">' + escapeHTML((message.From || "") + " -> " + to) + '</span></a><form class="delete-form list-delete" method="post" action="/messages/' + encodeURIComponent(message.ID) + '/delete"><button class="icon-button danger" type="submit" title="Delete message" aria-label="Delete message"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M10 11v6M14 11v6M6 7l1 14h10l1-14M9 7V4h6v3"/></svg></button></form></div>');
    }
  }

  function mailboxLine(messages) {
    const count = messages.length;
    const unread = messages.filter((message) => !message.Viewed).length;
    return count + " " + (count === 1 ? "mail" : "mails") + ", " + unread + " unread";
  }

  function showUnsubscribeResult(result) {
    const url = unsubscribeModal?.querySelector("[data-unsubscribe-url]");
    const resultNode = unsubscribeModal?.querySelector("[data-unsubscribe-result]");
    const status = unsubscribeModal?.querySelector("[data-unsubscribe-status]");
    const json = unsubscribeModal?.querySelector("[data-unsubscribe-json]");
    if (url) url.textContent = result.url || "(unavailable)";
    if (resultNode) {
      resultNode.textContent = result.success ? "Success" : "Failed";
      resultNode.className = result.success ? "result-success" : "result-failed";
    }
    if (status) {
      const code = Number(result.statusCode) || 0;
      status.textContent = code > 0 ? code + " " + (result.status || "") : (result.status || result.error || "No response");
    }
    if (json) {
      if (result.json !== undefined) {
        json.hidden = false;
        json.textContent = JSON.stringify(result.json, null, 2);
      } else {
        json.hidden = true;
        json.textContent = "";
      }
    }
    if (typeof unsubscribeModal?.showModal === "function") {
      unsubscribeModal.showModal();
    } else {
      unsubscribeModal?.setAttribute("open", "");
    }
  }

  function escapeHTML(value) {
    return String(value).replace(/[&<>"']/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[char]));
  }
  function escapeAttr(value) {
    return escapeHTML(value);
  }

  setInterval(() => refreshMessages().catch(() => {}), 2000);
});
