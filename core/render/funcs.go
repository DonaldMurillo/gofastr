package render

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// FuncMap is the set of template functions; add to it with [RegisterFunc].
var FuncMap = map[string]any{
	"ToUpper":      strings.ToUpper,
	"ToLower":      strings.ToLower,
	"Trim":         strings.TrimSpace,
	"Title":        strings.Title,
	"DateFormat":   func(t time.Time, layout string) string { return t.Format(layout) },
	"NumberFormat": func(n float64, prec int) string { return strconv.FormatFloat(n, 'f', prec, 64) },
	"Truncate": func(s string, maxRunes int) string {
		if maxRunes < 0 {
			return ""
		}
		if utf8.RuneCountInString(s) <= maxRunes {
			return s
		}
		runes := []rune(s)
		return string(runes[:maxRunes]) + "…"
	},
}

var funcMu sync.RWMutex

// RegisterFunc sets FuncMap[name] to fn, replacing any earlier one; it panics on a nil fn.
func RegisterFunc(name string, fn any) {
	if fn == nil {
		panic(fmt.Sprintf("render: RegisterFunc(%q, nil)", name))
	}
	funcMu.Lock()
	defer funcMu.Unlock()
	FuncMap[name] = fn
}
