package entityui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// param namespaces a list's query param by its key: "sort" for an unkeyed
// list, "due_sort" for .Key("due").
func param(key, name string) string {
	if key == "" {
		return name
	}
	return key + "_" + name
}

// cell is a value's text form.
func cell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case time.Time:
		if t.IsZero() {
			return ""
		}
		return t.Format(time.RFC3339)
	default:
		return fmt.Sprint(v)
	}
}

func truthy(s string) bool {
	switch strings.ToLower(s) {
	case "true", "1", "yes", "on", "t":
		return true
	}
	return false
}

// dateLayout and timestampLayout are how every screen prints a Date and
// a Timestamp: list cells, plain-text cells and a record's read-only
// fields alike.
const (
	dateLayout      = "Jan 2, 2006"
	timestampLayout = "Jan 2, 2006 3:04 PM"
)

// formatDate renders a date or timestamp. Drivers hand dates back as
// time.Time or as one of a few string layouts; anything else prints as is.
func formatDate(raw any, layout string) string {
	if t, ok := raw.(time.Time); ok {
		if t.IsZero() {
			return ""
		}
		return t.Format(layout)
	}
	val := cell(raw)
	for _, l := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05",
		time.DateOnly,
	} {
		if parsed, err := time.Parse(l, val); err == nil {
			return parsed.Format(layout)
		}
	}
	return val
}

// formatNumber prints a float without trailing zeros on whole values and
// with thousands grouping.
func formatNumber(f float64, decimals int) string {
	return groupDigits(strconv.FormatFloat(f, 'f', decimals, 64))
}

// groupDigits puts thousands separators into a plain decimal string
// ("-1234567.50" reads "-1,234,567.50").
func groupDigits(s string) string {
	s, neg := strings.CutPrefix(s, "-")
	whole, frac, _ := strings.Cut(s, ".")
	var grp []string
	for len(whole) > 3 {
		grp = append([]string{whole[len(whole)-3:]}, grp...)
		whole = whole[:len(whole)-3]
	}
	out := strings.Join(append([]string{whole}, grp...), ",")
	if frac != "" {
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}

// decimal prints a Decimal or Float value with two places and grouping.
// A currency is the app's kind to draw (Extensions.Kinds), not a guess.
func decimal(val string) string {
	f, err := strconv.ParseFloat(val, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return val
	}
	return formatNumber(f, 2)
}

// enumVariant picks a badge colour for common status words; anything else
// is neutral information.
func enumVariant(v string) ui.StatusVariant {
	switch strings.ToLower(v) {
	case "active", "paid", "succeeded", "completed", "done", "published", "approved":
		return ui.StatusSuccess
	case "open", "past_due", "pending", "trialing", "draft", "review":
		return ui.StatusWarning
	case "canceled", "cancelled", "void", "failed", "refunded", "inactive", "archived", "rejected":
		return ui.StatusNeutral
	}
	return ui.StatusInfo
}

func muted() render.HTML { return ui.EmptyValue() }
