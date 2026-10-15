package app_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// slugScreen reads the route's resolved slug in Load, the way a
// catch-all docs route reads its article.
type slugScreen struct {
	key  *app.Key[string]
	slug string
}

func (s *slugScreen) SetParams(map[string]string) {}
func (s *slugScreen) Load(ctx context.Context) error {
	v, err := s.key.Get(ctx)
	s.slug = v
	return err
}
func (s *slugScreen) Render() render.HTML { return render.Text("ARTICLE[" + s.slug + "]") }

// A screen whose Load reads a route resolver gets the loaded content in
// its llm.md, the same as its page render: the match and the resolvers
// run before Load. An unknown slug (the resolver's ErrNotFound) is no
// screen at all.
func TestLLMMDForPathRunsResolvers(t *testing.T) {
	a := app.NewApp("t")
	slug := app.NewKey[string]("slug")
	a.RegisterScreen(app.NewScreen("/d/{slug}", &slugScreen{key: slug}).
		Resolve(slug.From(func(ctx context.Context) (string, error) {
			m, _ := app.MatchFromContext(ctx)
			if m.Param("slug") == "missing" {
				return "", fmt.Errorf("no doc: %w", app.ErrNotFound)
			}
			return m.Param("slug"), nil
		})), nil)

	res, ok := app.ScreenLLMMDForPath(context.Background(), a, "/d/hello")
	if !ok || !strings.Contains(res.MD, "ARTICLE[hello]") {
		t.Errorf("llm.md for /d/hello (ok=%v) lacks the loaded article:\n%s", ok, res.MD)
	}
	if res, ok := app.ScreenLLMMDForPath(context.Background(), a, "/d/missing"); ok {
		t.Errorf("llm.md for an unknown slug answered; want no screen:\n%s", res.MD)
	}
}
