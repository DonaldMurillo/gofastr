package ui_test

import (
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/chromedp/chromedp"
)

func timelineTime(t *testing.T, display, machine string) ui.TimelineEvent {
	t.Helper()
	event := ui.TimelineEvent{Title: "Published", Meta: "by editor"}
	value := reflect.ValueOf(&event).Elem()
	for name, text := range map[string]string{"When": display, "Machine": machine} {
		field := value.FieldByName(name)
		if !field.IsValid() || field.Kind() != reflect.String {
			t.Fatalf("TimelineEvent cannot carry %s to its timestamp primitive", name)
		}
		field.SetString(text)
	}
	return event
}

func TestTimelinePreservesTimestamps(t *testing.T) {
	for _, machine := range []string{"2026-11-05T10:22:00Z", "2026-04-02T15:42:10-04:00", ""} {
		markup := ui.Timeline(ui.TimelineConfig{Events: []ui.TimelineEvent{timelineTime(t, "3 hours ago", machine)}})
		server := themeTestPageWithHead(t, "", string(markup))
		ctx := moduleTestCtxURL(t, server.URL)
		var result struct {
			Tag     string `json:"tag"`
			Text    string `json:"text"`
			Machine string `json:"machine"`
			Meta    string `json:"meta"`
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const item = document.querySelector('.fui-timeline__content'); const stamp = item.firstElementChild; return {tag:stamp.tagName, text:stamp.textContent, machine:stamp.getAttribute('datetime') || '', meta:item.querySelector('.fui-timeline__header .fui-timeline__meta').textContent}; })()`, &result)); err != nil {
			t.Fatal(err)
		}
		expectedTag := "TIME"
		if machine == "" {
			expectedTag = "SPAN"
		}
		if result.Tag != expectedTag || result.Text != "3 hours ago" || result.Machine != machine || result.Meta != "by editor" {
			t.Fatalf("timestamp or independent caption changed: %+v", result)
		}
	}
	t.Run("invalid machine", func(t *testing.T) {
		event := timelineTime(t, "Yesterday", "not-a-timestamp")
		defer func() {
			if recover() == nil {
				t.Error("invalid machine timestamp must be refused")
			}
		}()
		ui.Timeline(ui.TimelineConfig{Events: []ui.TimelineEvent{event}})
	})
}
