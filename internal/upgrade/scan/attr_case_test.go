package scan

import "testing"

func TestStringAttrsFoldASCII(t *testing.T) {
	source := `package main
import "example.com/kit/ui"
func main() {
 _ = ui.Attrs{"DATA-FUI-FILEUPLOAD": ""}
 _ = ui.Attrs{"DaTa-CuI-ToGgLe-open": ""}
 _ = "<INPUT DATA-FUI-FILEUPLOAD>"
 _ = "<div DaTa-CuI-ToGgLe-panel='x'>"
 _ = ui.Attrs{"data-fui-fileupload-extra": "", "ſtyle": ""}
 _ = "<div xDATA-FUI-FILEUPLOAD data-fui-fileupload-extra ſtyle>"
}
`
	root := newWorkspace(t, defaultKit, map[string]string{"main.go": source})
	for _, entry := range []struct {
		name string
		want int
	}{
		{"data-fui-fileupload", 2},
		{"data-cui-toggle-", 2},
		{"DATA-FUI-FILEUPLOAD", 2},
		{"style", 0},
	} {
		note := attrsNote(entry.name)
		result := mustRun(t, root, note)
		if len(result.Hits[note]) != entry.want {
			t.Errorf("%s: hits=%+v, want %d", entry.name, result.Hits[note], entry.want)
		}
	}
}
