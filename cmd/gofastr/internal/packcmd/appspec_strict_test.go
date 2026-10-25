package packcmd

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestAppSpecScreenWithDescriptionKeepsDescriber(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Demo", Module: "example.com/demo"},
		Screens: []appspec.AppSpecScreen{
			{Name: "about", Route: "/about", Title: "About", Description: "Who we are."},
		},
	}
	got := inlinePhrases(appgen.RenderAppSpecScreens(bp))
	clitest.AssertContains(t, got, `func (s *AboutScreen) ScreenDescription(ctx context.Context) string { return "Who we are." }`)
	if strings.Contains(got, `func (s *AboutScreen) ScreenSEO(`) {
		t.Fatal("described screen must not also emit the opt-out")
	}
}
