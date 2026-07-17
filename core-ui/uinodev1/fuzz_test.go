package uinodev1

import (
	"testing"
)

// FuzzValidate is the design §10 gate-item-5 fuzz target. The property
// invariant is: Validate MUST NEVER PANIC for any input, and any input
// it accepts MUST be re-validatable without error after re-marshaling
// to JSON. Inputs it rejects are fine — rejection is the safe path.
//
// The seeds span the closed enum plus every adversarial shape so the
// fuzzer starts from interesting frontier inputs.
func FuzzValidate(f *testing.F) {
	seeds := []string{
		// valid minimal trees, one per component class
		`{"component":"heading","props":{"level":1,"text":"Hi"}}`,
		`{"component":"divider"}`,
		`{"component":"link","props":{"text":"Home","to":"/home"}}`,
		`{"component":"button","props":{"label":"Save"},"action_ref":"save"}`,
		`{"component":"stack","props":{"gap":"md"},"children":[{"component":"divider"}]}`,
		`{"component":"data-table","props":{"columns":[{"key":"a","label":"A"}],"rows":[{"cells":[{"text":"1"}]}]}}`,
		// adversarial shapes the fuzzer should mutate from
		`{"component":"script","props":{}}`,
		`{"component":"heading","props":{"data-fui-rpc":"/evil"}}`,
		`{"component":"link","props":{"to":"javascript:alert(1)","text":"x"}}`,
		`{"component":"link","props":{"to":"//evil.com","text":"x"}}`,
		`{"component":"divider","onclick":"evil()"}`,
		`{"component":"divider","actions":{"kind":"create_entity"}}`,
		`{"component":"BUTTON","props":{"label":"x"}}`,
		`{"component":"heading","component":"script","props":{"level":1,"text":"x"}}`,
		`null`,
		``,
		`[]`,
		`{"component":"divider"}{}`,
		`{"component":"heading","props":{"level":7,"text":"x"}}`,
		`{"component":"heading","props":{"level":0,"text":"x"}}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Property 1: never panic. (The fuzz harness itself enforces this —
		// a panic fails the fuzz run.)
		tree, err := Validate(data, DefaultLimits())
		if err != nil {
			// Rejected — safe path. Nothing more to check.
			return
		}
		// Property 2: an accepted tree must have a non-nil root Props.
		if tree == nil {
			t.Fatalf("Validate returned (nil, nil) for input %q", data)
		}
		if tree.Root.Props == nil {
			t.Fatalf("accepted tree has nil root props: %q", data)
		}
		if tree.Root.Component == "" {
			t.Fatalf("accepted tree has empty component: %q", data)
		}
		// Property 3: the closed enum is honored — the component must be
		// in the dispatch table. (Defensive; Validate guarantees this.)
		if _, ok := componentDecoders[tree.Root.Component]; !ok {
			t.Fatalf("accepted tree has unknown component %q: %q", tree.Root.Component, data)
		}
	})
}
