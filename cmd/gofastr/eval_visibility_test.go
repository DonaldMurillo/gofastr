package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// evalScenario groups a "scaffold then query" integration test.
// Each scenario creates a temp project via `gofastr init`, then
// runs `gofastr docs` commands that an AI agent would run when
// asked about planning a site.
type evalScenario struct {
	name string
	// initArgs are passed to `gofastr init` (name is always "evalapp").
	initArgs []string
	// queries run after scaffolding. Each query is a (subcommand, args)
	// pair against the gofastr binary.
	queries []evalQuery
	// assertions check the combined output of all queries.
	assertions []evalAssertion
}

type evalQuery struct {
	desc string
	args []string // e.g. []string{"docs", "--grep", "auth"}
}

type evalAssertion struct {
	desc    string
	check   func(t *testing.T, outputs []string)
}

// TestEvalNewUserPlansSite simulates a fresh user who:
//   1. Runs `gofastr init my-blog`
//   2. Asks "I want to build a blog with auth, what features does the framework have?"
//
// The eval verifies the docs pipeline surfaces the right answers.
func TestEvalNewUserPlansSite(t *testing.T) {
	bin := buildGofastrBin(t)
	work := t.TempDir()

	// Step 1: scaffold
	initCmd := exec.Command(bin, "init", "my-blog", "--module=example.com/myblog")
	initCmd.Dir = work
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("gofastr init: %v\n%s", err, out)
	}
	project := filepath.Join(work, "my-blog")

	// Step 2: verify onboarding files exist (what an agent sees first)
	t.Run("onboarding_files_present", func(t *testing.T) {
		for _, f := range []string{
			"CLAUDE.md",
			"AGENTS.md",
			".claude/skills/gofastr-host/SKILL.md",
			".gitignore",
			".git/HEAD", // git init ran
		} {
			p := filepath.Join(project, f)
			if _, err := os.Stat(p); err != nil {
				t.Errorf("missing %q: %v", f, err)
			}
		}
	})

	// Step 3: verify CLAUDE.md surfaces docs (agent reads this first)
	t.Run("claude_md_points_to_docs", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(project, "CLAUDE.md"))
		if err != nil {
			t.Fatalf("read CLAUDE.md: %v", err)
		}
		body := string(data)
		for _, want := range []string{
			"gofastr docs",
			"gofastr docs --grep",
			"AGENTS.md",
			"gofastr-host",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("CLAUDE.md missing %q", want)
			}
		}
	})

	// Step 4: simulate "I need auth for my blog" → search docs
	t.Run("docs_search_auth", func(t *testing.T) {
		cmd := exec.Command(bin, "docs", "--grep", "auth")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("gofastr docs --grep auth: %v\n%s", err, out)
		}
		body := string(out)
		// Must surface auth-related topics
		for _, want := range []string{"auth", "session"} {
			if !strings.Contains(strings.ToLower(body), strings.ToLower(want)) {
				t.Errorf("docs grep 'auth' missing %q in output:\n%s", want, body)
			}
		}
	})

	// Step 5: simulate "show me the auth docs" → read a topic
	t.Run("docs_read_auth", func(t *testing.T) {
		cmd := exec.Command(bin, "docs", "auth")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("gofastr docs auth: %v\n%s", err, out)
		}
		body := string(out)
		for _, want := range []string{
			"battery/auth",
			"EntityUserStore",
			"SessionMiddleware",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("auth doc missing %q", want)
			}
		}
	})

	// Step 6: simulate "I need an admin panel" → search + read
	t.Run("docs_search_admin", func(t *testing.T) {
		cmd := exec.Command(bin, "docs", "admin")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("gofastr docs admin: %v\n%s", err, out)
		}
		body := string(out)
		if !strings.Contains(body, "battery/admin") {
			t.Errorf("admin doc missing battery/admin reference:\n%s", body)
		}
	})

	// Step 7: simulate "how do I style my pages?" → UI docs
	t.Run("docs_search_ui", func(t *testing.T) {
		cmd := exec.Command(bin, "docs", "--grep", "StyleSheet theme")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("gofastr docs --grep StyleSheet theme: %v\n%s", err, out)
		}
		body := string(out)
		// Should surface UI-related docs
		found := false
		for _, topic := range []string{"ui-getting-started", "ui-new-components", "theme"} {
			if strings.Contains(body, topic) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("docs grep 'StyleSheet theme' found no UI topics:\n%s", body)
		}
	})

	// Step 8: verify AGENTS.md links to battery details
	t.Run("agents_md_has_battery_details", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(project, "AGENTS.md"))
		if err != nil {
			t.Fatalf("read AGENTS.md: %v", err)
		}
		body := string(data)
		// Must reference at least auth (critical for planning)
		if !strings.Contains(strings.ToLower(body), "auth") {
			t.Error("AGENTS.md doesn't reference auth battery")
		}
	})
}

// TestEvalNewUserPlansSiteNoEntity verifies the same docs pipeline
// works when the user passes --no-entity (UI-only project).
func TestEvalNewUserPlansSiteNoEntity(t *testing.T) {
	bin := buildGofastrBin(t)
	work := t.TempDir()

	initCmd := exec.Command(bin, "init", "portfolio", "--module=example.com/portfolio", "--no-entity")
	initCmd.Dir = work
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("gofastr init: %v\n%s", err, out)
	}
	project := filepath.Join(work, "portfolio")

	t.Run("onboarding_files_present", func(t *testing.T) {
		for _, f := range []string{
			"CLAUDE.md",
			"AGENTS.md",
			".claude/skills/gofastr-host/SKILL.md",
			".git/HEAD",
		} {
			if _, err := os.Stat(filepath.Join(project, f)); err != nil {
				t.Errorf("missing %q: %v", f, err)
			}
		}
	})

	t.Run("no_entities_dir", func(t *testing.T) {
		if _, err := os.Stat(filepath.Join(project, "entities")); err == nil {
			t.Error("entities/ should not exist with --no-entity")
		}
	})

	t.Run("docs_still_works", func(t *testing.T) {
		cmd := exec.Command(bin, "docs", "--list")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("gofastr docs --list: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "ui-getting-started") {
			t.Error("docs list missing ui-getting-started topic")
		}
	})
}
