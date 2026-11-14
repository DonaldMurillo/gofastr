package admin

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

var docTitle = regexp.MustCompile(`<title>([^<]*)</title>`)

func titleOf(body string) string {
	m := docTitle.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return m[1]
}

// A page's TabTitle names the view for the browser tab from the
// request; the sidebar, the crumb and an empty or panicking answer keep
// Title. It is never asked for a caller the page's Access refuses.
func TestPageTabTitleNamesTheView(t *testing.T) {
	asked := 0
	p := page("/reports", "Reports", says("report body"))
	p.Nav = &entity.EntityNav{}
	p.TabTitle = func(ctx context.Context) string {
		asked++
		switch r := appui.RequestFromContext(ctx); r.URL.Query().Get("show") {
		case "late":
			return "Late <reports>"
		case "boom":
			panic("title store down")
		}
		return ""
	}
	x := setup(t, nil, Config{Pages: []Page{p}}, nil)
	for query, want := range map[string]string{"?show=late": "Late &lt;reports&gt;", "": "Reports", "?show=boom": "Reports"} {
		rr := get(x.as(theAdmin), "/admin/reports"+query)
		body := rr.Body.String()
		if rr.Code != http.StatusOK || !strings.HasPrefix(titleOf(body), want+" ") && titleOf(body) != want {
			t.Errorf("%q: status %d, title %q, want %q", query, rr.Code, titleOf(body), want)
		}
		if !strings.Contains(body, "report body") || !strings.Contains(body, ">Reports<") {
			t.Errorf("%q: the page body or its nav label is gone", query)
		}
	}
	if asked == 0 {
		t.Error("TabTitle was never asked")
	}

	asked = 0
	p.Access = func(context.Context) bool { return false }
	x = setup(t, nil, Config{Pages: []Page{p}}, nil)
	if rr := get(x.as(theAdmin), "/admin/reports?show=late"); rr.Code != http.StatusForbidden || strings.Contains(rr.Body.String(), "Late") {
		t.Errorf("SECURITY: a refused caller got %d and the view's title", rr.Code)
	}
	if asked != 0 {
		t.Errorf("SECURITY: TabTitle ran %d times for a caller the page refuses", asked)
	}
}
