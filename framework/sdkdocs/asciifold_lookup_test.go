package sdkdocs

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A reference URL segment, and a name in Config.Entities, match an entity
// by an ASCII-only fold: upper-case ASCII still names the page, while the
// KELVIN SIGN spelling of "kiosk", which strings.ToLower folds onto k, is
// a different name and 404s instead of serving a duplicate URL.
func TestLookupFoldsASCIIOnly(t *testing.T) {
	kiosk := entity.Define("kiosk", entity.EntityConfig{Exposure: &entity.ExposureConfig{Public: true},
		Fields: []schema.Field{{Name: "label", Type: schema.String}}})
	s := &site{cfg: Config{Registry: &fakeRegistry{entities: []*entity.Entity{kiosk}}, BasePath: "/docs/api"}}

	if _, ok := s.lookup("KIOSK"); !ok {
		t.Error("an upper-case ASCII segment no longer names the entity")
	}
	if _, ok := s.lookup("Kiosk"); ok {
		t.Error("the KELVIN SIGN spelling resolved the kiosk page")
	}

	s.cfg.Entities = []string{" KIOSK "}
	if got := len(s.includedEntities()); got != 1 {
		t.Errorf("an upper-case ASCII Entities name included %d entities, want 1", got)
	}
	s.cfg.Entities = []string{"Kiosk"}
	if got := len(s.includedEntities()); got != 0 {
		t.Errorf("the KELVIN SIGN Entities name included %d entities, want 0", got)
	}
}
