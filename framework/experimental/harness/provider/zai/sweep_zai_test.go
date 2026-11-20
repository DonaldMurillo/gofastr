package zai

import (
	"context"
	"testing"
)

func TestSweepBaseURLAndCatalog(t *testing.T) {
	if got := (&Provider{}).baseURL(); got != defaultBase {
		t.Errorf("default base = %q", got)
	}
	if got := (&Provider{CodingPlan: true}).baseURL(); got != defaultCodingBase {
		t.Errorf("coding plan base = %q", got)
	}
	if got := (&Provider{CodingPlan: true, BaseURL: "http://x"}).baseURL(); got != "http://x" {
		t.Errorf("explicit base = %q, want it to win over CodingPlan", got)
	}
	ms, _ := (&Provider{}).Models(context.Background())
	if len(ms) == 0 || ms[0].ID != "glm-5.1" {
		t.Errorf("first model = %+v, want glm-5.1 first", ms)
	}
}
