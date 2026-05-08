package elements

import "github.com/gofastr/gofastr/core/render"

// OrderedList produces an <ol> element with role="list".
func OrderedList(cfg ListConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", RoleList)
	return render.Tag("ol", attrs, children...)
}

// UnorderedList produces a <ul> element with role="list".
func UnorderedList(cfg ListConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", RoleList)
	return render.Tag("ul", attrs, children...)
}

// ListItem produces an <li> element with role="listitem".
func ListItem(cfg ListItemConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	setAttr(attrs, "role", RoleListItem)
	return render.Tag("li", attrs, children...)
}

// DescriptionList produces a <dl> element for name-value groups.
func DescriptionList(cfg TextConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	return render.Tag("dl", attrs, children...)
}

// DescriptionTerm produces a <dt> element for a term in a description list.
func DescriptionTerm(cfg TextConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	return render.Tag("dt", attrs, children...)
}

// DescriptionDetail produces a <dd> element for a description in a
// description list.
func DescriptionDetail(cfg TextConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.Attrs, cfg.ID, cfg.Class)
	return render.Tag("dd", attrs, children...)
}
