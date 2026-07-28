package evalrunner

import "testing"

func TestParseTokenUsageUsesLastReport(t *testing.T) {
	log := []byte("tokens used\n1,234\nnoise\ntokens used\n56,789\n")
	if got := ParseTokenUsage(log); got != 56789 {
		t.Fatalf("ParseTokenUsage() = %d, want 56789", got)
	}
}

func TestParseTokenUsageMissing(t *testing.T) {
	if got := ParseTokenUsage([]byte("no telemetry")); got != 0 {
		t.Fatalf("ParseTokenUsage() = %d, want 0", got)
	}
}
