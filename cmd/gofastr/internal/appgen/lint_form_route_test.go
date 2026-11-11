package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// A screen route answers GET and HEAD only (uihost refuses every other
// method with 405), so a custom_form that POSTs to one never submits:
// the union fixture's /contact form did exactly that, with or without
// JavaScript. validate and generate warn, naming the screen and route.
func TestPostFormToScreenRouteWarns(t *testing.T) {
	form := func(action, method string) appspec.AppSpecBlock {
		props := map[string]any{"action": action}
		if method != "" {
			props["method"] = method
		}
		return appspec.AppSpecBlock{Kind: "custom_form", Props: props}
	}
	spec := func(blocks ...appspec.AppSpecBlock) appspec.AppSpec {
		return appspec.AppSpec{Screens: []appspec.AppSpecScreen{
			{Name: "contact", Route: "/contact", Body: []appspec.AppSpecBlock{{Type: "section", Children: blocks}}},
			{Name: "post", Route: "/posts/{id}"},
		}}
	}
	for _, c := range []struct{ action, method string }{
		{"/contact", ""},
		{"/contact", "post"},
		{"/contact", "POST"},
		{"/contact?sent=1", ""},
		{"/posts/7", ""},
		{"contact", ""},
		{"https://example.com/contact", "post"}, // another host: not this app's route
	} {
		got := LintPostFormsOnScreens(spec(form(c.action, c.method)))
		if c.action == "https://example.com/contact" {
			if len(got) != 0 {
				t.Errorf("absolute action %q warned: %v", c.action, got)
			}
			continue
		}
		if len(got) != 1 || !strings.Contains(got[0].Message(), `"contact"`) || !strings.Contains(got[0].Message(), "405") {
			t.Errorf("POST custom_form action %q method %q: findings %v", c.action, c.method, got)
		}
	}
	for _, c := range []struct{ action, method string }{
		{"/contact", "get"},
		{"/contact", "GET"},
		{"/api/messages", ""},
		{"/posts", ""},
		{"/posts/7/publish", ""},
	} {
		if got := LintPostFormsOnScreens(spec(form(c.action, c.method))); len(got) != 0 {
			t.Errorf("custom_form action %q method %q warned: %v", c.action, c.method, got)
		}
	}
}
