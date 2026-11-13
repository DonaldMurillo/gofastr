package html

import (
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// TableConfig configures a <table> element. Automatically adds role="table".
type TableConfig struct {
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// CaptionConfig configures a <caption> element. No required fields.
type CaptionConfig struct {
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// TableSectionConfig configures thead/tbody/tfoot elements.
type TableSectionConfig struct {
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// TableRowConfig configures a <tr> element.
type TableRowConfig struct {
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// THConfig configures a <th> element. Scope defaults to "col" (columnheader).
type THConfig struct {
	Scope      string // defaults to "col"
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// TDConfig configures a <td> element.
type TDConfig struct {
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// Table produces a <table> element with role="table".
func Table(cfg TableConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", RoleTable)
	return render.Tag("table", attrs, children...)
}

// Caption produces a <caption> element for a table description.
func Caption(cfg CaptionConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.Tag("caption", attrs, children...)
}

// Thead produces a <thead> element with role="rowgroup".
func Thead(cfg TableSectionConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", RoleRowGroup)
	return render.Tag("thead", attrs, children...)
}

// Tbody produces a <tbody> element with role="rowgroup".
func Tbody(cfg TableSectionConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", RoleRowGroup)
	return render.Tag("tbody", attrs, children...)
}

// Tfoot produces a <tfoot> element with role="rowgroup".
func Tfoot(cfg TableSectionConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", RoleRowGroup)
	return render.Tag("tfoot", attrs, children...)
}

// TableRow produces a <tr> element with role="row".
func TableRow(cfg TableRowConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", RoleRow)
	return render.Tag("tr", attrs, children...)
}

// TH produces a <th> element. The scope attribute defaults to "col"
// (columnheader) unless cfg.Scope is set; the keyword is folded to lower
// case, as HTML reads it ASCII case-insensitively. The role is
// "rowheader" for scope="row" and "rowgroup", which head rows, and
// "columnheader" for every other scope, "col" and "colgroup" included.
func TH(cfg THConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	scope := asciicase.Lower(cfg.Scope)
	if scope == "" {
		scope = "col"
	}
	setAttr(attrs, "scope", scope)
	if scope == "row" || scope == "rowgroup" {
		setAttr(attrs, "role", "rowheader")
	} else {
		setAttr(attrs, "role", "columnheader")
	}
	return render.Tag("th", attrs, children...)
}

// TD produces a <td> element with role="cell".
func TD(cfg TDConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", "cell")
	return render.Tag("td", attrs, children...)
}
