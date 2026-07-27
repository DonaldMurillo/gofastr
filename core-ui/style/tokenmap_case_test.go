package style_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func TestDeclBreakerIsCaseInsensitive(t *testing.T) {
	base := style.DefaultTheme()
	for _, v := range []string{
		"0 0 0 Url(https://attacker/x)",
		"0 0 0 URL(https://attacker/x)",
		"0 0 0 uRl(https://attacker/x)",
		"0 0 0 url(https://attacker/x)",
	} {
		if _, err := style.ApplyTokens(base, map[string]string{"shadow-md": v}); err == nil {
			t.Errorf("ACCEPTED %q", v)
		}
	}
	// Legitimate values must still pass after the fold.
	for _, v := range []string{"0 1px 3px rgba(0,0,0,0.12)", "none", "0 0 0 1px var(--color-border)"} {
		if _, err := style.ApplyTokens(base, map[string]string{"shadow-md": v}); err != nil {
			t.Errorf("REJECTED legitimate %q: %v", v, err)
		}
	}
}
