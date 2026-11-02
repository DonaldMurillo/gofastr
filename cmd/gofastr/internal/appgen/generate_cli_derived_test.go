package appgen

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/codegen"
)

func TestCLIEntryYAMLQuotesStar(t *testing.T) {
	got, err := cliEntryYAML(CliOptions{OutDir: "cmd/shop", ApiPrefix: "api", verbs: "posts=*;documents=list"}, "shop")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `posts: ["*"]`) || !strings.Contains(got, "documents: [list]") {
		t.Fatalf("a bare * is a YAML alias:\n%s", got)
	}
}

func TestSettingsCLIFromOpenAPI(t *testing.T) {
	dir := oaTempModule(t)
	clitest.WriteSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: oapp\n        from_openapi: openapi.json\n")
	out := clitest.CovT_capStdout(t, func() { GenerateProject(nil) })
	if !clitest.FileExists(dir, "cmd/oapp/operations.gen.go") {
		t.Fatalf("from_openapi entry did not build the CLI:\n%s", out)
	}
}

// The printed entry must read back, through the settings parser, as the
// options that printed it.
func TestCLIEntryYAMLRoundTrips(t *testing.T) {
	for name, opts := range map[string]CliOptions{
		"bool binary":    {Binary: "true", OutDir: "cmd/true", ApiPrefix: "api"},
		"number binary":  {Binary: "7", OutDir: "cmd/7", ApiPrefix: "1.5"},
		"empty prefix":   {Binary: "shop", OutDir: "cmd/shop", ApiPrefix: ""},
		"repeated verbs": {Binary: "shop", OutDir: "cmd/shop", ApiPrefix: "api", verbs: "posts=list;posts=get;documents=*"},
		"global verbs":   {Binary: "shop", OutDir: "cmd/shop", ApiPrefix: "api", verbs: "list,get", only: []string{"posts"}},
		"openapi prefix": {Binary: "oapp", OutDir: "cmd/oapp", FromOpenAPI: "openapi.json", ApiPrefix: "v9", apiPrefixSet: true},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			entry, err := cliEntryYAML(opts, opts.Binary)
			if err != nil {
				t.Fatal(err)
			}
			clitest.WriteSettings(t, dir, entry)
			discovery, err := codegen.DiscoverConfig(dir)
			if err != nil {
				t.Fatal(err)
			}
			got, err := decodeCLIEntry(0, discovery.Config.Codegen.Generators[0])
			if err != nil {
				t.Fatalf("%v\n%s", err, entry)
			}
			wantGlobal, wantPer, _ := parseVerbSelection(opts.verbs)
			gotGlobal, gotPer, _ := parseVerbSelection(got.verbs)
			if got.Binary != opts.Binary || got.OutDir != opts.OutDir || got.ApiPrefix != opts.ApiPrefix ||
				!slices.Equal(got.only, opts.only) || !slices.Equal(gotGlobal, wantGlobal) || !maps.EqualFunc(gotPer, wantPer, slices.Equal) {
				t.Fatalf("read back %+v from:\n%s", got, entry)
			}
		})
	}
}

// --from-openapi on the command line takes the entry's binary and output,
// not its entity selection.
func TestOpenAPIFlagSkipsEntrySelection(t *testing.T) {
	dir := oaTempModule(t)
	clitest.WriteSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: oapp\n        api_prefix: v1\n        only: [posts]\n")
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunGenerateCLI([]string{"--from-openapi=openapi.json"}) })
	})
	if code != -1 || !clitest.FileExists(dir, "cmd/oapp/operations.gen.go") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}

// strconv.Quote writes a byte that is not UTF-8 as \xff, which YAML 1.2
// (and core/yaml) reads as U+00FF, so a printed entry for a --out path
// holding such a byte read back as a different directory. A value YAML
// cannot carry is refused instead of printed.
func TestCLIEntryYAMLRefusesNonUTF8(t *testing.T) {
	for name, opts := range map[string]CliOptions{
		"output": {Binary: "shop", OutDir: "cmd/\xffshop", ApiPrefix: "api"},
		"binary": {Binary: "sh\xffop", OutDir: "cmd/shop", ApiPrefix: "api"},
		"prefix": {Binary: "shop", OutDir: "cmd/shop", ApiPrefix: "a\xffpi"},
		"verbs":  {Binary: "shop", OutDir: "cmd/shop", ApiPrefix: "api", verbs: "po\xffsts=list"},
		"only":   {Binary: "shop", OutDir: "cmd/shop", ApiPrefix: "api", only: []string{"po\xffsts"}},
	} {
		t.Run(name, func(t *testing.T) {
			entry, err := cliEntryYAML(opts, opts.Binary)
			if err == nil {
				t.Fatalf("a value that is not UTF-8 was printed as an entry:\n%s", entry)
			}
		})
	}
}
