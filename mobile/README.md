# gofastr-mobile (prototype)

A GoFastr app running inside a phone app's process, serving its WebView
**in-process**: no listener socket, no port, no loopback origin. This is an
experiment to decide how mobile support should be built. It is not a supported
feature, and nothing here is wired into CI.

The module path is `github.com/DonaldMurillo/gofastr-mobile`, not a
subdirectory of the framework's path. Go's `internal/` rule is by import path,
so this module sees exactly what a separate repository would: the framework's
public API and nothing more.

## Layout

| Path | What it is |
|---|---|
| `mobilehost/` | The host. `Options(dataDir)` opens the SQLite DB and secret. `New(app, cfg)` registers the `mobile` battery (single-user identity) and `Start` runs the app with `framework.App.Serve` on an in-memory `net.Pipe` listener. `Do` / `Open` serve one WebView request buffered or streamed. |
| `examples/notes/notesapp/` | desktop-notes' entity and entityui screens with the desktop half removed. Zero CSS, adaptive theme. |
| `examples/notes/notesmobile/` | The surface `gomobile bind` would export: `Start`, `Stop`, `Handle`, `Open`, with strings, bytes and ints only. A scaffold would generate this file. |
| `cmd/gofastr-mobile/` | The CLI that builds the phone app and runs it on the iOS simulator or an Android emulator. No Xcode project, no Gradle (see below). |
| `cmd/gofastr-mobile/templates/` | The generated native hosts: `App.swift` + `Info.plist` (WKURLSchemeHandler, custom scheme `gofastr://app`) and `MainActivity.java` + `AndroidManifest.xml` (`shouldInterceptRequest`, origin `https://appassets.androidplatform.net`). |
| `sim/` | A Chromium set up as a phone WebView (390x844 @3x, touch, iPhone UA) with every request intercepted through the DevTools Fetch domain and answered by `mobilehost` in-process. |

## Run it

```bash
cd mobile && go test ./...
```

```bash
cd mobile && SIM_SHOTS=/tmp/shots go test ./sim/ -run TestPhoneWebView -v
```

`SIM_ANDROID_LIMITS=1` drops request bodies the way Android's
`WebResourceRequest` does (see findings).

## Run it on a phone simulator or emulator

`gofastr-mobile` does for phones what `gofastr desktop build` does for the
Mac: it generates a small native host, compiles it with the platform's
command-line tools, assembles the `.app` / `.apk` itself, then installs and
launches it. Xcode and the Android SDK must be **installed**; neither is ever
opened, and there is no Xcode project or Gradle build to maintain.

Check the toolchain first (read-only, prints the fix for anything missing):

```bash
cd mobile && go run ./cmd/gofastr-mobile doctor
```

iOS simulator (Mac with Xcode installed):

```bash
cd mobile && go run ./cmd/gofastr-mobile run --platform ios-sim --pkg ./examples/notes/notesmobile --id dev.gofastr.notes --name Notes --screenshot dist/ios.png
```

Android emulator (Android SDK with build-tools, a platform, the NDK, the
emulator and one AVD; a JDK 17+):

```bash
cd mobile && go run ./cmd/gofastr-mobile run --platform android-emu --pkg ./examples/notes/notesmobile --id dev.gofastr.notes --name Notes --screenshot dist/android.png
```

`build` instead of `run` stops after the `.app` / `.apk`. `--dry-run` prints
every step without running any; `--device` picks a simulator (name or UDID),
an adb serial or an AVD. Output lands in `dist/mobile/<platform>/`.

The steps, so a failure is easy to place:

| iOS simulator | Android emulator |
|---|---|
| build the pinned `gomobile` + `gobind` (go.mod `tool` directive) | same |
| `gomobile bind -target=iossimulator` -> `Notesmobile.xcframework` | `gomobile bind -target=android/arm64,android/amd64` -> `notesmobile.aar` |
| render `App.swift`, `Info.plist` | render `MainActivity.java`, `AndroidManifest.xml` |
| `xcrun swiftc` links the host to the framework | `javac` -> `d8` -> `aapt2 link`, then dex + `libgojni.so` added in Go |
| `codesign -s -` (ad hoc; all the simulator needs) | `zipalign`, `apksigner` (debug key) |
| `simctl boot/install/launch`, `simctl io screenshot` | `emulator -avd`, `adb install/am start`, `screencap` |

Debugging: Safari > Develop > Simulator attaches the Web Inspector to the iOS
WebView; `chrome://inspect` attaches DevTools to the Android one. The run
prints the log command for each.

**Expect on first run:** these builds have never run on real hardware (this
repository was developed on Linux). What is checked here: the generated Swift
calls the exact functions and method shapes `gobind` generates for the bind
package, the generated Java compiles with `javac` against `gobind`'s real Java
bindings and signature-exact Android stubs, the plist and manifest are
well-formed, and the dry-run plans. Not checked: `swiftc`, `gomobile bind`
with a real SDK, `d8`/`aapt2`/`apksigner`, and the apps themselves. On
Android, saving a note fails until the body bridge exists (finding 6). A
physical iPhone also needs a development team and provisioning profile,
which is not wired yet.

## What was verified here

- **The whole app runs in-process.** The list page, client-side navigation to
  the create form (a window marker survives the click: no full reload), a form
  save (`POST /api/notes -> 201`), the toast, the updated list, light and dark
  themes. 42 requests answered in-process, zero reached the network.
- **Single-user identity.** Rows are stamped with a per-install id that
  survives a stop/start on the same data dir (the app killed and relaunched).
- **Streaming.** `Host.Open` returns headers before the body ends and hands
  each flushed chunk over as it is written, the SSE shape (Go test; CDP cannot
  stream a fulfilled response).
- **Cookies stay in Go.** `Set-Cookie` never reaches the WebView; the host's
  jar replays it, `Secure` included.
- **Cross-compiles.** Every package compiles for `ios/arm64`, `ios/amd64`,
  `android/arm64` and `android/amd64` with `CGO_ENABLED=0`, including the
  pure-Go SQLite driver. A static `android/arm64` executable links with no
  Android toolchain (35 MB stripped). `ios/arm64` needs cgo external linking,
  and `-buildmode=c-shared` / `c-archive` need the NDK / Xcode clang. Neither
  is available in this container.

## What was NOT verified (no Xcode, no Android SDK, no KVM)

The native hosts have never been compiled by `swiftc` or run on a simulator,
emulator or device (see "Expect on first run" above). The platform behaviours
below come from the platforms' documented APIs and need a device to confirm.

## Findings

### Core (this repo) needed one change

1. **`framework.App.Serve(net.Listener)`**. The app could only start by
   binding a TCP address. `Serve` runs the identical lifecycle on a
   caller-supplied listener. It shipped with tests and docs
   (`framework/docs/content/plugins.md`).

### What a separate repo cannot reach

2. **`internal/localuser` is copied** (`mobilehost/identity.go`). Beyond drift,
   two copies break `localuser.Install`'s check for whether the installed owner
   extractor is battery/auth's. It recognizes its own extractor by code pointer,
   so a process linking desktop and mobile would treat the other's extractor
   as auth's.
3. **Data-dir helpers are copied** (`mobilehost/datadir.go`): owner-only
   create, exclusive mint, DB open. Desktop uses `internal/fileperm` and
   `internal/atomicfile` for these.
4. **Test helpers and file writes too.** `internal/fileperm.WriteOwnerOnly`
   is copied into the CLI (`writeOwnerOnly`). `internal/chromedptest` (the launch-allowance
   Chrome harness the repo's gate requires) is out of reach, so `sim/`
   spells the two start timeouts itself.
5. **The desktop contract is not reusable as-is.** `desktop.Shell` assumes a
   process-owning event loop, windows, a tray and menus, and the boot gate
   assumes a loopback listener. None of it fits. What mobile wants from desktop
   is the OS-neutral half (the capability registry, grants, bridge chokepoint,
   app state), and that lives inside `battery/desktop`, tied to the desktop
   battery.

### Platform constraints the runtime will meet

6. **Android drops request bodies.** `WebResourceRequest` has no body, so
   every `fetch()` POST reaches Go empty. With `SIM_ANDROID_LIMITS=1` the
   simulated save fails (`POST /api/notes -> 400`). Fix options: a JS shim that
   hands bodies to a `WebMessageListener` keyed by a request id, or, cleaner, a
   **runtime transport hook in core-ui** that lets a native host replace
   `fetch` for same-origin calls. That hook would be core work.
7. **Neither platform can return a redirect** from an interception API
   (`WebResourceResponse` rejects 3xx; a scheme task cannot redirect). The
   notes flow never redirects. Auth flows (`login -> 303`) will. The host
   probably needs to follow redirects itself for sub-requests and tell the
   shell to load the target for top-level navigations.
8. **Cookies cannot cross the interception boundary**, so the jar lives in Go
   (done). Consequence: page JS never sees a cookie. The runtime's
   `serverAction` reads `gofastr-session` from `document.cookie`
   (`core-ui/runtime/runtime.js`). Server actions would send an empty session
   until the runtime takes it from somewhere else (the `<meta name=
   "gofastr-sse">` session already exists for SSE).
9. **iOS origin is a custom scheme** (`gofastr://app`). To verify on device:
   POST bodies on custom schemes (historically dropped by WebKit), whether the
   page is a secure context (Web Crypto, service workers), `localStorage`
   persistence.
10. **No socket means no loopback hardening to port.** The desktop boot token,
   Host pin and stable-origin profile exist because any local process can reach
   a loopback port. Nothing outside the process can reach an in-memory listener,
   so the identity middleware needs no gate.

### Smaller findings

11. The startup banner prints `http://memory/...` and "No auth is wired"
    warnings. Harmless, but a host-owned transport should be able to quiet them.
12. The adaptive theme is opt-in (`site.WithTheme(theme.Default())`);
    desktop-notes does not set it. A mobile scaffold should, because phone users
    expect OS dark mode.
13. Design-system bug seen along the way, independent of mobile: the entityui
    list's empty state is clipped at 390px (same over plain TCP; correct at
    1280px).

## Recommended next steps

1. Keep `App.Serve` in core.
2. Make `localuser` and the data-dir helpers public (or one public
   "local app data" package that desktop and mobile share).
3. Design the runtime transport hook (finding 6) and the session-source change
   (finding 8) in core-ui. They decide whether Android works at all.
4. Then split this module into its own repository, add a `gofastr mobile`
   scaffold, and verify the shims on a Mac with Xcode and an Android emulator.
