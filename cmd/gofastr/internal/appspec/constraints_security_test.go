package appspec

import (
	"fmt"
	"strings"
	"testing"
)

func TestConstraintFlagsRejectNonBools(t *testing.T) {
	for _, target := range []string{"required", "unique", "index"} {
		for _, value := range []string{"yes", "on", "y", "1", "null", "[]"} {
			t.Run(target+"/"+value, func(t *testing.T) {
				entity := "  - name: users\n    fields:\n      - name: email\n        type: string\n"
				key := target
				if target == "index" {
					key = "unique"
					entity += fmt.Sprintf("    indices:\n      - columns: [email]\n        unique: %s\n", value)
				} else {
					entity += fmt.Sprintf("        %s: %s\n", target, value)
				}
				_, err := loadBPWithEntity(t, entity)
				if err == nil || !strings.Contains(err.Error(), key) {
					t.Fatalf("%s: %s: want error naming %s, got %v", target, value, key, err)
				}
				if strings.Contains(err.Error(), "gates access or exposure") {
					t.Fatalf("constraint flag diagnosed as an access flag: %v", err)
				}
			})
		}
	}
}

func TestConstraintFlagsKeepBooleans(t *testing.T) {
	for _, value := range []string{"true", "false", `"true"`, `"false"`} {
		bp, err := loadBPWithEntity(t, fmt.Sprintf("  - name: users\n    fields:\n      - name: email\n        type: string\n        required: %s\n        unique: %s\n    indices:\n      - columns: [email]\n        unique: %s\n", value, value, value))
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Contains(value, "true")
		if field := bp.Entities[0].Fields[0]; field.Required != want || field.Unique != want || bp.Entities[0].Indices[0].Unique != want {
			t.Fatalf("%s: constraint flags changed", value)
		}
	}
}
