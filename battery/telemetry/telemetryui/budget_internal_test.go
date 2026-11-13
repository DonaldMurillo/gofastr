package telemetryui

import (
	"context"
	"fmt"
	"html"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/telemetry"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	itel "github.com/DonaldMurillo/gofastr/internal/telemetry"
)

var olderHref = regexp.MustCompile(`<a[^>]*href="([^"]*before=[^"]*)"[^>]*>(?:<[^>]*>)*\s*Older`)

// A search that runs out of time shows what it found, says where it
// stopped, and its Older link carries on from there even when it found
// nothing yet: followed far enough, it reaches the one matching line at
// the old end of the day and then says nothing about stopping.
func TestLogSearchOutOfTimeCarriesOn(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	base := now.Add(-2 * time.Hour)
	if base.UTC().YearDay() != now.UTC().YearDay() {
		t.Skip("within two hours of midnight UTC: the seed would straddle two days")
	}
	var logs []byte
	for i := range 6000 {
		msg := "filler"
		if i == 3 {
			msg = "the-needle"
		}
		logs = append(logs, fmt.Sprintf(`{"time":%q,"level":"INFO","msg":%q,"n":%d}`+"\n", base.Add(time.Duration(i)*time.Millisecond).UTC().Format(time.RFC3339Nano), msg, i)...)
	}
	if err := os.WriteFile(filepath.Join(dir, itel.FileName(itel.KindLogs, base)), logs, 0o600); err != nil {
		t.Fatal(err)
	}
	tel := telemetry.New(telemetry.Config{Dir: dir, ExternalTracerProvider: true})
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(tel)
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })

	s := NewScreens(tel, "/t", false)
	s.searchBudget = time.Nanosecond
	draw := func(url string) string {
		r := httptest.NewRequest("GET", url, nil)
		return string(s.Logs(appui.WithRequest(r.Context(), r)))
	}
	url := "/t/logs?q=the-needle"
	for hop := 0; ; hop++ {
		body := draw(url)
		if strings.Contains(body, ">the-needle<") {
			if strings.Contains(body, "The search stopped") {
				t.Fatal("the page that found the line still says the search stopped")
			}
			if hop == 0 {
				t.Fatal("the first page found the line: the budget never ran out, so nothing was tested")
			}
			return
		}
		m := olderHref.FindStringSubmatch(body)
		if !strings.Contains(body, "The search stopped") || m == nil {
			t.Fatalf("hop %d found nothing, and offers no way on (note: %v, older link: %v)", hop, strings.Contains(body, "The search stopped"), m != nil)
		}
		if next := html.UnescapeString(m[1]); next == url || !strings.Contains(next, "q=the-needle") {
			t.Fatalf("hop %d: the Older link goes nowhere new or drops the search: %s", hop, next)
		} else {
			url = next
		}
		if hop > 20 {
			t.Fatal("twenty hops and the line was not reached")
		}
	}
}

// A rule that is firing is said at the top of the requests and issues
// pages, in the reader's words, and not on a quiet day.
func TestFiringAlertIsShownOnThePages(t *testing.T) {
	tel := telemetry.New(telemetry.Config{Dir: t.TempDir(), ExternalTracerProvider: true})
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(tel)
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	s := NewScreens(tel, "/t", true)
	draw := func(f func(context.Context) render.HTML) string {
		r := httptest.NewRequest("GET", "/t/x", nil)
		return string(f(appui.WithRequest(r.Context(), r)))
	}
	for name, f := range map[string]func(context.Context) render.HTML{"requests": s.Requests, "issues": s.Issues} {
		if body := draw(f); strings.Contains(body, "is firing") || strings.Contains(body, "fui-callout") {
			t.Errorf("%s: an alert notice, or an empty one, is shown with none firing", name)
		}
	}
	s.firingNow = func() []telemetry.Alert {
		return []telemetry.Alert{
			{Rule: telemetry.AlertRule{Name: "failed <requests>", Kind: telemetry.AlertFailed, AtLeast: 10, Within: 10 * time.Minute}, Firing: true, Count: 37},
			{Rule: telemetry.AlertRule{Name: "slow", Kind: telemetry.AlertSlow, AtLeast: 5}, Firing: true, Count: 6},
		}
	}
	for name, f := range map[string]func(context.Context) render.HTML{"requests": s.Requests, "issues": s.Issues} {
		body := draw(f)
		for _, want := range []string{"37 failed requests in the last 10 minutes", "6 slow requests in the last 5 minutes", "failed &lt;requests&gt;"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %q", name, want)
			}
		}
		if strings.Contains(body, "<requests>") {
			t.Errorf("%s: a rule's name reached the page as markup", name)
		}
	}
	if body := draw(s.Logs); strings.Contains(body, "is firing") {
		t.Error("the logs page shows the alert: it is on the two pages that explain it")
	}
}
