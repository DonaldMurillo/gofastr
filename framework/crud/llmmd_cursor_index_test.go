package crud

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestCursorIndexOmitsTotal(t *testing.T) {
	registry := stubRegistry{byName: map[string]*entity.Entity{"posts": covRelEntity()}}
	got := RegistryLLMMD(registry, "example", nil, "")
	if !strings.Contains(got, "returns `{data, cursor, hasMore}`") || strings.Contains(got, "{data, cursor, hasMore, total}") {
		t.Fatal("registry index advertises a cursor total the route does not return")
	}
}
