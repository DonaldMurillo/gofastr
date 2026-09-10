// desktop-focus page behaviour: the countdown, the timer buttons, the
// widget's controls, and the toasts. Everything mutates through the
// `focus` bridge capability; the page never touches the engine any
// other way, and every mutation answer re-syncs the widgets from the
// returned state instead of guessing locally.
//
// External same-origin script (served from the embedded bytes, wired
// with uihost.WithExtraScripts), never inline. Nothing here logs
// payloads.
(() => {
  'use strict';

  const mmss = (s) => {
    const n = Math.max(0, Math.floor(s));
    return String(Math.floor(n / 60)).padStart(2, '0') + ':' + String(n % 60).padStart(2, '0');
  };

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
  // then, and in a plain browser (--serve), these are absent.
  const desktopNS = () => {
    const ns = window.__gofastr;
    return ns && ns.desktop && ns.desktop.available ? ns.desktop : null;
  };

  const setText = (sel, text) => {
    document.querySelectorAll(sel).forEach((el) => { el.textContent = text; });
  };

  // applyState syncs every countdown, phase, and task label on the
  // page, plus which timer buttons are visible. The same rules the
  // server rendered the page with.
  const applyState = (p) => {
    if (!p || typeof p !== 'object') return;
    const phase = typeof p.phase === 'string' ? p.phase : 'idle';
    const clock = phase === 'idle' ? '--:--' : mmss(p.remaining || 0);
    setText('[data-focus-countdown]', clock);
    setText('[data-focus-phase]', phase);
    setText('[data-focus-task]', typeof p.taskTitle === 'string' ? p.taskTitle : '');
    const visible = {
      start: phase === 'idle',
      pause: phase === 'work' || phase === 'break',
      resume: phase === 'paused',
      skip: phase !== 'idle'
    };
    document.querySelectorAll('[data-focus-action]').forEach((btn) => {
      const want = visible[btn.getAttribute('data-focus-action')];
      btn.hidden = want === false;
    });
  };

  // call runs one focus capability method and re-syncs from the answer.
  const call = (method, input) => {
    const d = desktopNS();
    if (!d || !d.focus) {
      // Browser mode (--serve): no bridge, no timer. The buttons say
      // so instead of silently doing nothing.
      show('The timer runs in the desktop app');
      return;
    }
    d.focus[method](input || {})
      .then((r) => { if (r && r.state) applyState(r.state); })
      .catch((e) => { show(e && e.message ? e.message : 'Timer error'); });
  };

  // Delegated clicks: the buttons live inside tables and cards the
  // runtime re-renders.
  document.addEventListener('click', (e) => {
    const action = e.target.closest('[data-focus-action]');
    if (action) {
      e.preventDefault();
      call(action.getAttribute('data-focus-action'));
      return;
    }
    const start = e.target.closest('[data-focus-start]');
    if (start) {
      e.preventDefault();
      call('start', { taskId: start.getAttribute('data-focus-start') });
      return;
    }
    // The widget's Open task: tell the MAIN window to navigate; this
    // window stays on the timer.
    const open = e.target.closest('[data-focus-open]');
    if (open) {
      e.preventDefault();
      const d = desktopNS();
      if (d && d.windows) {
        d.windows.post({
          to: 'main',
          name: 'show_task',
          payload: { id: open.getAttribute('data-focus-open') }
        }).catch(() => {});
      } else {
        show('The timer runs in the desktop app');
      }
      return;
    }
    // The widget's Close button. Only the page knows which window it
    // lives in: the host marker carries the id, and windows.close takes
    // it. In a plain browser there is no window to close.
    const close = e.target.closest('[data-focus-widget-close]');
    if (close) {
      const d = desktopNS();
      const marker = window.__gofastr_desktop;
      const id = marker && typeof marker.window === 'string' ? marker.window : '';
      if (d && d.windows && id) d.windows.close({ id }).catch(() => {});
    }
  });

  // The sidebar zone: the page owns the sidebar's width, so it
  // reports the measured nav to the shell through window.setChrome
  // (the shell contract's intended producer is a ResizeObserver on
  // the sidebar element). Windows without a sidebar (the widget)
  // report nothing.
  let lastSidebarWidth = -1;
  const reportSidebar = () => {
    const d = desktopNS();
    const nav = document.querySelector('.layout-body > nav');
    if (!d || !d.window || typeof d.window.setChrome !== 'function' || !nav) return;
    const w = Math.round(nav.getBoundingClientRect().width);
    if (w <= 0 || w === lastSidebarWidth) return;
    lastSidebarWidth = w;
    d.window.setChrome({ sidebarWidth: w }).catch(() => {});
  };
  const observeSidebar = () => {
    const nav = document.querySelector('.layout-body > nav');
    if (!nav || typeof ResizeObserver === 'undefined') return;
    // The initial observe fires the callback once, which sends the
    // first report; every later resize (the window, the layout)
    // re-sends only when the width actually changed.
    new ResizeObserver(reportSidebar).observe(nav);
  };

  // The source list's marker: the server stamps aria-current on the
  // current row, and the runtime's activelink module keeps it fresh on
  // every navigation once it has idle-loaded. Between first paint and
  // that load a navigation leaves the server's row stale (two rows
  // then read as current), so the page reconciles the marker itself on
  // every navigation, the exact-match rule both use. Once the module
  // loads, the two agree: it stamps the same aria-current on the same
  // row.
  const reconcileSourceList = () => {
    const path = location.pathname + location.search;
    document.querySelectorAll('.desktopui-sourcelist__item').forEach((a) => {
      if ((a.getAttribute('href') || '') === path) {
        a.setAttribute('aria-current', 'page');
      } else {
        a.removeAttribute('aria-current');
      }
    });
  };
  window.addEventListener('gofastr:navigate', reconcileSourceList);

  const wire = () => {
    const ns = window.__gofastr;
    if (!ns || !ns.desktop || typeof ns.desktop.on !== 'function') return;

    // The sidebar report needs the desktop namespace; the observer
    // outlives SPA navigations because the layout (and its nav)
    // stays mounted across content swaps.
    observeSidebar();
    // The engine's heartbeat and lifecycle events.
    ns.desktop.on('focus_tick', (p) => applyState(p));
    ns.desktop.on('focus_done', (p) => {
      const m = p && typeof p.message === 'string' ? p.message : '';
      show(m || 'Session done');
    });

    // The widget asked the main window to open a task. Only the main
    // window navigates: the widget keeps showing the timer.
    ns.desktop.on('show_task', (p) => {
      const marker = window.__gofastr_desktop;
      const own = marker && typeof marker.window === 'string' ? marker.window : 'main';
      const id = p && typeof p.id === 'string' ? p.id : '';
      if (own !== 'main' || !id) return;
      if (window.__gofastr && typeof window.__gofastr.navigate === 'function') {
        window.__gofastr.navigate('/tasks/' + encodeURIComponent(id));
      }
    });

    // The OS opened a gofastr-focus:// link; the battery already navigated.
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

    // Sync once on load: the server rendered a snapshot, the engine
    // knows better.
    if (ns.desktop.focus && typeof ns.desktop.focus.state === 'function') {
      ns.desktop.focus.state()
        .then((r) => { if (r && r.state) applyState(r.state); })
        .catch(() => {});
    }
  };

  const ns = window.__gofastr;
  if (ns && typeof ns.loadModule === 'function') {
    ns.loadModule('desktop').then(wire).catch(() => {});
  }
})();
