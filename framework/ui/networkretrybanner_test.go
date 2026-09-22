package ui

import (
	"strings"
	"testing"
)

func TestRetryBannerRequiresHealth(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic without HealthEndpoint")
		}
	}()
	NetworkRetryBanner(NetworkRetryBannerConfig{})
}

func TestRetryBannerHiddenByDefault(t *testing.T) {
	got := string(NetworkRetryBanner(NetworkRetryBannerConfig{HealthEndpoint: "/health"}))
	if !strings.Contains(got, "hidden=") {
		t.Errorf("expected hidden attribute by default, got: %s", got)
	}
}

func TestRetryBannerAttrs(t *testing.T) {
	got := string(NetworkRetryBanner(NetworkRetryBannerConfig{
		HealthEndpoint: "/health",
		Title:          "Offline",
	}))
	for _, want := range []string{
		`data-fui-comp="ui-network-retry-banner"`,
		`role="alert"`,
		`aria-live="assertive"`,
		`href="/health"`,
		`data-hui-network-retry=""`,
		`data-hui-system-offline=""`,
		`data-hui-system=""`,
		"Offline",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q, got: %s", want, got)
		}
	}
}

func TestRetryBannerDefaults(t *testing.T) {
	got := string(NetworkRetryBanner(NetworkRetryBannerConfig{HealthEndpoint: "/h"}))
	for _, want := range []string{
		`data-hui-system-offline=""`, // default
		"Connection lost",            // default title
		"Retry now",                  // default retry label
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing default %q, got: %s", want, got)
		}
	}
}

func TestRetryBannerCustomRetryLabel(t *testing.T) {
	got := string(NetworkRetryBanner(NetworkRetryBannerConfig{
		HealthEndpoint: "/h",
		RetryLabel:     "Try again",
	}))
	if !strings.Contains(got, "Try again") {
		t.Errorf("expected custom retry label, got: %s", got)
	}
}

func TestRetryBannerExtraAttrsOnRoot(t *testing.T) {
	h := NetworkRetryBanner(NetworkRetryBannerConfig{
		HealthEndpoint: "/health",
		ExtraAttrs:     map[string]string{"data-test": "hook"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("banner root missing data-test:\n%s", root)
	}
}
