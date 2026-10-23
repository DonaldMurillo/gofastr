package app

//gofastr:test-reads /core/i18n/dir.go

import (
	"os"
	"regexp"
	"slices"
	"testing"
)

// The module's right-to-left scripts are the server's: a script on one
// list only would make a client navigation and a full load disagree on
// the same page's direction.
func TestDocDirScriptsMatchI18n(t *testing.T) {
	goSrc, err := os.ReadFile("../../core/i18n/dir.go")
	if err != nil {
		t.Fatal(err)
	}
	quoted := regexp.MustCompile(`"([A-Z][a-z]{3})"`)
	var server, client []string
	for _, m := range quoted.FindAllStringSubmatch(string(goSrc), -1) {
		server = append(server, m[1])
	}
	set := regexp.MustCompile(`new Set\(\[([^\]]*)\]\)`).FindStringSubmatch(docDirJS)
	if set == nil {
		t.Fatal("docdir.js has no RTL script set")
	}
	for _, m := range regexp.MustCompile(`'([A-Z][a-z]{3})'`).FindAllStringSubmatch(set[1], -1) {
		client = append(client, m[1])
	}
	slices.Sort(server)
	slices.Sort(client)
	if len(server) == 0 || !slices.Equal(server, client) {
		t.Errorf("right-to-left scripts differ: core/i18n %v, docdir.js %v", server, client)
	}
}
