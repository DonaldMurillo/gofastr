package pluginhost

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var pluginAttrPattern = regexp.MustCompile(`data-fui-plugin[a-z-]*`)

// tokensIn returns the sorted unique data-fui-plugin* tokens in a file's text.
func tokensIn(t *testing.T, path string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	set := map[string]bool{}
	for _, m := range pluginAttrPattern.FindAllString(string(raw), -1) {
		m = strings.TrimRight(m, "-") // drop a token that wrapped mid-name in a comment
		set[m] = true
	}
	return set
}

// Hard Rule 5: every data-fui-plugin* attribute the mount marker EMITS and the
// broker JS READS must be documented in the core-ui/ARCHITECTURE.md attribute
// table. This is the automated half the rule requires (the doc rows are the
// other half) — mirroring core-ui/runtime's attrdoc test for this surface.
func TestPluginAttrsAreDocumented(t *testing.T) {
	const archPath = "../../core-ui/ARCHITECTURE.md"
	documented := tokensIn(t, archPath)

	used := map[string]bool{}
	for tok := range tokensIn(t, "mount.go") {
		used[tok] = true
	}
	for tok := range tokensIn(t, "host/pluginhost.js") {
		used[tok] = true
	}

	var missing []string
	for tok := range used {
		if !documented[tok] {
			missing = append(missing, tok)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("data-fui-plugin* attributes used in code but NOT documented in %s: %v", archPath, missing)
	}
}
