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

func TestRetryBannerMarkupAndAttrs(t *testing.T) {
	got := string(NetworkRetryBanner(NetworkRetryBannerConfig{
		HealthEndpoint:   "/health",
		FailureThreshold: 5,
		SSESilenceMs:     20000,
		Title:            "Offline",
	}))
	for _, want := range []string{
		`data-fui-comp="ui-network-retry-banner"`,
		`role="alert"`,
		`aria-live="assertive"`,
		`data-fui-network-retry-health="/health"`,
		`data-fui-network-retry-threshold="5"`,
		`data-fui-network-retry-sse-silence="20000"`,
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
		`data-fui-network-retry-threshold="3"`, // default
		"Connection lost",                       // default title
		"Retry now",                             // default retry label
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
