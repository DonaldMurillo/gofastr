package i18n

import "testing"

func TestLocaleTagsRejectEmptySegments(t *testing.T) {
	for _, tag := range []string{"-en", "en-", "en--us"} {
		if got := sanitizeTag(tag); got != "" {
			t.Errorf("malformed tag %q accepted as %q", tag, got)
		}
	}
}

func TestCatalogOwnsPluralMaps(t *testing.T) {
	c := NewMapCatalog()
	msg := Message{Plural: map[string]string{"one": "one item", "other": "many items"}}
	c.Set("en", "items", msg)
	msg.Plural["one"] = "changed by setter"
	got, _ := c.Get("en", "items")
	if got.Plural["one"] != "one item" {
		t.Errorf("Set retained caller's mutable plural map: %+v", got)
	}
	got.Plural["other"] = "changed by reader"
	again, _ := c.Get("en", "items")
	if again.Plural["other"] != "many items" {
		t.Errorf("Get exposed stored plural map: %+v", again)
	}
}
