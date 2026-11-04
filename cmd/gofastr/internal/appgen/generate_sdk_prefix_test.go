package appgen

import (
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/codegen"
)

func TestSDKAppPrefixDefaults(t *testing.T) {
	for _, test := range []struct {
		name, spec, settings, flag, want string
		flagSet                          bool
	}{
		{name: "NoSpec"},
		{name: "AppDefault", spec: "app:\n  name: repo\n", want: "/api"},
		{name: "AppCustom", spec: "app:\n  name: repo\n  api_prefix: v2\n", want: "/v2"},
		{name: "AppEmpty", spec: "app:\n  name: repo\n  api_prefix: ''\n"},
		{name: "Setting", spec: "app:\n  name: repo\n", settings: "v3", want: "/v3"},
		{name: "EmptySetting", spec: "app:\n  name: repo\n", settings: "''"},
		{name: "Flag", spec: "app:\n  name: repo\n", settings: "v3", flagSet: true, flag: "v4", want: "/v4"},
		{name: "EmptyFlag", spec: "app:\n  name: repo\n", settings: "v3", flagSet: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			t.Chdir(directory)
			if test.spec != "" {
				clitest.WriteTestFile(t, filepath.Join(directory, "gofastr.yml"), test.spec)
			}
			if test.settings != "" {
				clitest.WriteSettings(t, directory, "codegen:\n  generators:\n    - name: sdk\n      config:\n        api_prefix: "+test.settings+"\n")
			}
			var arguments []string
			if test.flagSet {
				arguments = []string{"--api-prefix=" + test.flag}
			}
			opts, err := parseSDKOptions(arguments)
			if err != nil {
				t.Fatal(err)
			}
			if err := ApplySDKConfigDefaults(&opts); err != nil {
				t.Fatal(err)
			}
			spec, err := buildSDKSpec(sdkFixtureDecls(), &opts)
			if err != nil {
				t.Fatal(err)
			}
			if spec.APIPrefix != test.want {
				t.Fatalf("prefix = %q, want %q", spec.APIPrefix, test.want)
			}
			if !test.flagSet {
				config := codegen.GeneratorConfig{Name: "sdk", Config: map[string]any{}}
				if test.settings != "" {
					prefix := test.settings
					if prefix == "''" {
						prefix = ""
					}
					config.Config["api_prefix"] = prefix
				}
				entry, err := decodeSDKEntry(-1, config)
				if err != nil {
					t.Fatal(err)
				}
				if err := applySDKAppDefaults(directory, &entry); err != nil {
					t.Fatal(err)
				}
				generated, err := buildSDKSpec(sdkFixtureDecls(), &entry)
				if err != nil {
					t.Fatal(err)
				}
				if generated.APIPrefix != test.want {
					t.Fatalf("entry prefix = %q, want %q", generated.APIPrefix, test.want)
				}
			}
		})
	}
}

func TestSDKAppPrefixProgrammaticOverride(t *testing.T) {
	directory := t.TempDir()
	clitest.WriteTestFile(t, filepath.Join(directory, "gofastr.yml"), "app:\n  name: repo\n")
	opts := SdkOptions{apiPrefix: "owned"}
	if err := applySDKAppDefaults(directory, &opts); err != nil {
		t.Fatal(err)
	}
	if opts.apiPrefix != "owned" {
		t.Fatalf("programmatic prefix overwritten: %q", opts.apiPrefix)
	}
}

func TestSDKAppPrefixMalformedSpec(t *testing.T) {
	directory := t.TempDir()
	clitest.WriteTestFile(t, filepath.Join(directory, "gofastr.yml"), "app: [not-an-app]\n")
	if err := applySDKAppDefaults(directory, &SdkOptions{}); err == nil {
		t.Fatal("malformed spec silently became an unprefixed SDK")
	}
}

func TestSDKAppPrefixFromJSONSpec(t *testing.T) {
	directory := t.TempDir()
	clitest.WriteTestFile(t, filepath.Join(directory, "gofastr.json"), `{"app": {"name": "repo"}}`)
	opts := SdkOptions{}
	if err := applySDKAppDefaults(directory, &opts); err != nil {
		t.Fatal(err)
	}
	if opts.apiPrefix != "api" {
		t.Fatalf("gofastr.json prefix = %q, want %q", opts.apiPrefix, "api")
	}
}
