// GoFastr runtime module — SSE island stream
//
// Connects to the framework's server-side SSE bus declared on the
// page via <meta name="gofastr-sse" content="<url>"> and reflects
// "island" events into matching [data-island] regions. The transport
// is one long-lived EventSource per session; the bus multiplexes
// updates by event type so multiple islands share one connection.
//
// Loads on demand:
//   - core looks for the <meta name="gofastr-sse"> tag on
//     DOMContentLoaded; if present, idle-loads this module.
//   - reconnects on transport error (3s back-off).
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  function connect() {
    const sseUrl = document.querySelector('meta[name="gofastr-sse"]')?.getAttribute('content');
    if (!sseUrl) return;

    const source = new EventSource(sseUrl);

    source.addEventListener('island', (event) => {
      try {
        const { island, html } = JSON.parse(event.data);
        if (island === undefined || html === undefined) return;
        const el = document.querySelector(`[data-island="${island}"]`);
        if (!el) return;
        el.innerHTML = html;
        el.classList.add('island-updated');
        setTimeout(() => el.classList.remove('island-updated'), 1000);
      } catch { /* ignore malformed SSE frames */ }
    });

    source.onerror = () => {
      source.close();
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
