package uihost

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type retiredTitlePage struct{}

func (retiredTitlePage) Render() render.HTML { return "body" }
func (retiredTitlePage) ScreenTitle() string { return "Old title" }

// The host's own 404 and 500 screens never pass through app.Register, so
// their options refuse the retired no-argument title method themselves.
func TestHostScreensRefuseRetiredTitle(t *testing.T) {
	for name, opt := range map[string]func(component.Component) Option{
		"not-found": WithNotFoundScreen,
		"error":     WithErrorScreen,
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if msg, _ := recover().(string); !strings.Contains(msg, "ScreenTitle(ctx context.Context) string") {
					t.Fatalf("panic = %q, want the retired-title refusal", msg)
				}
			}()
			opt(retiredTitlePage{})
		})
	}
}
