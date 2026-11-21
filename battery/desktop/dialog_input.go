package desktop

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// NativeText reports whether s is valid UTF-8 with no NUL, so it can cross into a native string.
func NativeText(s string) bool { return textsafe.Storable(s) }

// FilterPatterns turns a file filter's extensions into glob patterns, refusing unsafe ones.
func FilterPatterns(extensions []string) ([]string, error) {
	if len(extensions) == 0 {
		return []string{"*"}, nil
	}
	patterns := make([]string, 0, len(extensions))
	for _, ext := range extensions {
		ext = strings.TrimPrefix(strings.TrimSpace(ext), ".")
		if ext == "*" {
			patterns = append(patterns, "*")
			continue
		}
		if ext == "" || !NativeText(ext) || strings.ContainsAny(ext, "/\\*?[];") {
			return nil, invalidDialogInput("invalid file filter extension")
		}
		patterns = append(patterns, "*."+ext)
	}
	return patterns, nil
}

func invalidDialogInput(message string) error {
	return &Error{Code: CodeInvalidInput, Message: message}
}

// CheckFileDialog checks a file dialog's input and returns each filter's patterns (FilterPatterns).
func CheckFileDialog(opts OpenOptions, defaultName string) ([][]string, error) {
	if !NativeText(defaultName) || strings.ContainsAny(defaultName, `/\`) {
		return nil, invalidDialogInput("default name must be a file name")
	}
	patterns := make([][]string, len(opts.Filters))
	for i, filter := range opts.Filters {
		if !NativeText(filter.Name) {
			return nil, invalidDialogInput("invalid file filter name")
		}
		var err error
		if patterns[i], err = FilterPatterns(filter.Extensions); err != nil {
			return nil, err
		}
	}
	return patterns, nil
}
