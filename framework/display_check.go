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

// ValidateDisplay runs the display boot check App.Entity runs on an
// unregistered declaration, for code generators, and returns the error the
// app would refuse it with at boot, naming the entity.
func ValidateDisplay(name string, cfg EntityConfig) (err error) {
	if err := entity.ValidateDisplay(name, cfg); err != nil {
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	ent := entity.Define(name, cfg)
	if err := validateDisplayQueries(ent); err != nil {
		return fmt.Errorf("entity %q: %w", ent.Config.Name, err)
	}
	return nil
}

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

func checkShowWhen(target, text string, fields []schema.Field, hints map[string]entity.FieldDisplay) error {
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
