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
