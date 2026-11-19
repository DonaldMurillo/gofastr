package main

import (
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The CI test job installs one pinned TypeScript and sets
// GOFASTR_REQUIRE_TSC, so TestOpenAPISDKTypesCompile type-checks the
// generated SDK declarations there instead of skipping.
func TestCITypeScriptIsPinned(t *testing.T) {
	var wf struct {
		Jobs map[string]struct {
			Env   map[string]string `yaml:"env"`
			Steps []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(workflowSource(t, "ci.yml")), &wf); err != nil {
		t.Fatalf("parse ci.yml: %v", err)
	}
	job, ok := wf.Jobs["test"]
	if !ok {
		t.Fatal("ci.yml has no test job")
	}
	if job.Env["GOFASTR_REQUIRE_TSC"] != "1" {
		t.Errorf("the test job does not set GOFASTR_REQUIRE_TSC: \"1\", so the d.ts check skips in CI")
	}
	pinned := regexp.MustCompile(`npm install -g typescript@[0-9]+\.[0-9]+\.[0-9]+(\s|$)`)
	installs := 0
	for _, s := range job.Steps {
		if strings.Contains(s.Run, "typescript") {
			installs++
			if !pinned.MatchString(s.Run) {
				t.Errorf("TypeScript install is not one exact version: %q", s.Run)
			}
		}
	}
	if installs != 1 {
		t.Errorf("the test job installs TypeScript %d times, want once", installs)
	}
}
