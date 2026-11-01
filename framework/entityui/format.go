package entityui

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
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
	case float64:
		// Plain decimal, never Go's 1e+06: the locale formatter reads
		// digits only.
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case map[string]any, []any:
		// A JSON field reads back decoded. Its text is the document,
		// never Go's map[k:v], which the form's JSON check refused.
		if b, err := json.Marshal(t); err == nil {
			return string(b)
		}
		return fmt.Sprint(v)
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

// formatDate renders a date, or with withTime a timestamp, in the
// reader's locale: "Mar 5, 2026" and "Mar 5, 2026, 2:05 PM" in English,
// "5 mars 2026" in French. Every screen prints them this way: list
// cells, plain-text cells and a record's read-only fields alike.
// Drivers hand dates back as time.Time or as one of a few string
// layouts; anything else prints as is.
func formatDate(ctx context.Context, raw any, withTime bool) string {
	if t, ok := raw.(time.Time); ok && t.IsZero() {
		return ""
	}
	if t, ok := parseTime(raw); ok {
		if withTime {
			return i18n.DateTime(ctx, t, i18n.DateMedium, i18n.TimeShort)
		}
		return i18n.Date(ctx, t, i18n.DateMedium)
	}
	return cell(raw)
}

// parseTime reads a date or timestamp in any of the shapes drivers hand
// back: a time.Time or one of a few string layouts.
func parseTime(raw any) (time.Time, bool) {
	if t, ok := raw.(time.Time); ok {
		return t, !t.IsZero()
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
			return parsed, true
		}
	}
	return time.Time{}, false
}

// twoPlaces writes a plain decimal string with two places in the
// reader's locale, exactly: a NUMERIC column's digits never pass
// through a float64.
func twoPlaces(ctx context.Context, val string) (string, bool) {
	return i18n.Decimal(ctx, val, i18n.MinFraction(2), i18n.MaxFraction(2))
}

// decimal prints a Decimal or Float value in the reader's locale, with
// at least two places and at most the column's scale (fractionDigits),
// so 0.0049 in a DECIMAL(19,4) column does not read 0.00. Text that is
// not a number prints as stored.
func decimal(ctx context.Context, f schema.Field, val string) string {
	scale := fractionDigits(f)
	if s, ok := i18n.Decimal(ctx, val, i18n.MinFraction(min(2, scale)), i18n.MaxFraction(scale)); ok {
		return s
	}
	return val
}

// defaultScale is the fraction digits of the column AutoMigrate creates
// for a Decimal, DECIMAL(19,4); a Float shows as many.
const defaultScale = 4

// fractionDigits is a numeric column's scale: the s of a RawType
// NUMERIC(p,s) or DECIMAL(p,s), else defaultScale.
func fractionDigits(f schema.Field) int {
	t := strings.ToLower(strings.ReplaceAll(f.RawType, " ", ""))
	for _, name := range []string{"numeric(", "decimal("} {
		args, ok := strings.CutPrefix(t, name)
		if !ok {
			continue
		}
		args, ok = strings.CutSuffix(args, ")")
		if !ok {
			break
		}
		if _, sc, ok := strings.Cut(args, ","); ok {
			if n, err := strconv.Atoi(sc); err == nil && n >= 0 && n <= 15 {
				return n
			}
		}
		break
	}
	return defaultScale
}

// integer writes an integer column's value in the reader's digits with
// no grouping: years, ids and codes live in int columns, and 2026 must
// not read as 2,026. A value that is no integer prints as stored.
func integer(ctx context.Context, val string) string {
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return val
	}
	return i18n.Number(ctx, n, i18n.NoGrouping())
}

// enumVariant picks a badge colour for common status words: settled is
// success, in flight is information, needing attention is a warning, a
// failure is danger, and a closed or unstarted record is neutral.
// Anything else is neutral information.
func enumVariant(v string) ui.StatusVariant {
	switch strings.ToLower(v) {
	case "active", "paid", "succeeded", "completed", "done", "published", "approved":
		return ui.StatusSuccess
	case "open", "trialing", "running", "review":
		return ui.StatusInfo
	case "past_due", "pending", "refunded":
		return ui.StatusWarning
	case "failed", "rejected":
		return ui.StatusDanger
	case "canceled", "cancelled", "void", "draft", "inactive", "archived":
		return ui.StatusNeutral
	}
	return ui.StatusInfo
}

func muted() render.HTML { return ui.EmptyValue() }

// money prints an amount as the catalog's currency symbol and two
// places in the reader's locale: $1,234.00, and -$5.00 for a negative
// one, the symbol inside the locale's minus sign. Text that is not a
// number prints as stored.
func money(ctx context.Context, val string) string {
	sym := i18nui.EntityCurrency.Text(ctx)
	mag, neg := strings.CutPrefix(val, "-")
	abs, ok := twoPlaces(ctx, mag)
	if !ok || strings.HasPrefix(mag, "-") || strings.HasPrefix(mag, "+") {
		return val
	}
	if !neg {
		return sym + abs
	}
	// The locale's minus is whatever the signed form adds in front.
	signed, _ := twoPlaces(ctx, val)
	return strings.TrimSuffix(signed, abs) + sym + abs
}
