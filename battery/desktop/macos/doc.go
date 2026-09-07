// Package macos is the darwin arm of the desktop host: the Shell over
// AppKit and WKWebView (window, menus, tray, dialogs, pasteboard,
// notifications, deep links), built on the pure-Go Objective-C bridge in
// battery/desktop/internal/objc with no cgo anywhere.
//
// New returns that shell on darwin/arm64 and the unsupported shell on
// every other GOOS/GOARCH; hosts reach it through battery/desktop/native
// (native.Shell(), or native.New(desktop.Config) as the desktop.New
// default), never by importing this package directly for the selection.
//
// Lazy loading rule: constructing the shell loads nothing. Frameworks
// are dlopened on the first Run, because cmd/gofastr blank-imports the
// battery for the AGENTS inventory and must not load AppKit at startup.
// The file-level build tags are //go:build darwin && arm64 (the shell)
// and its complement in shell_other.go (the fallback).
package macos
