package static

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestSSGParamNoTraversal asserts that StaticPaths param values containing
// path separators or ".." can never produce a build-output file outside
// OutDir. The expansion must reject the value rather than substitute it raw.
func TestSSGParamNoTraversal(t *testing.T) {
	ctx := context.Background()
	pattern := "/products/:slug"

	cases := []struct {
		name string
		slug string
	}{
		{"happy", "alpha"},                     // legitimate slug must still expand
		{"dotdot", "../../../etc/cron.d/evil"}, // classic traversal
		{"embedded_slash", "a/b/c"},            // single separator escapes the leaf dir
		{"absolute", "/etc/passwd"},            // absolute-looking value
		{"trailing_dotdot", "x/.."},            // collapses back up a level
	}

	out := t.TempDir()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			urls, err := expandRouteWith(ctx, pattern, map[string]string{"slug": tc.slug})
			if tc.name == "happy" {
				if err != nil {
					t.Fatalf("legitimate slug rejected: %v", err)
				}
				if len(urls) != 1 || urls[0] != "/products/alpha" {
					t.Fatalf("happy path: got %v", urls)
				}
				return
			}
			// Attack shapes: either the expansion errors out, or whatever URL
			// it produces must map to a file CONTAINED within OutDir.
			if err != nil {
				return // rejected — the desired fail-closed outcome
			}
			for _, u := range urls {
				dst := filepath.Join(out, pathToFile(u))
				rel, rerr := filepath.Rel(out, dst)
				if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
					t.Fatalf("slug %q escapes OutDir: dst=%q rel=%q", tc.slug, dst, rel)
				}
			}
		})
	}
}

// expandRouteWith drives expandRoute with a fixed StaticPaths map by invoking
// the param-substitution path directly through a test provider.
func expandRouteWith(ctx context.Context, pattern string, params map[string]string) ([]string, error) {
	return expandParams(ctx, pattern, []map[string]string{params})
}
