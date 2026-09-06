# Desktop host: proof-of-concept plan

Status: the macOS PoC (phases 0 to 4) is BUILT on branch
`feat/desktop-host` as of 2026-09-04, uncommitted. Two decisions
shaped it after the first draft: no cgo anywhere (pure Go bindings to
every OS WebView), and the host ships as an experimental battery. The
sections below are the design as built, with the deviations recorded
where they happened. Windows and Linux (phases 5 to 7) are not started.

What exists and what proved it:

- `battery/desktop` with `internal/ffi` (register-loading call
  trampoline + callback table on `runtime.cgocall`/`cgocallback`),
  `internal/fakecgo` (the darwin/arm64 port of `runtime/cgo`'s C
  glue), `internal/objc` (dlopen, `Send`, `SendF`, blocks, classes,
  main-thread hop), the darwin `Shell`, the OS-agnostic battery core,
  `core-ui/runtime/src/desktop.js`, `examples/desktop-notes`,
  `gofastr desktop run|build|types`, `framework/docs/content/desktop.md`.
- Every `CGO_ENABLED=0` gate green on this Mac: build and vet, linux
  and windows and darwin/amd64 cross-compiles, unit tests (battery at
  82 percent), `make analyze`, `gofastr verify`, docs and inventory
  and stability tests.
- The GC regression test: a 50,000-callback `qsort` under a GC hammer
  passes three times; the same test on the old `syscall9` path dies
  with `shrinking stack in libcall`.
- The tagged window tests (`go test -tags desktop_e2e ./battery/desktop/...`):
  the spike (two screens, island RPC, `postMessage` into Go, 30 s soak
  with GC every 50 ms, 315 callbacks all on the main thread) and the
  Shell e2e (four real permission alerts clicked, pasteboard read back
  natively, menu navigate, snapshot, quit role, grants persisted in
  `app.db`). PNGs in the session scratchpad `spike-out/`.
- `gofastr desktop build` produced `dist/Notes.app`; opened from the
  bundle it shows the native window, the synthesized menu bar, the
  server-rendered notes screen, and creates the data dir 0700 with the
  identity and secret at 0600. An external quit (AppleScript, Dock)
  drains through `Run`.

Known gaps to close before Windows work starts: AppleScript reports
"User canceled" on quit because the delegate answers `NSTerminateCancel`
while quitting on its own path (answer `NSTerminateLater` and reply
after the drain); `SIGTERM` is not yet routed through `Quit`; `fs`
allow-listing treats a picked folder as one path, not a prefix;
notifications are untested in a signed bundle.

## What the PoC has to prove

One sentence: a GoFastr app, unchanged, runs as a local-first desktop
app inside the operating system's own WebView, from a `CGO_ENABLED=0`
binary, with a typed JS bridge to native capabilities that plugins
extend in plain Go.

| Claim | Gate |
|---|---|
| The web rendering path is reused untouched | `examples/desktop-notes` uses `uihost.New` + `framework/ui` screens with zero desktop-specific markup or CSS, and the same `main.go` still serves over HTTP behind one flag |
| Local-first | Boots with no network and no port the user sees; SQLite under the OS app-data dir; migrations run at start |
| The OS WebView, not a bundled browser | `WKWebView` on macOS, WebView2 on Windows, WebKitGTK on Linux, all in-process; no Chromium shipped, no second runtime |
| Pure Go | `go env CGO_ENABLED` is `0` for every build; `GOOS=windows go build` and `GOOS=darwin go build` of the real package succeed on the ubuntu CI runner, no stubs |
| Typed bridge, not raw messaging | App code calls `__gofastr.desktop.clipboard.writeText({text})`; nobody in the example writes `webkit.messageHandlers`, COM, or a hand `fetch('/__gofastr/desktop/...')` |
| Capabilities are a model, not a list | A plugin registers a capability; it appears in the manifest, the generated JS, the permission prompt, and the `.d.ts` with no host change |
| Escape hatches exist | The example reaches the raw native WebView handle once, on purpose, and the docs show where |
| Pixels, not probes | The e2e screenshots the live window through the bridge's own `window.snapshot` capability and asserts on the PNG |

The PoC is macOS on arm64. Windows and Linux are designed here so the
macOS code lands in the right shape, and they are their own phases
after the PoC gate.

## Decision 1: pure Go bindings, no cgo

`ebitengine/purego` is the library that does this for other projects.
It is off the table under the no-third-party-dependency rule, so we
write the slice of it each OS needs. That is not a hack; every hook it
uses is one the Go team keeps public on purpose. Verified in the Go
1.27 source:

- `//go:cgo_import_dynamic` is legal in ordinary Go files. The
  compiler's own comment says it is "permitted for general use because
  Solaris code relies on it in golang.org/x/sys/unix and others". It
  lets a non-cgo binary import `dlopen`, `dlsym`, and `dlerror` from
  `libSystem` on macOS with three one-line assembly trampolines, the
  same pattern as `x/sys/unix/zsyscall_darwin_arm64.s`.
- `syscall.syscall`, `syscall6`, `syscall6X`, and `syscall9` are pushed
  by `syscall/syscall_darwin.go` ("golang.org/x/sys linknames the
  following syscalls") for outside callers. Each calls any C function
  pointer with up to nine integer arguments on the thread's system
  stack, wrapped in `entersyscall` / `exitsyscall`. `syscall9` is the
  whole "call `objc_msgSend`" primitive for macOS, with no assembly of
  ours. (`syscall.syscalln`, the variadic one underneath, is pushed
  only from runtime into syscall, not outward.)
- `runtime.cgocall`, `runtime.cgocallback`, `runtime.iscgo`, and
  `runtime.set_crosscall2` carry the "hall of shame, do not remove or
  change the type signature, see go.dev/issue/67401" push directive
  naming purego. Those are the callback and fake-cgo hooks.
- `runtime.cgocall` throws `cgocall unavailable` when `iscgo` is false
  on every OS except Windows, Solaris, and illumos. This single line is
  why macOS goes through `syscall.syscall9` instead, and why Linux has
  to port the fake-cgo layer (decision detail under Linux below).
- On Windows the standard library already has everything: `syscall.
  NewLazyDLL`, `syscall.SyscallN`, and `syscall.NewCallback`. No
  linkname, no assembly.

The risk, stated once here and again in the risk table: the darwin and
linux paths lean on symbols the Go team supports for `x/sys` and
tolerates for purego. Nobody promises them to us. purego has needed a
fix roughly once per Go release; we would make that fix ourselves, in
one package, with a test that fails on the day it breaks.

What we get for it: one static binary per OS, cross-compiled from
anywhere, no clang or Xcode on the dev machine, and the ubuntu CI job
compiles the real desktop package for all three targets instead of a
stub.

## Decision 2: it is a battery, marked experimental

`battery/desktop`, not `framework/experimental/desktop`. It depends on
other batteries (auth, when present), owns a structured start and stop,
and other packages reach it the way they reach every battery, through
`framework.GetAs[*desktop.Battery](app.Batteries, "desktop")`. That
is the seam plugins use to register capabilities.

The stability manifest classifies `battery/` as Provisional by prefix,
so `stability/stability.go` gets one added rule,
`{"battery/desktop", Experimental}`; longest prefix wins in `Classify`.
The package doc and `framework/docs/content/desktop.md` open with the
experimental banner, matching the `apiversions` and `webmcp` wording,
and the CHANGELOG entry says pin a version.

The battery shape in a `main.go`:

```go
opts, err := desktop.AppOptions("dev.gofastr.notes")   // WithDB(sqlite in app-data dir) + WithSecret(secret file)
app := framework.NewApp(append(opts, framework.WithConfig(framework.AppConfig{Name: "notes"}))...)
app.Mount(uihost.New(...))                                 // unchanged
d := desktop.New(desktop.Config{ID: "dev.gofastr.notes", Title: "Notes"})
app.RegisterBattery(d)                                     // Init wires routes + middleware
if err := d.Run(app); err != nil { log.Fatal(err) }        // replaces app.Start(addr)
```

`Init(app)` is where routes, middleware, and the runtime module land,
the single integration point every battery has. `Run(app)` is the
thing only a host does: it owns the process's main thread, starts the
App on a goroutine, and blocks in the OS event loop. `WithDB` and
`WithSecret` are constructor options, so `New` cannot open the
database for you; `desktop.AppOptions(id)` does, returning the two
options with SQLite opened in the app-data dir and the secret file
read or minted, and `main.go` spreads them into `NewApp`.

## How the host fits the architecture

Nothing in the SSR, hydration, island, poll, or SSE model changes. The
desktop host is another way to reach the same `http.Handler`.

`Run` does, in order:

1. (Before `NewApp`, in `AppOptions`.) Resolves the app-data dir from
   `os.UserConfigDir()` plus the app ID, creates it 0700 (the
   `worldreadable` analyzer enforces the mode), opens or creates
   `app.db` through `sqlite/stdlib`, reads or mints the 32-byte secret
   file (0600, `crypto/rand`; `timestampid` forbids anything else) and
   returns `WithDB` + `WithSecret` so sessions survive restarts.
2. Confirms `Init` already installed the boot-token gate, the `Host`
   pin, the local identity middleware, the bridge routes, and the
   runtime module (`RegisterExternalScript`, the same rail
   `experimental/webmcp` uses, so no inline script and no new
   `data-fui-*` attribute).
3. Runs `app.Start("127.0.0.1:0")` on a goroutine. `App.Start` already
   splits bind from serve, so `OnReady(addr)` delivers the real port.
   That hook is the only place the port is read. Worktree isolation is
   forced off for this call (open question 2, leaning yes).
4. On the main OS thread, locked in the package `init`, brings up the
   native window and WebView and navigates it to
   `http://127.0.0.1:<port>/__gofastr/desktop/enter?t=<boot token>`.
   The handler sets a signed HttpOnly cookie and 302s to `/`. The token
   is minted in-process, never logged, single-use.
5. Blocks in the OS event loop. Window close or the OS quit command
   runs `app.Shutdown(ctx)` with the normal drain, then exits.

Loopback HTTP, not a custom URL scheme: WebKit custom schemes are not
secure contexts, cookie storage under them is unreliable, and every
existing test, the CSRF middleware, the `Sec-Fetch-Site` checks, and
the SSE bus assume HTTP. `uihost` already drops the `Secure` cookie
flag when `requestIsSecure(r)` is false, so plain loopback works
today. A local process can connect to the port but cannot present the
boot cookie, so it 403s before the router. The `Host` pin closes DNS
rebinding from a browser tab (2026-07-24 audit: Origin checks never
stop rebinding, pin `Host`).

### The UI thread

Every native toolkit here wants one thread: AppKit's main thread, a
Win32 STA thread, GTK's main context. The package locks the main
goroutine to the main OS thread in `init` and exposes one primitive:

```go
// mainThread runs fn on the UI thread and waits, with a deadline.
func (d *Battery) mainThread(fn func()) error
```

Bridge handlers run on HTTP goroutines, hop to the UI thread for the
native call, and return. Native callbacks arrive on the UI thread and
hand off to a channel at once so the UI never waits on Go. A hung
native call returns the `internal` error code after the deadline
instead of freezing the window. The `callbackunderlock` and
`recovercallback` analyzers already describe the shape.

### Native events into the page

`sse.js` carries island frames only, and the desktop host has a direct
in-process channel to its own page, so native events do not ride the
SSE bus after all. The battery owns delivery: `(*Battery).Emit(name,
payload)` evaluates JavaScript in the window that calls
`__gofastr.desktop._dispatch(name, payload)`, with the payload
embedded as a quoted JSON string handed to `JSON.parse` (never an
object literal). This is framework-owned transport, not app code
writing JavaScript, so rule 3 is untouched; app code still only sees
`__gofastr.desktop.on(name, fn)`. Island refreshes after a native
event stay server-driven through `island.Manager.PushUpdate`. Menu
items declared in Go carry a `Navigate` path (the host calls
`__gofastr.navigate(path)`, a client-side nav with no reload, rule 4)
or a Go `Handler`. `Window.Eval(js)` is the same primitive exposed as
the escape hatch.

### The local identity

The PoC ships without `battery/auth`. Owner scoping in `framework/
crud` does not read the user directly: it calls `owner.Get(ctx)`,
which runs the process-wide `owner.Extractor`, and `battery/auth`
installs one in its `init` returning `GetCurrentUser(ctx).GetID()`.
The desktop `LocalUser` middleware therefore does two things:
`handler.SetUser` with a value that has `GetID() string`, and
`owner.SetExtractor` only while `owner.GetExtractor()` is nil. An app
that also imports `battery/auth` keeps auth's extractor; the local
identity is a fallback, never an override. Hard rule 6 holds with no
login screen.

## The native layer, OS by OS

This is the part that decides whether "pure Go" scales. Three OSes,
three different amounts of work, and only two of them share code.

### What every OS has in common

| Concern | Shared answer |
|---|---|
| Window + WebView + event loop | `Shell` and `Window` interfaces (below), one implementation per OS behind build tags |
| UI thread hop | `mainThread`, backed per OS by `performSelectorOnMainThread`, `PostMessage`, or `g_idle_add` |
| Bridge, capabilities, grants, manifest, generated JS, `.d.ts` | One Go implementation, OS-agnostic, tested against a fake `Shell` with no native code at all |
| Screenshot for the pixels gate | `WKWebView takeSnapshot`, `ICoreWebView2::CapturePreview`, `webkit_web_view_get_snapshot`; all three produce PNG bytes |
| Data dir | `os.UserConfigDir()` resolves to Application Support, `%AppData%`, and `$XDG_CONFIG_HOME` |
| Loopback + boot cookie + `Host` pin | Identical |

### macOS (the PoC target, arm64 first)

Pieces, in dependency order:

1. Dynamic loading. `cgo_import_dynamic` for `dlopen`, `dlsym`,
   `dlerror` from `/usr/lib/libSystem.B.dylib` plus their trampolines.
   At startup `dlopen` AppKit, WebKit, and `libobjc.A.dylib` and
   `dlsym` the C entry points: `objc_msgSend`, `objc_getClass`,
   `sel_registerName`, `objc_allocateClassPair`, `class_addMethod`,
   `objc_registerClassPair`, `_NSConcreteGlobalBlock`.
2. Message sending for integer and pointer arguments through
   `syscall.syscall9` (self, selector, and up to seven more). Covers
   most of AppKit and every WKWebView method the PoC needs.
3. Floats and structs by value. `syscall9` loads only integer
   registers, and `initWithContentRect:styleMask:backing:defer:` takes
   an `NSRect` in four float registers. Use `NSMethodSignature` +
   `NSInvocation`: every argument goes by pointer through
   `setArgument:atIndex:`, returns come back through `getReturnValue:`,
   including 32-byte struct returns that need the x8 indirect slot.
   Slower, and it is window-setup code, so nobody notices.
4. Callbacks from Objective-C into Go. A table of numbered assembly
   trampolines whose addresses become IMPs; each saves the incoming
   registers into a frame and enters `runtime.cgocallback` with a Go
   dispatcher keyed on the slot number. This is the one real assembly
   file, about 300 lines of arm64 for the table and the generic entry,
   and it is shared with Linux on the same arch (see below). Every
   callback on the PoC path takes pointers and integers only:
   `userContentController:didReceiveScriptMessage:`,
   `windowShouldClose:`, `applicationShouldTerminate:`, menu actions,
   `applicationDidFinishLaunching:`.
5. Blocks. `takeSnapshotWithConfiguration:completionHandler:` and
   the panel completion handlers take blocks, which are a struct with
   an `isa`, flags, and an `invoke` pointer aimed at a trampoline from
   step 4. About 40 lines. `runModal` lets dialogs skip blocks;
   snapshot has no synchronous form, so blocks are in.
6. Classes. Register one `GofastrBridge` class at runtime and add the
   delegate and handler methods from step 4.

One rule for the whole layer: the package `init` does nothing but
`runtime.LockOSThread()` and capturing the main thread id. Every
`dlopen` and `dlsym` happens lazily on first use from `Run`. The
battery is blank-imported by `cmd/gofastr` for the AGENTS inventory,
so an eager init would make the CLI load AppKit and WebKit at startup
on every Mac.

Why macOS needs the fake-cgo layer after all (spike finding,
2026-09-04). Calling C through `syscall.syscall9` goes via
`runtime.libcCall`, which records the parked goroutine in
`m.libcallsp` for the whole C call. `[NSApp run]` never returns, so
the record is live for the process lifetime, and the first GC that
wants to shrink that goroutine's stack while a native callback runs
Go code on it throws `shrinking stack in libcall` (`stack.go:1305`),
ahead of any GODEBUG escape. The spike only survived with GC off. The
runtime's own cgo path has no such record: `runtime.cgocall` does
`entersyscall` / `asmcgocall` / `exitsyscall`, and `asmcgocall`
recomputes the goroutine SP from the stack bound after the call, so
stack copies during callbacks are designed for. `cgocall` refuses to
run unless `iscgo` is true, and `iscgo` true obliges us to provide
`_cgo_init`, `_cgo_thread_start`, `_cgo_sys_thread_create`,
`_cgo_notify_runtime_init_done`, `_cgo_setenv`, `_cgo_unsetenv`, and
`_cgo_pthread_key_created` (`proc.go:228-250`). That is the fake-cgo
layer the Linux phase needed anyway; it moves forward to phase 0b,
ported from `runtime/cgo`'s C sources for darwin/arm64 (`gcc_unix.c`,
`gcc_libinit_unix.c`, `pthread_unix.c`, `gcc_setenv.c`,
`gcc_arm64.S`). With it, every C call goes through `cgocall` and a
register-loading trampoline of ours that also carries float
arguments, so `NSInvocation` is no longer needed for `NSRect`
signatures (homogeneous float aggregates travel in `d0..d3`).

What the fake-cgo port taught (phase 0b, 2026-09-04, all verified
against the Go 1.27 source and reproduced before fixing):

- `iscgo` must be true before `schedinit`, not merely before the first
  C call: `mcommoninit` allocates an M's `cgoCallers` only when
  `iscgo` is already true (`proc.go:1046`), and `m0` passes through it
  during `schedinit`, so a package-init assignment leaves `m0` without
  the array and the first `cgocall` nil-faults (`cgocall.go:151`).
  The store therefore happens in `x_cgo_init`, which `rt0_go` calls
  before any Go code (`asm_arm64.s:123-138`).
- `set_crosscall2` is checked and called in `runtime.main`
  (`proc.go:249-252`) BEFORE the program's package inits
  (`doInit(runtime_inittasks)` at line 207 covers the runtime's own
  tasks only; package inits start at line 260). It is assigned from
  `x_cgo_init` too, to a no-op function value.
- `_cgo_setenv` / `_cgo_unsetenv` are pushed with a bare linkname
  (`env_posix.go:52-65`) and bind as `runtime._cgo_setenv`, unlike
  the externally named `_cgo_init`, `_cgo_thread_start`, and friends.
- Go 1.27 sets `g0.stacklo` to the raw `addr - size`; the `+4096`
  guard in older sources (which purego kept) is gone.
- The address of a `cgo_import_dynamic` symbol must originate in a
  `.s` file (`GLOBL`/`DATA` trampoline-address pair). A Go-side
  `var x byte` plus linkname materializes a Go data symbol and yields
  a bogus address. This applies to the Linux port unchanged.
- The call trampoline passes integer arguments 9 to 16 as 8-byte C
  stack slots. Darwin arm64 packs sub-8-byte stack arguments tightly,
  so a selector with a `short` or `BOOL` beyond the eighth integer
  argument needs a packing-aware trampoline; none on the PoC path does.

The other spike results all held: `Sec-Fetch-Site` is sent on every
loopback request (`none` for the document, `same-origin` for
subresources and the island RPC), no service worker is registered
without `WithPWA`, worktree isolation leaves port 0 alone, plain
`http://127.0.0.1` needs no App Transport Security exception, and all
17 native callbacks ran on the main thread.

Two constraints that hold with or without cgo: `UNUserNotificationCenter`
refuses to run outside a signed `.app` bundle, so notifications work
from `gofastr desktop build` output and never from `go run`; and amd64
(Intel and Rosetta) has a different ABI, ported in phase 7 (below),
so the `.app` is arm64-only until the shell's build tags are widened.

ABI facts the ports established, each proved by running the system
compiler or libc rather than by reading (2026-09-04, phase 7):

- On x86_64 an `NSRect` argument is MEMORY class (four SSE eightbytes
  exceed the two-eightbyte limit), so it travels entirely on the stack
  and consumes no register; a 16-byte `NSPoint` does go in xmm0/xmm1.
  `ffi.CallStack` exists for this and `objc.SendRect` is arch-split.
- On arm64 an `NSRect` RETURN comes back in d0..d3: AAPCS64 treats an
  aggregate of up to four same-type floats as a homogeneous
  floating-point aggregate whatever its size, and x8 is for everything
  else. `ffi.CallRetHFA4` reads it. An earlier draft of this document
  said the opposite.
- `objc_msgSend_fpret` is never needed (x87 `long double` only; a
  returned double arrives in xmm0). `objc_msgSend_stret` IS needed on
  amd64 for returns larger than 16 bytes and does not exist in arm64
  libobjc.
- Apple's arm64 ABI passes every variadic argument on the stack, so
  `snprintf` with two integers formats them from stack words 0 and 1,
  not from x3/x4; System V amd64 fills rcx/r8/r9 first and needs `al`
  to carry the vector-register count for variadic callees.
- The Go amd64 assembler inserts a frame-pointer save in any
  non-NOFRAME TEXT that contains a CALL, even at `$0`; frame sizes
  that are multiples of 16 keep the callee's SP aligned.
- amd64 callback-table entries are 5 bytes (`CALL rel32`), so the slot
  index comes from the pushed return address, the shape
  `runtime/zcallback_windows.s` uses; arm64 entries are 8 bytes.
- `ffi.Call` takes pointers as `uintptr`, invisible to escape
  analysis: a `make([]byte, 64)` buffer whose address crosses into C
  can be stack-allocated and vacated before C writes it. Buffers that
  cross the boundary must be forced to the heap.

### Windows (cheapest to build, second phase)

Windows is the OS Go was built to do this on. Everything is standard
library:

- Loading: `syscall.NewLazyDLL` for `user32`, `kernel32`, `ole32`,
  `shell32`.
- Calls: `syscall.SyscallN`.
- Callbacks: `syscall.NewCallback` for the window procedure and for
  every COM event handler. The runtime integration is done for us.
- COM: an interface is a pointer to a vtable of function pointers.
  `ICoreWebView2Environment`, `ICoreWebView2Controller`,
  `ICoreWebView2`, and the handler interfaces we implement (each an
  `IUnknown` triple plus `Invoke`) are Go structs of `uintptr`. This is
  typing, not design; `jchv/go-webview2` and Wails prove the shape in
  pure Go, and we write our own for the same reason we write the
  ObjC layer.
- WebView2 runtime discovery. Microsoft's `WebView2Loader.dll` is a
  helper that finds the Evergreen runtime. We do what it does: read
  the runtime path from the registry (`EdgeUpdate\Clients\{F3017226-…}`
  under `HKLM`, then `HKCU`) or `WEBVIEW2_BROWSER_EXECUTABLE_FOLDER`,
  `LoadLibrary` the runtime's `EmbeddedBrowserWebView.dll`, and call
  its environment-creation export. That export is internal to
  Microsoft; the documented fallback is the official loader DLL placed
  next to the exe, which `gofastr desktop build` downloads from NuGet
  at build time and which is never committed (no binaries in the tree).
- Threading: `CoInitializeEx(COINIT_APARTMENTTHREADED)` on the locked
  main thread, a `GetMessage` / `DispatchMessage` loop, `mainThread`
  via `PostMessage` with a private message id. WebView2 calls are
  legal only on that thread, the same rule as AppKit.
- Capabilities: `IFileOpenDialog` / `IFileSaveDialog` (COM),
  `OpenClipboard` + `SetClipboardData` (Win32), Win32 menus with
  `WM_COMMAND`, `CapturePreview` for snapshot. Notifications: WinRT
  toasts need an AppUserModelID and a Start Menu shortcut for an
  unpackaged exe, so the PoC-era Windows phase uses a tray balloon
  (`Shell_NotifyIcon` with `NIF_INFO`) and toasts come with packaging.
- WebView2 wants a writable user-data folder; it goes in the app-data
  dir.

Cost: about 1,200 lines, zero assembly, zero runtime linknames. The
practical constraint is the development machine: this repo is
developed on a Mac, so the Windows phase iterates on a Windows VM
with the WebView2 runtime installed.

### Linux (hardest to build, cheapest to test, third phase)

The OS WebView is WebKitGTK. Target `libwebkit2gtk-4.1` (GTK3) because
it is what Ubuntu 22.04 onward and Tauri ship against; `webkitgtk-6.0`
(GTK4) can be probed second later. The user must have the library
installed; that is the same requirement Tauri imposes and the doc says
it up front.

Why Linux needs the fake-cgo layer and macOS does not. A non-cgo Linux
binary is static, links no libc, and creates threads with raw `clone`.
Calling into GTK from such a thread crashes on the first glibc TLS
access. And the only way to run C on the system stack from Go on Linux
is `runtime.cgocall`, which throws unless `iscgo` is true. Setting
`iscgo` is allowed (it is pushed), but the moment it is true the
runtime demands `_cgo_init`, `_cgo_thread_start`, and
`_cgo_notify_runtime_init_done` (proc.go throws on each if missing)
and creates every M through `_cgo_thread_start`. So Linux means
porting `runtime/cgo`'s C glue to Go and assembly: purego's
`internal/fakecgo` is the reference, roughly 800 lines per arch. Once
it exists every M is a real pthread with glibc TLS, `dlopen` works,
and callbacks from any thread work. It also needs the Go internal
linker to emit a dynamic executable with a `PT_INTERP`; it does that
when `cgo_import_dynamic` symbols are present, and the Linux spike
verifies it on the first day rather than assuming.

The rest of Linux reuses macOS pieces on the same arch: the call
trampoline that loads integer and float registers (GTK takes doubles
in a few places and has no `NSInvocation` to hide behind), and the
callback trampoline table, feed `g_signal_connect_data` for
`script-message-received`, `close-request`, and menu activation.
`g_idle_add` is the `mainThread` hop. Dialogs are `GtkFileChooserNative`,
clipboard is `gtk_clipboard_set_text`, menus are `GtkMenuBar`,
notifications go through `libnotify` (`dlopen` `libnotify.so.4`,
optional at runtime), snapshot is `webkit_web_view_get_snapshot`.

Cost: the fake-cgo port plus about 900 lines of GTK and WebKit glue,
per arch (amd64 is the primary Linux desktop, so amd64 assembly lands
here). Testing is the cheap part: `apt install libwebkit2gtk-4.1-0`
plus `xvfb-run` gives any Linux box or container a display, so the
same e2e that snapshots the window runs in a VM on the Mac. It is
also the OS where adding the e2e to CI would cost nothing later,
since the ubuntu runner already exists.

### Summary table

| | macOS | Windows | Linux |
|---|---|---|---|
| WebView | WKWebView | WebView2 (Evergreen) | WebKitGTK 4.1 |
| Loading | `cgo_import_dynamic` + `dlopen` | `NewLazyDLL` | `dlopen` after fake-cgo init |
| Calling C | `runtime.cgocall` + own register trampoline (floats included) | `SyscallN` | same as macOS |
| Callbacks | own trampolines + `cgocallback`, UI thread only | `syscall.NewCallback` | own trampolines + `cgocallback`, any thread |
| Fake cgo needed | Yes (the libcall GC hazard, see the macOS section) | No | Yes |
| Own assembly | ~450 lines arm64 incl. fake-cgo | none | shared with macOS per arch |
| Runtime linknames | `cgocall`, `cgocallback`, `iscgo`, `set_crosscall2`, the `_cgo_*` definitions | none | same set |
| UI thread hop | `performSelectorOnMainThread` | `PostMessage` | `g_idle_add` |
| Notifications | needs signed `.app` | tray balloon now, toast with packaging | libnotify, optional |
| Snapshot | `takeSnapshot` (block) | `CapturePreview` | `get_snapshot` |
| Package | `.app` bundle | `.exe` (MSIX later) | binary (AppImage/deb later) |
| Where the e2e runs | a Mac with a display | a Windows VM or machine | any Linux with a display or xvfb |
| Build order | 1 | 2 | 3 |

### The interfaces the three implementations share

```go
type Shell interface {
    Run(ctx context.Context, cfg Config, ready func()) error // owns the UI thread and loop
    Main(fn func()) error                                    // hop to the UI thread
    NewWindow(WindowConfig) (Window, error)
    Quit()
}

type Window interface {
    Navigate(url string) error
    Eval(js string) error
    Snapshot(ctx context.Context) ([]byte, error)  // PNG
    SetTitle(string) error
    Native() uintptr                               // the escape hatch
    Close() error
}
```

Dialogs, clipboard, notifications, and menus are not on `Shell`; each
core capability has per-OS files (`clipboard_darwin.go`,
`clipboard_windows.go`, `clipboard_linux.go`) built on the OS package
below it. A test double implements both interfaces with maps and
channels and no native code, and that double is what the coverage
floor runs against.

## Testing policy: e2e exists, CI never runs it

Decided 2026-09-04. Every test that opens a real window is tagged
`//go:build desktop_e2e` and is run by hand on a machine with a
display (`go test -tags desktop_e2e ./battery/desktop/...`). CI, the
pre-commit hook, and `scripts/test-all.sh` in its default lane never
see it. What CI does run for this battery:

- `go build` and `go vet` of the real package for darwin, windows, and
  linux with `CGO_ENABLED=0` (the existing cross-compile smoke, with
  the stubs gone).
- The unit tests against the test double: handshake, registry,
  chokepoint, grants, manifest and JS generation, menu model. These
  carry the coverage floor.
- The harness suites (added 2026-09-05): `desktoptest.Run` drives the
  real `Run` flow with the fake shell as the OS, so the settings
  window, tray, notifications gate, dialogs plus the fs allow-list,
  clipboard, menus, and native events are tested from the page's and
  the user's side (`battery/desktop/ui_harness_test.go`,
  `examples/desktop-notes/harness_test.go`). One headless-Chrome test
  (`browser_harness_e2e_test.go`) runs the page's JavaScript for real
  against the harness: the runtime, the desktop module, bridge.js, and
  Emit through the fake window's eval hook. Its first run found two
  bugs the unit tests had missed (the second-battery extractor
  confusion, the dotted example event name).
- The linkname self-test in `internal/ffi`, which calls each runtime
  hook once so a Go release that removes one fails here first.

The e2e results are still evidence: the PR description links the
snapshot PNGs and names the machine and OS version they came from.
Adding the Linux e2e to the ubuntu runner under xvfb is a later,
separate decision.

## Package layout (all new)

```
battery/desktop/
  doc.go              EXPERIMENTAL banner, package doc, agents.md embed
  desktop.go          New, Config, Battery (Name/Init/OnStart/OnStop), Run, FromApp
  shell.go            Shell + Window interfaces, ErrUnsupported
  shell_darwin.go     //go:build darwin && arm64
  shell_windows.go    //go:build windows
  shell_linux.go      //go:build linux && (amd64 || arm64)
  shell_other.go      everything else -> ErrUnsupported
  bridge.go           /__gofastr/desktop/{enter,manifest.json,call/<cap>/<method>}
  bridge_js.go        generates the typed module from the registry
  capability.go       Capability, Method, Permission, Registry, manifest JSON
  grants.go           desktop_grants table, Grant store, prompt policy
  localuser.go        LocalUser middleware + identity file
  datadir.go          app-data dir, secret file
  menu.go             Menu, MenuItem (Navigate | Handler | Role)
  cap_*.go            window, dialogs, clipboard, notifications, fs, defined against Shell (a caps/ subpackage would import-cycle)
  internal/ffi/       arch assembly shared by darwin+linux: register-loading call, callback table
  internal/objc/      darwin: dlopen, msgSend, NSInvocation, blocks, class registration
  internal/win32/     windows: DLLs, window class, COM helpers, WebView2 discovery
  internal/gtk/       linux: fakecgo, dlopen, GObject signal glue, WebKitGTK
core-ui/runtime/src/desktop.js     demand module, no DOM marker (ws.js precedent), <= 3 KB gzip
cmd/gofastr/desktop.go             `gofastr desktop run|build|types`
examples/desktop-notes/            the dogfood app
framework/docs/content/desktop.md  the user doc, embedded in the binary
stability/stability.go             + {"battery/desktop", Experimental}
```

`battery/desktop` imports `framework` the way every battery does. The
`internal/` packages are Internal by the stability manifest's path
rule, so the ObjC and Win32 layers never become public API, which
matters because they are the part most likely to change.

## The capability model

```go
type Capability struct {
    Name        string        // "clipboard"; [a-z][a-z0-9_]*
    Version     int           // bumped on any breaking change to a method
    Description string
    Methods     []Method
}

type Method struct {
    Name        string        // "writeText"
    Description string
    Input       any           // Go struct; JSON schema via core/schema
    Output      any
    Permission  string        // "clipboard:write" (access.ScopeMatch algebra, as pluginhost.Allow uses)
    Handler     func(ctx context.Context, in json.RawMessage) (any, error)
}
```

A plugin or battery registers with `desktop.FromApp(app).Register(cap)`
from its `Init`; the registry accepts until `Run` freezes it at
`OnReady`, so plugins (which init before batteries) and batteries that
depend on `"desktop"` both work. Duplicate names panic at registration
with the registering module's name, the `initPluginSafe` attribution
path.

The manifest at `/__gofastr/desktop/manifest.json`:

```json
{"schema": 1, "host": {"os": "darwin", "version": "0.83.0"},
 "capabilities": [{"name": "clipboard", "version": 1,
   "methods": [{"name": "writeText", "permission": "clipboard:write",
                "input": {...json schema...}, "output": {...}}]}]}
```

The bridge module is generated from the same registry, served at
`/__gofastr/desktop/bridge.js` through `uihost.ScriptHandler` with the
content hash in the URL, and put on the script rail with
`RegisterExternalScript`. It produces:

```js
__gofastr.desktop.clipboard.writeText({text}) // -> Promise<void>
__gofastr.desktop.dialogs.openFile({filters}) // -> Promise<{path}|null>
__gofastr.desktop.on(event, fn)
__gofastr.desktop.manifest
```

Every call is `POST /__gofastr/desktop/call/<cap>/<method>` as JSON.
The framework installs no CSRF middleware by default (only the BFF
posture in `battery/auth` does), so the chokepoint defends itself:
POST only, `Content-Type: application/json` only, `Sec-Fetch-Site`
refused unless `same-origin` or `none`, and the boot cookie required.
`desktop.js` still forwards `X-CSRF-Token` from the meta tag when an
app adds the BFF posture, the same `_csrf` helper `rpc.js` uses. Responses are
`{ok, result}` or `{ok:false, error:{code, message}}` with a closed set
of codes (`denied`, `unsupported`, `invalid_input`, `cancelled`,
`internal`). Nothing native reaches the page except through this one
handler, so the permission check has one chokepoint, and the transport
is identical on all three OSes.

Why HTTP and not the WebView's own message channel: it reuses CSRF,
the boot cookie, request logging, the audit log, rate limiting, and
the `unboundedbody` posture for free, and it does not care which
WebView is underneath. The native message channel carries one thing
only, injected at document start: `window.__gofastr_desktop = {os,
version}`, so the runtime module can short-circuit in a plain browser.

Methods are valid MCP tool names on purpose. Phase 3 checks whether
registering each method as a gated MCP tool (`mcp.WithToolGate`) is a
one-liner; if it is, an agent connected to the desktop app can open a
file dialog on the user's behalf, the local flavour of the framework's
agentic-web pitch.

### Permissions

The `desktop_grants` table (in the app's own SQLite, migrated through
`framework/migrate` like every other table): `permission`, `decision`
(`allow`/`deny`), `granted_at`. The chokepoint reads it; on a miss it
hops to the UI thread and shows the OS alert with Allow / Allow once /
Deny, then persists anything but "once". Deny persists too, so a page
cannot re-prompt in a loop. Core capabilities declare their own
permissions; `window.*` is ungated because the page already lives in
that window.

`fs.*` reads and writes only paths the `dialogs` capability returned
during this process's lifetime (a per-session allow-list keyed by
cleaned absolute path, the `rootwrite` posture). Whole-disk access is a
plugin's call, not the core default.

### Versioning

Capability `Version` goes into the manifest and the generated `.d.ts`.
A removed method or a changed input shape is a version bump; the
generated JS and the manifest come from one freeze, so the only way
they disagree is a stale cached script, which the content-hash URL
already prevents. Plugin capabilities version independently of the
host. The manifest's `host.os` lets a plugin declare a capability on
one OS and be absent on another; the generated JS then exposes a
method that rejects with `unsupported`, so app code has one code path
with a real error instead of an undefined function.

## The runtime module

`core-ui/runtime/src/desktop.js`, loaded with `loadModule('desktop')`
like `ws.js`. It carries only the transport: `call`, `on`, feature
detection, error mapping. The generated `bridge.js` adds the typed
namespaces on top, so the fixed module stays under the 3 KB gzip
budget `TestRuntimeModuleSizeBudgets` enforces, and the per-app
generated part is outside the budget (the `pluginhost.js` split). No
new `data-fui-*` attribute, so rule 5 and the attribute table in
`core-ui/ARCHITECTURE.md` stay untouched.

## Escape hatches (documented, tested once each)

- `Window.Native()` returns the native WebView handle as a `uintptr`
  (`WKWebView*`, `ICoreWebView2*`, `WebKitWebView*`). The example uses
  it once, with the `internal/objc` package's exported `Send`, to set
  `allowsBackForwardNavigationGestures`, so the doc has a real snippet.
- `Window.Eval(js)` for one-off scripts.
- `Register(Capability)` for custom bridge functions, in plain Go with
  whatever the Go program can reach.
- `app.Router()` is untouched: any route, any Go library.
- `Config.Shell` accepts a caller-provided `Shell`, the door for a
  test double or for someone who wants a sidecar process after all.

## The example: `examples/desktop-notes`

A notes app with one owner-scoped entity (`notes`, FTS5 search via
`SearchFields`), a list screen, an editor screen, and a menu. It
exercises every core capability: File > Export uses `dialogs.saveFile`
+ `fs.write`, a Copy link button uses `clipboard`, a saved note fires
`notifications.show`, the window title follows the open note. It
registers one plugin capability (`systeminfo.cpuCount`, trivial on
purpose) to prove the plugin path.

`main.go` keeps one flag: `--serve :8080` runs the same app over HTTP
with `app.Start`, which is the host-independence claim in executable
form; page-side desktop tests run inside the real WKWebView (phase 10).

## Phases and gates

No phase starts before the previous gate is green. Phases 0 to 4 are
the PoC.

### Phase 0: the macOS spike (throwaway allowed)

Goal: real SSR pixels inside `WKWebView` from a `CGO_ENABLED=0`
binary, and the thread model proven.

- `internal/objc` steps 1 to 6 above, minimum viable: window, WebView,
  `loadRequest`, one script-message callback, one block for
  `takeSnapshot`, quit.
- `framework.NewApp` + `uihost.New` with two screens and one island,
  `Start("127.0.0.1:0")` on a goroutine, `OnReady` hands the port to
  the window.
- Gate: `go test -tags desktop_e2e` on this Mac opens the window,
  snapshots it, and the PNG has the theme background colour at four
  corners plus the heading region non-uniform; then a human reads the
  PNG. Client-side nav between the two screens works, the island RPC
  round-trips, `__gofastr.sseStatus` reports connected, and every
  callback asserts it ran on the main thread id captured at `init`.
- Questions the spike answers: does WebKit send `Sec-Fetch-Site` on
  loopback; does the runtime's service-worker path stay dormant with
  no `WithPWA`; does `isolation.Resolve` leave `127.0.0.1:0` alone in
  a worktree; does any callback ever arrive off the main thread.

### Phase 1: the battery

`desktop.New`, `Config`, `Init`, `Run`, data dir, SQLite bootstrap,
secret file, boot token, `Host` pin, `LocalUser`, the `Shell` and
`Window` interfaces with the darwin implementation and the test
double, `shell_other.go`, clean shutdown through `app.Shutdown`, the
stability manifest rule.

Gate: unit tests against the double for the token handshake (refused
without cookie, refused with a forged cookie, refused on a wrong
`Host`, accepted once, refused on replay), data-dir modes, secret
minting, shutdown ordering. Each guard broken on purpose once (hard
rule 11). `go vet`, `make analyze`, `gofastr verify` clean. `GOOS=
windows` and `GOOS=linux` builds of the package succeed with
`CGO_ENABLED=0` (they compile to `ErrUnsupported` until their phases).

### Phase 2: capabilities and the bridge

Registry, manifest, generated JS, chokepoint, grants table, OS prompt,
the five core capabilities on darwin, `desktop.js`.

Gate: unit tests for the registry and chokepoint against the double
(denied permission never reaches the handler; "once" does not persist;
"deny" persists; unknown method 404s without listing others; input
failing schema validation is `invalid_input`; a method absent on this
OS is `unsupported`). Runtime budget test green with no override. One
e2e on this Mac: click "Copy link", read the pasteboard back through
the capability, assert equality; snapshot after the permission dialog
is accepted.

### Phase 3: plugins and types

`Register` from a plugin's `Init` and from a battery depending on
`"desktop"`, the `systeminfo` example, `gofastr desktop types` writing
`desktop.d.ts` from the manifest, version bumps, the MCP-tool
experiment.

Gate: the plugin's method appears in manifest, JS, and `.d.ts` with no
host change; a duplicate name panics naming the module.

### Phase 4: CLI, bundle, docs (end of PoC)

`gofastr desktop run` (build + launch, `GOFASTR_DEV=1` so livereload
injection and the dev MCP work inside the window), `gofastr desktop
build` producing `dist/<Name>.app` with `Info.plist`, the binary, and
an `.icns` from the `WithAppIcon` PNG via `iconutil`. No signing,
notarization, or updater; the doc's first paragraph says so.

Docs in the same PR (the `gofastr-docs` rule): `framework/docs/
content/desktop.md`, the battery's `agents.md`, a CHANGELOG entry, the
example README, one paragraph in `core-ui/ARCHITECTURE.md` under the
runtime section (no new attribute, events ride the SSE bus), and the
`framework/ARCHITECTURE.md` mention of the new battery and its
Internal sub-packages.

Gate: `./scripts/test-all.sh` green on this Mac, plus the tagged e2e
run by hand; `go test ./...` green on the ubuntu runner with the
darwin and windows cross-compiles of the real package and no e2e; the
`.app` launches from Finder on a Mac that never ran `go`.

### Phase 5: Windows

`internal/win32` and `shell_windows.go` per the Windows section, the
five capabilities' `_windows.go` files, `gofastr desktop build` for
`.exe` plus the loader-DLL download. Iterated on a Windows VM.
Gate: the same e2e, snapshot through `CapturePreview`, run by hand
on that VM; CI compiles the package and runs the double-backed tests.

### Phase 6: Linux

Foundation DONE (2026-09-04): `internal/fakecgo` and `internal/ffi`
for linux/arm64 and linux/amd64, and `internal/gtk` (dynamic loading
plus a 56-entry GTK 3 / WebKitGTK 4.1 symbol table), all passing in a
Debian bookworm container on both arches (GC hammer, 256 foreign
pthreads, stack-argument spill). Facts established there:

- `//go:cgo_import_dynamic name name "libc.so.6"` plus one assembly
  reference makes the Go internal linker emit a dynamic ELF with
  `PT_INTERP`, `PT_DYNAMIC`, and `PT_TLS` (`cmd/link/internal/ld/go.go:105-160`
  sets `havedynamic`); no `-linkmode` flag, no versioned symbol names.
- When `iscgo`, the runtime demands the same seven hooks as darwin;
  `_cgo_mmap`, `_cgo_munmap`, `_cgo_sigaction`, `_cgo_getstackbound`,
  and `_cgo_bindm` are nil-checked and may stay nil.
- The TLS coupling is why the binary must be dynamic: arm64
  `save_g`/`load_g` branch on `iscgo` and keep g at `TPIDR_EL0 +
  runtime.tls_g`; amd64 skips `settls` when `_cgo_init` is non-nil and
  uses glibc's FS block.
- C-source deltas from darwin: `sigset_t` is 128 bytes, `SIG_SETMASK`
  is 2, `EAGAIN` is 11, stack bounds come from `pthread_getattr_np` +
  `pthread_attr_getstack`, and glibc's mutex and cond initializers are
  all zero.
- PLT stubs clobber x16/x17 on arm64 and r10/r11 on amd64, so the
  amd64 callback table carries its slot index in r10 (r12 is
  callee-saved there).
- glibc 2.34 or newer is the floor: every pthread and dlopen symbol is
  imported from `libc.so.6`, where they moved in that release.

Left: `shell_linux.go` (GTK window, WebKitGTK view, `g_idle_add` hop,
signals through `g_signal_connect_data`, the document-start user
script, `evaluate_javascript` with a `GAsyncReadyCallback` slot,
snapshot through cairo and gdk-pixbuf), the capabilities' `_linux.go`
files, flipping `shell_default.go`, and the e2e under `xvfb-run` in
the container (the image needs `xvfb` added). CI compiles the package
and runs the double-backed tests.

### Phase 7: macOS amd64

DONE for the native layer (2026-09-04): `internal/ffi`, `internal/fakecgo`,
and `internal/objc` build and pass on darwin/amd64 under Rosetta,
including the GC, foreign-thread, and stack-argument stress tests, and
`objc_msgSend_stret` for large struct returns. Left: widen the
`shell_darwin*.go` and `shell_default.go` build tags to
`darwin && (arm64 || amd64)`, run the window e2e under Rosetta once, and
teach `gofastr desktop build` a universal bundle via `lipo`.

## Phase 8: the app-quality features (decided 2026-09-05)

Windows and Linux are tabled: the contracts below are written OS-neutral
(the `Shell` interface, `WindowConfig`, `WindowSpec`, `UpdateConfig`,
`DeepLinkConfig`) and every non-darwin shell answers `unsupported`; no
Windows or Linux implementation work happens in this phase. Six things
land, all on macOS arm64, each with a harness test, a browser test
where the page is involved, and a native e2e step.

### 8a. Cross-window architecture

Today `Emit` reaches the main window only and pages cannot talk to
each other. Decided:

- Every window knows itself. `desktop.BootstrapJS(windowID)` now
  carries `"window": "<id>"` and every window gets its own copy: on
  darwin each window has its own `WKUserContentController` (its own
  user script and message handler) while sharing the main window's
  `WKWebsiteDataStore` through the configuration, so the cookie still
  crosses. The desktop module exposes `__gofastr.desktop.windowID`.
- `Battery.Emit(name, payload)` now delivers to EVERY open window (a
  native event is app-wide); `EmitTo(windowID, name, payload)` targets
  one; `not_found` when the id is not open.
- Pages message each other through the server, never directly:
  `windows.post({to, name, payload})` and `windows.broadcast({name,
  payload})` on the bridge, delivered as `_dispatch(name, payload)`
  evals in the target windows; the payload is JSON, at most 64 KiB, and
  the name follows the event grammar. `windows.self()` returns the
  caller's id as the page reported it (the header
  `X-Gofastr-Window` set by the desktop module from the marker; a
  missing header means main). Ungated: only same-origin pages exist.
- The harness records events per window: `h.Events()` stays the main
  window's, `h.Window(id).Events()` is any window's.

### 8b. Window styles: frameless, floating, widgets

`WindowSpec` gains a `Style`:

```go
type WindowStyle struct {
    Chrome        WindowChrome // ChromeDefault | ChromeHiddenTitle | ChromeNone
    Float         bool         // stays above normal windows (NSFloatingWindowLevel)
    Panel         bool         // non-activating utility window: clicking it does not steal focus from the frontmost app (NSPanel, nonactivatingPanel); implies Float
    Transparent   bool         // clear window background; the page paints its own
    Resizable     *bool        // nil = true
    AllSpaces     bool         // visible on every Space / desktop
    X, Y          *int         // initial origin (screen points, top-left based); nil = centered
}
```

`ChromeHiddenTitle` keeps the traffic lights and lets the page paint
under the title bar (`fullSizeContentView`, transparent title bar);
`ChromeNone` is borderless. A borderless window drags through the page:
`__gofastr.desktop.window.startDrag()` on mousedown, or the attribute
`data-fui-window-drag` on any element (the desktop module wires the
listener). The drag request goes through the script message handler,
not the HTTP bridge, because `performWindowDragWithEvent:` needs the
current mouse-down event on the main thread; it is the ONE message the
handler acts on. A floating widget is a `WindowSpec` with `Chrome:
ChromeNone, Panel: true, Transparent: true`; `desktop.Widget(path, w,
h)` builds one. `Config.Widgets []WindowSpec` opens them at launch.
The main window takes a `Style` too (`Config.Style`).

### 8c. Deep links

`Config.DeepLink = &DeepLinkConfig{Scheme: "gofastr-notes"}`. The bundle
builder writes `CFBundleURLTypes` for the scheme (`gofastr desktop build
--scheme gofastr-notes`, default: none). On darwin the bridge registers the
`kAEGetURL` Apple Event handler at launch; the shell hands the raw URL
to `WindowConfig.OnDeepLink`. The battery refuses any other scheme,
maps `gofastr-notes://host/path?q` to the app path `/host/path?q` (validated
like a menu Navigate path), focuses and navigates the MAIN window, and
emits `deep_link` with `{url, path}` to every window. `Config.OnDeepLink
func(u *url.URL) (path string, ok bool)` overrides the mapping. Links
before the window is up are queued (at most 16) and flushed after the
boot navigation. Harness: `h.OpenURL(raw)`; fake shell:
`FireDeepLink(raw)`.

### 8d. Notarization

`gofastr desktop build` gains `--notarize` (with `--sign <Developer ID
identity>` required), `--notary-profile <keychain profile>` (the name
given to `xcrun notarytool store-credentials`), and `--entitlements
<plist>` (default: a generated hardened-runtime plist with no extra
entitlements). Signing under `--notarize` uses `--options runtime
--timestamp`. The bundle is zipped in pure Go (`archive/zip`,
executable bits kept, no symlinks in our bundles), submitted with
`xcrun notarytool submit --wait`, then `xcrun stapler staple`. Every
external command is built through one function and tested with fake
`xcrun`/`codesign` scripts on PATH; nothing here can run for real
without an Apple Developer account, and the doc says so.

### 8e. Auto-update

Pure Go, no Sparkle. `Config.Update = &UpdateConfig{FeedURL, PublicKey
(ed25519, hex), Interval (default 6h), Channel}`. The feed is
`manifest.json` next to a detached `manifest.json.sig` (ed25519 over
the exact bytes), listing per platform (`darwin-arm64`) the version,
archive URL, sha256, size, and notes. The updater: fetch over https
(plain http only to 127.0.0.1, for tests), verify the signature with
the embedded key, compare semver against the running bundle's
`CFBundleShortVersionString` (unbundled runs never update), download
to the data dir, verify sha256 and size, unzip in pure Go, verify the
new bundle's signature with `codesign --verify --deep --strict`, swap
(`Foo.app` → `Foo.app.old`, new → `Foo.app`), relaunch with `open -n`,
quit; the old bundle is removed on the next launch. Surface: `updates`
capability (`check()` → `{available, version, notes}`,
`install()` behind the permission `updates:install` so the OS prompt
asks first), the `update_available` event to every window,
`Battery.CheckForUpdates()` for menu handlers. CLI: `gofastr desktop
keygen -o key` (ed25519 pair, 0600) and `gofastr desktop feed --key
key --version 1.2.0 --archive Notes.zip --url https://...` (writes the
manifest and signature). The example shows a "Check for updates…"
File item.

## Phase 11: window state that survives a relaunch (decided 2026-09-05)

A desktop app remembers where its windows were. Decided:

- `Config.RememberWindows bool`: when set, the battery persists every
  window's frame (position and size, screen points, top-left based,
  the `WindowStyle.X/Y` convention) keyed by window id, and the main
  window's last path, in `windows.json` (0600) in the data dir. Written
  on every move or resize the shell reports, debounced 500 ms, and on
  quit. The file is the battery's, never the OS's autosave: it moves
  with the data dir and is readable by the harness.
- Restore: the main window opens at its remembered frame; a secondary
  opened by id ("settings", "w2", a widget) gets its remembered frame
  when one exists; otherwise the configured default (centered). A
  remembered frame that no longer intersects any screen's visible area
  (a monitor was unplugged) is dropped and the window centers. The boot
  handshake's redirect goes to the main window's last path instead of
  `/` (validated like a menu Navigate path, `/` on any doubt).
- Contracts (OS-neutral): `Frame{X, Y, Width, Height int}`;
  `WindowConfig.Frame *Frame` and `WindowSpec.Frame *Frame` (the
  initial frame, nil means default); `WindowConfig.OnWindowFrame
  func(id string, f Frame)` (the shell reports moves and resizes, any
  thread, unthrottled); `Window.Frame() (Frame, error)` and
  `Window.SetFrame(Frame) error`. The fake shell records SetFrame and
  the harness's `MoveWindow(id, f)` plays the user dragging.
- darwin: the bridge (already the window delegate) gains
  `windowDidMove:` and `windowDidEndLiveResize:` (resize reports at the
  end of the drag, not per frame); `setFrame:display:` applies a
  remembered frame after the screen check
  (`[NSScreen screens]` visibleFrame intersection); the top-left
  flip uses the screen the frame lands on, not only the main screen.

## Phase 12: app state, settings, and single-user mode stay in the battery (decided 2026-09-06)

The evaluation after phase 11 found the battery had grown four
persistence mechanisms of its own (identity file, secret file,
`windows.json`, the grants table) and both examples hand-built a
settings screen on an entity. The decision: these become offerings of
the experimental battery, not of `framework/`. The main library gets
bug fixes only; anything new lives under `battery/desktop`.

- **App state**: `battery/desktop/appstate`, a small durable key/value
  store for app-level state. One file per store (`state.json`, 0600,
  in the data dir) or a table when the app has a DB; typed
  `Get[T]`/`Set`/`Delete`, a change hook, debounced writes, `Flush` on
  quit. The window store (`windowstate.go`) becomes its first client
  (key `windows`), so the store's write and corruption semantics are
  the ones phase 11 already proved. A `state` capability exposes
  `get`/`set`/`delete` to the page for app-owned keys (validated key
  grammar, a size cap per value, ungated like `window.setPath`: the
  page is the app).
  - Built (2026-09-06): `battery/desktop/appstate/` (the store and its
    suite), `battery/desktop/cap_state.go` (the capability, plus
    `keys`), `battery/desktop/windowstate.go` (the window store now
    one client of the app state), `battery/desktop/desktop.go` (the
    `Run` open, the quit flush, `Battery.State()`). The file answer
    shipped; the DB-table variant did not.
- **Settings**: a declared list of preferences (`desktop.Preference{
  Key, Label, Kind, Default}`, kinds bool, int, string, choice) on
  `Config.Preferences`. The battery stores them in the app state under
  `settings` and reads them back typed; the settings window renders
  them through a host-side helper composed from `framework/ui`
  components (the battery cannot reach the render pipeline, so the
  helper is a screen builder the host mounts on `Config.Settings.Path`).
  Both examples drop their hand-built settings entity and screen.
  - Built (2026-09-06): `battery/desktop/preferences.go` (declaration
    validation, the typed reader/writer),
    `battery/desktop/cap_preferences.go` (the capability),
    `battery/desktop/preferences_screen.go`
    (`PreferencesScreen` + the `POST /__gofastr/desktop/preferences`
    route). Both examples migrated
    (`examples/desktop-focus/main.go`, `examples/desktop-notes/main.go`).
- **Single-user mode** stays what `localuser.go` is today: the
  battery's local identity on top of `battery/auth`. It is documented
  as the offering it already is; no change to auth.
  - Built as documentation only (2026-09-06): the "Single-user mode"
    section in `framework/docs/content/desktop.md` (it extends the old
    "The local identity" section). `localuser.go` and `battery/auth`
    are unchanged.
- Order: land the phase 0 to 11 branch first (split into review-sized
  PRs), then app state with the window store migrated onto it, then
  preferences with both examples migrated, then the doc section.
  - Done (2026-09-06): this branch. The doc section is
    `framework/docs/content/desktop.md` ("App state", "Preferences",
    "Single-user mode").

## Deliberately out of scope

Windows and Linux implementations (tabled 2026-09-05: contracts only,
see phase 8), drag-and-drop from the file manager, GTK4, WinRT toasts,
MSIX/AppImage/deb packaging, mobile. Signing, notarization,
auto-update, multiple windows, tray icons, and deep links moved into
scope with phases 4 and 8.

## Risks, with the mitigation chosen

| Risk | What we do |
|---|---|
| The darwin and linux paths lean on runtime linknames kept for x/sys and purego, not for us | Isolate every linkname in `internal/ffi` with a doc comment naming the Go issue; a unit test calls each hook so a Go release that removes one fails in the first `go test`, not in a user's app; the fix is ours to make and lives in one package |
| A callback arrives on a non-main thread on macOS and there is no `needm` without fake cgo | The spike asserts the thread id in every callback; if it ever fires, macOS adopts the Linux fake-cgo layer, which is on the roadmap anyway |
| The Linux fake-cgo port is the most delicate code in the repo | It is its own phase after the PoC, ported from `runtime/cgo`'s C sources with purego's port as the reference shape, with the linkname self-test guarding it in CI |
| Analyzers fire on the native layer (`controlbytes` on strings crossing into C, `rootwrite` on `fs.write`) | Design for them: scrub before the boundary, keep the `fs` allow-list on cleaned absolute paths, `//gofastr:allow(<analyzer>) <why>` only where the shape is deliberate |
| `takeSnapshot` needs an on-screen window; the macOS e2e cannot run headless | Tagged, runs on a Mac with a display, never in CI; the test double carries the coverage floor |
| Blocking the UI thread from a bridge handler freezes the window | One `mainThread` primitive with a deadline; a hung native call returns `internal` |
| The generated JS drifts from the registry | Generated at freeze time from the struct that serves the manifest; content hash in the URL; a test diffs manifest and JS namespaces |
| Scope creep into every hardware API | The core is five capabilities; everything else is a plugin, and the doc says so where a reviewer will read it |
| Windows WebView2 internal entry point changes | The official loader DLL next to the exe is the documented fallback, fetched at build time |
| Distro without WebKitGTK | `Run` returns a clear error naming the package to install; the doc lists the apt/dnf names |

## Open questions to settle before phase 1 (not blockers for phase 0)

1. Answered: owner scoping goes through `owner.SetExtractor` (see the
   local identity section).
2. Whether `Run` forces worktree isolation off. A desktop app never
   runs from a worktree in production and the remap warning is noise
   there. Leaning yes.
3. Whether the boot cookie plus `Host` pin is enough without an
   `Origin` check. Leaning enough; the audit note says `Origin` adds
   nothing against rebinding.
4. Whether `internal/objc.Send` should be exported through a public
   `battery/desktop/objc` package for plugin authors, or stay internal
   until a second consumer exists. Leaning internal for the PoC.

## How the work gets built

Per the subagent policy: the top model plans, reviews, and verifies;
typing goes to omp GLM workers. Slices, one worker each, sequential
because each gate feeds the next:

| Slice | Contents | Rough worker tokens |
|---|---|---|
| 0 | macOS spike: `internal/objc`, `internal/ffi` arm64, one-screen app, snapshot e2e | 500k to 800k |
| 1 | Battery, interfaces, double, handshake tests, stability rule | 400k to 600k |
| 2 | Registry, bridge, grants, five darwin capabilities, desktop.js | 600k to 900k |
| 3 | Plugin example, types command, MCP experiment | 200k to 300k |
| 4 | CLI, bundle, docs, example polish | 300k to 400k |
| 5 | Windows | 700k to 1M |
| 6 | Linux incl. fake cgo | 1M to 1.5M |
| 7 | macOS amd64 | 200k to 300k |

Review cost on the main model, reading diffs and running gates, about
500k for the PoC and about the same again for phases 5 to 7. Slice 0
carries the most judgment (assembly, the ObjC ABI, thread assertions);
if the worker stalls there, that slice is the one to pull in-process
rather than retry. The PoC ships as one PR after slice 4's gate,
under 100 files so CodeRabbit reviews it, with the
`pr-review-findings.sh --gate` step before merge. Phases 5 to 7 are
one PR each.
