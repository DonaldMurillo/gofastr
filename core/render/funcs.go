package render

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// FuncMap is the default set of template functions available for use
// in rendering pipelines. Call [RegisterFunc] to add custom functions.
var FuncMap = map[string]any{
	"ToUpper":     strings.ToUpper,
	"ToLower":     strings.ToLower,
	"Trim":        strings.TrimSpace,
	"Title":       strings.Title,
	"DateFormat":  func(t time.Time, layout string) string { return t.Format(layout) },
	"NumberFormat": func(n float64, prec int) string { return strconv.FormatFloat(n, 'f', prec, 64) },
	"Truncate":     func(s string, maxRunes int) string {
		if utf8.RuneCountInString(s) <= maxRunes {
			return s
		}
		runes := []rune(s)
		return string(runes[:maxRunes]) + "…"
	},
}

var funcMu sync.RWMutex

// RegisterFunc adds a named function to the global FuncMap.
// It panics if fn is nil or a function is already registered under name.
func RegisterFunc(name string, fn any) {
	if fn == nil {
		panic(fmt.Sprintf("render: RegisterFunc(%q, nil)", name))
	}
	funcMu.Lock()
	FuncMap[name] = fn
	funcMu.Unlock()
}
