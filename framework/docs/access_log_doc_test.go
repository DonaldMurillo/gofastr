package docs

import (
	"strings"
	"testing"
)

func TestAccessLogKeepsDefaultMiddleware(t *testing.T) {
	document, err := Get("log")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(document), "WithoutDefaultMiddleware()") || strings.Contains(string(document), "middleware.LoggingFn(app.Logger)") {
		t.Fatal("log docs recommend disabling default middleware for a nonexistent duplicate access entry")
	}
}
