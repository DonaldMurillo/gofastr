package headless

import "testing"

func TestComboboxRowsRefusesDuplicateIDs(t *testing.T) {
	for _, options := range [][]ComboboxOption{
		{{ID: "repeated", Label: "Cedar"}, {ID: "repeated", Label: "Birch"}},
		{{ID: "results-opt-1", Label: "Cedar"}, {Label: "Birch"}},
		{{Label: "Cedar"}, {ID: "results-opt-0", Label: "Birch"}},
	} {
		t.Run(options[0].ID, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("ComboboxRows accepted duplicate option IDs")
				}
			}()
			ComboboxRows("results", options, nil)
		})
	}
}
