//go:build red

package rtc

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// CONTRACT-QUESTION: config-layer input (operator-authored URLs), but the property is the
// function's own documented contract ("an ICE URI a browser will accept") and the scheme-only
// fix abfee3ee established exactly this class; the leak leg (user:pass@ disclosed to every
// room member via iceServersFor verbatim copy) is the security payload.
// Property: ICE server URLs handed to browsers match RFC 7064/7065 grammar — port 1-65535, no
// empty port, no userinfo.
// Surfaces: turn.go::validICEURLs :27-55 (verified: accepts turn:host:99999, turn:host:,
// turn:user:secret@host:3478, stun:host:70000), rtc.go::validateICEServers→New,
// room.go::iceServersFor :150-156 + turn.go::Credentials :77-86 (verbatim copy into peer
// snapshots).
// Finding: validICEURLs checks the scheme, the host, and the transport query, but not the
// port grammar or userinfo. An out-of-range or empty port reaches the browser as an
// RTCIceServer the WebRTC parser rejects, and userinfo in the URL is copied verbatim by
// iceServersFor into every peer snapshot — a long-lived credential disclosed to every room
// member.
// Fix direction: in validICEURLs, split the endpoint's trailing :port (bracketed-IPv6
// aware), refuse an empty, non-numeric, or out-of-1-65535 port, and refuse any "@"
// (RFC 7065 has no userinfo production); New keeps panicking "rtc:"-prefixed.

import (
	"strings"
	"testing"
)

// iceDepthMustPanic mirrors rtc_test.go's mustPanic (recover-capture of the
// "rtc:"-prefixed construction panic) but reports the round-5 SECURITY tag on
// the no-panic leg, so a red run fails with the finding itself.
func iceDepthMustPanic(t *testing.T, what, url string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("SECURITY: [rtc-ice-uri-depth] New accepted %s URL %q: want an \"rtc:\"-prefixed panic at construction", what, url)
			return
		}
		msg, ok := r.(string)
		if !ok || !strings.HasPrefix(msg, "rtc:") {
			t.Errorf("SECURITY: [rtc-ice-uri-depth] New(%s %q) panic = %#v, want an \"rtc:\"-prefixed message", what, url, r)
		}
	}()
	fn()
}

func TestICEURIDepthRedRefusesBadPorts(t *testing.T) {
	for _, u := range []string{
		"turn:host:99999",            // port above 65535
		"turn:host:",                 // empty port
		"turn:user:secret@host:3478", // userinfo: RFC 7065 has no such production
		"stun:host:70000",            // port above 65535, stun spelling
	} {
		if validICEURLs([]string{u}) {
			t.Errorf("SECURITY: [rtc-ice-uri-depth] validICEURLs accepted %q: RFC 7064/7065 grammar requires a port in 1-65535 (never empty) and no userinfo; iceServersFor copies the URL verbatim into every peer snapshot", u)
		}
		iceDepthMustPanic(t, "ICEServer", u, func() {
			if s := New(Config{ICEServers: []ICEServer{{URLs: []string{u}}}}); s != nil {
				s.Close()
			}
		})
		iceDepthMustPanic(t, "TURN", u, func() {
			if s := New(Config{TURN: &TURN{URLs: []string{u}, Secret: "k"}}); s != nil {
				s.Close()
			}
		})
	}
	// Positive control, guarding over-refusal: a bracketed IPv6 host with a
	// port and the transport query are legal RFC 7065 URIs today and must
	// stay legal after the depth check lands.
	for _, u := range []string{
		"turn:[2001:db8::1]:3478",
		"turn:host:3478?transport=tcp",
	} {
		if !validICEURLs([]string{u}) {
			t.Errorf("validICEURLs over-refuses %q: a legal RFC 7065 ICE URI", u)
		}
	}
	if s := New(Config{ICEServers: []ICEServer{{URLs: []string{"turn:[2001:db8::1]:3478"}}}}); s == nil {
		t.Error("setup broken: New refused a legal ICE URI")
	} else {
		s.Close()
	}
}
