package app_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

func TestSubGroupNormalizesRelativePrefix(t *testing.T) {
	for _, test := range []struct {
		parent string
		child  string
		want   string
	}{
		{"/docs", "guides", "/docs/guides/"},
		{"/docs", "/guides/", "/docs/guides/"},
		{"/accounts/", "members/", "/accounts/members/"},
		{"/accounts", "", "/accounts/"},
		{"/accounts", "/", "/accounts/"},
		{"/", "//reports//daily//", "/reports/daily/"},
	} {
		t.Run(test.parent+"+"+test.child, func(t *testing.T) {
			group := app.NewScreenGroup(test.parent, nil).SubGroup(test.child, nil)
			if got := group.Prefix(); got != test.want {
				t.Errorf("Prefix() = %q, want %q", got, test.want)
			}
			screen := app.NewScreen("detail", &stubComp{html: "detail"})
			group.Screen(screen, nil)
			if screen.Path != test.want+"detail" {
				t.Errorf("screen path = %q, want %q", screen.Path, test.want+"detail")
			}
		})
	}
}
