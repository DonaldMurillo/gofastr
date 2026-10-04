package main

// labDemoCSS labels runtime regions without changing their textContent
// and styles the diagnostic timeline.
const labDemoCSS = `
/* ---- tokens ---------------------------------------------------------- */
:root {
  --lab-line: #cbd5e1;
  --lab-box-bg: rgba(100, 116, 139, 0.07);
  --lab-panel-bg: #f8fafc;
  --lab-fg: #0f172a;
  --lab-fg-dim: #64748b;
  --lab-c-outlet: #2563eb;
  --lab-c-area: #7c3aed;
  --lab-c-slot: #059669;
  --lab-c-layer: #d97706;
  --lab-c-flash: #dc2626;
  --lab-mono: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
@media (prefers-color-scheme: dark) {
  :root {
    --lab-line: #3b4a5f;
    --lab-box-bg: rgba(148, 163, 184, 0.10);
    --lab-panel-bg: #101828;
    --lab-fg: #e2e8f0;
    --lab-fg-dim: #94a3b8;
    --lab-c-outlet: #60a5fa;
    --lab-c-area: #a78bfa;
    --lab-c-slot: #34d399;
    --lab-c-layer: #fbbf24;
  }
}

#shell-header .lab-route,
#lab-crumbs-bind, #lab-mix-bind {
  font-family: var(--lab-mono); font-size: 12px;
  background: var(--lab-box-bg); border: 1px solid var(--lab-line);
  border-radius: 6px; padding: 1px 7px; margin: 1px 6px 1px 0;
  display: inline-block;
}
/* Prefixes are pseudo content: the bindings' textContent is exact
   test surface (labRoute), so the DOM gains no text. */
#lab-route-title::before { content: "title: "; opacity: 0.6; }
#lab-route-param::before { content: "id: "; opacity: 0.6; }
#lab-crumbs-bind::before { content: "crumbs: "; opacity: 0.6; }
#lab-mix-bind::before { content: "mix: "; opacity: 0.6; }


/* ---- labelled regions ------------------------------------------------ */
.shell [data-cui-outlet],
.shell [data-cui-area],
.shell main[data-cui-layout-slot],
.shell .layout-content[data-cui-layout-slot],
.shell [data-cui-layout-key]:not([data-cui-lang]),
.layout-bare main[data-cui-layout-slot] {
  position: relative;
  border: 1px solid var(--lab-line);
  border-radius: 8px;
  background: var(--lab-box-bg);
  padding: 20px 12px 28px;
  min-height: 22px;
}
.shell [data-cui-outlet]::before,
.shell [data-cui-area]::before,
.shell main[data-cui-layout-slot]::before,
.shell .layout-content[data-cui-layout-slot]::before,
.shell [data-cui-layout-key]:not([data-cui-lang])::before,
.layout-bare main[data-cui-layout-slot]::before {
  position: absolute; top: -9px; left: 10px;
  font: 600 10.5px/1.6 var(--lab-mono);
  letter-spacing: 0.02em; white-space: nowrap;
  color: #fff; background: var(--lab-c, var(--lab-c-outlet));
  padding: 0 8px; border-radius: 999px;
}
/* Generic names first (attr() carries the raw address), then the
   readable per-region names override them. */
.shell [data-cui-outlet] { --lab-c: var(--lab-c-outlet); }
.shell [data-cui-outlet]::before { content: "outlet · " attr(data-cui-outlet) var(--lab-wait, ""); }
.shell [data-cui-area] { --lab-c: var(--lab-c-area); }
.shell [data-cui-area]::before { content: "area · " attr(data-cui-area) var(--lab-wait, ""); }
.shell main[data-cui-layout-slot] { --lab-c: var(--lab-c-slot); }
.shell main[data-cui-layout-slot]::before { content: "main slot" var(--lab-wait, ""); }
.shell .layout-content[data-cui-layout-slot] { --lab-c: var(--lab-c-slot); }
.shell .layout-content[data-cui-layout-slot]::before { content: "slot" var(--lab-wait, ""); }
.shell [data-cui-layout-key]:not([data-cui-lang]) { --lab-c: var(--lab-c-layer); }
.shell [data-cui-layout-key]:not([data-cui-lang])::before { content: "kept layer" var(--lab-wait, ""); }
.layout-bare main[data-cui-layout-slot] { --lab-c: var(--lab-c-slot); }
.layout-bare main[data-cui-layout-slot]::before { content: "main slot" var(--lab-wait, ""); }

.shell [data-cui-outlet="l:shell#toolbar"]::before { content: "outlet · toolbar" var(--lab-wait, ""); }
.shell [data-cui-outlet="l:shell#aside"]::before { content: "outlet · aside" var(--lab-wait, ""); }
.shell [data-cui-outlet="l:shell#rail"]::before { content: "outlet · rail" var(--lab-wait, ""); }
.shell [data-cui-area="l:shell~crumbs"]::before { content: "route area · crumbs" var(--lab-wait, ""); }
.shell main[data-cui-layout-slot="l:shell"]::before { content: "main slot · shell" var(--lab-wait, ""); }
.layout-bare main[data-cui-layout-slot="l:bare"]::before { content: "main slot · bare" var(--lab-wait, ""); }
.shell [data-cui-layout-key="g:/items/:items"]:not([data-cui-lang])::before { content: "kept layer · items list" var(--lab-wait, ""); }
.shell .layout-content[data-cui-layout-slot="g:/items/:items"]::before { content: "slot · item detail" var(--lab-wait, ""); }

/* Region state: the runtime's marks become tag suffixes. data-cui-loadstate
   wins over aria-busy when a region shows loading content (it is the
   sharper fact). */
[aria-busy="true"] { --lab-wait: " · waiting"; }
[data-cui-loadstate] { --lab-wait: " · loading content"; }

/* Transition and change tags sit below content, not across the region
   name: both labels remain readable in a narrow nested detail pane. */
[data-cui-vt]::after,
[data-lab-flash]::after {
  position: absolute; bottom: 4px; right: 10px;
  font: 500 10.5px/1.6 var(--lab-mono);
  white-space: nowrap; padding: 0 8px; border-radius: 999px;
  border: 1px solid var(--lab-line); color: var(--lab-fg-dim);
  background: var(--lab-panel-bg);
}
[data-cui-vt]::after { content: "transition: " attr(data-cui-vt); }
[data-lab-flash] { animation: lab-flash 600ms ease-out; }
[data-lab-flash]::after {
  content: attr(data-lab-flash);
  color: #fff; background: var(--lab-c-flash); border-color: var(--lab-c-flash);
}
@keyframes lab-flash {
  0% { box-shadow: 0 0 0 4px rgba(220, 38, 38, 0.5); }
  100% { box-shadow: 0 0 0 4px rgba(220, 38, 38, 0); }
}

/* ---- region content ---------------------------------------------------- */
[data-lab-marker] {
  font: 600 12px/1.5 var(--lab-mono);
  display: inline-block; padding: 2px 8px; margin: 0 6px 6px 0;
  border-radius: 6px; background: rgba(100, 116, 139, 0.16);
}
#lab-crumbs { font: 12px/1.5 var(--lab-mono); color: var(--lab-fg-dim); }

/*: fallback badges — an outlet degraded to its Default or showing a
   fill's own error fallback carries a dashed badge naming which one it
   is. Text is pseudo content from the attribute (the region's exact
   textContent stays a test surface), the region tags' trick. */
[data-lab-badge] { min-height: 20px; }
[data-lab-badge]::before {
  content: attr(data-lab-badge);
  display: inline-block; max-width: 100%; box-sizing: border-box;
  margin: 0 6px 6px 0; padding: 2px 8px;
  border: 1px dashed var(--lab-c-flash); border-radius: 8px;
  font: 600 11px/1.5 var(--lab-mono); color: var(--lab-c-flash);
}


/* ---- timeline panel ---------------------------------------------------- */
/* Fixed bottom-right on wide screens; part of the flow (below the
   content) under 640px so it never covers the nav. Never a
   div[hidden]: a test counts those as parked nodes. */
#lab-timeline {
  position: fixed; right: 14px; bottom: 14px; z-index: 60;
  width: 380px; max-width: calc(100vw - 28px);
  border: 1px solid var(--lab-line); border-radius: 10px;
  background: var(--lab-panel-bg); color: var(--lab-fg);
  font: 12px/1.5 var(--lab-mono);
  box-shadow: 0 8px 28px rgba(15, 23, 42, 0.18);
}
#lab-timeline .lab-tl-bar {
  display: flex; align-items: center; gap: 8px;
  padding: 6px 10px; border-bottom: 1px solid var(--lab-line);
}
#lab-timeline .lab-tl-bar strong { font-size: 12px; }
#lab-timeline .lab-tl-bar button {
  margin-left: auto; font: inherit; font-size: 11px; cursor: pointer;
}
#lab-timeline .lab-tl-log { max-height: 32vh; overflow: auto; padding: 6px 10px 8px; }
#lab-timeline.lab-tl-min .lab-tl-log { display: none; }
#lab-timeline .lab-tl-hint { color: var(--lab-fg-dim); }
.lab-tl-nav { margin: 6px 0 2px; }
.lab-tl-nav .lab-tl-path { font-weight: 700; }
.lab-tl-line { display: flex; gap: 8px; align-items: baseline; }
.lab-tl-ms { flex: 0 0 64px; text-align: right; color: var(--lab-fg-dim); }
.lab-tl-kind-change { color: var(--lab-c-outlet); }
.lab-tl-kind-state { color: var(--lab-c-layer); }
.lab-tl-kind-error { color: var(--lab-c-flash); font-weight: 700; }
.lab-tl-kind-motion { color: var(--lab-c-area); }
.lab-tl-kind-nav { color: var(--lab-c-slot); }
.lab-tl-kind-scroll { color: var(--lab-fg-dim); }
@media (max-width: 640px) {
  #lab-timeline {
    position: static; width: auto; max-width: none;
    margin: 14px; box-shadow: none;
  }
  #lab-timeline .lab-tl-log { max-height: 40vh; }
}
`
