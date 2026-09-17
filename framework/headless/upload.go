package headless

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// FileUpload parts.
const (
	PartDropZone  Part = "drop-zone"
	PartDropCTA   Part = "drop-cta"
	PartDropHint  Part = "drop-hint"
	PartDropInput Part = "drop-input"
	PartDropList  Part = "drop-list"
)

// FileUploadProps is a file picker with a drop target.
type FileUploadProps struct {
	// Name is the field name. Required.
	Name string
	// ID is the input's id. Required: the zone is a <label for=…>, and
	// without the pair the biggest click target on the control does
	// nothing.
	ID string
	// Label is the instruction inside the zone. Required.
	Label string
	// CTA is the part of the instruction that reads as the action —
	// "choose a file". It is NOT a button: a button inside a label
	// swallows the label's click, so the whole zone stops opening the
	// picker. It is styled text, and the real control is the input.
	CTA string
	// Hint is the accepted types and size, tied to the input by
	// aria-describedby so it is read with the field rather than being
	// small print beside it.
	Hint string
	// Accept is the accept attribute; Multiple allows several files.
	Accept   string
	Multiple bool
	Required bool
	Disabled bool
	Invalid  bool
	// DescribedBy is an extra id to reference, from a Field.
	DescribedBy string

	// Parts: attrs and binds on the root, the zone, the input, the
	// list and the status. Strings carry the sentences the
	// runtime says when files are chosen.
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings

	ExtraAttrs html.Attrs
}

// FileUpload renders the control.
//
// The input is a real <input type="file">, visually hidden and fully
// present: focusable, labelled, keyboard-operable, and submitting with
// the form. The drop zone is its <label>, so clicking anywhere in the
// zone opens the picker with no script at all.
//
// Dragging is an ENHANCEMENT and never the only route. WCAG 2.5.7
// (Dragging Movements, AA since 2.2) says any drag action needs a
// single-pointer alternative, and the population that cannot drag —
// tremor, switch access, head pointer, touch with a stylus — is larger
// than the population that finds dragging convenient. Here the
// alternative is the same control: the zone is a label, so it is a
// click target before any script runs.
//
// The chosen files are announced through a polite live region, because
// picking a file otherwise changes nothing a screen reader notices:
// the input's value is not read back, and the list of names appears
// silently.
func FileUpload(p FileUploadProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	if p.Name == "" {
		panic("headless: FileUpload requires Name")
	}
	if p.ID == "" {
		panic("headless: FileUpload requires ID — the zone is a label for it")
	}
	if p.Label == "" {
		panic("headless: FileUpload requires Label")
	}
	hintID := ""
	if p.Hint != "" {
		// "-accept", not "-hint": a Field wrapping this control
		// already derives "<id>-hint" for ITS hint, and two elements
		// sharing an id break both references. The audit found exactly
		// that — dsl-up2-hint twice on one page.
		hintID = p.ID + "-accept"
	}
	describedBy := joinIDs(p.DescribedBy, hintID)

	input := Merge(Safe(p.ExtraAttrs, "type", "id", "name"), Attrs(map[string]string{
		"type": "file", "id": p.ID, "name": p.Name,
		"accept": p.Accept, "aria-describedby": describedBy,
	}))
	Flag(input, "multiple", p.Multiple)
	Flag(input, "required", p.Required)
	Flag(input, "disabled", p.Disabled)
	if p.Invalid {
		input["aria-invalid"] = "true"
	}

	zoneKids := []render.HTML{
		b.El("span", PartText, nil, render.Text(p.Label)),
	}
	if p.CTA != "" {
		zoneKids = append(zoneKids, b.El("span", PartDropCTA, nil, render.Text(p.CTA)))
	}
	if p.Hint != "" {
		zoneKids = append(zoneKids, b.El("span", PartDropHint,
			Attrs(map[string]string{"id": hintID}), render.Text(p.Hint)))
	}

	// The two sentences the runtime substitutes the names and the
	// count into when the reader has picked. They travel as data-*
	// rather than being built by the module so a caller can localise
	// them, the same way the reveal button's labels do.
	own := Attrs(map[string]string{
		"data-hui-drop-input": p.ID,
		"data-hui-drop-one":   p.Strings.Resolve().FileSelected,
		"data-hui-drop-many":  p.Strings.Resolve().FilesSelected,
	})
	own["data-hui-drop"] = ""
	return b.El("div", PartRoot, own,
		b.El("label", PartDropZone, Attrs(map[string]string{"for": p.ID}), zoneKids...),
		b.El("input", PartDropInput, input),
		// Populated by the runtime as files are chosen, so the names
		// are on screen as well as announced.
		b.El("ul", PartDropList, Mark(Attrs(map[string]string{"role": "list"}), "data-hui-drop-list")),
		// role=status already means polite; stating it twice can
		// announce twice.
		b.El("span", PartStatus, Mark(Attrs(map[string]string{
			"role": "status",
		}), "data-hui-drop-status")),
	)
}

// joinIDs joins two id references, dropping empties.
func joinIDs(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + " " + b
	}
}

func init() {
	Register(Spec{
		Name:    "FileUpload",
		Anatomy: []Part{PartRoot, PartDropZone, PartText, PartDropCTA, PartDropHint, PartDropInput, PartDropList, PartStatus},
		Hooks: []string{"data-hui-drop", "data-hui-drop-input", "data-hui-drop-list",
			"data-hui-drop-status", "data-hui-drop-one", "data-hui-drop-many"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return FileUpload(FileUploadProps{Name: "seam-upload", ID: "seam-upload",
				Label: "Drag an archive here, or ", CTA: "choose a file", Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "with a hint",
				Why:  "the zone is a label around a real input, so the whole target opens the picker — and the call to action is styled text, because a button inside a label swallows the label's click",
				HTML: FileUpload(FileUploadProps{Name: "backup", ID: "backup",
					Label: "Drag an archive here, or ", CTA: "choose a file",
					Hint: ".tar.gz up to 2 GB", Accept: ".tar.gz"}, s),
			}}
		},
	})
}
