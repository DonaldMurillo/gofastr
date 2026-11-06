package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnmountedHeaderLineEndings(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		t.Run(strings.ReplaceAll(ending, "\n", "LF"), func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), FileName)
			prefix := "# keep this comment" + ending + "version: 1" + ending
			writeFile(t, filename, prefix+unmountedHeader+ending+"unmounted:"+ending+
				"  - source: old"+ending)
			for range 3 {
				if err := SetUnmounted(filename, []Unmounted{authCopy()}); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(filename)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Count(string(data), unmountedHeader) != 1 || !strings.HasPrefix(string(data), prefix) {
					t.Fatalf("rewrite duplicated its header or changed other bytes: %q", data)
				}
			}
			if err := SetUnmounted(filename, nil); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), unmountedHeader) || !strings.HasPrefix(string(data), prefix) {
				t.Fatalf("removal left its header or changed other bytes: %q", data)
			}
		})
	}
}
