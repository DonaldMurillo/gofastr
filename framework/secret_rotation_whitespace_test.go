package framework

import (
	"bytes"
	"strconv"
	"testing"
)

// TestSecretRotationKeepsAWhitespaceCarryingSecret: GOFASTR_SECRET is used
// as given, so a value read from a file with a trailing newline (a
// Kubernetes secret built with --from-file) signs with the newline in it.
// Rotating moves that same value into GOFASTR_SECRET_PREVIOUS, whose
// entries were trimmed: the verify-only key was derived from a different
// secret and every session the old one signed stopped verifying.
func TestSecretRotationKeepsAWhitespaceCarryingSecret(t *testing.T) {
	const base = "0123456789abcdef0123456789abcdef-old"
	for _, old := range []string{base + "\n", base + "\r\n", base + " ", base + "\t", " " + base, "\t" + base + " "} {
		t.Run(strconv.Quote(old), func(t *testing.T) {
			t.Setenv("GOFASTR_SECRET", old)
			t.Setenv("GOFASTR_SECRET_PREVIOUS", "")
			before := NewApp()
			signed, _, err := sessionKeysForMount(before.secret, before.previousSecrets, false)
			if err != nil {
				t.Fatal(err)
			}

			t.Setenv("GOFASTR_SECRET", "0123456789abcdef0123456789abcdef-new")
			t.Setenv("GOFASTR_SECRET_PREVIOUS", old)
			after := NewApp()
			_, previous, err := sessionKeysForMount(after.secret, after.previousSecrets, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, k := range previous {
				if bytes.Equal(k, signed) {
					return
				}
			}
			t.Fatalf("no previous key verifies what %q signed: the rotation logs everyone out", old)
		})
	}
}
