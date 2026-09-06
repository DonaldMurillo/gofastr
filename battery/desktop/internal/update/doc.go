// Package update is the auto-updater's engine: the signed feed format,
// semver comparison, capped downloads, pure-Go zip extraction, and the
// OS-specific apply step behind one interface.
//
// The feed is manifest.json plus a detached manifest.json.sig carrying
// a base64 ed25519 signature over the exact manifest bytes. The
// signature is verified BEFORE anything is parsed, so a tampered feed
// is rejected without its content ever reaching a decoder.
//
// Every error this package returns is one of the sentinel errors below
// or wraps one with no path or URL in the message: the strings cross
// into the bridge's closed error-code set and into logs.
package update
