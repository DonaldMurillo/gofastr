package local

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/check"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
)

// The registration is live: the host serves the module, the loader
// knows its requirement, and the marker is the one this package renders.
func TestBehaviorIsRegisteredAndServed(t *testing.T) {
	e, ok := registry.LookupBehavior(BehaviorName)
	if !ok {
		t.Fatalf("%s is not registered", BehaviorName)
	}
	if len(e.Markers) != 1 || e.Markers[0] != "[data-local-store]" {
		t.Fatalf("markers = %v", e.Markers)
	}
	if len(e.Requires) != 1 || e.Requires[0] != "local" {
		t.Fatalf("requires = %v, want the local primitive", e.Requires)
	}
	for _, name := range []string{BehaviorName, BridgeName, MigrateName} {
		if _, ok := runtime.Module(name); !ok {
			t.Fatalf("runtime.Module(%q) does not serve the registered source", name)
		}
	}
	// The bridge evaluates after the store it reaches into, on the seed
	// marker; an RPC trigger names it in data-fui-rpc-with instead.
	b, ok := registry.LookupBehavior(BridgeName)
	if !ok || len(b.Markers) != 1 || b.Markers[0] != "[data-local-seed]" || len(b.Requires) != 1 || b.Requires[0] != BehaviorName {
		t.Fatalf("%s = %+v, want the seed marker and Requires(%q)", BridgeName, b, BehaviorName)
	}
	// The migration engine is never on the critical path: it loads at
	// idle, and local-store forces it by name the moment a rewrite is
	// due.
	m, ok := registry.LookupBehavior(MigrateName)
	if !ok || !m.Idle || len(m.Requires) != 1 || m.Requires[0] != BehaviorName {
		t.Fatalf("%s = %+v, want LoadIdle and Requires(%q)", MigrateName, m, BehaviorName)
	}
}

// Every registered module of this package is held to the JavaScript
// lints every embedded module is held to, and those lints still refuse
// a raw storage key and an unencoded cookie key. The clean half runs
// over this package's own source; the refusing half runs over mutated
// copies, so each guard is watched failing, not reasoned about.
func TestStorageKeyLintReachesTheModulesAndStillRefusesARawKey(t *testing.T) {
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for name, lint := range map[string]func(...string) (*check.Result, error){
		"storage-key-raw":   check.LintStorageKeyRaw,
		"cookie-concat":     check.LintCookieConcat,
		"proto-key-write":   check.LintProtoKeyWrite,
		"registry-own-prop": check.LintRegistryOwnProps,
		"decode-uri-raw":    check.LintDecodeURIRaw,
	} {
		res, err := lint(here)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.HasErrors() {
			t.Errorf("%s over this package's modules:\n%s", name, res.Error())
		}
	}

	// The storage-key lint has three arms and they say different things.
	// Asserting on the word "raw", or on the "[storage-key-raw]" prefix
	// every one of them carries, asserts nothing: the message always
	// contains both, so the mutation could stop failing and the test
	// could not tell. Each arm is asserted on ITS OWN wording, and the
	// wording is pinned against the lint's source so a rewrite there
	// breaks this test rather than silently hollowing it out.
	for _, tc := range []struct {
		name    string
		mutate  func(string) string
		wantMsg string
	}{
		{
			// A key from a marker attribute reaches localStorage raw.
			name:    "a raw attribute-borne key",
			wantMsg: "uses \"el.getAttribute('data-fui-signal')\" raw",
			mutate: func(src string) string {
				return strings.Replace(src, "const wire = (el) => {",
					"const wire = (el) => {\n    localStorage.setItem(el.getAttribute('data-fui-signal'), '1');", 1)
			},
		},
		{
			// The namespace is an application-chosen prefix, not the
			// framework's: an attribute value still names every key
			// under it.
			name:    "a foreign namespace",
			wantMsg: "behind a namespace that is not the framework's",
			mutate: func(src string) string {
				return strings.Replace(src, "const wire = (el) => {",
					"const wire = (el) => {\n    localStorage.setItem('local.' + encodeURIComponent(el.getAttribute('data-local-seed')), '1');", 1)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			mutated := tc.mutate(localStoreJS)
			if mutated == localStoreJS {
				t.Fatal("the mutation did not apply: the anchor moved")
			}
			if err := os.WriteFile(filepath.Join(dir, "local-store.js"), []byte(mutated), 0o600); err != nil {
				t.Fatal(err)
			}
			res, err := check.LintStorageKeyRaw(dir)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(res.Error(), tc.wantMsg) {
				t.Fatalf("the storage-key lint did not raise the %s arm (%q):\n%s", tc.name, tc.wantMsg, res.Error())
			}
		})
	}

	// And the cookie lint, over the module that writes the cookies.
	dir := t.TempDir()
	mutated := strings.Replace(localBridgeJS,
		"document.cookie = 'gofastr.local.' + encodeURIComponent(app + '.' + coll + '.' + key) + '=' + encodeURIComponent(text) + '; path=/; max-age=31536000; SameSite=Lax; Secure';",
		"document.cookie = 'gofastr.local.' + key + '=' + encodeURIComponent(text) + '; path=/; max-age=31536000; SameSite=Lax; Secure';",
		1)
	if mutated == localBridgeJS {
		t.Fatal("the cookie mutation did not apply: the anchor moved")
	}
	if err := os.WriteFile(filepath.Join(dir, "local-bridge.js"), []byte(mutated), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := check.LintCookieConcat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Error(), "document.cookie concatenates \"key\" raw") {
		t.Fatalf("the cookie lint let an unencoded mirror key through:\n%s", res.Error())
	}
}

// The wordings the assertions above depend on are the lint's own. If a
// rewrite in core-ui/check changes them, this fails here rather than
// leaving the mutation assertions unable to fail.
func TestTheLintWordingsThisPackageAssertsStillExist(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "core-ui", "check", "runtimeshapes.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"uses %q raw",
		"behind a namespace that is not the framework's",
		"document.cookie concatenates %q raw",
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("core-ui/check no longer emits %q — the mutation assertions in this file are asserting a message that does not exist", want)
		}
	}
}
