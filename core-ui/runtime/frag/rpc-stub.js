// rpc-stub.js: static-export RPC affordance. Replaces `rpc` in the `static`
// composition; MUTUALLY EXCLUSIVE with `rpc` (never compose both).
//
// What it is NOT: no dispatchRPC, no form encoder, no CSRF header read, no
// response-header handling, no in-flight abort controllers. A serverless
// export has no RPC endpoint, so none of that code can run.
//
// What it IS: click/submit interception on data-fui-rpc elements, surfacing
// a "Needs the Go server" notice via _showNavToast (declared in the nav
// fragment). RPC genuinely needs the server, so this is the correct place
// for the notice. _showNavToast is a const inside the shared IIFE; the call
// is at event time, by which point nav has been evaluated, so the
// composition order (rpc-stub before nav) is safe.
//
// data-fui-open is NOT intercepted here. The static composition ships
// widgets-boot-static (in place of widgets-boot), which fetches the dumped
// /__gofastr/widgets.json catalog the static exporter writes
// (framework/static.Builder.dumpWidgetAssets) plus the per-widget chrome
// HTML at /core-ui/widget/<name>/chrome. openWidget resolves against that
// static tree, so a data-fui-open click in a static export opens the
// overlay, it does NOT need the server and must not be told it does.
//
// The same-origin guards live in kernel because nav and split modules need
// them before any demand module arrives. The stub owns only the static notice.

  // Install ONCE at script load. The separate static flag also tells shared
  // boot code not to install the live bridge or prefetch src/rpc.js.
  if (!document.__fuiStaticDispatch) {
    document.__fuiStaticDispatch = true;
    document.addEventListener('click', (e) => {
      // data-fui-rpc on a non-FORM element (button, link, etc.), a dead RPC
      // trigger. preventDefault so any legacy <form action> or href doesn't
      // also fire, then surface the notice.
      const rpc = e.target.closest && e.target.closest('[data-fui-rpc]');
      if (rpc && rpc.tagName !== 'FORM') {
        e.preventDefault();
        _showNavToast('Needs the Go server.');
        return;
      }
      // (data-fui-open is handled by widgets-boot-static's eager
      // delegator, it resolves the click against the dumped catalog.
      // No interception here; see file header.)
    });
    document.addEventListener('submit', (e) => {
      const form = e.target.closest && e.target.closest('form');
      if (!form) return;
      // data-fui-rpc on a <form>, a dead RPC submit. Same notice.
      if (form.hasAttribute('data-fui-rpc')) {
        e.preventDefault();
        _showNavToast('Needs the Go server.');
      }
    });
  }
