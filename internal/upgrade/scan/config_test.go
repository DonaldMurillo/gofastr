package scan

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

func TestConfigNestedKey(t *testing.T) {
	yml := `app:
  name: demo
entities:
  tasks:
    api_prefix: /api/v1/
`
	n := &upgrade.Note{Find: upgrade.Find{Config: []upgrade.ConfigMatch{{Key: "entities.tasks.api_prefix"}}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go":     "package main\n\nfunc main() {}\n",
		"gofastr.yml": yml,
	}), n)
	wantHits(t, res, n,
		fmt.Sprintf("gofastr.yml:%d:0 config entities.tasks.api_prefix", lineOf(yml, "api_prefix")))
}

func TestConfigStarOverList(t *testing.T) {
	yml := `entities:
  - name: tasks
    api_prefix: /api/v1/
  - name: notes
    api_prefix: /api/v2/
    other: x
`
	n := &upgrade.Note{Find: upgrade.Find{Config: []upgrade.ConfigMatch{{Key: "entities.*.api_prefix"}}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go":     "package main\n\nfunc main() {}\n",
		"gofastr.yml": yml,
	}), n)
	wantHits(t, res, n,
		fmt.Sprintf("gofastr.yml:%d:0 config entities.0.api_prefix", lineOf(yml, "api_prefix: /api/v1/")),
		fmt.Sprintf("gofastr.yml:%d:0 config entities.1.api_prefix", lineOf(yml, "api_prefix: /api/v2/")))
}

func TestConfigValueRegex(t *testing.T) {
	yml := `auth: "yes"
theme: system
`
	n := &upgrade.Note{Find: upgrade.Find{Config: []upgrade.ConfigMatch{
		{Key: "auth", Value: regexp.MustCompile(`^(yes|no|on|off)$`)},
	}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go":     "package main\n\nfunc main() {}\n",
		"gofastr.yml": yml,
	}), n)
	// theme is not the matched key; auth's scalar matches.
	wantHits(t, res, n,
		fmt.Sprintf("gofastr.yml:%d:0 config auth", lineOf(yml, `auth: "yes"`)))
}

func TestConfigMissingFileNotError(t *testing.T) {
	n := &upgrade.Note{Find: upgrade.Find{Config: []upgrade.ConfigMatch{{Key: "entities"}}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	}), n)
	wantHits(t, res, n)
}
