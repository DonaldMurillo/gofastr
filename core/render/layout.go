package render

import (
	"fmt"
	"sync"
)

// Layout is a function that wraps page content with a common chrome
// (head, navigation, footer, etc.). The title parameter allows the
// page to specify its own <title>.
type Layout func(title string, content HTML) HTML

var (
	layoutMu       sync.RWMutex
	layoutRegistry = make(map[string]Layout)
)

// RegisterLayout registers a Layout under the given name for later
// retrieval via [RenderWithLayout].
func RegisterLayout(name string, layout Layout) {
	if layout == nil {
		panic("render: RegisterLayout called with nil layout")
	}
	layoutMu.Lock()
	layoutRegistry[name] = layout
	layoutMu.Unlock()
}

// RenderWithLayout looks up a registered Layout by name and applies it
// to the given title and content. Panics if no layout is registered
// under the given name.
func RenderWithLayout(layoutName, title string, content HTML) HTML {
	layoutMu.RLock()
	layout, ok := layoutRegistry[layoutName]
	layoutMu.RUnlock()
	if !ok {
		panic(fmt.Sprintf("render: unknown layout %q", layoutName))
	}
	return layout(title, content)
}
