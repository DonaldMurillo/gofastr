// Package objc is the pure-Go Objective-C runtime bridge for the darwin
// desktop host: dlopen of AppKit, WebKit, and libobjc, objc_msgSend
// through the internal/ffi call trampoline, class registration with Go
// IMPs, block literals, and the dispatch main-queue hop.
//
// It began as the desktop host's phase-0 spike and stays internal on
// purpose: nothing outside battery/desktop imports it, and it is the
// layer most likely to change.
//
// darwin/arm64 and darwin/amd64. Almost all of it is shared; the message
// send is where the two ABIs genuinely differ and msgsend_darwin_arm64.go
// and msgsend_darwin_amd64.go carry that difference, each with the clang
// output that proves its layout. On every other GOOS/GOARCH the package
// is empty.
package objc
