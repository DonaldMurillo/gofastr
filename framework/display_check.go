package framework

import (
	"fmt"
	"maps"
	"slices"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/dsl"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// validateDisplayQueries parses the query strings an entity's Display
// holds, the half of the Display boot check framework/entity cannot run
// because it cannot import the DSL: every view's Where and Sort, and
// every field's ShowWhen. A bad one refuses the registration, naming
// the entity and the offender, the same way entity.Validate refuses a
// bad field name. Sort goes through dsl.ParseSort — the same grammar
// ?sort= parses — so a view's sort and a caller's sort can never
// disagree about what a term means.
func validateDisplayQueries(ent *entity.Entity) error {
	d := ent.Config.Display
	if d == nil {
		return nil
	}
	fields := ent.Config.Fields
	for _, v := range d.Views {
		if v.Where != "" {
			if _, err := dsl.ParsePredicate(v.Where, fields); err != nil {
				return fmt.Errorf("display view %q where: %w", v.Key, err)
			}
		}
		if v.Sort != "" {
			if _, err := dsl.ParseSort(v.Sort, fields); err != nil {
				return fmt.Errorf("display view %q sort: %w", v.Key, err)
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(d.Fields)) {
		if sw := d.Fields[name].ShowWhen; sw != "" {
			if err := checkShowWhen(name, sw, fields, d.Fields); err != nil {
				return fmt.Errorf("display fields[%s] show_when: %w", name, err)
			}
		}
	}
	return nil
}

// checkShowWhen holds a ShowWhen condition to the shape the form's when
// behaviour evaluates in the browser: one `field = value` or
// `field in [...]` term over an editable Enum or Bool field, naming only
// values that field can hold. Anything wider would render a condition the
// browser cannot evaluate, so it is refused here rather than ignored there.
func checkShowWhen(target, text string, fields []schema.Field, hints map[string]entity.FieldDisplay) error {
	// A condition is evaluated in the browser, not in SQL, so NoQuery
	// does not bar a controller; Hidden still does (it is never on the
	// form). Parse against a copy with NoQuery cleared.
	parseFields := slices.Clone(fields)
	for i := range parseFields {
		parseFields[i].NoQuery = false
	}
	p, err := dsl.ParsePredicate(text, parseFields)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("is blank")
	}
	// An and/or group carries no Op, so this also refuses compounds.
	if p.Op != filter.OpEq && p.Op != filter.OpIn {
		return fmt.Errorf("must be one `field = value` or `field in [...]` term")
	}
	if p.Field == target {
		return fmt.Errorf("field %q cannot depend on itself", target)
	}
	var ctl *schema.Field
	for i := range fields {
		if fields[i].Name == p.Field {
			ctl = &fields[i]
		}
	}
	if ctl == nil {
		return fmt.Errorf("names unknown field %q", p.Field)
	}
	// The controller must be on the form and editable, or the condition
	// can never change in the browser.
	if ctl.ReadOnly || ctl.AutoGenerate != schema.AutoNone || hints[ctl.Name].Omit || hints[ctl.Name].Locked {
		return fmt.Errorf("controller %q is not an editable form field", ctl.Name)
	}
	var allowed []string
	switch ctl.Type {
	case schema.Enum:
		allowed = ctl.Values
	case schema.Bool:
		allowed = []string{"true", "false"}
	default:
		return fmt.Errorf("controller %q must be an Enum or Bool field", ctl.Name)
	}
	values := p.Values
	if p.Op == filter.OpEq {
		values = []string{p.Value}
	}
	for _, v := range values {
		if !slices.Contains(allowed, v) {
			return fmt.Errorf("value %q is not one of %q's values", v, ctl.Name)
		}
	}
	return nil
}
