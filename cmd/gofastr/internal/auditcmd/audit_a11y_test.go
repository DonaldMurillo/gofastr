package auditcmd

import (
	"testing"
)

func TestParseA11yArgsSpaceAndEqualsForms(t *testing.T) {
	for _, args := range [][]string{
		{"--url", "http://localhost:8080", "--pages", "/a,/b"},
		{"--url=http://localhost:8080", "--pages=/a,/b"},
	} {
		opts := parseA11yArgs(args)
		if opts.badFlag != "" || opts.help {
			t.Fatalf("args %v: unexpected badFlag=%q help=%v", args, opts.badFlag, opts.help)
		}
		if opts.root != "." || opts.baseURL != "http://localhost:8080" {
			t.Errorf("args %v: root=%q base=%q", args, opts.root, opts.baseURL)
		}
		if len(opts.pages) != 2 || opts.pages[0] != "/a" || opts.pages[1] != "/b" {
			t.Errorf("args %v: pages=%v", args, opts.pages)
		}
	}
}

func TestParseA11yArgsRootAndBadFlag(t *testing.T) {
	opts := parseA11yArgs([]string{"./app", "--nope"})
	if opts.root != "./app" {
		t.Errorf("root=%q", opts.root)
	}
	if opts.badFlag != "--nope" {
		t.Errorf("badFlag=%q", opts.badFlag)
	}
}

func TestParseA11yCredentials(t *testing.T) {
	opts := parseA11yArgs([]string{
		"--url", "http://localhost:8080",
		"--email", "admin@example.com",
		"--password", "secret",
	})
	if opts.badFlag != "" {
		t.Fatalf("credentials rejected as %q", opts.badFlag)
	}
	if opts.email != "admin@example.com" || opts.password != "secret" {
		t.Fatalf("credentials not parsed: %+v", opts)
	}
}
