package scan

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

func TestMissingGoDirectiveUsesDefault(t *testing.T) {
	for _, threshold := range []string{"1.27", "1.27.3", "1.16"} {
		root := newWorkspace(t, nil, map[string]string{
			"go.mod":  "module example.com/app\n",
			"main.go": "package main\nfunc main() {}\n",
		})
		note := gomodNote(threshold)
		result := mustRun(t, root, note)
		if threshold == "1.16" {
			wantHits(t, result, note)
		} else {
			wantHits(t, result, note, "go.mod:1:0 gomod go 1.16 < "+threshold)
		}
	}
}

func TestNestedGoDirectiveUsesDefault(t *testing.T) {
	root := newWorkspace(t, nil, map[string]string{
		"main.go":        "package main\nfunc main() {}\n",
		"nested/go.mod":  "module example.com/nested\n",
		"nested/main.go": "package main\nfunc main() {}\n",
	})
	note := gomodNote("1.27")
	wantHits(t, mustRun(t, root, note), note, "nested/go.mod:1:0 gomod go 1.16 < 1.27")
}

func TestInvalidGoModIsUnscanned(t *testing.T) {
	root := newWorkspace(t, nil, map[string]string{
		"go.mod":  "module example.com/app\ninvalid directive\n",
		"main.go": "package main\nfunc main() {}\n",
	})
	matcher := &engine{root: root, gomodNotes: []*upgrade.Note{gomodNote("1.27")}}
	matcher.gomodMatchers(root)
	for _, entry := range matcher.unscanned {
		if strings.HasPrefix(entry, "go.mod (did not parse: ") {
			return
		}
	}
	t.Fatalf("invalid go.mod missing from Unscanned: %v", matcher.unscanned)
}
