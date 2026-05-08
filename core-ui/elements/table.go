package elements

import "github.com/gofastr/gofastr/core/render"

// Table produces a <table> element with role="table".
func Table(cfg TableConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", RoleTable)
	return render.Tag("table", attrs, children...)
}

// Caption produces a <caption> element for a table description.
func Caption(cfg CaptionConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	return render.Tag("caption", attrs, children...)
}

// Thead produces a <thead> element with role="rowgroup".
func Thead(cfg TableSectionConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", RoleRowGroup)
	return render.Tag("thead", attrs, children...)
}

// Tbody produces a <tbody> element with role="rowgroup".
func Tbody(cfg TableSectionConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", RoleRowGroup)
	return render.Tag("tbody", attrs, children...)
}

// Tfoot produces a <tfoot> element with role="rowgroup".
func Tfoot(cfg TableSectionConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", RoleRowGroup)
	return render.Tag("tfoot", attrs, children...)
}

// TableRow produces a <tr> element with role="row".
func TableRow(cfg TableRowConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", RoleRow)
	return render.Tag("tr", attrs, children...)
}

// TH produces a <th> element. The scope attribute defaults to "col"
// (columnheader) unless cfg.Scope is set. The role is set to
// "columnheader" for scope="col" and "rowheader" for scope="row".
func TH(cfg THConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	scope := cfg.Scope
	if scope == "" {
		scope = "col"
	}
	setAttr(attrs, "scope", scope)
	if scope == "row" {
		setAttr(attrs, "role", "rowheader")
	} else {
		setAttr(attrs, "role", "columnheader")
	}
	return render.Tag("th", attrs, children...)
}

// TD produces a <td> element with role="cell".
func TD(cfg TDConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", "cell")
	return render.Tag("td", attrs, children...)
}
