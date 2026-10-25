package appspec

import (
	"testing"
)

func TestToCamelJSON(t *testing.T) {
	if ToCamelJSON("") != "" {
		t.Fatal("empty")
	}
	if ToCamelJSON("user_name") != "userName" {
		t.Fatalf("got %q", ToCamelJSON("user_name"))
	}
}
