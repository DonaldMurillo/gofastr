package elements

import "github.com/gofastr/gofastr/core/render"

// Table produces a <table> element with role="table".
func Table(attrs Attrs, children ...render.HTML) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	if _, ok := attrs["role"]; !ok {
		attrs["role"] = RoleTable
	}
	return render.Tag("table", attrs, children...)
}

// Caption produces a <caption> element for a table description.
func Caption(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("caption", attrs, children...)
}

// Thead produces a <thead> element with role="rowgroup".
func Thead(attrs Attrs, children ...render.HTML) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	if _, ok := attrs["role"]; !ok {
		attrs["role"] = RoleRowGroup
	}
	return render.Tag("thead", attrs, children...)
}

// Tbody produces a <tbody> element with role="rowgroup".
func Tbody(attrs Attrs, children ...render.HTML) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	if _, ok := attrs["role"]; !ok {
		attrs["role"] = RoleRowGroup
	}
	return render.Tag("tbody", attrs, children...)
}

// Tfoot produces a <tfoot> element with role="rowgroup".
func Tfoot(attrs Attrs, children ...render.HTML) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	if _, ok := attrs["role"]; !ok {
		attrs["role"] = RoleRowGroup
	}
	return render.Tag("tfoot", attrs, children...)
}

// TableRow produces a <tr> element with role="row".
func TableRow(attrs Attrs, children ...render.HTML) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	if _, ok := attrs["role"]; !ok {
		attrs["role"] = RoleRow
	}
	return render.Tag("tr", attrs, children...)
}

// TH produces a <th> element. The scope attribute defaults to "col"
// (columnheader) unless attrs already sets it. The role is set to
// "columnheader" for scope="col" and "rowheader" for scope="row".
func TH(attrs Attrs, children ...render.HTML) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 2)
	}
	scope := "col"
	if v, ok := attrs["scope"]; ok {
		scope = v
	} else {
		attrs["scope"] = scope
	}
	if _, ok := attrs["role"]; !ok {
		if scope == "row" {
			attrs["role"] = "rowheader"
		} else {
			attrs["role"] = "columnheader"
		}
	}
	return render.Tag("th", attrs, children...)
}

// TD produces a <td> element with role="cell".
func TD(attrs Attrs, children ...render.HTML) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	if _, ok := attrs["role"]; !ok {
		attrs["role"] = "cell"
	}
	return render.Tag("td", attrs, children...)
}
