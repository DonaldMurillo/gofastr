// desktop-notes page behaviour: the copy-link button, the export
// toast, and mirroring document.title into the native window title.
//
// External same-origin script (served from static/, wired with
// uihost.WithExtraScripts), never inline. Nothing here logs payloads.
(() => {
  'use strict';

  // Surface a short result. The runtime's toast when the toasts module
  // is loaded, console.info otherwise.
  const show = (msg) => {
    const ns = window.__gofastr;
    if (ns && typeof ns.toast === 'function') {
      ns.toast({ title: msg, variant: 'success' });
    } else {
      console.info(msg);
    }
  };

  // The desktop namespace appears once the runtime's desktop module
  // has loaded (the generated bridge.js triggers the same load). Until
  // then, and in a plain browser, these are absent.
  const desktopNS = () => {
    const ns = window.__gofastr;
    return ns && ns.desktop && ns.desktop.available ? ns.desktop : null;
  };

  const copyLink = (btn) => {
    const d = desktopNS();
    if (d && d.clipboard) {
      d.clipboard.writeText({ text: location.href })
        .then(() => show('Link copied'))
        .catch(() => show('Copy failed'));
      return;
    }
    // Browser mode (--serve): the plain async clipboard.
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(location.href)
        .then(() => show('Link copied'))
        .catch(() => show('Copy failed'));
      return;
    }
    show('Copy not available');
  };

   // Delegated: the button lives inside a re-rendered form island.
   document.addEventListener('click', (e) => {
     const btn = e.target.closest('[data-notes-copy]');
     if (btn) copyLink(btn);
  });

  // The quick-note widget's Close button. Only the page knows which
  // window it lives in: the host marker carries the id, and
  // windows.close takes it. In a plain browser there is no window to
  // close; the button is inert.
  document.addEventListener('click', (e) => {
    const btn = e.target.closest('[data-notes-widget-close]');
    if (!btn) return;
    const d = desktopNS();
    const marker = window.__gofastr_desktop;
    const id = marker && typeof marker.window === 'string' ? marker.window : '';
    if (d && d.windows && id) d.windows.close({ id }).catch(() => {});
  });
  const wire = () => {
    const ns = window.__gofastr;
    if (!ns || !ns.desktop) return;

    // File > Export finished: name the path the user picked.
    if (typeof ns.desktop.on === 'function') {
      ns.desktop.on('notes_exported', (payload) => {
        const p = payload && typeof payload.path === 'string' ? payload.path : '';
        show(p ? 'Exported to ' + p : 'Export finished');
      });
      // The OS opened a gofastr-notes:// link; the battery already navigated.
      ns.desktop.on('deep_link', (payload) => {
        const p = payload && typeof payload.path === 'string' ? payload.path : '';
        show(p ? 'Opened ' + p : 'Opened a link');
      });
      // File > Check for updates, and the scheduled check.
      ns.desktop.on('update_available', (payload) => {
        const v = payload && typeof payload.version === 'string' ? payload.version : '';
        show(v ? 'Update ' + v + ' is available' : 'An update is available');
      });
      ns.desktop.on('update_none', (payload) => {
        const v = payload && typeof payload.version === 'string' ? payload.version : '';
        show(v ? 'Up to date (' + v + ')' : 'Updates are not configured for this build');
      });
    }

    // The window title follows the open note: the framework already
    // keeps document.title current per screen; mirror it natively.
    const syncTitle = () => {
      const d = desktopNS();
      if (d && d.window && document.title) {
        d.window.setTitle({ title: document.title }).catch(() => {});
      }
    };
    const el = document.querySelector('title');
    if (el && typeof MutationObserver === 'function') {
      new MutationObserver(syncTitle).observe(el, {
        childList: true,
        characterData: true,
        subtree: true
      });
      syncTitle();
    }
  };

  const ns = window.__gofastr;
  if (ns && typeof ns.loadModule === 'function') {
    ns.loadModule('desktop').then(wire).catch(() => {});
  }
})();
