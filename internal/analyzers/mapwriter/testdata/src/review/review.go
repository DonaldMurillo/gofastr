package review

import "fmt"

func singleton(input map[string]string) error {
	for key := range map[string]bool{"name": true} {
		if input[key] == "" {
			return fmt.Errorf("missing %s", key)
		}
	}
	return nil
}

func singletonAliases(input map[string]string) error {
	fields := map[string]bool{"name": true}
	first := fields
	second := first
	for key := range second {
		if input[key] == "" {
			return fmt.Errorf("missing %s", key)
		}
	}
	return nil
}

func multipleAliases(input map[string]string) error {
	fields := map[string]bool{"name": true, "path": true}
	first := fields
	second := first
	third := second
	for key := range third {
		if input[key] == "" {
			return fmt.Errorf("missing %s", key) // want `returning an error built from a map range's entry`
		}
	}
	return nil
}

func cyclicBindings(fields map[string]bool) error {
	alias := fields
	fields = alias
	for key := range alias {
		if key == "" {
			return fmt.Errorf("invalid %s", key)
		}
	}
	return nil
}

func grownSingleton(input map[string]string) error {
	fields := map[string]bool{"name": true}
	fields["path"] = true
	for key := range fields {
		if input[key] == "" {
			return fmt.Errorf("missing %s", key) // want `returning an error built from a map range's entry`
		}
	}
	return nil
}

func grownAlias(input map[string]string) error {
	fields := map[string]bool{"name": true}
	alias := fields
	alias["path"] = true
	for key := range fields {
		if input[key] == "" {
			return fmt.Errorf("missing %s", key) // want `returning an error built from a map range's entry`
		}
	}
	return nil
}

func overwriteSingleton(input map[string]string) error {
	fields := map[string]bool{"name": true}
	fields["name"] = false
	for key := range fields {
		if input[key] == "" {
			return fmt.Errorf("missing %s", key)
		}
	}
	fields["path"] = true
	return nil
}

func addField(fields map[string]bool) { fields["path"] = true }

func escapedSingleton(input map[string]string) error {
	fields := map[string]bool{"name": true}
	addField(fields)
	for key := range fields {
		if input[key] == "" {
			return fmt.Errorf("missing %s", key) // want `returning an error built from a map range's entry`
		}
	}
	return nil
}
