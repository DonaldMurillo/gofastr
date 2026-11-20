package main

import (
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The CI test job installs one pinned TypeScript, the version its
// GOFASTR_TSC_VERSION names, and sets GOFASTR_REQUIRE_TSC, so
// TestOpenAPISDKTypesCompile type-checks the generated SDK declarations
// there instead of skipping, against the tsc the variable records.
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
	version := job.Env["GOFASTR_TSC_VERSION"]
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(version) {
		t.Errorf("the test job's GOFASTR_TSC_VERSION is %q, want one exact version", version)
	}
	installs := 0
	for _, s := range job.Steps {
		if strings.Contains(s.Run, "typescript") {
			installs++
			if !strings.Contains(s.Run, `npm install -g "typescript@${GOFASTR_TSC_VERSION}"`) && !strings.Contains(s.Run, "npm install -g typescript@"+version+" ") {
				t.Errorf("the TypeScript install does not install GOFASTR_TSC_VERSION (%s): %q", version, s.Run)
			}
		}
	}
	if installs != 1 {
		t.Errorf("the test job installs TypeScript %d times, want once", installs)
	}
}
