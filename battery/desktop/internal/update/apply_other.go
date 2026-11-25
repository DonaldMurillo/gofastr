//go:build !windows && !linux && (!darwin || (!arm64 && !amd64))

package update

type unsupportedPlatform struct{}

// DefaultPlatform returns the refusing Platform.
func DefaultPlatform(onMain func(func()) error, runner CommandRunner) Platform {
	return unsupportedPlatform{}
}

func (unsupportedPlatform) Version() string { return "" }

func (unsupportedPlatform) Identifier() string { return "" }

func (unsupportedPlatform) Bundle() (string, string, bool) { return "", "", false }

func (unsupportedPlatform) VerifySignature(string) error { return ErrUnsupportedApply }

func (unsupportedPlatform) Relaunch(string) error { return ErrUnsupportedApply }
