package ui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
)

func checkRPCPath(component, field, path string) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || urlsafe.CleanAnchor(path) == "" {
		panic("ui: " + component + " " + field + " must be a safe same-origin path starting with /, not " + path)
	}
}
