package crud

import (
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestStreamCapDefault(t *testing.T) {
	// Without entity opt-in, ?stream=true must NOT lift the per-page cap.
	// limit=1000 should be rejected (out of range) and fall back to default 20.
	req := httptest.NewRequest("GET", "/?stream=true&limit=1000", nil)
	_, perPage := parsePagination(req, 0)
	if perPage > 100 {
		t.Fatalf("stream=true without opt-in lifted cap: perPage=%d", perPage)
	}
}

func TestStreamCapWithOptIn(t *testing.T) {
	// With MaxListLimit=500, limit=500 is allowed.
	req := httptest.NewRequest("GET", "/?limit=500", nil)
	_, perPage := parsePagination(req, 500)
	if perPage != 500 {
		t.Fatalf("entityMax=500 limit=500: perPage=%d, want 500", perPage)
	}
}

func TestStreamCapHonorsThreshold(t *testing.T) {
	// Entity claiming MaxListLimit=99999 must still cap at streamListThreshold.
	req := httptest.NewRequest("GET", "/?limit=99999", nil)
	_, perPage := parsePagination(req, 99999)
	if perPage > streamListThreshold {
		t.Fatalf("perPage=%d exceeded streamListThreshold=%d", perPage, streamListThreshold)
	}
}

func TestStreamDefaultCapUnchanged(t *testing.T) {
	// limit=50 (within default cap) is honored.
	req := httptest.NewRequest("GET", "/?limit=50", nil)
	_, perPage := parsePagination(req, 0)
	if perPage != 50 {
		t.Fatalf("perPage=%d, want 50", perPage)
	}
}

func TestVisibleFieldsReturnsCopy(t *testing.T) {
	ent := entity.Define("docs", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "internal_notes", Type: schema.String, Hidden: true},
		},
	}.WithTimestamps(false))
	ch := NewCrudHandler(ent, nil)

	fields := ch.VisibleFields()
	fields[0] = "mutated"

	got := ch.VisibleFields()
	if got[0] == "mutated" {
		t.Fatal("VisibleFields returned mutable handler cache")
	}
	if len(got) != 2 || got[0] != "id" || got[1] != "title" {
		t.Fatalf("VisibleFields = %v, want [id title]", got)
	}

	ent.Config.Fields[1].Hidden = true
	got = ch.VisibleFields()
	if len(got) != 1 || got[0] != "id" {
		t.Fatalf("VisibleFields after entity mutation = %v, want [id]", got)
	}
}
