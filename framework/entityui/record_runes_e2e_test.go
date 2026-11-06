package entityui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestRecordInputsKeepRuneSizedText(t *testing.T) {
	config := entity.EntityConfig{Table: "texts", Fields: []schema.Field{
		{Name: "title", Type: schema.String, Min: fptr(2), Max: fptr(2)},
		{Name: "body", Type: schema.Text, Min: fptr(2), Max: fptr(2)},
		{Name: "markdown", Type: schema.Text, Min: fptr(2), Max: fptr(2)},
		{Name: "code", Type: schema.Text, Min: fptr(2), Max: fptr(2)},
	}, Display: &entity.DisplayConfig{Fields: map[string]entity.FieldDisplay{"markdown": {Input: "markdown"}, "code": {Input: "code"}}}}
	fixture := newTestUI(t, map[string]entity.EntityConfig{"texts": config.WithTimestamps(false)}, nil, withAPI(map[string]string{"texts": "/api/texts"}))
	body := fixture.ui.Create("texts").Base("/texts").RenderCtx(fixture.userCtx("/texts/create", "", "user"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<!doctype html><body>"+string(body))
	}))
	defer server.Close()
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"title", "body", "markdown", "code"} {
		var value string
		if err := chromedp.Run(ctx,
			chromedp.SendKeys(`[name="`+name+`"]`, "🙂🙂", chromedp.ByQuery),
			chromedp.Evaluate(`document.querySelector('[name="`+name+`"]').value`, &value),
		); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if value != "🙂🙂" {
			t.Errorf("%s truncated a two-rune value to %q", name, value)
		}
	}
}
