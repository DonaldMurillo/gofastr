package entityui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestNullBoolListIsUnset(t *testing.T) {
	config := entity.EntityConfig{
		Fields:   []schema.Field{{Name: "name", Type: schema.String}, {Name: "active", Type: schema.Bool}},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false)
	fixture := newTestUI(t, map[string]entity.EntityConfig{"flags": config}, map[string][]map[string]any{"flags": {
		{"id": "unset", "name": "Unset"},
		{"id": "off", "name": "Disabled", "active": false},
		{"id": "on", "name": "Enabled", "active": true},
	}})
	body := listHTML(t, fixture.ui.List("flags").Base("/flags").Columns("name", "active"), fixture.ctx("/flags", ""))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(writer, "<!doctype html><body>"+body)
	}))
	t.Cleanup(server.Close)
	var labels []string
	if err := chromedp.Run(chromedptest.Context(t), chromedp.Navigate(server.URL), chromedp.Evaluate(`['unset', 'off', 'on'].map(id => document.getElementById(id).querySelectorAll('td')[1].textContent.trim())`, &labels)); err != nil {
		t.Fatal(err)
	}
	if want := []string{"—", "No", "Yes"}; !reflect.DeepEqual(labels, want) {
		t.Fatalf("boolean cells = %q, want %q", labels, want)
	}
}
