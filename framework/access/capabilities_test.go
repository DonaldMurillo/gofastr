package access_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

func TestStrictErrorIsTyped(t *testing.T) {
	policy := access.NewRolePolicy().StrictCapabilities()
	policy.Register("teams:admin")

	err := policy.Grant("editor", "temas:admin")
	var unknown *access.UnknownCapabilityError
	if !errors.As(err, &unknown) {
		t.Fatalf("want *UnknownCapabilityError, got %T (%v)", err, err)
	}
	if unknown.Grant != "temas:admin" || unknown.Nearest != "teams:admin" {
		t.Errorf("Grant/Nearest = %q/%q", unknown.Grant, unknown.Nearest)
	}
}

func TestCapabilitiesSorted(t *testing.T) {
	policy := access.NewRolePolicy()
	policy.Register("teams:write", "teams:read", "teams:write")

	got := policy.Capabilities()
	want := []access.Permission{"teams:read", "teams:write"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Capabilities() = %v, want %v", got, want)
	}

	got[0] = "changed"
	if next := policy.Capabilities(); !reflect.DeepEqual(next, want) {
		t.Fatalf("Capabilities() exposed internal state: %v", next)
	}
}

func TestCapabilitiesConcurrent(t *testing.T) {
	policy := access.NewRolePolicy()
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			policy.Register("teams:read", "teams:write")
		}()
		go func() {
			defer wg.Done()
			_ = policy.Capabilities()
		}()
	}
	wg.Wait()

	want := []access.Permission{"teams:read", "teams:write"}
	if got := policy.Capabilities(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Capabilities() = %v, want %v", got, want)
	}
}

func TestGrantDeduplicates(t *testing.T) {
	policy := access.NewRolePolicy()
	if err := policy.Grant("editor", "teams:read", "teams:read"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if err := policy.Grant("editor", "teams:read"); err != nil {
		t.Fatalf("Grant duplicate: %v", err)
	}

	want := []access.Permission{"teams:read"}
	if got := policy.PermissionsOf("editor"); !reflect.DeepEqual(got, want) {
		t.Fatalf("PermissionsOf(editor) = %v, want %v", got, want)
	}
}

func TestUnknownGrantWarns(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	policy := access.NewRolePolicy()
	policy.Register("teams:admin", "teams:read")
	if err := policy.Grant("editor", "temas:admin"); err != nil {
		t.Fatalf("Grant returned error in warning mode: %v", err)
	}

	logged := logs.String()
	for _, want := range []string{"temas:admin", "teams:admin", "will never match"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("warning %q missing %q", logged, want)
		}
	}
	if got := policy.PermissionsOf("editor"); !reflect.DeepEqual(got, []access.Permission{"temas:admin"}) {
		t.Fatalf("warning mode changed backward-compatible grant: %v", got)
	}
}

func TestStrictGrantRejects(t *testing.T) {
	policy := access.NewRolePolicy().StrictCapabilities()
	policy.Register("teams:admin", "teams:read")

	err := policy.Grant("editor", "temas:admin")
	if err == nil {
		t.Fatal("Grant returned nil for an unknown strict capability")
	}
	if !strings.Contains(err.Error(), "teams:admin") {
		t.Fatalf("Grant error %q missing nearest capability", err)
	}
	if got := policy.PermissionsOf("editor"); len(got) != 0 {
		t.Fatalf("strict Grant persisted rejected capability: %v", got)
	}
}

func TestResourceWildcardExpands(t *testing.T) {
	policy := access.NewRolePolicy()
	policy.Register("posts:read", "teams:write", "teams:read")
	if err := policy.Grant("editor", "teams:*"); err != nil {
		t.Fatalf("Grant resource wildcard: %v", err)
	}

	got := policy.PermissionsOf("editor")
	want := []access.Permission{"teams:read", "teams:write"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PermissionsOf(editor) = %v, want expanded %v", got, want)
	}
	ctx := access.WithRoles(access.WithPolicy(context.Background(), policy), []string{"editor"})
	if !policy.Can(ctx, "teams:read") || !policy.Can(ctx, "teams:write") {
		t.Fatal("expanded capabilities do not authorize their exact gates")
	}
	if policy.Can(ctx, "posts:read") || policy.Can(ctx, "teams:*") {
		t.Fatal("resource wildcard expanded outside its prefix or remained granted")
	}
}

func TestEmptyRegistryWildcardWarns(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	policy := access.NewRolePolicy()
	if err := policy.Grant("editor", "teams:*"); err != nil {
		t.Fatalf("Grant returned error in warning mode: %v", err)
	}
	if !strings.Contains(logs.String(), "will never match") {
		t.Fatalf("warning missing from %q", logs.String())
	}
	if got := policy.PermissionsOf("editor"); !reflect.DeepEqual(got, []access.Permission{"teams:*"}) {
		t.Fatalf("empty-registry fallback changed legacy grant: %v", got)
	}
}
