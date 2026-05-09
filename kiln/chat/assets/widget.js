// Kiln floating chat widget. Vanilla JS, no build step.
// Auto-mounts when the script loads. Renders a floating button in a
// chosen corner; click to expand a translucent control-panel chat
// surface. Aware of the current page (window.location) and includes it
// in every chat message context.
(function () {
  "use strict";

  if (window.__kilnWidgetMounted) return;
  window.__kilnWidgetMounted = true;

  // ---- Config (overridable via <script data-*>) ------------------------
  const script = document.currentScript || document.querySelector('script[src*="/kiln/chat/widget.js"]');
  const CORNER = (script && script.dataset.corner) || "bottom-right";
  const KILN_URL = (script && script.dataset.kilnUrl) || ""; // empty = same origin
  // Panel default is OPEN. Build-mode is the agent's surface — there's
  // no reason to start hidden. Caller can pass data-open="false" to
  // override per-embed; transient close stays per-page-load only.
  const START_OPEN = !(script && script.dataset.open === "false");

  // ---- Stylesheet -----------------------------------------------------
  function loadStyles() {
    if (document.getElementById("kiln-widget-style")) return;
    const link = document.createElement("link");
    link.id = "kiln-widget-style";
    link.rel = "stylesheet";
    link.href = (KILN_URL || "") + "/kiln/chat/widget.css";
    document.head.appendChild(link);
  }

  // ---- DOM scaffold ---------------------------------------------------
  function el(tag, attrs, ...children) {
    const e = document.createElement(tag);
    if (attrs) for (const k in attrs) {
      if (k === "class") e.className = attrs[k];
      else if (k === "text") e.textContent = attrs[k];
      else e.setAttribute(k, attrs[k]);
    }
    for (const c of children) {
      if (c == null) continue;
      e.appendChild(typeof c === "string" ? document.createTextNode(c) : c);
    }
    return e;
  }

  // Build banner: appears at the top of the page on every world_edit
  // and auto-hides ~1.5s after the last edit. Visible regardless of
  // whether the user has the chat panel open.
  function buildBanner() {
    return el("div", { class: "kiln-build-banner", id: "kiln-build-banner", role: "status", "aria-live": "polite" },
      el("span", { class: "kiln-build-spinner" }),
      el("span", { class: "kiln-build-label", id: "kiln-build-label" }, "agent is building…"),
    );
  }

  let buildBannerHideTimer = null;
  function flashBuildBanner(opName) {
    const b = document.getElementById("kiln-build-banner");
    const l = document.getElementById("kiln-build-label");
    if (!b || !l) return;
    l.textContent = opName ? "applying " + opName + "…" : "agent is building…";
    b.classList.add("kiln-build-banner-on");
    if (buildBannerHideTimer) clearTimeout(buildBannerHideTimer);
    buildBannerHideTimer = setTimeout(() => {
      l.textContent = "applied";
      buildBannerHideTimer = setTimeout(() => {
        b.classList.remove("kiln-build-banner-on");
      }, 600);
    }, 900);
  }

  function buildRoot() {
    const root = el("div", { class: "kiln-widget kiln-corner-" + CORNER });

    // Mount the build banner outside the panel so it's visible even
    // when the panel is closed.
    document.body.appendChild(buildBanner());

    const fab = el("button", { class: "kiln-fab", "aria-label": "Open Kiln", type: "button" }, "✶");
    const panel = el("section", { class: "kiln-panel", role: "dialog", "aria-label": "Kiln agent" });

    const head = el("header", { class: "kiln-panel-head" },
      el("span", { class: "kiln-panel-title" }, "Kiln"),
      el("span", { class: "kiln-panel-page", id: "kiln-page" }, ""),
      el("button", {
        class: "kiln-panel-config",
        "aria-label": "Agent settings",
        type: "button",
        title: "Agent settings",
        id: "kiln-config",
      }, "⚙"),
      el("button", {
        class: "kiln-panel-reset",
        "aria-label": "Reset session — wipes the journal and starts fresh",
        type: "button",
        title: "Reset session",
        id: "kiln-reset",
      }, "↺"),
      el("button", { class: "kiln-panel-close", "aria-label": "Close", type: "button" }, "×"),
    );
    const log = el("ol", { class: "kiln-log", id: "kiln-log" });
    const empty = el("div", { class: "kiln-empty", id: "kiln-empty" },
      el("div", { class: "kiln-empty-mark" }, "✶"),
      el("p", null, "No messages yet."),
      el("p", { class: "kiln-empty-sub" }, "Type below — your message gets journaled and any wired-up agent picks it up. ⏎ to send · ⇧⏎ for a newline."),
    );
    const thinking = el("div", { class: "kiln-thinking", id: "kiln-thinking" },
      el("span", { class: "kiln-thinking-dots" },
        el("span", { class: "kiln-dot" }),
        el("span", { class: "kiln-dot" }),
        el("span", { class: "kiln-dot" }),
      ),
      el("span", { class: "kiln-thinking-label", id: "kiln-thinking-label" }, "agent is thinking…"),
    );
    thinking.style.display = "none";
    const form = el("form", { class: "kiln-form" },
      el("textarea", {
        class: "kiln-input", id: "kiln-input",
        placeholder: "Tell the agent what to build…",
        rows: "1", autocomplete: "off",
      }),
      el("button", { class: "kiln-send", id: "kiln-send", type: "submit" }, "Send"),
    );
    const status = el("div", { class: "kiln-status", id: "kiln-status", role: "status", "aria-live": "polite" });

    panel.append(head, log, empty, thinking, status, form);
    root.append(fab, panel);
    return { root, fab, panel, head, log, empty, thinking, form, status, close: head.querySelector(".kiln-panel-close") };
  }

  // setThinking shows/hides the typing indicator. Optionally updates the
  // label so we can surface tool-progress text alongside the dots.
  function setThinking(on, label) {
    const t = document.getElementById("kiln-thinking");
    if (!t) return;
    t.style.display = on ? "flex" : "none";
    if (label) {
      const l = document.getElementById("kiln-thinking-label");
      if (l) l.textContent = label;
    } else if (on) {
      const l = document.getElementById("kiln-thinking-label");
      if (l) l.textContent = "agent is thinking…";
    }
  }

  // Long-running agent visibility: while the thinking indicator is on,
  // tick a counter so the user sees elapsed time and knows it's not
  // frozen. No hard timeout — the agent keeps the chat_assistant SSE
  // event coming when it eventually finishes.
  let thinkingStartedAt = 0;
  let thinkingTimerID = null;
  let thinkingCustomLabel = null;
  function startThinkingTick() {
    setThinking(true);
    thinkingStartedAt = Date.now();
    thinkingCustomLabel = null;
    if (thinkingTimerID) clearInterval(thinkingTimerID);
    const tick = () => {
      const sec = Math.floor((Date.now() - thinkingStartedAt) / 1000);
      const base = thinkingCustomLabel || "agent is thinking";
      let suffix = "…";
      if (sec >= 5) suffix = ` · ${sec}s`;
      const l = document.getElementById("kiln-thinking-label");
      if (l) l.textContent = base + suffix;
    };
    tick();
    thinkingTimerID = setInterval(tick, 1000);
  }
  function stopThinkingTick() {
    if (thinkingTimerID) {
      clearInterval(thinkingTimerID);
      thinkingTimerID = null;
    }
    setThinking(false);
  }
  function noteThinkingActivity(label) {
    thinkingCustomLabel = label;
  }

  // ---- API helpers ----------------------------------------------------
  function url(path) { return (KILN_URL || "") + path; }

  async function getJSON(path) {
    const r = await fetch(url(path));
    if (!r.ok) throw new Error("HTTP " + r.status);
    return r.json();
  }
  async function postJSON(path, body) {
    const r = await fetch(url(path), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    return r.json();
  }

  function pageContext() {
    return { page: location.pathname, query: location.search, title: document.title };
  }

  // ---- Rendering ------------------------------------------------------
  function render(role, text, opts) {
    opts = opts || {};
    const li = document.createElement("li");
    li.className = "kiln-msg kiln-msg-" + role + (opts.pending ? " kiln-msg-pending" : "");
    if (opts.id) li.dataset.id = opts.id;
    li.textContent = text;
    return li;
  }

  function setEmpty(visible) {
    const e = document.getElementById("kiln-empty");
    if (e) e.style.display = visible ? "" : "none";
  }

  // summarizeArgs gives the user a glanceable summary of a tool call
  // instead of dumping the full JSON. add_entity → "name=posts fields=3";
  // add_page → "path=/x"; etc.
  function summarizeArgs(args) {
    if (!args) return "{}";
    const parts = [];
    if (args.entity) {
      const e = args.entity;
      if (e.name) parts.push("name=" + e.name);
      if (Array.isArray(e.fields)) parts.push("fields=" + e.fields.length);
    }
    if (args.page) {
      const p = args.page;
      if (p.path) parts.push("path=" + p.path);
    }
    if (args.field) {
      const f = args.field;
      if (f.name && f.type) parts.push(`field=${f.name}:${f.type}`);
    }
    if (args.hook) {
      const h = args.hook;
      if (h.id) parts.push("id=" + h.id);
      if (h.entity && h.when) parts.push(h.entity + "/" + h.when);
    }
    if (args.route) {
      const r = args.route;
      if (r.method && r.path) parts.push(r.method + " " + r.path);
    }
    if (args.seed) {
      const s = args.seed;
      if (s.entity) parts.push("entity=" + s.entity);
      if (Array.isArray(s.rows)) parts.push("rows=" + s.rows.length);
    }
    if (args.name && parts.length === 0) parts.push("name=" + args.name);
    if (args.path && parts.length === 0) parts.push("path=" + args.path);
    if (parts.length === 0) {
      const s = JSON.stringify(args);
      return s.length > 80 ? s.slice(0, 80) + "…" : s;
    }
    return parts.join(" ");
  }

  // stripPagePrefix removes the "[page=/foo] " context header the widget
  // prepends to user messages so the bubble shows the clean user text.
  function stripPagePrefix(text) {
    if (typeof text !== "string") return text;
    const m = text.match(/^\[page=[^\]]*\]\s+/);
    return m ? text.slice(m[0].length) : text;
  }

  function hydrate(parts, plans) {
    const log = document.getElementById("kiln-log");
    if (!log) return;
    log.innerHTML = "";
    const planList = Object.values(plans || {});
    if ((!parts || !parts.length) && !planList.length) {
      setEmpty(true);
      return;
    }
    setEmpty(false);
    // Failsafe: if the latest event is an assistant message, the agent
    // is definitely done — stop the thinking ticker even if the
    // chat_assistant SSE event was missed (race after page reload).
    const last = parts && parts.length ? parts[parts.length - 1] : null;
    if (last && last.message && last.kind === "chat_assistant") {
      stopThinkingTick();
    }
    // Build a unified, time-sorted timeline: chat events + plan entries.
    const items = [];
    for (const e of parts || []) {
      items.push({ kind: "chat", t: e.ts || 0, e });
    }
    for (const p of planList) {
      items.push({ kind: "plan", t: p.proposed_at || 0, p });
    }
    items.sort((a, b) => {
      const ta = typeof a.t === "string" ? Date.parse(a.t) : a.t;
      const tb = typeof b.t === "string" ? Date.parse(b.t) : b.t;
      return (ta || 0) - (tb || 0);
    });
    for (const it of items) {
      if (it.kind === "chat") {
        const e = it.e;
        const id = e.entry_id || e.entryID;
        if (e.message) {
          const role = e.kind === "chat_user" ? "user" : "assistant";
          const text = role === "user" ? stripPagePrefix(e.message.text) : e.message.text;
          log.appendChild(render(role, text, { id }));
        } else if (e.call) {
          log.appendChild(render("tool", "→ " + e.call.name + " " + summarizeArgs(e.call.args), { id }));
        } else if (e.result) {
          const r = e.result;
          log.appendChild(render(r.ok ? "tool" : "tool-error",
            r.ok ? "← ok" : "← " + (r.kind || "error") + ": " + (r.error || ""),
            { id }));
        }
      } else if (it.kind === "plan") {
        log.appendChild(renderPlan(it.p));
      }
    }
    log.scrollTop = log.scrollHeight;
  }

  // renderPlan builds the proposed-plan card. While the plan is pending
  // (not yet approved or rejected) it shows Approve and Reject buttons.
  // Once decided, the buttons collapse to a status line.
  function renderPlan(plan) {
    const root = el("li", {
      class: "kiln-msg kiln-msg-plan",
      "data-plan-id": plan.plan_id,
    });
    const head = el("div", { class: "kiln-plan-head" },
      el("span", { class: "kiln-plan-title" }, "Plan: " + plan.plan_id),
    );
    if (plan.reason) {
      head.appendChild(el("span", { class: "kiln-plan-reason" }, plan.reason));
    }
    root.appendChild(head);

    const steps = el("ol", { class: "kiln-plan-steps" });
    for (const s of plan.steps || []) {
      steps.appendChild(el("li", null, s));
    }
    root.appendChild(steps);

    if (Array.isArray(plan.targets) && plan.targets.length) {
      const tg = el("div", { class: "kiln-plan-targets" },
        el("span", { class: "kiln-plan-targets-label" }, "Will run: "),
      );
      const list = plan.targets
        .map((t) => t.op + " " + t.name)
        .join(", ");
      tg.appendChild(document.createTextNode(list));
      root.appendChild(tg);
    }

    if (plan.approved) {
      root.appendChild(el("div", { class: "kiln-plan-status kiln-plan-status-approved" },
        "✓ Approved"));
      return root;
    }
    if (plan.rejected) {
      const txt = "✕ Rejected" + (plan.reject_reason ? ": " + plan.reject_reason : "");
      root.appendChild(el("div", { class: "kiln-plan-status kiln-plan-status-rejected" }, txt));
      return root;
    }

    // Pending — show Approve / Reject buttons.
    const actions = el("div", { class: "kiln-plan-actions" });
    const approve = el("button", {
      type: "button",
      class: "kiln-plan-btn kiln-plan-btn-approve",
      "data-plan-action": "approve",
      "data-plan-id": plan.plan_id,
    }, "Approve");
    const reject = el("button", {
      type: "button",
      class: "kiln-plan-btn kiln-plan-btn-reject",
      "data-plan-action": "reject",
      "data-plan-id": plan.plan_id,
    }, "Reject");
    actions.append(approve, reject);
    root.appendChild(actions);
    return root;
  }

  function setStatus(text, kind) {
    const s = document.getElementById("kiln-status");
    if (!s) return;
    s.textContent = text || "";
    s.className = "kiln-status" + (kind ? " kiln-status-" + kind : "");
  }

  function autosize(ta) {
    ta.style.height = "auto";
    const max = 160;
    ta.style.height = Math.min(ta.scrollHeight, max) + "px";
  }

  // openAgentConfigModal renders the agent-settings dialog. Built lazily
  // on first click. Reads current state from /kiln/agent, lists every
  // built-in adapter with its installed flag, lets the user pick or
  // type a custom command, and POSTs the selection back. Surfaces an
  // explicit warning when a turn is in flight — switching will cancel.
  async function openAgentConfigModal(panel) {
    let state;
    try {
      state = await getJSON("/kiln/agent");
    } catch (err) {
      setStatus("agent config: " + err.message, "error");
      return;
    }

    // Backdrop swallows clicks outside the modal.
    const backdrop = el("div", { class: "kiln-modal-backdrop" });
    const modal = el("div", { class: "kiln-modal", role: "dialog", "aria-modal": "true", "aria-label": "Agent settings" });
    const title = el("h2", { class: "kiln-modal-title" }, "Agent settings");
    const subtitle = el("p", { class: "kiln-modal-sub" },
      "Pick which CLI agent kiln spawns when you send a message. Each adapter brings its own auth — kiln spawns the binary; the binary handles login.");

    const list = el("div", { class: "kiln-adapter-list" });
    const customInputWrap = el("div", { class: "kiln-adapter-custom" });
    const customInput = el("input", {
      type: "text",
      class: "kiln-adapter-custom-input",
      placeholder: `e.g. "pi -p --provider zai --model glm-5.1"`,
      "aria-label": "Custom agent command",
    });
    customInputWrap.appendChild(customInput);

    const currentName = state && state.current && state.current.name ? state.current.name : "none";

    // Special row: "none".
    list.appendChild(buildAdapterRow("none", "(no agent — chat goes to journal but nothing runs)", true, currentName === "none"));

    // Built-in adapters from the registry.
    for (const a of (state.available || [])) {
      list.appendChild(buildAdapterRow(a.name, a.display, a.installed, currentName === a.name));
    }

    // Custom row.
    const customRow = buildAdapterRow("custom", "Custom command (BYO any agent — prompt is appended as final arg)", true, false);
    list.appendChild(customRow);
    customRow.appendChild(customInputWrap);

    // Warning rendered below the list, only visible when a turn is in flight.
    const warning = el("div", { class: "kiln-modal-warning" });
    if (state.in_flight) {
      warning.classList.add("kiln-modal-warning-visible");
      warning.textContent = "⚠ A turn is running. Switching will cancel it; the partial work above is preserved.";
    }

    const btnRow = el("div", { class: "kiln-modal-actions" });
    const cancelBtn = el("button", { type: "button", class: "kiln-modal-cancel" }, "Cancel");
    const applyBtn = el("button", { type: "button", class: "kiln-modal-apply" }, "Apply");
    btnRow.append(cancelBtn, applyBtn);

    modal.append(title, subtitle, list, warning, btnRow);
    backdrop.appendChild(modal);
    panel.appendChild(backdrop);

    function close() { backdrop.remove(); }
    backdrop.addEventListener("click", (e) => { if (e.target === backdrop) close(); });
    cancelBtn.addEventListener("click", close);

    applyBtn.addEventListener("click", async () => {
      const sel = list.querySelector('input[name="kiln-adapter"]:checked');
      if (!sel) return;
      const name = sel.value;
      const body = name === "custom"
        ? { name: "custom", custom: customInput.value.trim() }
        : { name };
      if (name === "custom" && !body.custom) {
        setStatus("custom adapter needs a command", "error");
        return;
      }
      applyBtn.disabled = true;
      try {
        const r = await postJSON("/kiln/agent", body);
        if (!r.ok) {
          setStatus("agent switch: " + (r.error || "unknown"), "error");
          applyBtn.disabled = false;
          return;
        }
        setStatus("agent: " + (r.current && r.current.display || name), "ok");
        close();
      } catch (err) {
        setStatus("network error: " + err.message, "error");
        applyBtn.disabled = false;
      }
    });
  }

  function buildAdapterRow(name, display, installed, isCurrent) {
    const row = el("label", {
      class: "kiln-adapter-row" + (installed ? "" : " kiln-adapter-row-disabled"),
      "data-installed": installed ? "1" : "0",
    });
    const radio = el("input", {
      type: "radio",
      name: "kiln-adapter",
      value: name,
      class: "kiln-adapter-radio",
    });
    if (!installed) radio.disabled = true;
    if (isCurrent) radio.checked = true;
    const label = el("div", { class: "kiln-adapter-label" },
      el("div", { class: "kiln-adapter-name" }, name),
      el("div", { class: "kiln-adapter-display" }, display + (!installed ? "  — not installed" : "")),
    );
    row.append(radio, label);
    return row;
  }

  async function refresh() {
    try {
      const data = await getJSON("/kiln/world");
      hydrate(
        data.session && data.session.chat,
        data.session && data.session.plans,
      );
    } catch (_) {
      setStatus("offline — retrying…", "warn");
    }
  }

  // ---- Wiring ---------------------------------------------------------
  function attach() {
    loadStyles();
    const { root, fab, panel, form, close, log } = buildRoot();
    document.body.appendChild(root);

    const pageEl = document.getElementById("kiln-page");
    if (pageEl) pageEl.textContent = location.pathname;

    let open = !!START_OPEN;
    function setOpen(o) {
      open = o;
      panel.classList.toggle("kiln-open", open);
      fab.classList.toggle("kiln-fab-hidden", open);
      if (open) {
        refresh();
        const i = document.getElementById("kiln-input");
        if (i) i.focus();
      }
    }
    setOpen(open);

    fab.addEventListener("click", () => setOpen(true));
    close.addEventListener("click", () => setOpen(false));

    // Reset button: wipes the journal and reloads to an empty world.
    // Confirms first because it can't be undone.
    const resetBtn = document.getElementById("kiln-reset");
    if (resetBtn) {
      resetBtn.addEventListener("click", async () => {
        if (!window.confirm(
          "Reset the Kiln session? This wipes the journal and clears the world. " +
          "The chat history and all built entities/pages/hooks/routes will be gone. " +
          "There is no undo for this.",
        )) return;
        resetBtn.disabled = true;
        try {
          const r = await postJSON("/kiln/tool/reset_session", {});
          if (!r.ok) {
            setStatus("reset failed: " + ((r && (r.error || r.kind)) || "unknown"), "error");
          } else {
            // Force a full reload — the page itself may not exist anymore.
            location.reload();
          }
        } catch (err) {
          setStatus("network error: " + err.message, "error");
        } finally {
          resetBtn.disabled = false;
        }
      });
    }

    // Config gear: opens the agent-settings modal. The modal lists
    // available adapters (claude-code, pi, codex), shows which are
    // installed, lets the user switch + supply a custom command. The
    // current selection is highlighted; switching warns the user that
    // any in-flight agent turn will be cancelled.
    const configBtn = document.getElementById("kiln-config");
    if (configBtn) {
      configBtn.addEventListener("click", () => openAgentConfigModal(panel));
    }

    const input = document.getElementById("kiln-input");
    input.addEventListener("input", () => autosize(input));
    // Enter sends; Shift+Enter inserts a newline.
    input.addEventListener("keydown", (e) => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        form.requestSubmit();
      }
    });

    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const text = input.value.trim();
      if (!text) return;
      const sendBtn = document.getElementById("kiln-send");
      sendBtn.disabled = true;

      // Optimistic render: show the user's bubble immediately.
      const ctx = pageContext();
      const wrapped = `[page=${ctx.page}${ctx.query ? " " + ctx.query : ""}] ${text}`;
      setEmpty(false);
      const optimistic = render("user", text, { pending: true });
      log.appendChild(optimistic);
      log.scrollTop = log.scrollHeight;

      input.value = "";
      autosize(input);
      setStatus("sending…");

      try {
        const r = await postJSON("/kiln/tool/chat", { role: "user", text: wrapped });
        if (r && r.ok) {
          setStatus("");
          // Stop any prior tick before starting a new one.
          stopThinkingTick();
          startThinkingTick();
          await refresh();
        } else {
          optimistic.classList.add("kiln-msg-failed");
          setStatus("send failed: " + ((r && (r.error || r.kind)) || "unknown"), "error");
          stopThinkingTick();
        }
      } catch (err) {
        optimistic.classList.add("kiln-msg-failed");
        setStatus("network error: " + err.message, "error");
        stopThinkingTick();
      } finally {
        sendBtn.disabled = false;
        input.focus();
      }
    });

    // Plan Approve/Reject buttons inside the panel. We intercept these
    // BEFORE the data-kiln-tool delegation below so the widget chrome
    // exclusion doesn't block them.
    panel.addEventListener("click", async (ev) => {
      const btn = ev.target.closest("[data-plan-action]");
      if (!btn) return;
      ev.preventDefault();
      const action = btn.getAttribute("data-plan-action");
      const planID = btn.getAttribute("data-plan-id");
      if (!planID) return;
      btn.disabled = true;
      const tool = action === "approve" ? "approve_plan" : "reject_plan";
      try {
        const r = await postJSON("/kiln/tool/" + tool, { plan_id: planID });
        if (!r.ok) {
          setStatus(tool + " failed: " + ((r && (r.error || r.kind)) || "unknown"), "error");
          btn.disabled = false;
        }
        // SSE plan_approved / plan_rejected event will trigger refresh
        // and re-render the row in its new state.
      } catch (err) {
        setStatus("network error: " + err.message, "error");
        btn.disabled = false;
      }
    });

    // Click delegation: any element with [data-kiln-tool] dispatches to
    // the matching tool. Args come from data-kiln-args (JSON) or from
    // the enclosing form's fields. Optional confirm prompt.
    document.addEventListener("click", async (ev) => {
      const target = ev.target.closest("[data-kiln-tool]");
      if (!target) return;
      if (target.closest(".kiln-widget")) return; // never intercept widget chrome

      const tool = target.getAttribute("data-kiln-tool");
      const argsAttr = target.getAttribute("data-kiln-args");
      const confirm = target.getAttribute("data-kiln-confirm");
      const onSuccess = target.getAttribute("data-kiln-on-success");

      if (confirm && !window.confirm(confirm)) return;
      ev.preventDefault();

      let args = {};
      if (argsAttr) {
        try { args = JSON.parse(argsAttr); }
        catch (e) { setStatus("bad args JSON on " + tool, "error"); return; }
      } else {
        const form = target.closest("form");
        if (form) args = Object.fromEntries(new FormData(form).entries());
      }
      setStatus("calling " + tool + "…");
      try {
        const r = await postJSON("/kiln/tool/" + tool, args);
        if (r && r.ok) {
          setStatus("ok: " + tool, "ok");
          if (onSuccess === "reload") location.reload();
          else if (onSuccess && onSuccess.startsWith("navigate:")) location.assign(onSuccess.slice("navigate:".length));
          else setTimeout(() => setStatus(""), 1500);
        } else {
          setStatus(tool + " failed: " + ((r && (r.error || r.kind)) || "unknown"), "error");
        }
      } catch (err) {
        setStatus("network error on " + tool + ": " + err.message, "error");
      }
    }, true);

    // Form submit interception: agent-built forms post JSON to their
    // action URL (the framework's CRUD endpoint expects JSON). Skips the
    // widget's own form, multipart forms, and forms with data-kiln-skip.
    document.addEventListener("submit", async (ev) => {
      const form = ev.target.closest("form");
      if (!form || form.closest(".kiln-widget")) return;
      if (form.hasAttribute("data-kiln-skip")) return;
      const enctype = form.getAttribute("enctype") || "";
      if (enctype.indexOf("multipart") >= 0) return; // file uploads go native
      const action = form.getAttribute("action");
      if (!action) return;
      const method = (form.getAttribute("method") || "POST").toUpperCase();
      ev.preventDefault();

      const body = Object.fromEntries(new FormData(form).entries());
      setStatus("submitting…");
      try {
        const r = await fetch(url(action), {
          method,
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        });
        const onSuccess = form.getAttribute("data-kiln-on-success") || "reload";
        if (r.ok) {
          setStatus("ok", "ok");
          if (onSuccess === "reload") location.reload();
          else if (onSuccess.startsWith("navigate:")) location.assign(onSuccess.slice("navigate:".length));
        } else {
          let txt = "";
          try { txt = await r.text(); } catch (_) {}
          setStatus("submit failed (" + r.status + "): " + txt.slice(0, 200), "error");
        }
      } catch (err) {
        setStatus("submit network error: " + err.message, "error");
      }
    }, true);

    // Inline feed: when SSE delivers a world edit / tool call / result,
    // append a system row to the chat log so users see what's happening.
    function appendSystemRow(text, tone) {
      const log = document.getElementById("kiln-log");
      if (!log) return;
      setEmpty(false);
      const li = document.createElement("li");
      li.className = "kiln-msg kiln-msg-" + (tone || "tool");
      li.textContent = text;
      log.appendChild(li);
      log.scrollTop = log.scrollHeight;
    }

    // Detect whether this page is the Kiln host fallback (no real user
    // page registered) so we know whether to hot-reload on world edits.
    function isHostFallback() {
      return !!document.querySelector(".kiln-host");
    }

    // SSE: rerender on every world edit / chat / tool call.
    try {
      const es = new EventSource(url("/.kiln/events"));
      es.addEventListener("ready", refresh);
      es.addEventListener("chat_user", refresh);
      es.addEventListener("plan_proposed", refresh);
      es.addEventListener("plan_approved", refresh);
      es.addEventListener("plan_rejected", refresh);
      // tool_call/tool_result come from the journal so they'll show up
      // when refresh() rehydrates the chat — no need to manually append.
      es.addEventListener("tool_call", refresh);
      es.addEventListener("tool_result", refresh);
      // Surface a more specific thinking label whenever a world_edit
      // op fires while we're waiting for the agent.
      es.addEventListener("world_edit", () => {
        try {
          const t = document.getElementById("kiln-thinking");
          if (t && t.style.display !== "none") {
            noteThinkingActivity("agent is building");
          }
        } catch (_) {}
      });
      // Assistant text means the agent finished its turn.
      es.addEventListener("chat_assistant", () => {
        stopThinkingTick();
        refresh();
      });
      // world_edit isn't part of the chat journal; append a synthetic
      // system row so users see what the agent just changed AND flash
      // the top-of-page banner so the change is visible even when the
      // panel is collapsed.
      es.addEventListener("world_edit", (e) => {
        let opName = "world_edit";
        let summary = "";
        try {
          const parsed = JSON.parse(e.data);
          opName = parsed.op || opName;
          summary = parsed.summary || "";
        } catch (_) {}
        const rowText = summary ? "✦ " + opName + " " + summary : "✦ " + opName;
        appendSystemRow(rowText, "tool");
        flashBuildBanner(opName);

        // Page-affecting ops change what the current URL renders.
        // Always reload on these regardless of whether we're currently
        // on the host fallback — the host might be about to be
        // superseded by a real page, or the real page rewritten.
        const pageAffecting = (
          opName === "add_page" ||
          opName === "delete_page" ||
          opName === "update_page" ||
          opName === "add_route" ||
          opName === "delete_route"
        );
        if (pageAffecting) {
          // Tiny delay so the system row is visible briefly before the
          // reload kicks in.
          setTimeout(() => location.reload(), 350);
        }
      });
      es.addEventListener("open", () => {
        setStatus("");
        window.__kilnSSEReady = true;
      });
      es.onerror = () => setStatus("stream offline — retrying…", "warn");
    } catch (_) {
      setInterval(refresh, 5000);
    }

    refresh();
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", attach);
  else attach();
})();
