package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/framework"
)

// `gofastr generate cli` emits a customer-facing terminal client for the
// app's HTTP API: a standalone, stdlib-only `package main` under --out
// (default cmd/<binary>/, so `go install <module>/cmd/<binary>@latest`
// produces a correctly-named binary) that imports only the app's generated
// entities/client package. It is the terminal twin of the typed client — every selected
// entity gets list/get/create/update/patch/delete, the _batch verbs, and a
// live `watch` (SSE), plus login/logout commands that store a scoped API
// token. Like the blueprint, generation is one-shot owned code: it refuses
// to overwrite (--force escapes), except custom.go — the dev-owned extension
// seam — which is only ever created when absent.

// cliVerbs is the full verb set, in help order.
var cliVerbs = []string{
	"list", "get", "create", "update", "patch", "delete",
	"batch-create", "batch-update", "batch-delete", "watch",
}

// cliReservedFlags are flag names owned by the CLI itself on entity verbs;
// a field whose derived flag collides fails generation (no auto-renaming —
// the developer renames the field or excludes the entity).
var cliReservedFlags = map[string]bool{
	"url": true, "token": true, "o": true, "json": true, "param": true,
	"sort": true, "page": true, "limit": true, "cursor": true,
	"include": true, "fields": true, "q": true, "trashed": true,
	"help": true, "h": true, "with-token": true,
}

type cliOptions struct {
	outDir    string
	binary    string
	apiPrefix string
	only      []string
	exclude   []string
	verbs     string
	force     bool
	dryRun    bool
	json      bool
}

// cliField is the derived per-field model the renderers consume.
type cliField struct {
	Snake      string // declaration name (snake_case)
	Wire       string // JSON wire key (camelCase)
	Flag       string // CLI flag name (kebab-case)
	Type       string // normalized declaration type
	GoType     string // string|int|float64|bool|map[string]any
	ReadOnly   bool   // excluded from mutation flags
	Comparable bool   // gets -gt/-gte/-lt/-lte range filter flags
	Likeable   bool   // gets a -like filter flag
	Values     []string
}

// cliEntity is the derived per-entity model — the shared manifest shape a
// future SDK generator (issue #86) should consume rather than re-deriving
// from raw declarations.
type cliEntity struct {
	Struct     string // Go type name in the client package (Posts)
	Command    string // CLI command word (kebab of the table)
	Table      string // route path segment
	Verbs      []string
	Fields     []cliField
	Search     bool
	SoftDelete bool
}

type cliSpec struct {
	Binary       string
	EnvPrefix    string
	APIPrefix    string // "" or "/api" (leading slash, no trailing)
	ClientImport string
	Selection    string // flag string echoed into the header for regen
	Entities     []cliEntity
}

// runGenerateCLI implements `gofastr generate cli`.
func runGenerateCLI(args []string) {
	opts, err := parseCLIOptions(args)
	if err != nil {
		fail("%v", err)
		info("Usage: gofastr generate cli [--out=cmd/<binary>] [--binary=<name>] [--api-prefix=api] [--only=a,b] [--exclude=c] [--verbs=list,get | --verbs='posts=list,get;users=*'] [--force] [--dry-run] [--json]")
		osExit(1)
		return
	}

	decls, err := packReadEntities(".")
	if err != nil {
		fail("read entities: %v", err)
		osExit(1)
		return
	}
	if len(decls) == 0 {
		fail("no entities package found in the current directory")
		info("Run from your app root (the directory holding entities/), or generate the app first: gofastr generate --from=<blueprint>.")
		osExit(1)
		return
	}

	abs, err := filepath.Abs(".")
	if err != nil {
		fail("%v", err)
		osExit(1)
		return
	}
	modulePath, moduleRoot := findEnclosingGoMod(abs)
	if modulePath == "" {
		fail("no enclosing go.mod — the generated CLI imports your entities/client package by module path")
		osExit(1)
		return
	}
	importBase := modulePath
	if rel, relErr := filepath.Rel(moduleRoot, abs); relErr == nil && rel != "." {
		importBase = modulePath + "/" + filepath.ToSlash(rel)
	}
	if opts.binary == "" {
		opts.binary = strings.ToLower(filepath.Base(abs))
	}
	// Default layout is cmd/<binary>/ — the standard installable-main
	// convention, so `go install <module>/cmd/<binary>@latest` (public
	// modules) names the binary correctly.
	if opts.outDir == "" {
		opts.outDir = filepath.ToSlash(filepath.Join("cmd", opts.binary))
	}

	spec, err := buildCLISpec(decls, opts, importBase+"/entities/client")
	if err != nil {
		if opts.dryRun && opts.json {
			printGeneratedErrorsJSON(err)
			osExit(1)
			return
		}
		fail("%v", err)
		osExit(1)
		return
	}
	files := renderCLIFiles(spec)
	if err := validateOutputDir(opts.outDir); err != nil {
		fail("%v", err)
		osExit(1)
		return
	}

	// custom.go is the dev-owned seam: created when absent, NEVER
	// overwritten — not even under --force.
	kept := files[:0]
	for _, f := range files {
		if f.name == "custom.go" {
			if _, statErr := os.Stat(filepath.Join(opts.outDir, f.name)); statErr == nil {
				continue
			}
		}
		kept = append(kept, f)
	}
	files = kept

	if !opts.force {
		var conflicts []string
		for _, f := range files {
			if _, statErr := os.Stat(filepath.Join(opts.outDir, f.name)); statErr == nil {
				conflicts = append(conflicts, filepath.Join(opts.outDir, f.name))
			}
		}
		if len(conflicts) > 0 {
			sort.Strings(conflicts)
			cerr := fmt.Errorf("generate cli is one-shot and would overwrite existing files: %s — re-run with --force to regenerate (custom.go is always preserved)", strings.Join(conflicts, ", "))
			if opts.json {
				printGeneratedErrorsJSON(cerr)
				osExit(1)
				return
			}
			fail("%v", cerr)
			osExit(1)
			return
		}
	}

	if opts.dryRun {
		if opts.json {
			printGeneratedFilesJSON(files)
			return
		}
		info("Would generate %d file(s) in %s:", len(files), opts.outDir)
		for _, f := range files {
			fmt.Printf("    %s\n", filepath.Join(opts.outDir, f.name))
		}
		return
	}

	fileSet, err := fileSetFromGeneratedFiles(files, "cli")
	if err != nil {
		fail("CLI code generation failed: %v", err)
		osExit(1)
		return
	}
	writeOpts := codegen.WriteOptions{
		OutputRoot:   opts.outDir,
		SkipManifest: true,
		Conflict:     codegen.ConflictOverwrite,
	}
	if err := codegen.WriteFiles(fileSet, writeOpts); err != nil {
		fail("Failed to write generated files: %v", err)
		osExit(1)
		return
	}
	if opts.json {
		printGeneratedFilesJSON(files)
		return
	}
	success("Generated %d file(s) in %s", len(files), opts.outDir)
	fmt.Println()
	fmt.Println("  Next steps:")
	fmt.Printf("    go build -o %s ./%s   — build the CLI\n", spec.Binary, opts.outDir)
	fmt.Printf("    ./%s login --url https://your-app.example.com --with-token   — store a scoped API token\n", spec.Binary)
	fmt.Println("    custom.go is yours: add or override commands there; regens never touch it")
}

// splitCommaList splits a comma-separated flag value, trimming whitespace
// and dropping empty items.
func splitCommaList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseCLIOptions(args []string) (cliOptions, error) {
	opts := cliOptions{apiPrefix: "api"}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		nextValue := func() (string, bool) {
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				i++
				return args[i], true
			}
			return "", false
		}
		value := func(prefix string) (string, bool) {
			if strings.HasPrefix(arg, prefix+"=") {
				return strings.TrimPrefix(arg, prefix+"="), true
			}
			if arg == prefix {
				return nextValue()
			}
			return "", false
		}
		switch {
		case arg == "--force":
			opts.force = true
		case arg == "--dry-run":
			opts.dryRun = true
		case arg == "--json":
			opts.json = true
		default:
			if v, ok := value("--out"); ok {
				opts.outDir = v
			} else if v, ok := value("--binary"); ok {
				opts.binary = v
			} else if v, ok := value("--api-prefix"); ok {
				opts.apiPrefix = v
			} else if v, ok := value("--only"); ok {
				opts.only = splitCommaList(v)
			} else if v, ok := value("--exclude"); ok {
				opts.exclude = splitCommaList(v)
			} else if v, ok := value("--verbs"); ok {
				opts.verbs = v
			} else {
				return opts, fmt.Errorf("unknown flag: %s", arg)
			}
		}
	}
	return opts, nil
}

// parseVerbSelection parses --verbs: either a bare global allow-list
// ("list,get") or per-entity pairs ("posts=list,get;users=*"). Returns
// (global, perEntity); both nil means all verbs everywhere.
func parseVerbSelection(s string) ([]string, map[string][]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil, nil
	}
	valid := func(verbs []string) error {
		for _, v := range verbs {
			found := false
			for _, known := range cliVerbs {
				if v == known {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("--verbs: unknown verb %q (valid: %s)", v, strings.Join(cliVerbs, ", "))
			}
		}
		return nil
	}
	if !strings.Contains(s, "=") {
		verbs := splitCommaList(s)
		if err := valid(verbs); err != nil {
			return nil, nil, err
		}
		return verbs, nil, nil
	}
	per := map[string][]string{}
	for _, pair := range strings.Split(s, ";") {
		if pair = strings.TrimSpace(pair); pair == "" {
			continue
		}
		name, list, ok := strings.Cut(pair, "=")
		if !ok || strings.TrimSpace(name) == "" {
			return nil, nil, fmt.Errorf("--verbs: malformed pair %q (want entity=verb,verb or entity=*)", pair)
		}
		if strings.TrimSpace(list) == "*" {
			per[strings.ToLower(strings.TrimSpace(name))] = append([]string(nil), cliVerbs...)
			continue
		}
		verbs := splitCommaList(list)
		if err := valid(verbs); err != nil {
			return nil, nil, err
		}
		per[strings.ToLower(strings.TrimSpace(name))] = verbs
	}
	return nil, per, nil
}

// cliEntityMatches reports whether a selection name refers to decl — by
// entity name, table, or the kebab command form.
func cliEntityMatches(decl framework.EntityDeclaration, name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	table := decl.Table
	if table == "" {
		table = decl.Name
	}
	return n == strings.ToLower(decl.Name) ||
		n == strings.ToLower(table) ||
		n == strings.ToLower(strings.ReplaceAll(table, "_", "-"))
}

// cliAnyEntityMatches reports whether a selection name resolves to any
// declared entity.
func cliAnyEntityMatches(decls []framework.EntityDeclaration, name string) bool {
	for _, d := range decls {
		if cliEntityMatches(d, name) {
			return true
		}
	}
	return false
}

func buildCLISpec(decls []framework.EntityDeclaration, opts cliOptions, clientImport string) (cliSpec, error) {
	globalVerbs, perEntityVerbs, err := parseVerbSelection(opts.verbs)
	if err != nil {
		return cliSpec{}, err
	}
	// Selection names must all resolve to a declared entity — a typo'd
	// --only silently generating everything (or nothing) is a trap.
	for _, sel := range [][]string{opts.only, opts.exclude} {
		for _, name := range sel {
			if !cliAnyEntityMatches(decls, name) {
				return cliSpec{}, fmt.Errorf("--only/--exclude: no entity named %q", name)
			}
		}
	}
	for name := range perEntityVerbs {
		if !cliAnyEntityMatches(decls, name) {
			return cliSpec{}, fmt.Errorf("--verbs: no entity named %q", name)
		}
	}

	apiPrefix := strings.Trim(strings.TrimSpace(opts.apiPrefix), "/")
	if apiPrefix != "" {
		apiPrefix = "/" + apiPrefix
	}

	binary := strings.TrimSpace(opts.binary)
	if binary == "" {
		return cliSpec{}, fmt.Errorf("--binary must not be empty")
	}

	spec := cliSpec{
		Binary:       binary,
		EnvPrefix:    cliEnvPrefix(binary),
		APIPrefix:    apiPrefix,
		ClientImport: clientImport,
		Selection:    cliSelectionNote(opts),
	}

	matchesAny := func(decl framework.EntityDeclaration, names []string) bool {
		for _, name := range names {
			if cliEntityMatches(decl, name) {
				return true
			}
		}
		return false
	}
	for _, decl := range decls {
		if len(opts.only) > 0 && !matchesAny(decl, opts.only) {
			continue
		}
		if matchesAny(decl, opts.exclude) {
			continue
		}

		verbs := append([]string(nil), cliVerbs...)
		if globalVerbs != nil {
			verbs = globalVerbs
		}
		for name, v := range perEntityVerbs {
			if cliEntityMatches(decl, name) {
				verbs = v
				break
			}
		}

		ent, err := buildCLIEntity(decl, verbs)
		if err != nil {
			return cliSpec{}, err
		}
		spec.Entities = append(spec.Entities, ent)
	}
	if len(spec.Entities) == 0 {
		return cliSpec{}, fmt.Errorf("selection matches no entities — nothing to generate")
	}
	return spec, nil
}

// cliReservedCommands are command words (and scaffold file basenames) the
// generated CLI owns: an entity whose command form collides would either
// shadow a built-in command at dispatch or emit a duplicate filename —
// including custom.go, whose create-if-absent handling would then silently
// stop regenerating that entity.
var cliReservedCommands = map[string]bool{
	"main": true, "config": true, "auth": true, "output": true, "custom": true,
	"login": true, "logout": true, "version": true, "help": true,
}

// buildEntityModel derives the shared per-entity model (struct name, route
// table, wire/snake field names, filterability) that both the generated CLI
// and the generated SDKs (`gofastr generate sdk`) consume. It never fails —
// CLI-specific validation (reserved commands, flag collisions) lives in
// buildCLIEntity.
func buildEntityModel(decl framework.EntityDeclaration, verbs []string) cliEntity {
	table := decl.Table
	if table == "" {
		table = decl.Name
	}
	ent := cliEntity{
		Struct:     toCamelCase(decl.Name),
		Command:    strings.ReplaceAll(strings.ToLower(table), "_", "-"),
		Table:      table,
		Verbs:      verbs,
		Search:     len(decl.SearchFields) > 0,
		SoftDelete: decl.SoftDelete,
	}
	for _, fd := range decl.Fields {
		if fd.Name == "id" || fd.Hidden {
			continue
		}
		typ := strings.ToLower(fd.Type)
		switch typ {
		case "image", "file":
			// Uploads are multipart, which the generated client doesn't
			// speak yet — the whole field stays off the CLI surface.
			continue
		}
		f := cliField{
			Snake:    fd.Name,
			Wire:     toCamelJSON(fd.Name),
			Flag:     strings.ReplaceAll(fd.Name, "_", "-"),
			Type:     typ,
			GoType:   goTypeForField(fd.Type),
			ReadOnly: fd.ReadOnly,
			Values:   fd.Values,
		}
		switch typ {
		case "int", "integer", "float", "number", "decimal", "timestamp", "datetime", "date":
			f.Comparable = true
		case "string", "text":
			f.Likeable = true
		}
		ent.Fields = append(ent.Fields, f)
	}
	return ent
}

func buildCLIEntity(decl framework.EntityDeclaration, verbs []string) (cliEntity, error) {
	ent := buildEntityModel(decl, verbs)
	if cliReservedCommands[ent.Command] {
		return cliEntity{}, fmt.Errorf("entity %q: its command form %q collides with a CLI built-in — exclude it with --exclude=%s, or rename the table", decl.Name, ent.Command, decl.Name)
	}
	seen := map[string]string{} // flag name → field, to catch duplicates
	for _, f := range ent.Fields {
		flags := []string{f.Flag}
		if f.Comparable {
			flags = append(flags, f.Flag+"-gt", f.Flag+"-gte", f.Flag+"-lt", f.Flag+"-lte")
		}
		if f.Likeable {
			flags = append(flags, f.Flag+"-like")
		}
		for _, name := range flags {
			if cliReservedFlags[name] {
				return cliEntity{}, fmt.Errorf("entity %q: field %q derives CLI flag --%s, which is reserved — rename the field, or exclude the entity with --exclude=%s", decl.Name, f.Snake, name, decl.Name)
			}
			if prev, dup := seen[name]; dup {
				return cliEntity{}, fmt.Errorf("entity %q: fields %q and %q both derive CLI flag --%s — rename one", decl.Name, prev, f.Snake, name)
			}
			seen[name] = f.Snake
		}
	}
	return ent, nil
}

// cliEnvPrefix turns a binary name into the UPPER_SNAKE env-var prefix:
// "my-app" → "MY_APP".
func cliEnvPrefix(binary string) string {
	var sb strings.Builder
	for _, r := range strings.ToUpper(binary) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('_')
		}
	}
	out := strings.Trim(sb.String(), "_")
	if out == "" {
		return "APP"
	}
	return out
}

// cliSelectionNote reconstructs the selection flags for the regen hint baked
// into the generated main.go header.
func cliSelectionNote(opts cliOptions) string {
	var parts []string
	if opts.binary != "" {
		parts = append(parts, "--binary="+opts.binary)
	}
	if opts.apiPrefix != "api" {
		parts = append(parts, "--api-prefix="+opts.apiPrefix)
	}
	if len(opts.only) > 0 {
		parts = append(parts, "--only="+strings.Join(opts.only, ","))
	}
	if len(opts.exclude) > 0 {
		parts = append(parts, "--exclude="+strings.Join(opts.exclude, ","))
	}
	if opts.verbs != "" {
		parts = append(parts, "--verbs='"+opts.verbs+"'")
	}
	if opts.outDir != "" && opts.outDir != filepath.ToSlash(filepath.Join("cmd", opts.binary)) {
		parts = append(parts, "--out="+opts.outDir)
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, " ")
}

// ---------------------------------------------------------------------------
// Renderers
// ---------------------------------------------------------------------------

func renderCLIFiles(spec cliSpec) []generatedFile {
	files := []generatedFile{
		{name: "main.go", content: renderCLIMain(spec)},
		{name: "config.go", content: renderCLIConfig(spec)},
		{name: "auth.go", content: renderCLIAuth(spec)},
		{name: "output.go", content: renderCLIOutput(spec)},
		{name: "custom.go", content: renderCLICustom(spec)},
	}
	for _, ent := range spec.Entities {
		if len(ent.Verbs) == 0 {
			continue
		}
		files = append(files, generatedFile{
			name:    ent.Command + ".go",
			content: renderCLIEntityFile(spec, ent),
		})
	}
	return files
}

func renderCLIMain(spec cliSpec) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, `// %s is a terminal client for your app's HTTP API, generated by
// `+"`gofastr generate cli%s`"+`. The code is yours to own and edit; to
// regenerate from the current entity set, re-run that command with --force
// (custom.go — your extension seam — is never overwritten).
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

const (
	binaryName = %q
	envPrefix  = %q
	apiPrefix  = %q
)

// command is one dispatchable CLI command. name is the space-joined path
// ("login", %q); the longest match wins, and entries from customCommands()
// (custom.go) override generated ones with the same name.
type command struct {
	name    string
	summary string
	run     func(args []string) int
}

func main() { os.Exit(run(os.Args[1:])) }

// commandMap merges customCommands() over the generated set — a custom
// entry with a generated name replaces it.
func commandMap() map[string]command {
	cmds := map[string]command{}
	for _, c := range builtinCommands() {
		cmds[c.name] = c
	}
	for _, c := range customCommands() {
		cmds[c.name] = c
	}
	return cmds
}

func run(args []string) int {
	cmds := commandMap()
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printUsage(cmds)
		return 0
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Printf("%%s (generated by gofastr)\n", binaryName)
		return 0
	}
	if len(args) >= 2 {
		if c, ok := cmds[args[0]+" "+args[1]]; ok {
			return c.run(args[2:])
		}
	}
	if c, ok := cmds[args[0]]; ok {
		return c.run(args[1:])
	}
	fmt.Fprintf(os.Stderr, "%%s: unknown command %%q\n\n", binaryName, strings.Join(args[:min(2, len(args))], " "))
	printUsage(cmds)
	return 2
}

func builtinCommands() []command {
	cmds := []command{
		{name: "login", summary: "store the server URL and a scoped API token", run: runLogin},
		{name: "logout", summary: "remove the stored API token", run: runLogout},
	}
`, spec.Binary, spec.Selection, spec.Binary, spec.EnvPrefix, spec.APIPrefix, exampleCommandName(spec))
	for _, ent := range spec.Entities {
		if len(ent.Verbs) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "\tcmds = append(cmds, %sCommands()...)\n", lowerFirst(ent.Struct))
	}
	sb.WriteString(`	return cmds
}

func printUsage(cmds map[string]command) {
	fmt.Printf("Usage: %s <command> [args]\n\nCommands:\n", binaryName)
	names := make([]string, 0, len(cmds))
	for name := range cmds {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Printf("  %-28s %s\n", name, cmds[name].summary)
	}
	fmt.Printf("\nConnection: --url/--token flags, %s_URL/%s_TOKEN env vars, or ` + "`%s login`" + `.\n", envPrefix, envPrefix, binaryName)
}

// groupUsage prints one command group's subcommands (the bare entity
// command lands here). A stray argument means an unknown subcommand:
// usage still prints, exit is 2.
func groupUsage(group string, args []string) int {
	code := 0
	if len(args) > 0 && args[0] != "--help" && args[0] != "-h" && args[0] != "help" {
		fmt.Fprintf(os.Stderr, "%s %s: unknown subcommand %q\n\n", binaryName, group, args[0])
		code = 2
	}
	cmds := commandMap()
	fmt.Printf("Usage: %s %s <subcommand> [flags]\n\nSubcommands:\n", binaryName, group)
	names := make([]string, 0, len(cmds))
	for name := range cmds {
		if strings.HasPrefix(name, group+" ") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Printf("  %-28s %s\n", name, cmds[name].summary)
	}
	return code
}
`)
	return sb.String()
}

// exampleCommandName picks a real two-word command for the doc comment.
func exampleCommandName(spec cliSpec) string {
	for _, ent := range spec.Entities {
		if len(ent.Verbs) > 0 {
			return ent.Command + " " + ent.Verbs[0]
		}
	}
	return "entity list"
}

func renderCLIConfig(spec cliSpec) string {
	return `package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// storedConfig is what ` + "`" + spec.Binary + ` login` + "`" + ` persists: the server URL and a
// scoped API token, at <user-config-dir>/` + spec.Binary + `/config.json (0600).
type storedConfig struct {
	URL   string ` + "`json:\"url,omitempty\"`" + `
	Token string ` + "`json:\"token,omitempty\"`" + `
}

func configPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, binaryName, "config.json"), nil
}

// loadConfig returns the stored config, or the zero value when there is
// none — a missing or unreadable file is not an error, it just means the
// caller falls through to flags/env.
func loadConfig() storedConfig {
	var cfg storedConfig
	path, err := configPath()
	if err != nil {
		return cfg
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	return cfg
}

func saveConfig(cfg storedConfig) (string, error) {
	path, err := configPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	// 0600: the file holds a bearer credential.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
`
}

func renderCLIAuth(spec cliSpec) string {
	return `package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// parseOrHelp parses args, mapping --help to exit 0 and bad flags to 2.
func parseOrHelp(fs *flag.FlagSet, args []string) (ok bool, code int) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return false, 0
		}
		return false, 2
	}
	return true, 0
}

// runLogin stores the server URL and an API token. Tokens are minted in the
// app (a logged-in browser session POSTs /auth/tokens); the CLI only stores
// one. --with-token reads it from stdin so it never appears in shell history
// or on screen:
//
//	echo "$TOKEN" | ` + spec.Binary + ` login --url https://app.example.com --with-token
func runLogin(args []string) int {
	fs := newFlagSet("login")
	urlF := fs.String("url", "", "server URL to store (e.g. https://app.example.com)")
	withToken := fs.Bool("with-token", false, "read the API token from stdin")
	if ok, code := parseOrHelp(fs, args); !ok {
		return code
	}
	cfg := loadConfig()
	if *urlF != "" {
		cfg.URL = strings.TrimRight(strings.TrimSpace(*urlF), "/")
	}
	if cfg.URL == "" {
		fmt.Fprintf(os.Stderr, "no server URL: pass --url https://your-app.example.com\n")
		return 2
	}
	var token string
	if *withToken {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		token = strings.TrimSpace(string(data))
	} else {
		// No termios in the stdlib, so interactive input echoes; warn and
		// point at the pipe-friendly path.
		fmt.Fprintf(os.Stderr, "Paste an API token minted in the app (input will echo — prefer --with-token via a pipe): ")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		token = strings.TrimSpace(line)
	}
	if token == "" {
		fmt.Fprintln(os.Stderr, "no token provided")
		return 2
	}
	cfg.Token = token
	path, err := saveConfig(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("Logged in to %s — config stored at %s\n", cfg.URL, path)
	return 0
}

// runLogout removes the stored token (the URL is kept for the next login).
func runLogout(args []string) int {
	fs := newFlagSet("logout")
	if ok, code := parseOrHelp(fs, args); !ok {
		return code
	}
	cfg := loadConfig()
	if cfg.Token == "" {
		fmt.Println("not logged in")
		return 0
	}
	cfg.Token = ""
	if _, err := saveConfig(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("Logged out — stored token removed. Revoke it in the app to invalidate it server-side.")
	return 0
}
`
}

func renderCLIOutput(spec cliSpec) string {
	return `package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	client "` + spec.ClientImport + `"
)

// global carries the per-invocation context and the authenticated API
// client. parseGlobals builds it after the verb registered its own flags.
type global struct {
	ctx    context.Context
	client *client.Client
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(binaryName+" "+name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

// parseGlobals registers the connection flags, parses args, and builds the
// client. Resolution order: flag > env > stored config. A nil *global means
// don't proceed: exit 0 for --help, 2 for usage/resolution failures.
func parseGlobals(fs *flag.FlagSet, args []string) (*global, int) {
	urlF := fs.String("url", "", "server URL (default $"+envPrefix+"_URL, then stored config)")
	tokenF := fs.String("token", "", "API token (default $"+envPrefix+"_TOKEN, then stored config)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, 0
		}
		return nil, 2
	}
	cfg := loadConfig()
	base := firstNonEmpty(*urlF, os.Getenv(envPrefix+"_URL"), cfg.URL)
	if base == "" {
		fmt.Fprintf(os.Stderr, "no server URL: pass --url, set %s_URL, or run ` + "`%s login`" + `\n", envPrefix, binaryName)
		return nil, 2
	}
	token := firstNonEmpty(*tokenF, os.Getenv(envPrefix+"_TOKEN"), cfg.Token)
	c := client.NewClient(strings.TrimRight(base, "/")+apiPrefix, nil)
	c.Token = token
	configureClient(c)
	return &global{ctx: context.Background(), client: c}, 0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// singleResponse / listResponse mirror the server's envelopes with raw maps,
// so output is presence-faithful (a typed struct's omitempty would hide
// zero-valued fields from display).
type singleResponse struct {
	Data map[string]any ` + "`json:\"data\"`" + `
}

type listResponse struct {
	Data       []map[string]any ` + "`json:\"data\"`" + `
	Total      int              ` + "`json:\"total\"`" + `
	Page       int              ` + "`json:\"page\"`" + `
	PerPage    int              ` + "`json:\"perPage\"`" + `
	TotalPages int              ` + "`json:\"totalPages\"`" + `
	Cursor     string           ` + "`json:\"cursor\"`" + `
	HasMore    bool             ` + "`json:\"hasMore\"`" + `
}

func printJSON(v any) int {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(string(data))
	return 0
}

// printListTable renders rows as an aligned table. keys are the JSON wire
// keys in column order; headers the matching titles.
func printListTable(headers, keys []string, rows []map[string]any) {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(headers, "\t"))
	for _, row := range rows {
		cells := make([]string, len(keys))
		for i, k := range keys {
			if v, ok := row[k]; ok && v != nil {
				cells[i] = fmt.Sprintf("%v", v)
			}
		}
		fmt.Fprintln(w, strings.Join(cells, "\t"))
	}
	w.Flush()
}

// apiFail prints err and maps it to an exit code: 4 for auth failures
// (401/403), 1 for every other API or transport error.
func apiFail(err error) int {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		fmt.Fprintln(os.Stderr, apiErr.Error())
		if apiErr.Status == 401 || apiErr.Status == 403 {
			fmt.Fprintf(os.Stderr, "auth failed — mint a token in the app and run ` + "`%s login`" + `\n", binaryName)
			return 4
		}
		return 1
	}
	fmt.Fprintln(os.Stderr, err)
	return 1
}

// takeID pops the leading positional argument: <verb> <id> [flags].
func takeID(verb string, args []string) (string, []string, bool) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintf(os.Stderr, "usage: %s %s <id> [flags]\n", binaryName, verb)
		return "", nil, false
	}
	return args[0], args[1:], true
}

// readJSONArg resolves a --json value: "-" reads stdin, "@path" reads a
// file, anything else is inline JSON.
func readJSONArg(v string) ([]byte, error) {
	switch {
	case v == "-":
		return io.ReadAll(os.Stdin)
	case strings.HasPrefix(v, "@"):
		return os.ReadFile(strings.TrimPrefix(v, "@"))
	default:
		return []byte(v), nil
	}
}

// buildBody assembles a mutation payload: either the --json argument
// verbatim, or a map built from the explicitly-set field flags (via
// fs.Visit, so unset flags stay absent and PATCH semantics hold). The two
// sources are mutually exclusive.
func buildBody(fs *flag.FlagSet, jsonArg string, apply func(flagName string, body map[string]any) error) (map[string]any, int) {
	var visited []string
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "url", "token", "json", "o":
			return
		}
		visited = append(visited, f.Name)
	})
	if jsonArg != "" {
		if len(visited) > 0 {
			fmt.Fprintln(os.Stderr, "--json and field flags are mutually exclusive")
			return nil, 2
		}
		raw, err := readJSONArg(jsonArg)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return nil, 2
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			fmt.Fprintf(os.Stderr, "--json: %v\n", err)
			return nil, 2
		}
		return body, 0
	}
	if len(visited) == 0 {
		fmt.Fprintln(os.Stderr, "nothing to send: pass field flags or --json")
		return nil, 2
	}
	body := map[string]any{}
	for _, name := range visited {
		if err := apply(name, body); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return nil, 2
		}
	}
	return body, 0
}

// readJSONArrayArg reads a --json value that must be a JSON array (batch
// verbs), returned raw so element shapes pass through untouched.
func readJSONArrayArg(v string) (json.RawMessage, int) {
	if v == "" {
		fmt.Fprintln(os.Stderr, "batch verbs need --json with a JSON array (inline, @file, or - for stdin)")
		return nil, 2
	}
	raw, err := readJSONArg(v)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, 2
	}
	var probe []json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		fmt.Fprintf(os.Stderr, "--json: expected a JSON array: %v\n", err)
		return nil, 2
	}
	return json.RawMessage(raw), 0
}

// doBatch sends a _batch request. The server answers a rolled-back batch
// with 400 and the same {committed, results[]} envelope, so that case is
// decoded and returned as a response — printBatch then surfaces it on
// stdout and exit code 1 — rather than treated as a transport error.
func doBatch(g *global, method, path string, body any) (client.BatchResponse, int) {
	var resp client.BatchResponse
	if err := g.client.Do(g.ctx, method, path, body, &resp); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 400 {
			if json.Unmarshal(apiErr.Body, &resp) == nil && len(resp.Results) > 0 {
				return resp, 0
			}
		}
		return resp, apiFail(err)
	}
	return resp, 0
}

// printBatch prints the batch envelope; a rolled-back batch exits 1 so
// scripts can gate on success.
func printBatch(resp client.BatchResponse) int {
	if code := printJSON(resp); code != 0 {
		return code
	}
	if !resp.Committed {
		return 1
	}
	return 0
}

// paramFlags collects repeatable --param key=value pairs — the escape hatch
// for query params the generated flags don't cover (e.g. created_at_gt on
// timestamp-managed columns).
type paramFlags struct {
	pairs [][2]string
}

func (p *paramFlags) String() string { return "" }

func (p *paramFlags) Set(v string) error {
	key, value, ok := strings.Cut(v, "=")
	if !ok || key == "" {
		return fmt.Errorf("--param wants key=value, got %q", v)
	}
	p.pairs = append(p.pairs, [2]string{key, value})
	return nil
}
`
}

func renderCLICustom(spec cliSpec) string {
	return `package main

import (
	client "` + spec.ClientImport + `"
)

// This file is yours — ` + "`gofastr generate cli --force`" + ` never overwrites it.

// customCommands is merged OVER the generated command set: an entry whose
// name matches a generated command ("` + exampleCommandName(spec) + `") replaces it, and new
// names become new commands. To wrap rather than replace a generated verb,
// call its run function (runPostsList-style) from your own.
func customCommands() []command {
	return nil
}

// configureClient runs on every request-bearing command right after the
// client is built — set default headers via a custom c.HTTP transport,
// tweak timeouts, or point at a mock during tests.
func configureClient(c *client.Client) {}
`
}

func renderCLIEntityFile(spec cliSpec, ent cliEntity) string {
	var sb strings.Builder
	has := func(verb string) bool {
		for _, v := range ent.Verbs {
			if v == verb {
				return true
			}
		}
		return false
	}

	hasJSONField := false
	for _, f := range ent.Fields {
		if f.GoType == "map[string]any" && !f.ReadOnly {
			hasJSONField = true
		}
	}
	hasMutation := has("create") || has("update") || has("patch")
	needsHTTP := has("list") || has("get") || has("delete") || hasMutation ||
		has("batch-create") || has("batch-update") || has("batch-delete")
	needsJSONImport := hasJSONField && hasMutation
	needsFmt := has("list") || has("delete") || has("batch-delete") || has("watch") || needsJSONImport
	// net/url: list builds query params; every id-addressed verb path-escapes
	// the positional id.
	needsURLValues := has("list") || has("get") || has("delete") || has("update") || has("patch")
	needsSignal := has("watch")

	sb.WriteString("package main\n\nimport (\n")
	if needsJSONImport {
		sb.WriteString("\t\"encoding/json\"\n")
	}
	if needsFmt {
		sb.WriteString("\t\"fmt\"\n")
	}
	if needsHTTP {
		sb.WriteString("\t\"net/http\"\n")
	}
	if needsURLValues {
		sb.WriteString("\t\"net/url\"\n")
	}
	if needsSignal {
		sb.WriteString("\t\"os\"\n\t\"os/signal\"\n")
	}
	sb.WriteString(")\n\n")

	// Command table. The bare entity command prints its subcommand list.
	fmt.Fprintf(&sb, "func %sCommands() []command {\n\treturn []command{\n", lowerFirst(ent.Struct))
	fmt.Fprintf(&sb, "\t\t{name: %q, summary: %q, run: func(args []string) int { return groupUsage(%q, args) }},\n",
		ent.Command, "manage "+ent.Table+" (run for subcommands)", ent.Command)
	summaries := map[string]string{
		"list":         "list " + ent.Table + " (filters, sort, pagination)",
		"get":          "fetch one record by id",
		"create":       "create a record from field flags or --json",
		"update":       "update fields on a record (PUT)",
		"patch":        "patch fields on a record (PATCH)",
		"delete":       "delete a record by id",
		"batch-create": "create up to 100 records atomically (--json array)",
		"batch-update": "patch up to 100 records atomically (--json array)",
		"batch-delete": "delete ids atomically (positional ids)",
		"watch":        "stream live create/update/delete events (SSE)",
	}
	for _, verb := range cliVerbs {
		if !has(verb) {
			continue
		}
		fmt.Fprintf(&sb, "\t\t{name: %q, summary: %q, run: run%s%s},\n",
			ent.Command+" "+verb, summaries[verb], ent.Struct, verbFuncSuffix(verb))
	}
	sb.WriteString("\t}\n}\n\n")

	if has("list") {
		renderCLIListVerb(&sb, ent)
	}
	if has("get") {
		fmt.Fprintf(&sb, `func run%sGet(args []string) int {
	id, rest, ok := takeID(%q, args)
	if !ok {
		return 2
	}
	fs := newFlagSet(%q)
	g, code := parseGlobals(fs, rest)
	if g == nil {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodGet, "/%s/"+url.PathEscape(id), nil, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

`, ent.Struct, ent.Command+" get", ent.Command+" get", ent.Table)
	}
	if has("create") {
		renderCLIMutationVerb(&sb, ent, "create")
	}
	if has("update") {
		renderCLIMutationVerb(&sb, ent, "update")
	}
	if has("patch") {
		renderCLIMutationVerb(&sb, ent, "patch")
	}
	if has("delete") {
		fmt.Fprintf(&sb, `func run%sDelete(args []string) int {
	id, rest, ok := takeID(%q, args)
	if !ok {
		return 2
	}
	fs := newFlagSet(%q)
	g, code := parseGlobals(fs, rest)
	if g == nil {
		return code
	}
	if err := g.client.Do(g.ctx, http.MethodDelete, "/%s/"+url.PathEscape(id), nil, nil); err != nil {
		return apiFail(err)
	}
	fmt.Printf("deleted %%s\n", id)
	return 0
}

`, ent.Struct, ent.Command+" delete", ent.Command+" delete", ent.Table)
	}
	if has("batch-create") {
		renderCLIBatchJSONVerb(&sb, ent, "batch-create", "BatchCreate", "items", "POST")
	}
	if has("batch-update") {
		renderCLIBatchJSONVerb(&sb, ent, "batch-update", "BatchUpdate", "items", "PATCH")
	}
	if has("batch-delete") {
		fmt.Fprintf(&sb, `// run%sBatchDelete deletes the positional ids in one transaction. Ids may
// appear before or after flags — flag.Parse stops at the first positional,
// so the trailing ones are collected from fs.Args().
func run%sBatchDelete(args []string) int {
	var ids []string
	for len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		ids = append(ids, args[0])
		args = args[1:]
	}
	fs := newFlagSet(%q)
	g, code := parseGlobals(fs, args)
	if g == nil {
		return code
	}
	for _, id := range fs.Args() {
		if id != "" && id[0] == '-' {
			fmt.Println(binaryName + " %s: flags must precede trailing ids (got " + id + " after an id)")
			return 2
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		fmt.Println("usage: " + binaryName + " %s <id> [id...]")
		return 2
	}
	resp, code := doBatch(g, http.MethodDelete, "/%s/_batch", map[string]any{"ids": ids})
	if code != 0 {
		return code
	}
	return printBatch(resp)
}

`, ent.Struct, ent.Struct, ent.Command+" batch-delete", ent.Command+" batch-delete", ent.Command+" batch-delete", ent.Table)
	}
	if has("watch") {
		fmt.Fprintf(&sb, `// run%sWatch streams the live event feed until interrupted; each event is
// one JSON line on stdout.
func run%sWatch(args []string) int {
	fs := newFlagSet(%q)
	g, code := parseGlobals(fs, args)
	if g == nil {
		return code
	}
	ctx, stop := signal.NotifyContext(g.ctx, os.Interrupt)
	defer stop()
	err := g.client.Watch%s(ctx, func(event string, data []byte) error {
		fmt.Printf("{\"event\":%%q,\"data\":%%s}\n", event, data)
		return nil
	})
	if err != nil && ctx.Err() == nil {
		return apiFail(err)
	}
	return 0
}

`, ent.Struct, ent.Struct, ent.Command+" watch", ent.Struct)
	}
	return sb.String()
}

func verbFuncSuffix(verb string) string {
	switch verb {
	case "batch-create":
		return "BatchCreate"
	case "batch-update":
		return "BatchUpdate"
	case "batch-delete":
		return "BatchDelete"
	default:
		return strings.ToUpper(verb[:1]) + verb[1:]
	}
}

func renderCLIListVerb(sb *strings.Builder, ent cliEntity) {
	fmt.Fprintf(sb, "func run%sList(args []string) int {\n", ent.Struct)
	fmt.Fprintf(sb, "\tfs := newFlagSet(%q)\n", ent.Command+" list")
	sb.WriteString(`	sortF := fs.String("sort", "", "sort field(s), comma-separated, - prefix for desc")
	page := fs.String("page", "", "page number (offset pagination)")
	limit := fs.String("limit", "", "page size")
	cursor := fs.String("cursor", "", "keyset cursor (from a prior response)")
	include := fs.String("include", "", "relations to eager-load (comma, dots for nesting)")
	fieldsF := fs.String("fields", "", "sparse field projection (comma-separated)")
	outF := fs.String("o", "json", "output format: json|table")
	var params paramFlags
	fs.Var(&params, "param", "extra query param key=value (repeatable)")
`)
	if ent.Search {
		sb.WriteString("\tqF := fs.String(\"q\", \"\", \"free-text search over the declared search fields\")\n")
	}
	if ent.SoftDelete {
		sb.WriteString("\ttrashed := fs.Bool(\"trashed\", false, \"include soft-deleted rows\")\n")
	}
	// Filter flags: eq per field, plus range/like variants.
	for _, f := range ent.Fields {
		help := "filter: " + f.Snake + " equals (comma list = IN)"
		if len(f.Values) > 0 {
			help += " [" + strings.Join(f.Values, "|") + "]"
		}
		fmt.Fprintf(sb, "\tflt%s := fs.String(%q, \"\", %q)\n", toCamelCase(f.Flag), f.Flag, help)
		if f.Comparable {
			for _, op := range []string{"gt", "gte", "lt", "lte"} {
				fmt.Fprintf(sb, "\tflt%s%s := fs.String(%q, \"\", %q)\n",
					toCamelCase(f.Flag), strings.ToUpper(op), f.Flag+"-"+op, "filter: "+f.Snake+" "+op)
			}
		}
		if f.Likeable {
			fmt.Fprintf(sb, "\tflt%sLike := fs.String(%q, \"\", %q)\n",
				toCamelCase(f.Flag), f.Flag+"-like", "filter: "+f.Snake+" contains")
		}
	}
	sb.WriteString(`	g, code := parseGlobals(fs, args)
	if g == nil {
		return code
	}
	q := url.Values{}
	set := func(key, val string) {
		if val != "" {
			q.Set(key, val)
		}
	}
	set("sort", *sortF)
	set("page", *page)
	set("limit", *limit)
	set("cursor", *cursor)
	set("include", *include)
	set("fields", *fieldsF)
`)
	if ent.Search {
		sb.WriteString("\tset(\"q\", *qF)\n")
	}
	if ent.SoftDelete {
		sb.WriteString("\tif *trashed {\n\t\tq.Set(\"trashed\", \"true\")\n\t}\n")
	}
	for _, f := range ent.Fields {
		fmt.Fprintf(sb, "\tset(%q, *flt%s)\n", f.Snake, toCamelCase(f.Flag))
		if f.Comparable {
			for _, op := range []string{"gt", "gte", "lt", "lte"} {
				fmt.Fprintf(sb, "\tset(%q, *flt%s%s)\n", f.Snake+"_"+op, toCamelCase(f.Flag), strings.ToUpper(op))
			}
		}
		if f.Likeable {
			fmt.Fprintf(sb, "\tset(%q, *flt%sLike)\n", f.Snake+"_like", toCamelCase(f.Flag))
		}
	}
	sb.WriteString(`	for _, kv := range params.pairs {
		q.Set(kv[0], kv[1])
	}
`)
	fmt.Fprintf(sb, "\tpath := \"/%s\"\n", ent.Table)
	sb.WriteString(`	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var resp listResponse
	if err := g.client.Do(g.ctx, http.MethodGet, path, nil, &resp); err != nil {
		return apiFail(err)
	}
	if *outF == "table" {
`)
	headers := []string{"id"}
	keys := []string{"id"}
	for _, f := range ent.Fields {
		headers = append(headers, f.Snake)
		keys = append(keys, f.Wire)
	}
	fmt.Fprintf(sb, "\t\tprintListTable([]string{%s}, []string{%s}, resp.Data)\n",
		quoteList(headers), quoteList(keys))
	sb.WriteString(`		if resp.Cursor != "" || resp.HasMore {
			fmt.Printf("%d rows — next cursor: %s\n", len(resp.Data), resp.Cursor)
		} else {
			fmt.Printf("page %d/%d — %d total\n", resp.Page, resp.TotalPages, resp.Total)
		}
		return 0
	}
	return printJSON(resp)
}

`)
}

// renderCLIMutationVerb emits create/update/patch: per-field flags OR --json,
// sent as a presence-faithful map so explicit zero values survive.
func renderCLIMutationVerb(sb *strings.Builder, ent cliEntity, verb string) {
	funcName := "run" + ent.Struct + verbFuncSuffix(verb)
	withID := verb != "create"
	method := map[string]string{"create": "http.MethodPost", "update": "http.MethodPut", "patch": "http.MethodPatch"}[verb]

	fmt.Fprintf(sb, "func %s(args []string) int {\n", funcName)
	if withID {
		fmt.Fprintf(sb, "\tid, rest, ok := takeID(%q, args)\n\tif !ok {\n\t\treturn 2\n\t}\n\targs = rest\n", ent.Command+" "+verb)
	}
	fmt.Fprintf(sb, "\tfs := newFlagSet(%q)\n", ent.Command+" "+verb)
	sb.WriteString("\tjsonBody := fs.String(\"json\", \"\", \"raw JSON body: inline, @file, or - for stdin\")\n")
	var writable []cliField
	for _, f := range ent.Fields {
		if f.ReadOnly {
			continue
		}
		writable = append(writable, f)
	}
	for _, f := range writable {
		help := f.Snake + " (" + f.Type + ")"
		if len(f.Values) > 0 {
			help += " [" + strings.Join(f.Values, "|") + "]"
		}
		switch f.GoType {
		case "int":
			fmt.Fprintf(sb, "\tfld%s := fs.Int(%q, 0, %q)\n", toCamelCase(f.Flag), f.Flag, help)
		case "float64":
			fmt.Fprintf(sb, "\tfld%s := fs.Float64(%q, 0, %q)\n", toCamelCase(f.Flag), f.Flag, help)
		case "bool":
			fmt.Fprintf(sb, "\tfld%s := fs.Bool(%q, false, %q)\n", toCamelCase(f.Flag), f.Flag, help)
		default: // string and json-typed fields both arrive as strings
			fmt.Fprintf(sb, "\tfld%s := fs.String(%q, \"\", %q)\n", toCamelCase(f.Flag), f.Flag, help)
		}
	}
	sb.WriteString(`	g, code := parseGlobals(fs, args)
	if g == nil {
		return code
	}
	body, code := buildBody(fs, *jsonBody, func(name string, body map[string]any) error {
		switch name {
`)
	for _, f := range writable {
		fmt.Fprintf(sb, "\t\tcase %q:\n", f.Flag)
		if f.GoType == "map[string]any" {
			fmt.Fprintf(sb, `			var v any
			if err := json.Unmarshal([]byte(*fld%s), &v); err != nil {
				return fmt.Errorf("--%s: %%w", err)
			}
			body[%q] = v
`, toCamelCase(f.Flag), f.Flag, f.Wire)
		} else {
			fmt.Fprintf(sb, "\t\t\tbody[%q] = *fld%s\n", f.Wire, toCamelCase(f.Flag))
		}
	}
	sb.WriteString(`		}
		return nil
	})
	if code != 0 {
		return code
	}
`)
	path := fmt.Sprintf("%q", "/"+ent.Table)
	if withID {
		path = fmt.Sprintf("\"/%s/\"+url.PathEscape(id)", ent.Table)
	}
	fmt.Fprintf(sb, `	var out singleResponse
	if err := g.client.Do(g.ctx, %s, %s, body, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

`, method, path)
}

// renderCLIBatchJSONVerb emits batch-create/batch-update: a --json array
// wrapped into the {items: [...]} envelope, decoded as client.BatchResponse.
func renderCLIBatchJSONVerb(sb *strings.Builder, ent cliEntity, verb, funcSuffix, key, httpMethod string) {
	methods := map[string]string{"POST": "http.MethodPost", "PATCH": "http.MethodPatch"}
	fmt.Fprintf(sb, `// run%s%s sends a --json array through the atomic _batch route. A rolled-
// back batch prints its {committed, results[]} envelope and exits 1.
func run%s%s(args []string) int {
	fs := newFlagSet(%q)
	jsonBody := fs.String("json", "", "JSON array of items: inline, @file, or - for stdin")
	g, code := parseGlobals(fs, args)
	if g == nil {
		return code
	}
	items, code := readJSONArrayArg(*jsonBody)
	if code != 0 {
		return code
	}
	resp, code := doBatch(g, %s, "/%s/_batch", map[string]any{%q: items})
	if code != 0 {
		return code
	}
	return printBatch(resp)
}

`, ent.Struct, funcSuffix, ent.Struct, funcSuffix, ent.Command+" "+verb, methods[httpMethod], ent.Table, key)
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func quoteList(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(quoted, ", ")
}
