package update

import (
	"bytes"
	"errors"
	"fmt"
)

// TrustState is what an Authenticode check found on one file.
type TrustState int

const (
	// Unsigned: the file carries no signature at all.
	Unsigned TrustState = iota
	// Trusted: the signature verifies and chains to a trusted root.
	Trusted
	// Untrusted: a signature is present but does not verify, or its chain does not reach a trusted root (tampered, self-signed, expired without a timestamp).
	Untrusted
)

// Trust is one file's Authenticode result.
type Trust struct {
	State   TrustState
	Subject []byte
	Detail  string
}

// ErrSignature is the sentinel for an update refused by its Authenticode signature.
var ErrSignature = errors.New("update signature check failed")

// CheckWindowsTrust is the Windows install's signature policy.
func CheckWindowsTrust(running, runningDLL, exe, dll Trust) error {
	if dll.State != Trusted {
		return fmt.Errorf("%w: %s is not signed by a trusted publisher (%s)", ErrSignature, WebView2LoaderName, dll.Detail)
	}
	if runningDLL.State == Trusted && (len(runningDLL.Subject) == 0 || !bytes.Equal(runningDLL.Subject, dll.Subject)) {
		return fmt.Errorf("%w: %s is signed by a different publisher than the installed one", ErrSignature, WebView2LoaderName)
	}
	if exe.State == Untrusted {
		return fmt.Errorf("%w: the new executable's signature does not verify (%s)", ErrSignature, exe.Detail)
	}
	if running.State != Trusted {
		return nil
	}
	if exe.State != Trusted {
		return fmt.Errorf("%w: the running app is signed and the update is not", ErrSignature)
	}
	if len(running.Subject) == 0 || !bytes.Equal(running.Subject, exe.Subject) {
		return fmt.Errorf("%w: the update is signed by a different publisher than the running app", ErrSignature)
	}
	return nil
}
