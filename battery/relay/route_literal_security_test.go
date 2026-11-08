package relay

import (
	"strings"
	"testing"
)

func TestRouteConfigRequiresLiteralPaths(t *testing.T) {
	for _, value := range []string{"{id}", "a{b", "a}b", "{$}", "cost$", "v/{rest...}/"} {
		for _, mount := range []bool{false, true} {
			cfg := Config{Routes: []Route{{Prefix: "event", Upstream: "https://example.com", Methods: []string{"GET"}}}}
			if mount {
				cfg.Path = "/api/" + strings.TrimSuffix(value, "/")
			} else {
				cfg.Routes[0].Prefix = value
			}
			var recovered any
			func() { defer func() { recovered = recover() }(); New(cfg) }()
			message, ok := recovered.(string)
			if !ok || !strings.Contains(message, "literal") {
				t.Errorf("mount=%v value=%q: New panic=%v, want literal-path refusal", mount, value, recovered)
			}
		}
	}
}
