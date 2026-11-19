package mcp

// Annotations are a tool's behaviour hints, served as `annotations` in tools/list; a nil hint is omitted.
type Annotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

// Bool returns a pointer to v, for the hint fields of Annotations.
func Bool(v bool) *bool { return &v }

// WithAnnotations sets the tool's annotations; the hints are copied, so changing the caller's bools later changes nothing.
func WithAnnotations(a Annotations) ToolOption {
	a = a.clone()
	return func(t *Tool) {
		c := a.clone()
		t.Annotations = &c
	}
}

func (a Annotations) clone() Annotations {
	cp := func(p *bool) *bool {
		if p == nil {
			return nil
		}
		return Bool(*p)
	}
	return Annotations{
		Title:           a.Title,
		ReadOnlyHint:    cp(a.ReadOnlyHint),
		DestructiveHint: cp(a.DestructiveHint),
		IdempotentHint:  cp(a.IdempotentHint),
		OpenWorldHint:   cp(a.OpenWorldHint),
	}
}

func missingListingHints(a *Annotations) []string {
	if a == nil {
		return []string{"readOnlyHint", "destructiveHint", "openWorldHint"}
	}
	var missing []string
	if a.ReadOnlyHint == nil {
		missing = append(missing, "readOnlyHint")
	}
	if a.DestructiveHint == nil {
		missing = append(missing, "destructiveHint")
	}
	if a.OpenWorldHint == nil {
		missing = append(missing, "openWorldHint")
	}
	return missing
}
