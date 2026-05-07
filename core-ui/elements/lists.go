package elements

import "github.com/gofastr/gofastr/core/render"

// OrderedList produces an <ol> element with role="list".
func OrderedList(attrs Attrs, children ...render.HTML) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	if _, ok := attrs["role"]; !ok {
		attrs["role"] = RoleList
	}
	return render.Tag("ol", attrs, children...)
}

// UnorderedList produces a <ul> element with role="list".
func UnorderedList(attrs Attrs, children ...render.HTML) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	if _, ok := attrs["role"]; !ok {
		attrs["role"] = RoleList
	}
	return render.Tag("ul", attrs, children...)
}

// ListItem produces an <li> element with role="listitem".
func ListItem(attrs Attrs, children ...render.HTML) render.HTML {
	if attrs == nil {
		attrs = make(Attrs, 1)
	}
	if _, ok := attrs["role"]; !ok {
		attrs["role"] = RoleListItem
	}
	return render.Tag("li", attrs, children...)
}

// DescriptionList produces a <dl> element for name-value groups.
func DescriptionList(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("dl", attrs, children...)
}

// DescriptionTerm produces a <dt> element for a term in a description list.
func DescriptionTerm(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("dt", attrs, children...)
}

// DescriptionDetail produces a <dd> element for a description in a
// description list.
func DescriptionDetail(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("dd", attrs, children...)
}
