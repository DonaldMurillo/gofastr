package docs

import (
	"strings"
	"testing"
)

func TestMacSandboxDocumentsReadGaps(t *testing.T) {
	document, err := Get("process-modules")
	if err != nil {
		t.Fatal(err)
	}
	for _, gap := range []string{"P2 (host secrets in temporary directories)", "P5 (host files such as `/etc/passwd`)"} {
		if !strings.Contains(string(document), gap) {
			t.Errorf("macOS sandbox gaps omit %s", gap)
		}
	}
}
