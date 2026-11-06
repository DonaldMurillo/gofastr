package ui_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/chromedp/chromedp"
)

func rpcPathControls(path string) map[string]func() render.HTML {
	return map[string]func() render.HTML{
		"clear": func() render.HTML {
			return ui.FilterChipBar(ui.FilterChipBarConfig{ClearAllPath: path, Filters: []ui.FilterChip{{Label: "Open", DismissPath: "/dismiss"}}})
		},
		"segment": func() render.HTML {
			return ui.SegmentedControl(ui.SegmentedControlConfig{Name: "view", RPCPath: path, Options: []ui.SegmentedOption{{Label: "Table", Value: "table"}, {Label: "Cards", Value: "cards"}}})
		},
	}
}

func TestRPCControlsRefuseUnsafePaths(t *testing.T) {
	for _, path := range []string{"javascript:alert(1)", "//evil.example/x", "/\\evil.example/x", "https://evil.example/x", "relative", "/path\nnext", "/path%0Dnext"} {
		for name, draw := range rpcPathControls(path) {
			t.Run(name+path, func(t *testing.T) {
				defer func() {
					if reason := recover(); reason == nil || !strings.Contains(fmt.Sprint(reason), "same-origin") {
						t.Fatalf("unsafe path %q must be refused at render, got %v", path, reason)
					}
				}()
				draw()
			})
		}
	}
	for _, draw := range rpcPathControls("/filters?state=open") {
		if markup := string(draw()); !strings.Contains(markup, `data-cui-rpc="/filters?state=open"`) {
			t.Fatal("valid same-origin path lost its RPC")
		}
	}
}

func TestRPCControlsKeepNativePaths(t *testing.T) {
	body := rpcPathControls("/filters?state=open")["clear"]()
	server := themeTestPageWithHead(t, "", string(body))
	ctx := moduleTestCtxURL(t, server.URL)
	var href, endpoint string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('.fui-filter-bar__clear').getAttribute('href')`, &href),
		chromedp.Evaluate(`document.querySelector('.fui-filter-bar__clear').getAttribute('data-cui-rpc')`, &endpoint),
	); err != nil {
		t.Fatal(err)
	}
	if href != "/filters?state=open" || endpoint != href {
		t.Fatalf("native and RPC destinations diverge: %q, %q", href, endpoint)
	}
}
