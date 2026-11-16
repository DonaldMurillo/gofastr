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
	Name string
	// ID is the input's id, which the zone's label points at.
	ID string
	// Label is the instruction inside the zone; CTA its action part.
	Label string
	CTA   string
	// Hint is the accepted types and size, described as ID+"-accept".
	Hint        string
	Accept      string
	Multiple    bool
	Required    bool
	Disabled    bool
	Invalid     bool
	DescribedBy string

	Parts Parts
	// Strings are the words it says; nil means the English defaults.
	Strings *Strings

	ExtraAttrs html.Attrs
}

// FileUpload renders a file input whose label is the drop zone; it panics
// without Name, ID or Label.
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

	own := Attrs(map[string]string{
		"data-hui-drop-input": p.ID,
		"data-hui-drop-one":   p.Strings.Resolve().FileSelected,
		"data-hui-drop-many":  p.Strings.Resolve().FilesSelected,
	})
	own["data-hui-drop"] = ""
	return b.El("div", PartRoot, own,
		b.El("label", PartDropZone, Internal(Attrs(map[string]string{"for": p.ID})), zoneKids...),
		b.El("input", PartDropInput, Internal(input)),
		b.El("ul", PartDropList, Internal(Mark(Attrs(map[string]string{"role": "list"}), "data-hui-drop-list"))),
		b.El("span", PartStatus, Internal(Mark(Attrs(map[string]string{
			"role": "status",
		}), "data-hui-drop-status"))),
	)
}

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
