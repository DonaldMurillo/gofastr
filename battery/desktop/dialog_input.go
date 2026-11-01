package desktop

import (
	"strings"
	"unicode/utf8"
)

// Every shell checks dialog input here first (Linux and Windows hand it
// to the OS as C or UTF-16 strings and glob patterns, macOS as
// NSStrings and UTTypes), so a page gets the same answer for the same
// input on every OS.

// NativeText reports whether s can cross into a native string: valid
// UTF-8 with no NUL, which would end a C or UTF-16 string early.
func NativeText(s string) bool { return utf8.ValidString(s) && !strings.ContainsRune(s, 0) }

// FilterPatterns turns a file filter's extensions into glob patterns:
// "png" and ".png" give "*.png", "*" gives "*", and no extensions give
// "*". An extension that is empty, not NativeText, or holds a path
// separator, a glob character or ';' (Windows' pattern separator) is
// refused: a filter labelled as text must not also match *.exe.
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

// CheckFileDialog checks a file dialog's input and returns each
// filter's patterns (FilterPatterns). Filter names and defaultName (the
// save dialog's suggested name, "" for none) must be NativeText, and
// defaultName is a file name: no '/' or '\', either of which is a path
// separator on some OS.
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
