package validatecmd

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
)

// stubFontFetcher swaps in a fake font fetcher for the duration of a test so
// the suite never touches the network.
func stubFontFetcher(t *testing.T, fn func(family string) ([]byte, error)) {
	t.Helper()
	prev := appgen.FontFetcher
	appgen.FontFetcher = fn
	t.Cleanup(func() { appgen.FontFetcher = prev })
}
