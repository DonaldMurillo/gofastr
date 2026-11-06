package ui_test

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/chromedp/chromedp"
)

func TestDiffLabelsFollowReader(t *testing.T) {
	catalog := i18n.NewMapCatalog()
	catalog.Set("fr", "ui.diff.old", i18n.Message{Text: "Ancien"})
	catalog.Set("fr", "ui.diff.new", i18n.Message{Text: "Nouveau"})
	french := i18n.WithTranslator(i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"}), i18n.NewTranslator(catalog, "en"))
	for _, test := range []struct {
		name  string
		ctx   context.Context
		left  string
		right string
		want  []string
	}{
		{"default", nil, "", "", []string{"Old", "New"}},
		{"french", french, "", "", []string{"Ancien", "Nouveau"}},
		{"overrides", french, "Avant <", "Après >", []string{"Avant <", "Après >"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := ui.DiffViewerConfig{Patch: "-before\n+after\n", Mode: ui.DiffSplit, LeftLabel: test.left, RightLabel: test.right}
			if test.ctx != nil {
				field := reflect.ValueOf(&config).Elem().FieldByName("Ctx")
				if !field.IsValid() {
					t.Fatal("DiffViewer cannot receive the reader's locale")
				}
				field.Set(reflect.ValueOf(test.ctx))
			}
			server := themeTestPageWithHead(t, "", string(ui.DiffViewer(config)))
			ctx := moduleTestCtxURL(t, server.URL)
			var labels []string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`[...document.querySelectorAll('.fui-diff-viewer__header-cell')].map(node => node.textContent)`, &labels)); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(labels, test.want) {
				t.Fatalf("labels = %q, want %q", labels, test.want)
			}
		})
	}
}
