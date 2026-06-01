package store

import (
	"context"
	"html"
	"strings"
	"testing"
)

// extractText returns the text content of a single-element Bind output:
// everything between the first '>' and the last '<'.
func extractText(s string) string {
	i := strings.IndexByte(s, '>')
	j := strings.LastIndexByte(s, '<')
	if i < 0 || j < 0 || j <= i {
		return ""
	}
	return s[i+1 : j]
}

// TestInvariant_SeedEqualsStampedValue is the single-source-of-truth
// guarantee: the value Bind stamps into the SSR DOM (after HTML-unescape,
// i.e. what the browser sees) is identical to the value carried in the
// seed (what JSON.parse yields). They can never drift — this is the
// structural fix for the Counter/Toggle "hardcoded 0 vs empty store" bug.
func TestInvariant_SeedEqualsStampedValue(t *testing.T) {
	cases := []struct {
		name string
		bind func(ctx context.Context) (signalName string, bound string)
	}{
		{"string", func(ctx context.Context) (string, string) {
			s := New("t").String("s", "Acme & Co <ok>")
			return s.Name(), string(s.Bind(ctx, "span", nil))
		}},
		{"int", func(ctx context.Context) (string, string) {
			s := New("t").Int("i", 42)
			return s.Name(), string(s.Bind(ctx, "span", nil))
		}},
		{"bool", func(ctx context.Context) (string, string) {
			s := New("t").Bool("b", true)
			return s.Name(), string(s.Bind(ctx, "span", nil))
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resetForTest()
			ctx := context.Background()
			signalName, bound := c.bind(ctx)
			seed := ResolveSeed(ctx, []string{signalName})
			seedStr := valueString(seed[signalName])
			domVal := html.UnescapeString(extractText(bound))
			if domVal != seedStr {
				t.Fatalf("DRIFT: DOM stamps %q but seed carries %q", domVal, seedStr)
			}
		})
	}
}

// TestInvariant_SeededRequestValueMatches confirms the invariant also
// holds when a producer overrides the default at request time: Bind and
// the seed both reflect the request value.
func TestInvariant_SeededRequestValueMatches(t *testing.T) {
	resetForTest()
	s := New("org").String("companyName", "DefaultCo")
	ctx := WithValues(context.Background())
	s.Seed(ctx, "Tenant <X>")

	bound := html.UnescapeString(extractText(string(s.Bind(ctx, "span", nil))))
	seed := valueString(ResolveSeed(ctx, []string{s.Name()})[s.Name()])
	if bound != seed || bound != "Tenant <X>" {
		t.Fatalf("request-value invariant broken: dom=%q seed=%q", bound, seed)
	}
}
