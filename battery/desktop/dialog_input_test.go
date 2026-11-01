package desktop

import (
	"errors"
	"reflect"
	"testing"
)

func TestFilterPatternsForms(t *testing.T) {
	for _, tc := range []struct{ extensions, want []string }{
		{nil, []string{"*"}}, {[]string{"png", ".jpg", "tar.gz"}, []string{"*.png", "*.jpg", "*.tar.gz"}},
		{[]string{"*", "txt"}, []string{"*", "*.txt"}}, {[]string{"  png  "}, []string{"*.png"}},
	} {
		got, err := FilterPatterns(tc.extensions)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q = %q, %v; want %q", tc.extensions, got, err, tc.want)
		}
	}
	// Windows joins a filter's patterns with ';', so "txt;exe" would
	// slip a second pattern under a filter labelled as text.
	for _, ext := range []string{"", ".", "../txt", "a/b", "a\\b", "t?xt", "[abc]", "p*ng", "a\x00b", "txt;*.exe", "txt;exe", "\xff"} {
		_, err := FilterPatterns([]string{ext})
		var de *Error
		if !errors.As(err, &de) || de.Code != CodeInvalidInput {
			t.Errorf("FilterPatterns(%q) = %v, want an invalid-input refusal", ext, err)
		}
	}
}

// One check for every shell's file dialog: filter names and the save
// dialog's default name are native text, and a default name is a file
// name, not a path on either OS.
func TestCheckFileDialogRefuses(t *testing.T) {
	ok := OpenOptions{Filters: []FileFilter{{Name: "Text", Extensions: []string{"txt"}}}}
	if got, err := CheckFileDialog(ok, "notes.txt"); err != nil || !reflect.DeepEqual(got, [][]string{{"*.txt"}}) {
		t.Fatalf("CheckFileDialog = %q, %v", got, err)
	}
	for name, tc := range map[string]struct {
		opts        OpenOptions
		defaultName string
	}{
		"NUL in filter name": {OpenOptions{Filters: []FileFilter{{Name: "Te\x00xt", Extensions: []string{"txt"}}}}, ""},
		"bad extension":      {OpenOptions{Filters: []FileFilter{{Name: "Text", Extensions: []string{"txt;*.exe"}}}}, ""},
		"NUL in name":        {OpenOptions{}, "a\x00.txt"},
		"slash in name":      {OpenOptions{}, "dir/a.txt"},
		"backslash in name":  {OpenOptions{}, `dir\a.txt`},
		"invalid UTF-8 name": {OpenOptions{}, "\xff.txt"},
	} {
		if _, err := CheckFileDialog(tc.opts, tc.defaultName); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
