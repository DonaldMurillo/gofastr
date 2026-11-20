package render

import (
	"fmt"
	"sync"
)

// Layout wraps page content, under the page's title, in shared chrome.
type Layout func(title string, content HTML) HTML

var (
	layoutMu       sync.RWMutex
	layoutRegistry = make(map[string]Layout)
)

// RegisterLayout stores layout under name for [RenderWithLayout]; it panics on a nil layout.
func RegisterLayout(name string, layout Layout) {
	if layout == nil {
		panic("render: RegisterLayout called with nil layout")
	}
	layoutMu.Lock()
	layoutRegistry[name] = layout
	layoutMu.Unlock()
}

// RenderWithLayout applies the layout registered under layoutName, panicking when there is none.
func RenderWithLayout(layoutName, title string, content HTML) HTML {
	layoutMu.RLock()
	layout, ok := layoutRegistry[layoutName]
	layoutMu.RUnlock()
	if !ok {
		panic(fmt.Sprintf("render: unknown layout %q", layoutName))
	}
	return layout(title, content)
}
