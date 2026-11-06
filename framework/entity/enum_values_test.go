package entity

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func TestEnumNeedsValues(t *testing.T) {
	for _, required := range []bool{false, true} {
		for _, values := range [][]string{nil, {}, {"draft"}} {
			field := schema.Field{Name: "stage", Type: schema.Enum, Required: required, Values: values}
			err := validateFields(t, field)
			if len(values) > 0 {
				if err != nil {
					t.Fatal(err)
				}
				continue
			}
			if err == nil || !strings.Contains(err.Error(), "stage") || !strings.Contains(err.Error(), "Values") {
				t.Fatalf("empty enum validation = %v; want field and Values", err)
			}
		}
	}
}
