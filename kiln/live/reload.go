package live

import (
	"fmt"
	"net/http"
)

// reloadJS is kiln's build-mode page-refresh client — the world-edit
// analog of framework/dev's livereload script, and like it a dev-mode
// exception to the "no bespoke EventSource" rule for app surfaces:
// an agent editing the world IS a genuine background push, and the
// affected surface is the rendered page itself, not a widget signal
// (the chat panel's signals poll /state instead — see kiln/chat).
//
// On a page-structure world edit (add/delete page or route) or a
// session reset, the current page's rendering may have changed under
// the visitor, so the script forces an SPA refresh through the
// framework runtime's navigate (cache-bypassing) — never a hard
// reload while the runtime is present.
const reloadJS = `// kiln build-mode reload (dev only)
(() => {
  'use strict';
  if (window.__kilnReloadWired) return;
  window.__kilnReloadWired = true;
  const RELOAD_OPS = { add_page: 1, delete_page: 1, add_route: 1, delete_route: 1 };
  const refresh = () => setTimeout(() => {
    const path = location.pathname + location.search + location.hash;
    if (window.__gofastr && window.__gofastr.navigate) {
      window.__gofastr.navigate(path, { force: true });
    } else {
      location.reload();
    }
  }, 200);
  // Body classes mirror the link state for the panel's connection dot
  // (.kiln-panel-conn styles off body.fui-sse-up / body.fui-sse-down —
  // the same contract the old per-widget SSE block maintained).
  const mark = (up) => {
    document.body.classList.toggle('fui-sse-up', up);
    document.body.classList.toggle('fui-sse-down', !up);
  };
  // dirty: a prior connection dropped, so a page-structure edit may have
  // been broadcast while we were gone (the broadcaster has no replay).
  // On the first 'ready' after a drop, refresh unconditionally to
  // converge — the whole page re-renders under the current world.
  let dirty = false;
  const connect = () => {
    const es = new EventSource('/.kiln/events');
    es.onopen = () => mark(true);
    es.addEventListener('ready', () => { if (dirty) { dirty = false; refresh(); } });
    es.addEventListener('world_edit', (ev) => {
      let op = '';
      try { op = (JSON.parse(ev.data) || {}).op || ''; } catch (_) {}
      if (RELOAD_OPS[op]) refresh();
    });
    es.addEventListener('session_reset', refresh);
    es.onerror = () => { mark(false); dirty = true; es.close(); setTimeout(connect, 3000); };
  };
  connect();
})();
`

// ServeReloadJS serves the build-mode reload client. Mount at
// "/.kiln/reload.js" and inject via uihost.WithExtraScripts.
func ServeReloadJS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, reloadJS)
}
