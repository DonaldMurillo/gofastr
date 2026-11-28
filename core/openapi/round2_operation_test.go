package openapi

import "testing"

func TestPathParameterIsAlwaysRequired(t *testing.T) {
	op := NewOperation()
	op.AddParameter("id", "path", "", false, nil)
	if op.Parameters[0]["required"] != true {
		t.Fatal("OpenAPI path parameters must be required")
	}
}

func TestZeroOperationAcceptsResponses(t *testing.T) {
	var op Operation
	op.AddResponse(200, "OK", nil)
	if op.Responses[200]["description"] != "OK" {
		t.Fatal("response missing")
	}
}

func TestSecurityScopesSnapshot(t *testing.T) {
	scopes := []string{"read"}
	requirement := securityRequirement("oauth", scopes)
	scopes[0] = "write"
	if got := requirement["oauth"][0]; got != "read" {
		t.Fatalf("scope changed to %q", got)
	}
}
