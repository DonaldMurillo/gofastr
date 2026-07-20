// GoFastr runtime module — SSE island stream
//
// Connects to the framework's server-side SSE bus declared on the
// page via <meta name="gofastr-sse" content="<url>"> and reflects
// "island" events into matching [data-island] regions. The transport
// is one long-lived EventSource per session; the bus multiplexes
// updates by event type so multiple islands share one connection.
//
// Connection state is mirrored onto window.__gofastr.sseStatus
// ({ connected, lastEventAt, retryCount }) — one object mutated in
// place so holders (NetworkRetryBanner, app code) keep a live
// reference. Transitions (connect / disconnect) announce a
// `gofastr:sse-status` CustomEvent on document; per-frame updates
// only touch lastEventAt silently. The banner reads lastEventAt to
// spot a silent link and listens for the event to re-probe health
// on reconnect.
//
// Loads on demand:
//   - core looks for the <meta name="gofastr-sse"> tag on
//     DOMContentLoaded; if present, idle-loads this module.
//   - reconnects on transport error (3s back-off).
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  // One live status object. Mutated in place — never reassigned — so
  // every reference (banner poll, app listener) sees updates.
  const status = NS.sseStatus = { connected: false, lastEventAt: 0, retryCount: 0 };
  const emit = () => document.dispatchEvent(new CustomEvent('gofastr:sse-status', { detail: status }));

  // remintSession recovers an idle page whose session token died under
  // it (server restart, key rotation, or 30-day expiry). An EventSource
  // onerror can't see the 401 that handleSSE returns, so a purely idle
  // page — one that never navigates — would otherwise reconnect-loop
  // forever on the dead stream id. POST /__gofastr/session mints a fresh
  // token (Set-Cookie) and returns its bare id; we rewrite the stream
  // meta so the next connect() uses an id that matches the new cookie.
  // Same-origin + credentials so the cookie round-trips. Best-effort.
  function remintSession() {
    return fetch('/__gofastr/session', { method: 'POST', credentials: 'same-origin' })
      .then((r) => (r.ok ? r.json() : null))
      .then((j) => {
        if (!j || !j.sessionId) return;
        const m = document.querySelector('meta[name="gofastr-sse"]');
        if (m) m.setAttribute('content', m.getAttribute('content').replace(/([?&]session=)[^&]*/, '$1' + j.sessionId));
      })
      .catch(() => {});
  }

  function connect() {
    const sseUrl = document.querySelector('meta[name="gofastr-sse"]')?.getAttribute('content');
    if (!sseUrl) return;

    const source = new EventSource(sseUrl);

    source.onopen = () => {
      status.connected = true;
      status.retryCount = 0;
      status.lastEventAt = Date.now();
      emit();
    };

    source.addEventListener('island', (event) => {
      // Refresh on every frame; no event dispatch (too chatty).
      status.lastEventAt = Date.now();
      try {
        const { island, html } = JSON.parse(event.data);
        if (island === undefined || html === undefined) return;
        // Escape the server-supplied island name before it enters the
        // CSS attribute selector — a crafted name (e.g. `x"], [data-…`)
        // would otherwise re-target the write to an unintended element,
        // or throw an invalid-selector error that silently drops the
        // legitimate island's update. Matches the CSS.escape pattern in
        // widgets.js / toasts.js.
        const el = document.querySelector('[data-island="' + CSS.escape(String(island)) + '"]');
        if (!el) return;
        el.innerHTML = html;
        el.classList.add('island-updated');
        setTimeout(() => el.classList.remove('island-updated'), 1000);
      } catch { /* ignore malformed SSE frames */ }
    });

    source.onerror = () => {
      status.connected = false;
      status.retryCount++;
      emit();
      source.close();
      // After repeated failures the cause is more likely a dead token
      // than a flapping network — attempt a re-mint (throttled to every
      // other failure past the 2nd, so a genuine outage doesn't hammer
      // /session). The rewrite lands on the meta; the reconnect below
      // picks it up on this or the next cycle. Reconnect timing is NOT
      // blocked on the re-mint (a hung fetch must not stall recovery).
      if (status.retryCount >= 2 && status.retryCount % 2 === 0) remintSession();
      setTimeout(connect, 3000);
    };
  }

  // Connect immediately when the module loads — by the time we're
  // executed core has already determined the SSE meta tag is on the
  // page (otherwise the marker scanner wouldn't have triggered us).
  NS.connectSSE = connect;
  connect();

  (NS.loadedModules ||= {}).sse = true;
})();
