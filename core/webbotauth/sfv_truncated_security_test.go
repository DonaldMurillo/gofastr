package webbotauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A dictionary member whose value is missing entirely ("x=") ends the input
// right after the '='. parseMember consumed the '=' and then peeked without
// an eof check, so the parser indexed one past the end and panicked. That is
// reachable from a plain request header, and in observe mode — whose whole
// promise is that a verification bug cannot take an app's traffic down.
func TestParseSFDictionary_TruncatedMemberIsRejected(t *testing.T) {
	for _, in := range []string{
		"x=",
		"x= ",
		"sig1=",
		"a=1, b=",
		"x=;p=1",
		"=",
	} {
		t.Run(in, func(t *testing.T) {
			// A panic here fails the test rather than killing the process,
			// so this stays a red-to-green proof rather than a crash.
			_, err := parseSFDictionary(in)
			if err == nil {
				t.Fatalf("parseSFDictionary(%q) = nil error, want a rejection", in)
			}
		})
	}
}

// The same bytes must not panic when they arrive on the wire. Observe mode
// annotates and logs; it never blocks and it must never take the request
// down either.
func TestVerifyRequest_TruncatedHeadersDoNotPanic(t *testing.T) {
	for _, tc := range []struct{ name, sigInput, sig, agent string }{
		{"signature-input truncated", "x=", "", ""},
		{"signature truncated", `sig1=("@method")`, "sig1=", ""},
		{"agent truncated", `sig1=("@method")`, "sig1=:AAAA:", "sig1="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.sigInput != "" {
				r.Header.Set("Signature-Input", tc.sigInput)
			}
			if tc.sig != "" {
				r.Header.Set("Signature", tc.sig)
			}
			if tc.agent != "" {
				r.Header.Set("Signature-Agent", tc.agent)
			}
			// Any return value is fine: the assertion is that we get one.
			_ = New(false, nil).VerifyRequest(r)
		})
	}
}
