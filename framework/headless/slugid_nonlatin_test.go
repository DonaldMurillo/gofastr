package headless

import (
	"regexp"
	"testing"
)

// slugID once kept ASCII letters and digits only, so every caption in a
// non-Latin script slugged to the same empty id: two tables with
// DIFFERENT Cyrillic captions both rendered id="table--caption", and
// the second scroll region's aria-labelledby named it after the first.
func TestTableCaptionIDsDifferForDifferentNonLatinCaptions(t *testing.T) {
	cols := []Column{{Key: "n", Header: "N"}}
	a := string(Table(TableProps{Caption: "Заказы", Columns: cols}, nil))
	b := string(Table(TableProps{Caption: "Клиенты", Columns: cols}, nil))
	idRe := regexp.MustCompile(`<caption id="([^"]+)"`)
	ia, ib := idRe.FindStringSubmatch(a), idRe.FindStringSubmatch(b)
	if ia == nil || ib == nil {
		t.Fatalf("no caption id rendered:\n%s\n%s", a, b)
	}
	if ia[1] == ib[1] {
		t.Fatalf("two different captions share the caption id %q; on one page the second region is named by the first caption", ia[1])
	}
}

// The same collapse in Fieldset (no Ctx, no dedupe) and in a Section
// rendered without Ctx.
func TestFieldsetAndSectionSlugIDsDifferForNonLatinTitles(t *testing.T) {
	fa := string(Fieldset(FieldsetProps{Legend: "Уведомления", Description: "a"}, nil))
	fb := string(Fieldset(FieldsetProps{Legend: "Доступ", Description: "b"}, nil))
	descRe := regexp.MustCompile(`aria-describedby="([^"]+)"`)
	if da, db := descRe.FindStringSubmatch(fa)[1], descRe.FindStringSubmatch(fb)[1]; da == db {
		t.Errorf("two fieldsets with different legends share description id %q", da)
	}
	sa := string(Section(SectionProps{Title: "概要"}, nil))
	sb := string(Section(SectionProps{Title: "設定"}, nil))
	lbRe := regexp.MustCompile(`aria-labelledby="([^"]+)"`)
	if la, lb := lbRe.FindStringSubmatch(sa)[1], lbRe.FindStringSubmatch(sb)[1]; la == lb {
		t.Errorf("two sections with different titles share heading id %q", la)
	}
}
