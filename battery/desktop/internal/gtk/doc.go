// Package gtk is the Linux half of the desktop host's native layer:
// dlopen/dlsym of the GTK 3 and WebKitGTK 4.1 shared libraries and the
// resolved C entry-point table the shell calls through internal/ffi.
//
// It is the linux counterpart of internal/objc. Nothing here loads a
// library at import time; the first Load call does that, so a CLI that
// blank-imports the desktop battery for its AGENTS inventory never pulls
// GTK into the process.
//
// The libraries are a runtime requirement on the user's machine, the
// same bargain Tauri makes. Load returns an error naming the distro
// packages when they are absent, and callers are expected to surface it
// rather than crash.
//
// This file carries no build tag on purpose (the shape internal/ffi and
// internal/objc use): it keeps the package non-empty for `GOOS=darwin`
// and `GOOS=windows` builds, where every other file in it is excluded
// and Load is absent. Everything real is in gtk_linux.go and its
// per-arch assembly.
package gtk
