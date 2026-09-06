//go:build !darwin || (!arm64 && !amd64)

package update

// unsupportedPlatform is the DefaultPlatform for every host without a
// native apply step (windows, linux, and any darwin the objc layer
// does not build for): every method refuses.
type unsupportedPlatform struct{}

// DefaultPlatform returns the refusing Platform.
func DefaultPlatform(onMain func(func()) error, runner CommandRunner) Platform {
	return unsupportedPlatform{}
}

// Version implements Platform: no bundle to read.
func (unsupportedPlatform) Version() string { return "" }

// Bundle implements Platform: never bundled.
func (unsupportedPlatform) Bundle() (string, string, bool) { return "", "", false }

// VerifySignature implements Platform.
func (unsupportedPlatform) VerifySignature(string) error { return ErrUnsupportedApply }

// Relaunch implements Platform.
func (unsupportedPlatform) Relaunch(string) error { return ErrUnsupportedApply }
