package codegen

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func extensionTestSuffix() string {
	if runtime.GOOS == "windows" {
		return ".cmd"
	}
	return ".sh"
}

func extensionTestCommand(path string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd.exe", "/d", "/c", path}
	}
	return []string{path}
}

func writeExtensionTestScript(t *testing.T, path, response, requestPath string) {
	t.Helper()
	var body string
	if runtime.GOOS == "windows" {
		body = "@echo off\r\n"
		if requestPath == "" {
			body += "more >nul\r\n"
		} else {
			body += "more > \"" + requestPath + "\"\r\n"
		}
		body += "echo " + response + "\r\n"
	} else {
		body = "#!/bin/sh\n"
		if requestPath == "" {
			body += "cat >/dev/null\n"
		} else {
			body += "cat >\"" + requestPath + "\"\n"
		}
		body += "printf '%s' '" + response + "'\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.codegen.yml")
	if err := os.WriteFile(path, []byte(`
version: 1
codegen:
  output: gen
  clean: false
  generators:
    - name: go/entities
      source:
        type: json_dir
        path: entities
      output: entities
    - name: custom/reports
      extension: report-generator
      source:
        type: json_file
        path: reports.codegen.json
      output: reports
      config:
        package: reports
  extensions:
    - name: report-generator
      command: [./tools/report-generator]
      config:
        package: reports
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Version != 1 || cfg.Codegen.Output != "gen" {
		t.Fatalf("config = %#v", cfg)
	}
	if cfg.Codegen.Clean == nil || *cfg.Codegen.Clean {
		t.Fatalf("clean = %#v, want false", cfg.Codegen.Clean)
	}
	if len(cfg.Codegen.Generators) != 2 {
		t.Fatalf("generators = %#v", cfg.Codegen.Generators)
	}
	if got := cfg.Codegen.Generators[1].Config["package"]; got != "reports" {
		t.Fatalf("generator config package = %#v", got)
	}
	if len(cfg.Codegen.Extensions) != 1 || cfg.Codegen.Extensions[0].Command[0] != "./tools/report-generator" {
		t.Fatalf("extensions = %#v", cfg.Codegen.Extensions)
	}
}

func TestDecodeConfigRejectsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.codegen.yml")
	if err := os.WriteFile(path, []byte(`
version: 1
codegen:
  output: gen
  wat: true
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), `unknown key "wat" in codegen`) {
		t.Fatalf("LoadConfig err = %v", err)
	}
}

func TestDecodeConfigRejectsMalformedScalars(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "bad output",
			body: `
version: 1
codegen:
  output: true
`,
			want: "codegen.output must be a string",
		},
		{
			name: "bad generator name",
			body: `
version: 1
codegen:
  generators:
    - name: 123
`,
			want: "codegen.generators[0].name must be a string",
		},
		{
			name: "bad source path",
			body: `
version: 1
codegen:
  generators:
    - name: go/entities
      source:
        type: json_dir
        path: true
`,
			want: "codegen.generators[0].source.path must be a string",
		},
		{
			name: "bad version",
			body: `
version: nope
codegen:
  output: gen
`,
			want: "version must be an integer",
		},
		{
			name: "bad clean",
			body: `
version: 1
codegen:
  clean: tru
`,
			want: "codegen.clean must be a boolean",
		},
		{
			name: "bad command item",
			body: `
version: 1
codegen:
  extensions:
    - name: bad
      command: [123]
`,
			want: "command[0] must be a string",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "gofastr.codegen.yml")
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadConfig(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadConfig err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestDiscoverConfigPrefersDedicatedFile(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, filepath.Join(dir, "gofastr.yml"), "from-main")
	writeConfigFile(t, filepath.Join(dir, "gofastr.codegen.yaml"), "from-dedicated")

	got, err := DiscoverConfig(dir)
	if err != nil {
		t.Fatalf("DiscoverConfig: %v", err)
	}
	if !got.Found || filepath.Base(got.Path) != "gofastr.codegen.yaml" {
		t.Fatalf("discovery = %#v", got)
	}
	if got.Config.Codegen.Generators[0].Name != "from-dedicated" {
		t.Fatalf("generator = %#v", got.Config.Codegen.Generators)
	}
}

func TestDiscoverConfigIgnoresBlueprintWithoutCodegen(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gofastr.yml"), []byte(`
app:
  name: Demo
`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverConfig(dir)
	if err != nil {
		t.Fatalf("DiscoverConfig: %v", err)
	}
	if got.Found {
		t.Fatalf("expected no config, got %#v", got)
	}
}

func TestDiscoverConfigReadsCodegenSectionAlongsideBlueprintKeys(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gofastr.yml"), []byte(`
app:
  name: Demo
codegen:
  output: gen
  generators:
    - name: go/entities
`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverConfig(dir)
	if err != nil {
		t.Fatalf("DiscoverConfig: %v", err)
	}
	if !got.Found || got.Config.Codegen.Generators[0].Name != "go/entities" {
		t.Fatalf("discovery = %#v", got)
	}
}

func TestDiscoverConfigWalksUpward(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, filepath.Join(dir, "gofastr.codegen.yml"), "from-root")
	child := filepath.Join(dir, "nested", "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverConfig(child)
	if err != nil {
		t.Fatalf("DiscoverConfig: %v", err)
	}
	if !got.Found || got.ProjectDir != dir || got.Config.Codegen.Generators[0].Name != "from-root" {
		t.Fatalf("discovery = %#v", got)
	}
}

func TestLoadSourceJSONDirAndFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "entities"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "entities", "posts.json"), []byte(`{"name":"posts"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "one.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	dirSource, err := LoadSource(dir, SourceConfig{Type: "json_dir", Path: "entities"})
	if err != nil {
		t.Fatalf("LoadSource json_dir: %v", err)
	}
	docs := dirSource.([]JSONDocument)
	if len(docs) != 1 || docs[0].Path != "entities/posts.json" {
		t.Fatalf("docs = %#v", docs)
	}
	fileSource, err := LoadSource(dir, SourceConfig{Type: "json_file", Path: "one.json"})
	if err != nil {
		t.Fatalf("LoadSource json_file: %v", err)
	}
	if fileSource.(map[string]any)["ok"] != true {
		t.Fatalf("fileSource = %#v", fileSource)
	}
}

func TestLoadSourceRejectsSymlinkOutsideProject(t *testing.T) {
	project := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.json"), filepath.Join(project, "secret.json")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := LoadSource(project, SourceConfig{Type: "json_file", Path: "secret.json"})
	if err == nil || !strings.Contains(err.Error(), "resolves outside the project") {
		t.Fatalf("LoadSource err = %v", err)
	}
}

func TestLoadSourceRejectsJSONDirFileSymlinkOutsideProject(t *testing.T) {
	project := t.TempDir()
	outside := t.TempDir()
	entities := filepath.Join(project, "entities")
	if err := os.Mkdir(entities, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.json"), filepath.Join(entities, "leak.json")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := LoadSource(project, SourceConfig{Type: "json_dir", Path: "entities"})
	if err == nil || !strings.Contains(err.Error(), "resolves outside the project") {
		t.Fatalf("LoadSource err = %v", err)
	}
}

func TestFileSetRejectsUnsafePathsAndCollisions(t *testing.T) {
	files := NewFileSet()
	if err := files.Add(GeneratedFile{Path: "../x.go", Content: "x", Owner: "a"}); err == nil {
		t.Fatal("unsafe path accepted")
	}
	if err := files.Add(GeneratedFile{Path: "x.go", Content: "one", Owner: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := files.Add(GeneratedFile{Path: "x.go", Content: "two", Owner: "b"}); err == nil {
		t.Fatal("collision accepted")
	}
}

func TestWriteFilesRejectsLeafSymlink(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir("out", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("outside.go", []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../outside.go", filepath.Join("out", "owned.go")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	files := NewFileSet()
	if err := files.Add(GeneratedFile{Path: "owned.go", Content: "new"}); err != nil {
		t.Fatal(err)
	}
	err = WriteFiles(files, WriteOptions{OutputRoot: "out"})
	if err == nil || !strings.Contains(err.Error(), "refusing to write through symlink") {
		t.Fatalf("WriteFiles err = %v", err)
	}
	data, err := os.ReadFile("outside.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("outside target changed: %q", data)
	}
}

func TestWriteFilesRejectsManifestSymlink(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir("out", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("outside.json", []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../outside.json", filepath.Join("out", ManifestName)); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	files := NewFileSet()
	if err := files.Add(GeneratedFile{Path: "owned.go", Content: "new"}); err != nil {
		t.Fatal(err)
	}
	err = WriteFiles(files, WriteOptions{OutputRoot: "out", Clean: true})
	if err == nil || !strings.Contains(err.Error(), "refusing to write through symlink") {
		t.Fatalf("WriteFiles err = %v", err)
	}
}

func TestWriteFilesRejectsRootSymlinkBeforeMkdirAll(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, "out"); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	files := NewFileSet()
	if err := files.Add(GeneratedFile{Path: "x.go", Content: "package x\n"}); err != nil {
		t.Fatal(err)
	}
	err = WriteFiles(files, WriteOptions{OutputRoot: filepath.Join("out", "nested")})
	if err == nil || !strings.Contains(err.Error(), "refusing to write through symlink") {
		t.Fatalf("WriteFiles err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "nested")); !os.IsNotExist(err) {
		t.Fatalf("WriteFiles created directory through symlink: %v", err)
	}
}

func TestWriteFilesCleansManifestOnly(t *testing.T) {
	dir := t.TempDir()
	old := NewFileSet()
	if err := old.Add(GeneratedFile{Path: "old.go", Content: "old"}); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := WriteFiles(old, WriteOptions{OutputRoot: "out", Clean: false}); err != nil {
		t.Fatalf("first WriteFiles: %v", err)
	}
	if err := os.WriteFile(filepath.Join("out", "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	next := NewFileSet()
	if err := next.Add(GeneratedFile{Path: "new.go", Content: "new"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteFiles(next, WriteOptions{OutputRoot: "out", Clean: true}); err != nil {
		t.Fatalf("second WriteFiles: %v", err)
	}
	if _, err := os.Stat(filepath.Join("out", "old.go")); !os.IsNotExist(err) {
		t.Fatalf("old generated file still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join("out", "keep.txt")); err != nil {
		t.Fatalf("unowned file was removed: %v", err)
	}
}

func TestWriteFilesRejectsUnsupportedManifestVersion(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir("out", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("out", ManifestName), []byte(`{"version":999,"files":["old.go"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	files := NewFileSet()
	if err := files.Add(GeneratedFile{Path: "new.go", Content: "new"}); err != nil {
		t.Fatal(err)
	}
	err = WriteFiles(files, WriteOptions{OutputRoot: "out", Clean: true})
	if err == nil || !strings.Contains(err.Error(), "manifest version 999 is not supported") {
		t.Fatalf("WriteFiles err = %v", err)
	}
}

func TestRegistryRunValidatesDirectConfig(t *testing.T) {
	cfg := Config{Version: 1, Codegen: CodegenConfig{
		Output: "gen",
		Generators: []GeneratorConfig{
			{ID: "same", Name: "one"},
			{ID: "same", Name: "two"},
		},
	}}
	_, err := NewRegistry().Run(context.Background(), t.TempDir(), cfg)
	if err == nil || !strings.Contains(err.Error(), `duplicate generator id "same"`) {
		t.Fatalf("Run err = %v", err)
	}
}

func TestRegistryRunsInProcessExtensionWithoutCommandConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "input.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Version: 1, Codegen: CodegenConfig{
		Output: "gen",
		Generators: []GeneratorConfig{{
			Name:      "custom/in-process",
			Extension: "in-process",
			Source:    SourceConfig{Type: "json_file", Path: "input.json"},
		}},
	}}
	reg := NewRegistry()
	if err := reg.RegisterExtension(staticExtension{name: "in-process", files: []GeneratedFile{{Path: "ok.go", Content: "package ok\n"}}}); err != nil {
		t.Fatal(err)
	}
	ctx, err := reg.Run(context.Background(), dir, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := ctx.Files.All()[0].Path; got != "ok.go" {
		t.Fatalf("path = %q", got)
	}
}

func TestRegistryOverwritesExtensionResponseOwner(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "input.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Version: 1, Codegen: CodegenConfig{
		Output: "gen",
		Generators: []GeneratorConfig{{
			Name:      "custom/spoof",
			Extension: "owner-extension",
			Source:    SourceConfig{Type: "json_file", Path: "input.json"},
		}},
	}}
	reg := NewRegistry()
	if err := reg.RegisterExtension(staticExtension{
		name:  "owner-extension",
		files: []GeneratedFile{{Path: "owned.go", Content: "package owned\n", Owner: "spoofed-owner"}},
	}); err != nil {
		t.Fatal(err)
	}
	ctx, err := reg.Run(context.Background(), dir, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	files := ctx.Files.All()
	if len(files) != 1 || files[0].Owner != "owner-extension" {
		t.Fatalf("files = %#v", files)
	}
}

func TestRegistryRejectsExtensionDeleteOfOtherOwner(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "input.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Version: 1, Codegen: CodegenConfig{
		Output: "gen",
		Generators: []GeneratorConfig{
			{Name: "base"},
			{
				Name:      "custom/delete",
				Extension: "deleter",
				Source:    SourceConfig{Type: "json_file", Path: "input.json"},
			},
		},
	}}
	reg := NewRegistry()
	if err := reg.RegisterGenerator(staticGenerator{name: "base", files: []GeneratedFile{{Path: "owned.go", Content: "package owned\n"}}}); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterExtension(staticExtension{name: "deleter", deletes: []string{"owned.go"}}); err != nil {
		t.Fatal(err)
	}
	_, err := reg.Run(context.Background(), dir, cfg)
	if err == nil || !strings.Contains(err.Error(), "delete collision") {
		t.Fatalf("Run err = %v", err)
	}
}

func TestRegistryRunsExternalExtension(t *testing.T) {
	dir := t.TempDir()
	requestPath := filepath.Join(dir, "gofastr-codegen-request.json")
	extPath := filepath.Join(dir, "ext"+extensionTestSuffix())
	writeExtensionTestScript(t, extPath, `{"files":[{"path":"report.go","content":"package reports\\n"}]}`, requestPath)
	if err := os.WriteFile(filepath.Join(dir, "reports.codegen.json"), []byte(`{"name":"reports"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Version: 1, Codegen: CodegenConfig{
		Output: "gen",
		Generators: []GeneratorConfig{{
			Name:      "custom/reports",
			Extension: "report-generator",
			Source:    SourceConfig{Type: "json_file", Path: "reports.codegen.json"},
			Output:    "reports",
		}},
		Extensions: []ExtensionConfig{{Name: "report-generator", Command: extensionTestCommand(extPath)}},
	}}
	reg := NewRegistry()
	if err := reg.RegisterCommandExtensions(cfg.Codegen, nil); err != nil {
		t.Fatal(err)
	}
	ctx, err := reg.Run(context.Background(), dir, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	files := ctx.Files.All()
	if len(files) != 1 || files[0].Path != "reports/report.go" {
		t.Fatalf("files = %#v", files)
	}
	data, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var req ExtensionRequest
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatalf("request json: %v", err)
	}
	if req.ProtocolVersion != ProtocolVersion || req.Phase == "" || req.Source == nil {
		t.Fatalf("request = %#v", req)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	generator := raw["generator"].(map[string]any)
	if _, ok := generator["Name"]; ok {
		t.Fatalf("extension request used Go field casing: %s", data)
	}
	if generator["name"] != "custom/reports" {
		t.Fatalf("extension request generator keys = %#v", generator)
	}
}

func TestRegistryRejectsUnknownExtensionResponseFields(t *testing.T) {
	dir := t.TempDir()
	extPath := filepath.Join(dir, "ext"+extensionTestSuffix())
	writeExtensionTestScript(t, extPath, `{"surprise":true}`, "")
	if err := os.WriteFile(filepath.Join(dir, "input.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Version: 1, Codegen: CodegenConfig{
		Output: "gen",
		Generators: []GeneratorConfig{{
			Name:      "custom/bad",
			Extension: "bad-extension",
			Source:    SourceConfig{Type: "json_file", Path: "input.json"},
		}},
		Extensions: []ExtensionConfig{{Name: "bad-extension", Command: extensionTestCommand(extPath)}},
	}}
	reg := NewRegistry()
	if err := reg.RegisterCommandExtensions(cfg.Codegen, nil); err != nil {
		t.Fatal(err)
	}
	_, err := reg.Run(context.Background(), dir, cfg)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Run err = %v", err)
	}
}

func TestRegistryRunsRelativeExtensionCommandFromProjectDir(t *testing.T) {
	dir := t.TempDir()
	tools := filepath.Join(dir, "tools")
	if err := os.Mkdir(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	extRel := filepath.Join("tools", "ext"+extensionTestSuffix())
	extPath := filepath.Join(tools, "ext"+extensionTestSuffix())
	writeExtensionTestScript(t, extPath, `{"files":[{"path":"relative.go","content":"package relative\\n"}]}`, "")
	if err := os.WriteFile(filepath.Join(dir, "input.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Version: 1, Codegen: CodegenConfig{
		Output: "gen",
		Generators: []GeneratorConfig{{
			Name:      "custom/relative",
			Extension: "relative-extension",
			Source:    SourceConfig{Type: "json_file", Path: "input.json"},
		}},
		Extensions: []ExtensionConfig{{Name: "relative-extension", Command: extensionTestCommand(extRel)}},
	}}
	reg := NewRegistry()
	if err := reg.RegisterCommandExtensions(cfg.Codegen, nil); err != nil {
		t.Fatal(err)
	}
	ctx, err := reg.Run(context.Background(), dir, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := ctx.Files.All()[0].Path; got != "relative.go" {
		t.Fatalf("path = %q", got)
	}
}

func TestRegistryRunsRelativeExtensionCommandWithRelativeProjectDir(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	tools := filepath.Join(project, "tools")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	extRel := filepath.Join("tools", "ext"+extensionTestSuffix())
	extPath := filepath.Join(tools, "ext"+extensionTestSuffix())
	writeExtensionTestScript(t, extPath, `{"files":[{"path":"relative.go","content":"package relative\\n"}]}`, "")
	if err := os.WriteFile(filepath.Join(project, "input.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Version: 1, Codegen: CodegenConfig{
		Output: "gen",
		Generators: []GeneratorConfig{{
			Name:      "custom/relative",
			Extension: "relative-extension",
			Source:    SourceConfig{Type: "json_file", Path: "input.json"},
		}},
		Extensions: []ExtensionConfig{{Name: "relative-extension", Command: extensionTestCommand(extRel)}},
	}}
	reg := NewRegistry()
	if err := reg.RegisterCommandExtensions(cfg.Codegen, nil); err != nil {
		t.Fatal(err)
	}
	ctx, err := reg.Run(context.Background(), "project", cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := ctx.Files.All()[0].Path; got != "relative.go" {
		t.Fatalf("path = %q", got)
	}
}

func writeConfigFile(t *testing.T, path, generator string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(`
version: 1
codegen:
  output: gen
  generators:
    - name: `+generator+`
`), 0o644); err != nil {
		t.Fatal(err)
	}
}

type staticGenerator struct {
	name  string
	files []GeneratedFile
}

func (g staticGenerator) Name() string { return g.name }

func (g staticGenerator) Generate(context.Context, *Context, GeneratorConfig) ([]GeneratedFile, error) {
	return g.files, nil
}

type staticExtension struct {
	name    string
	files   []GeneratedFile
	deletes []string
}

func (e staticExtension) Name() string { return e.name }

func (e staticExtension) RunPhase(context.Context, string, *Context, GeneratorConfig, ExtensionConfig) (ExtensionResponse, error) {
	return ExtensionResponse{Files: e.files, Deletes: e.deletes}, nil
}
