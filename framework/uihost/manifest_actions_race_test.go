package uihost_test

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type clickActions struct{ component.ContextOnly }

func (clickActions) Actions() { component.On("click", func(*component.ComponentContext) {}) }

// Run under -race: the manifest read of the action map raced a compile.
func TestManifestWhileCompilingActions(t *testing.T) {
	for attempt := 0; attempt < 25; attempt++ {
		host := uihost.New(app.NewApp("race"))
		r := router.New()
		host.Mount(r)
		host.CompileActions("initial", clickActions{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest("GET", "/__gofastr/manifest.js", nil))
			if rec.Code != 200 {
				t.Errorf("manifest status %d", rec.Code)
			}
		}()
		for id := 0; ; id++ {
			host.CompileActions(fmt.Sprintf("screen-%d", id), clickActions{})
			select {
			case <-done:
			default:
				continue
			}
			break
		}
	}
}
