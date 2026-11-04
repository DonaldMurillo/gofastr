package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/queue"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/internal/browserpath"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestQueueCountsUseLocale(t *testing.T) {
	executable, ok := browserpath.Find()
	if !ok {
		t.Skip("Chrome required")
	}
	env := queueEnv(t, &fakeQueue{stats: queue.JobStats{"pending": 1234}})
	handler := env.as(theAdmin)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		handler.ServeHTTP(writer, request.WithContext(i18n.WithContext(request.Context(), i18n.Locale{Tag: "ar-EG"})))
	}))
	t.Cleanup(server.Close)
	browser := chromedptest.Context(t, chromedptest.AllocatorOptions(chromedp.ExecPath(executable)))
	ctx, cancel := context.WithTimeout(browser, 30*time.Second)
	defer cancel()
	var count string
	var image []byte
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL+"/admin/queue"),
		chromedp.Text(".fui-seglinks__count", &count, chromedp.ByQuery), chromedp.FullScreenshot(&image, 90)); err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("GOFASTR_TEST_SCREENSHOT"); path != "" {
		if err := os.WriteFile(path, image, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if count != "١٬٢٣٤" {
		t.Errorf("ar-EG queue count=%q, want ١٬٢٣٤", count)
	}
}
