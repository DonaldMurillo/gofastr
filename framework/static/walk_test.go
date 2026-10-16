package static

import (
	"context"
	"errors"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type walkPanicScreen struct{}

func (walkPanicScreen) Render() render.HTML { panic("broken page") }

// Walk hands a page that cannot render to fn with its error: nil walks
// on without it, an error stops the walk with that error.
func TestWalkHandsFailuresToFn(t *testing.T) {
	a := coreapp.NewApp("walk")
	a.Register("/a", nfStaticScreen("A"), nil)
	a.Register("/broken", walkPanicScreen{}, nil)
	a.Register("/c", nfStaticScreen("C"), nil)
	host := uihost.New(a)

	var seen, failed []string
	err := Walk(context.Background(), host, nil, func(path, html string, err error) error {
		if err != nil {
			if html != "" {
				t.Errorf("%s failed but came with html", path)
			}
			failed = append(failed, path)
			return nil
		}
		seen = append(seen, path)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk = %v after fn skipped the failure", err)
	}
	if got := strings.Join(seen, " "); got != "/a /c" {
		t.Errorf("rendered %q, want /a /c", got)
	}
	if got := strings.Join(failed, " "); got != "/broken" {
		t.Errorf("failures %q, want /broken", got)
	}

	stop := errors.New("stop")
	err = Walk(context.Background(), host, nil, func(path, html string, err error) error {
		if err != nil {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Errorf("Walk = %v, want fn's error", err)
	}
}
