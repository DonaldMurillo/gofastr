package appgen

import (
	"fmt"
	"go/token"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// `gofastr generate cli` emits a customer-facing terminal client for the
// app's HTTP API: a standalone, stdlib-only `package main` under --out
// (default cmd/<binary>/, so `go install <module>/cmd/<binary>@latest`
// produces a correctly-named binary) that imports only the app's generated
// entities/client package. It is the terminal twin of the typed client: every selected
// entity gets list/get/create/update/patch/delete, the _batch verbs, and a
// live `watch` (SSE), plus login/logout commands that store a scoped API
// token. The output is derived: every run rebuilds the *.gen.go files from
// the source and removes the ones it no longer produces. custom.go, the
// dev-owned extension seam, is only ever created when absent.

// cliVerbs is the full verb set, in help order.
var cliVerbs = []string{
	"list", "get", "create", "update", "patch", "delete",
	"batch-create", "batch-update", "batch-delete", "watch",
}

// cliReservedFlags are flag names owned by the CLI itself on entity verbs;
// a field whose derived flag collides fails generation (no auto-renaming;
// the developer renames the field or excludes the entity).
var cliReservedFlags = map[string]bool{
	"url": true, "token": true, "o": true, "json": true, "param": true,
	"sort": true, "page": true, "limit": true, "cursor": true,
	"include": true, "fields": true, "q": true, "trashed": true,
	"help": true, "h": true, "with-token": true,
}

// cliEndpointReservedFlags are the flags an endpoint command registers
// besides its inputs (runEndpointVerb, parseGlobals, package flag), and
// o, which the shared body builder skips.
var cliEndpointReservedFlags = map[string]bool{"url": true, "json": true, "o": true, "help": true, "h": true}

type CliOptions struct {
	OutDir      string
	Binary      string
	ApiPrefix   string
	only        []string
	exclude     []string
	verbs       string
	force       bool
	dryRun      bool
	json        bool
	FromOpenAPI string // spec file or URL; switches off the entity path
	// apiPrefixSet: --api-prefix (or the entry's api_prefix) was given, so
	// a settings default does not replace it, "" included.
	apiPrefixSet bool
	// fromSettings: run from a gofastr.settings.yml cli entry by `gofastr
	// generate` or `gofastr build`. Unchanged files are not rewritten, and
	// the run says nothing when the CLI is already current.
	fromSettings bool
	// flagSet names the selection flags given on the command line, empty
	// ones included, so a settings entry fills only the rest.
	flagSet map[string]bool
	// settingsEntry: an explicit run took defaults from a cli entry, so its
	// next steps need not print one to paste.
	settingsEntry bool
	// entryIndex is the cli entry's position in codegen.generators, for
	// errors about values it supplied.
	entryIndex int
}

// selectionSource names where a selection value came from, so a typo in
// a settings entry points at the entry, not at a flag nobody typed.
func (o CliOptions) selectionSource(flag string) string {
	if o.fromSettings || (o.settingsEntry && !o.flagSet[flag]) {
		return fmt.Sprintf("codegen.generators[%d] (cli): config.%s", o.entryIndex, flag)
	}
	return "--" + flag
}

// cliField is the derived per-field model the renderers consume.
type cliField struct {
	Snake    string // declaration name (snake_case)
	Wire     string // JSON wire key (camelCase)
	Flag     string // CLI flag name (kebab-case)
	Type     string // normalized declaration type
	GoType   string // string|int|float64|bool|any
	ReadOnly bool   // excluded from mutation flags
	NoQuery  bool   // excluded from ALL filter flags (server rejects them)
	// FilterOps are the suffixed operator flags the list verb offers,
	// derived once from filter.FilterSuffixes ∩ filter.OpSuitsType.
	FilterOps []cliFilterOp
	// NoEq marks a type that refuses equality (a JSON column takes no
	// filter at all), so the list verb offers no bare flag for it.
	NoEq   bool
	Values []string
}

// cliFilterOp is one suffixed operator flag behind `<command> list`:
// the dash spelling joined to the field flag (--status-ne), the
// underscore spelling joined to the column for the query param
// (status_ne), and the help fragment naming the operator.
type cliFilterOp struct {
	Flag  string
	Param string
	Help  string
}

// cliOpHelp names each operator in help text.
var cliOpHelp = map[filter.FilterOp]string{
	filter.OpNe:   "not equal",
	filter.OpGt:   "greater than",
	filter.OpGte:  "greater than or equal",
	filter.OpLt:   "less than",
	filter.OpLte:  "less than or equal",
	filter.OpLike: "contains",
	filter.OpIn:   "in (comma list)",
}

// cliFilterOps derives the operator flags a field's list verb offers
// from filter.FilterSuffixes ∩ filter.OpSuitsType — the one type
// predicate every server filter surface applies — so the CLI can never
// offer a flag the list handler answers 400, nor hide one it accepts
// (the old hardcoded Comparable/Likeable lists did both). OpIn gets its
// own -in flag: the server reads a bare key as equality with the whole
// value, so a comma list on the bare flag matched the literal "a,b".
func cliFilterOps(t schema.FieldType) []cliFilterOp {
	var ops []cliFilterOp
	for _, s := range filter.FilterSuffixes {
		if !filter.OpSuitsType(s.Op, t) {
			continue
		}
		ops = append(ops, cliFilterOp{
			Flag:  "-" + strings.TrimPrefix(s.Suffix, "_"),
			Param: s.Suffix,
			Help:  cliOpHelp[s.Op],
		})
	}
	return ops
}

// cliTransition is one derived non-system move: the subcommand word (the
// key), its route segment, and the help line. System moves have no route
// and appear nowhere in generated clients.
type cliTransition struct {
	Key     string // the move's key: subcommand word and route segment
	Summary string // the CLI help line
}

// cliEntity is the derived per-entity model: the shared manifest shape a
// future SDK generator (issue #86) should consume rather than re-deriving
// from raw declarations.
type cliEntity struct {
	Struct      string   // Go type name in the client package (Posts)
	Command     string   // CLI command word (kebab of the table)
	Table       string   // route path segment
	Keys        []string // key columns in order: one positional argument each
	Verbs       []string
	Fields      []cliField
	Uploads     []cliField      // Image and File fields: a file flag each on create/update/patch
	Transitions []cliTransition // non-system state moves, one command each
	Endpoints   []cliEndpoint   // gofastr.settings.yml endpoints, one command each
	Search      bool
	SoftDelete  bool
}

// cliEndpoint is one settings endpoint's command: `<entity> <Command>`
// takes the path values as positional arguments and the rest as flags.
type cliEndpoint struct {
	Command string   // kebab of the endpoint name
	Func    string   // CamelCase, for the generated wrapper
	Method  string   // HTTP method
	Segs    []string // route segments, "{name}" for a path value
	Query   bool     // flags go in the query string, not a JSON body
	Fields  []cliEndpointField
	Summary string
}

// cliEndpointField is one flag of an endpoint command.
type cliEndpointField struct {
	Flag     string
	Wire     string
	Kind     string // fieldKind constant
	Usage    string
	Required bool
}

type cliSpec struct {
	Binary       string
	EnvPrefix    string
	APIPrefix    string // "" or "/api" (leading slash, no trailing)
	ClientImport string
	Entities     []cliEntity

	// --from-openapi mode (see generate_cli_openapi.go): Ops replace
	// Entities, SelfClient emits internal/client/ instead of importing
	// the entity SDK, DefaultURL seeds the server URL from servers[0],
	// and TokenHeader/TokenPrefix carry the spec's security scheme.
	Ops         []cliOp
	SelfClient  bool
	DefaultURL  string
	TokenHeader string
	TokenPrefix string
}

// RunGenerateCLI implements `gofastr generate cli`.
func RunGenerateCLI(args []string) {
	opts, err := parseCLIOptions(args)
	if err != nil {
		cli.Fail("%v", err)
		cli.Info("Usage: gofastr generate cli [--from-openapi=<spec.json|url>] [--out=cmd/<binary>] [--binary=<name>] [--api-prefix=api] [--only=a,b] [--exclude=c] [--verbs=list,get | --verbs='posts=list,get;users=*'] [--force] [--dry-run] [--json]")
		cli.Info("--force only matters once, to replace an edited CLI written before the CLI became derived.")
		cli.OsExit(1)
		return
	}
	used, err := ApplyCLIConfigDefaults(&opts)
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	opts.settingsEntry = used
	generateCLI(opts)
}

// generateCLI generates the CLI from parsed options, from entities/ or from
// an OpenAPI document.
func generateCLI(opts CliOptions) {
	if opts.FromOpenAPI != "" {
		runGenerateCLIFromOpenAPI(opts)
		return
	}

	decls, err := ReadAppDecls(".")
	if err != nil {
		cli.Fail("read entities: %v", err)
		cli.OsExit(1)
		return
	}
	if len(decls) == 0 {
		cli.Fail("no entities package found in the current directory")
		cli.Info("Run from your app root (the directory holding entities/), or generate the app first: gofastr generate app <spec.yml>.")
		cli.OsExit(1)
		return
	}

	abs, err := filepath.Abs(".")
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	modulePath, moduleRoot := project.FindEnclosingGoMod(abs)
	if modulePath == "" {
		cli.Fail("no enclosing go.mod: the generated CLI imports your entities/client package by module path")
		cli.OsExit(1)
		return
	}
	importBase := modulePath
	if rel, relErr := filepath.Rel(moduleRoot, abs); relErr == nil && rel != "." {
		importBase = modulePath + "/" + filepath.ToSlash(rel)
	}
	if opts.Binary == "" {
		opts.Binary = strings.ToLower(filepath.Base(abs))
	}
	// Default layout is cmd/<binary>/, the standard installable-main
	// convention, so `go install <module>/cmd/<binary>@latest` (public
	// modules) names the binary correctly.
	if opts.OutDir == "" {
		opts.OutDir = filepath.ToSlash(filepath.Join("cmd", opts.Binary))
	}

	spec, err := buildCLISpec(decls, opts, importBase+"/entities/client")
	if err != nil {
		if opts.dryRun && opts.json {
			printGeneratedErrorsJSON(err)
			cli.OsExit(1)
			return
		}
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	emitCLIFiles(opts, spec)
}

// emitCLIFiles is the shared write tail for both generation sources. The
// CLI is derived: every run rebuilds the *.gen.go files, removes the ones
// the source no longer produces (an entity dropped, a verb deselected),
// and creates custom.go only when it is absent.
func emitCLIFiles(opts CliOptions, spec cliSpec) {
	failWith := func(err error) {
		if opts.json {
			printGeneratedErrorsJSON(err)
		} else {
			cli.Fail("%v", err)
		}
		cli.OsExit(1)
	}
	files, err := stampCLIFiles(RenderCLIFiles(spec))
	if err == nil {
		err = refuseDuplicateDecls(files)
	}
	if err != nil {
		failWith(err)
		return
	}
	if err := validateOutputDir(opts.OutDir); err != nil {
		failWith(err)
		return
	}

	// custom.go is the dev-owned seam: created when absent, never
	// overwritten. One written for the other generation mode imports a
	// client this run does not build against, so the run refuses; --force
	// moves it to custom.go.bak (never over an earlier backup) and writes
	// a fresh one.
	backupCustom := false
	if stale := staleCustomClient(opts.OutDir, opts.OutDir, spec); stale != "" {
		customPath := filepath.Join(opts.OutDir, cliCustomFile)
		if !opts.force {
			failWith(fmt.Errorf("%s imports %s, the client of the other `gofastr generate cli` mode, so it does not build beside the CLI this run generates from %s. Re-run with --force: it moves the file to %s and writes a fresh custom.go. Then copy your customCommands entries and configureClient body from the backup and delete it", customPath, stale, cliSourceName(spec), cliCustomBackup))
			return
		}
		if fileExistsUnder(opts.OutDir, cliCustomBackup) {
			failWith(fmt.Errorf("%s was written for the other `gofastr generate cli` mode, and --force would move it to %s, which already exists. Move or delete %s, then re-run", customPath, filepath.Join(opts.OutDir, cliCustomBackup), cliCustomBackup))
			return
		}
		backupCustom = true
	}
	kept := files[:0]
	for _, f := range files {
		if f.Name == cliCustomFile && !backupCustom && fileExistsUnder(opts.OutDir, f.Name) {
			continue
		}
		kept = append(kept, f)
	}
	files = kept

	removals, err := planCLIRemovals(opts.OutDir, files)
	if err != nil {
		failWith(err)
		return
	}
	if opts.fromSettings {
		// After planCLIRemovals: an unchanged file is still produced and
		// must not read as stale.
		files = dropUnchangedFiles(opts.OutDir, files)
		if len(files) == 0 && len(removals) == 0 {
			return
		}
	}
	var editedLegacy []string
	for _, r := range removals {
		if r.legacy && r.edited {
			editedLegacy = append(editedLegacy, filepath.Join(opts.OutDir, r.name))
		}
	}
	if len(editedLegacy) > 0 && !opts.force {
		failWith(fmt.Errorf("%s: written by an earlier `gofastr generate cli`, and edited since or too old to carry the marker that would show it was not. The CLI is derived now: *.gen.go files replace these and every run rebuilds them. Move the edits into custom.go (customCommands can replace any command, login and logout included), then re-run with --force to remove the old files", strings.Join(editedLegacy, ", ")))
		return
	}

	if opts.dryRun {
		if opts.json {
			printGeneratedFilesJSON(files)
			return
		}
		if backupCustom {
			cli.Info("Would move %s to %s", filepath.Join(opts.OutDir, cliCustomFile), cliCustomBackup)
		}
		cli.Info("Would generate %d file(s) in %s:", len(files), opts.OutDir)
		for _, f := range files {
			fmt.Printf("    %s\n", filepath.Join(opts.OutDir, f.Name))
		}
		if len(removals) > 0 {
			cli.Info("Would remove %d file(s) the CLI no longer produces:", len(removals))
			for _, r := range removals {
				fmt.Printf("    %s\n", filepath.Join(opts.OutDir, r.name))
			}
		}
		return
	}

	if !opts.json {
		warnForceOverwrite(editedLegacy)
		for _, f := range files {
			if existing, rerr := readFileUnder(opts.OutDir, f.Name); rerr == nil {
				warnDerivedEdit("cli", filepath.Join(opts.OutDir, f.Name), string(existing), cliSourceName(spec))
			}
		}
		for _, r := range removals {
			if !r.legacy && r.edited {
				cli.Warn("%s was edited since `gofastr generate cli` wrote it; the CLI no longer produces it, so this run removes it and the edit is lost", filepath.Join(opts.OutDir, r.name))
			}
		}
	}

	fileSet, err := FileSetFromGeneratedFiles(files, "cli")
	if err != nil {
		failWith(fmt.Errorf("CLI code generation failed: %w", err))
		return
	}
	if backupCustom {
		if err := backupCLICustom(opts.OutDir); err != nil {
			failWith(err)
			return
		}
		if !opts.json {
			cli.Warn("%s was written for the other `gofastr generate cli` mode; moved it to %s and wrote a fresh one. Copy your commands across, then delete the backup", filepath.Join(opts.OutDir, cliCustomFile), cliCustomBackup)
		}
	}
	writeOpts := codegen.WriteOptions{
		OutputRoot:   opts.OutDir,
		SkipManifest: true,
		Conflict:     codegen.ConflictOverwrite,
	}
	if err := codegen.WriteFiles(fileSet, writeOpts); err != nil {
		failWith(fmt.Errorf("failed to write generated files: %w", err))
		return
	}
	if err := removeCLIFiles(opts.OutDir, removals); err != nil {
		failWith(err)
		return
	}
	if opts.json {
		printGeneratedFilesJSON(files)
		return
	}
	if opts.fromSettings {
		cli.Success("Rebuilt %d file(s) in %s", len(files), opts.OutDir)
	} else {
		cli.Success("Generated %d file(s) in %s", len(files), opts.OutDir)
	}
	if len(removals) > 0 {
		cli.Info("Removed %d file(s) the CLI no longer produces", len(removals))
	}
	if opts.fromSettings {
		return
	}
	fmt.Println()
	fmt.Println("  Next steps:")
	fmt.Printf("    go build -o %s ./%s   : build the CLI\n", spec.Binary, opts.OutDir)
	fmt.Printf("    ./%s login --url https://your-app.example.com --with-token   : store a scoped API token\n", spec.Binary)
	fmt.Println("    custom.go is yours: add or override commands there. The *.gen.go files are rebuilt on every run")
	if opts.settingsEntry {
		fmt.Println("    gofastr build rebuilds it from the cli entry in gofastr.settings.yml")
		return
	}
	fmt.Println("    add this entry to codegen.generators in gofastr.settings.yml and gofastr build keeps the CLI current (gofastr docs app-cli):")
	fmt.Println()
	for _, line := range strings.Split(strings.TrimRight(cliEntryYAML(opts, spec.Binary), "\n"), "\n") {
		fmt.Println("      " + line)
	}
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

func parseCLIOptions(args []string) (CliOptions, error) {
	opts := CliOptions{ApiPrefix: "api", flagSet: map[string]bool{}}
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
				opts.OutDir = v
			} else if v, ok := value("--binary"); ok {
				opts.Binary = v
			} else if v, ok := value("--api-prefix"); ok {
				opts.ApiPrefix = v
				opts.apiPrefixSet = true
			} else if v, ok := value("--only"); ok {
				opts.only = splitCommaList(v)
				opts.flagSet["only"] = true
			} else if v, ok := value("--exclude"); ok {
				opts.exclude = splitCommaList(v)
				opts.flagSet["exclude"] = true
			} else if v, ok := value("--verbs"); ok {
				opts.verbs = v
				opts.flagSet["verbs"] = true
			} else if v, ok := value("--from-openapi"); ok {
				opts.FromOpenAPI = v
				opts.flagSet["from-openapi"] = true
			} else {
				return opts, fmt.Errorf("unknown flag: %s", arg)
			}
		}
	}
	return opts, checkCLIOptions(opts)
}

// checkCLIOptions refuses option combinations that cannot mean anything,
// whether flags or a settings entry set them. --api-prefix applies in
// both modes: with --from-openapi it replaces servers[0].url's path.
func checkCLIOptions(opts CliOptions) error {
	if opts.FromOpenAPI != "" {
		if len(opts.only) > 0 || len(opts.exclude) > 0 || opts.verbs != "" {
			return fmt.Errorf("--from-openapi is spec-driven: --only/--exclude/--verbs apply to entity generation only")
		}
	}
	return nil
}

// cliAPIPrefix normalizes an API prefix to "" or "/a/b": a leading
// slash, no trailing one.
func cliAPIPrefix(p string) string {
	if p = strings.Trim(strings.TrimSpace(p), "/"); p != "" {
		return "/" + p
	}
	return ""
}

func isControlByte(r rune) bool { return r < 0x20 || r == 0x7f }

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

// cliEntityMatches reports whether a selection name refers to decl, by
// entity name, table, or the kebab command form, folding ASCII only.
func cliEntityMatches(decl framework.EntityDeclaration, name string) bool {
	n := asciicase.Lower(strings.TrimSpace(name))
	table := declTable(decl)
	return n == asciicase.Lower(decl.Name) ||
		n == asciicase.Lower(table) ||
		n == asciicase.Lower(strings.ReplaceAll(table, "_", "-"))
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

func buildCLISpec(decls []framework.EntityDeclaration, opts CliOptions, clientImport string) (cliSpec, error) {
	globalVerbs, perEntityVerbs, err := parseVerbSelection(opts.verbs)
	if err != nil {
		return cliSpec{}, err
	}
	// Selection names must all resolve to a declared entity: a typo'd
	// --only silently generating everything (or nothing) is a trap.
	for _, sel := range []struct {
		flag  string
		names []string
	}{{"only", opts.only}, {"exclude", opts.exclude}} {
		for _, name := range sel.names {
			if !cliAnyEntityMatches(decls, name) {
				return cliSpec{}, fmt.Errorf("%s: no entity named %q", opts.selectionSource(sel.flag), name)
			}
		}
	}
	for name := range perEntityVerbs {
		if !cliAnyEntityMatches(decls, name) {
			return cliSpec{}, fmt.Errorf("%s: no entity named %q", opts.selectionSource("verbs"), name)
		}
	}

	apiPrefix := cliAPIPrefix(opts.ApiPrefix)

	binary := strings.TrimSpace(opts.Binary)
	if binary == "" {
		return cliSpec{}, fmt.Errorf("--binary must not be empty")
	}
	// Binary lands in `binaryName = %q` (quoted, safe) but ALSO in the `//`
	// header comment of the generated main.go, where a newline would put the
	// remainder at statement position. Today that is only reachable from argv,
	// so it is the operator injecting code into their own project rather than a
	// privilege boundary, not a vulnerability. The guard is here because the
	// sibling generator IS config-readable: `generate sdk` takes its name from a
	// discovered gofastr.settings.yml, and if `generate cli` ever grows the same
	// convenience this stops being operator-only without anyone noticing.
	for _, r := range binary {
		if r < 0x20 || r == 0x7f {
			return cliSpec{}, fmt.Errorf("--binary %q contains a control character: it becomes a file name and a Go identifier's value", binary)
		}
	}

	spec := cliSpec{
		Binary:       binary,
		EnvPrefix:    cliEnvPrefix(binary),
		APIPrefix:    apiPrefix,
		ClientImport: clientImport,
	}
	// The prefix is joined into every request URL; a control byte there is
	// never a path, so refuse it here rather than ship a CLI that 404s.
	if strings.ContainsFunc(apiPrefix, isControlByte) {
		return cliSpec{}, fmt.Errorf("--api-prefix must not contain control characters: %q", opts.ApiPrefix)
	}
	matchesAny := func(decl framework.EntityDeclaration, names []string) bool {
		for _, name := range names {
			if cliEntityMatches(decl, name) {
				return true
			}
		}
		return false
	}
	// Derived struct names must be unique across the selection (see the
	// check inside the loop).
	structOwners := map[string]string{} // derived struct name → declaring entity name
	var selected []framework.EntityDeclaration
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
		// Distinct entities must derive distinct struct prefixes: events and
		// Events both become struct Events, so runEventsList is defined twice
		// across two individually-parsing files. The file-set guard only sees
		// paths and each file parses, so the operator's first signal is a
		// confusing compile error in code they were told is theirs.
		if prev, dup := structOwners[ent.Struct]; dup {
			return cliSpec{}, fmt.Errorf("entities %q and %q both derive the Go struct name %s: their generated command funcs collide and the CLI cannot compile. Rename one entity", prev, decl.Name, ent.Struct)
		}
		structOwners[ent.Struct] = decl.Name
		spec.Entities = append(spec.Entities, ent)
		selected = append(selected, decl)
	}

	if len(spec.Entities) == 0 {
		return cliSpec{}, fmt.Errorf("selection matches no entities: nothing to generate")
	}
	if err := checkEndpointReturns(selected); err != nil {
		return cliSpec{}, err
	}
	return spec, nil
}

// cliReservedCommands are command words (and scaffold file basenames) the
// generated CLI owns: an entity whose command form collides would either
// shadow a built-in command at dispatch or emit a duplicate filename,
// including custom.go, whose create-if-absent handling would then silently
// stop regenerating that entity.
var cliReservedCommands = map[string]bool{
	"main": true, "config": true, "auth": true, "output": true, "custom": true,
	"verbs": true, // scaffold basename: the shared verb bodies (verbs.go)
	"login": true, "logout": true, "version": true, "help": true,
}

// buildEntityModel derives the shared per-entity model (struct name, route
// table, wire/snake field names, filterability) that both the generated CLI
// and the generated SDKs (`gofastr generate sdk`) consume. It never fails.
// CLI-specific validation (reserved commands, flag collisions) lives in
// buildCLIEntity.
func buildEntityModel(decl framework.EntityDeclaration, verbs []string) cliEntity {
	table := declTable(decl)
	ent := cliEntity{
		Struct:     appspec.ToCamelCase(decl.Name),
		Command:    strings.ReplaceAll(strings.ToLower(table), "_", "-"),
		Table:      table,
		Keys:       keyColumnNames(decl),
		Verbs:      verbs,
		Search:     len(decl.SearchFields) > 0,
		SoftDelete: appspec.EntityDeclarationScope(decl).SoftDelete,
	}
	// Stamps are server-set on every write path under enforced States, so
	// they get no mutation flag. The state field keeps one: create shares
	// the flag table and may start at an initial value.
	stamps, _ := statesWriteDrops(decl.States)
	uploads := uploadFields(decl)
	keyIsID := appspec.DeclKeyIsID(decl)
	for _, fd := range decl.Fields {
		if (keyIsID && fd.Name == "id") || fd.Hidden {
			continue
		}
		typ := strings.ToLower(fd.Type)
		switch typ {
		case "image", "file":
			// A file flag on the mutation verbs, sent as a multipart part,
			// for the fields the client has an upload slot for; list and
			// get leave the stored URL out of their tables.
			if slices.ContainsFunc(uploads, func(u framework.FieldDeclaration) bool { return u.Name == fd.Name }) {
				ent.Uploads = append(ent.Uploads, cliField{Snake: fd.Name, Wire: appspec.ToCamelJSON(fd.Name), Flag: strings.ReplaceAll(fd.Name, "_", "-"), Type: typ})
			}
			continue
		}
		f := cliField{
			Snake:    fd.Name,
			Wire:     appspec.ToCamelJSON(fd.Name),
			Flag:     strings.ReplaceAll(fd.Name, "_", "-"),
			Type:     typ,
			GoType:   goTypeForField(fd.Type),
			ReadOnly: fd.ReadOnly || stamps[fd.Name],
			NoQuery:  fd.NoQuery,
			Values:   fd.Values,
		}
		// A NoQuery field stays on the CLI for reads and writes; only its
		// filter flags are suppressed, because the server answers every one
		// of them with 400 "cannot be filtered". The operator set comes
		// from the one predicate every filter surface applies, so a flag
		// the server refuses (--flag-like on a Bool) is never emitted.
		// fd.Field() resolves the canonical schema type; an unknown type
		// string reads as String, the same tolerance goTypeForField has
		// always had here (the generated app's own registration refuses
		// the declaration).
		if !f.NoQuery {
			sf, err := fd.Field()
			if err != nil {
				// An unknown type string is a declaration error the
				// generated app's own registration refuses; the CLI
				// derives its flags the way it always has — as a text
				// column, goTypeForField's default arm.
				sf = schema.Field{Type: schema.String}
			}
			f.FilterOps = cliFilterOps(sf.Type)
			f.NoEq = !filter.OpSuitsType(filter.OpEq, sf.Type)
		}
		ent.Fields = append(ent.Fields, f)
	}
	// One command per non-system move (the route and the MCP tools serve
	// the same set, Advisory entities included). System moves have no
	// route and appear nowhere.
	// The CLI's help prints the summary raw, so the declaration's state
	// values reach it with terminal-control and bidi runes dropped.
	for _, t := range crud.RoutableTransitions(decl.States) {
		summary := "move " + decl.States.Field + " from " + strings.Join(t.From, " or ") + " to " + t.To
		if t.Stamp != "" {
			summary += ", stamps " + t.Stamp
		}
		ent.Transitions = append(ent.Transitions, cliTransition{Key: t.Key, Summary: textsafe.StripUnsafe(summary)})
	}
	for _, ep := range sdkEndpoints(decl) {
		ent.Endpoints = append(ent.Endpoints, buildCLIEndpoint(ep, table))
	}
	return ent
}

// buildCLIEndpoint derives an endpoint's command from its declaration.
func buildCLIEndpoint(ep framework.Endpoint, table string) cliEndpoint {
	_, rest, inQuery := endpointInput(ep)
	summary := ep.Description
	if summary == "" {
		summary = ep.Method + " /" + table + "/" + ep.Path
	}
	ce := cliEndpoint{
		Command: strings.ReplaceAll(ep.Name, "_", "-"),
		Func:    appspec.ToCamelCase(ep.Name),
		Method:  ep.Method,
		Segs:    strings.Split(ep.Path, "/"),
		Query:   inQuery,
		Summary: textsafe.StripUnsafe(summary),
	}
	for _, f := range rest {
		usage := f.Name + " (" + strings.ToLower(strings.TrimPrefix(schemaTypeNames[f.Type], "schema.")) + ")"
		if f.Required {
			usage += ", required"
		}
		ce.Fields = append(ce.Fields, cliEndpointField{
			Flag:     strings.ReplaceAll(f.Name, "_", "-"),
			Wire:     f.Name,
			Kind:     cliEndpointKind(f.Type),
			Usage:    usage,
			Required: f.Required,
		})
	}
	return ce
}

// cliEndpointKind maps an endpoint input's type to the fieldKind constant
// the shared endpoint body switches on.
func cliEndpointKind(t schema.FieldType) string {
	switch t {
	case schema.Int:
		return "fieldInt"
	case schema.Float:
		return "fieldFloat"
	case schema.Bool:
		return "fieldBool"
	case schema.JSON:
		return "fieldJSON"
	default:
		return "fieldString"
	}
}

func buildCLIEntity(decl framework.EntityDeclaration, verbs []string) (cliEntity, error) {
	ent := buildEntityModel(decl, verbs)
	// The CLI re-emits declaration strings into Go source: ent.Struct in
	// identifier position (run%sList, %sCommands) and ent.Table inside
	// "/%s/" route literals — table slots go through url.PathEscape at
	// the emitter, so path-shaping bytes (?, #, spaces) survive as data
	// even past the literal gate below. The app spec path into
	// entities/ is guarded
	// upstream (validateAppSpec: isGoIdentifier + query.SafeIdent), but
	// packReadEntities also reads hand-written entity files, so the guard
	// belongs at this emitter too: refuse what would break out of the
	// emitted code rather than munge it, mirroring validateAppSpec.
	if !project.IsGoIdentifier(ent.Struct) {
		return cliEntity{}, fmt.Errorf("entity %q does not produce a valid Go identifier (%q); it is emitted as generated function names. Rename it to letters, digits and underscores starting with a letter", decl.Name, ent.Struct)
	}
	if strings.ContainsFunc(ent.Table, func(r rune) bool { return r == '"' || r == '\\' || r < 0x20 || r == 0x7f }) {
		return cliEntity{}, fmt.Errorf("entity %q table %q contains a character that cannot survive the Go string literals it is emitted into. Rename the table", decl.Name, ent.Table)
	}
	if cliReservedCommands[ent.Command] {
		return cliEntity{}, fmt.Errorf("entity %q: its command form %q collides with a CLI built-in. Exclude it with --exclude=%s, or rename the table", decl.Name, ent.Command, decl.Name)
	}
	seen := map[string]string{} // flag name → field, to catch duplicates
	for _, f := range ent.Fields {
		// Field names land in identifier positions with no re-escaping:
		// fld%s/flt%s variable declarations and their *fld%s reads. The
		// entity/table guards above cover the declaration's other strings;
		// the field was the one input without its own boundary. token.
		// IsIdentifier is unicode-aware (Go identifiers allow unicode
		// letters), matching what the derived name can legally be. The raw
		// name (not %q) goes in the message: a quote or newline in the name
		// must survive verbatim so tooling can match on it.
		if ident := appspec.ToCamelCase(f.Flag); !token.IsIdentifier(ident) {
			return cliEntity{}, fmt.Errorf("entity %q: field %s does not derive a valid Go identifier (it is emitted as generated variable names); rename it to letters, digits and underscores starting with a letter", decl.Name, f.Snake)
		}
		// The full flag set the list verb registers for this field,
		// derived from the same FilterOps the emitter uses, so the
		// duplicate check can never miss a flag that ships (it missed
		// -ne: a spec with `status` and `status_ne` passed validation
		// and panicked flag.FlagSet at runtime).
		flags := []string{f.Flag}
		for _, op := range f.FilterOps {
			flags = append(flags, f.Flag+op.Flag)
		}
		for _, name := range flags {
			if cliReservedFlags[name] {
				return cliEntity{}, fmt.Errorf("entity %q: field %q derives CLI flag --%s, which is reserved. Rename the field, or exclude the entity with --exclude=%s", decl.Name, f.Snake, name, decl.Name)
			}
			if prev, dup := seen[name]; dup {
				return cliEntity{}, fmt.Errorf("entity %q: fields %q and %q both derive CLI flag --%s: rename one", decl.Name, prev, f.Snake, name)
			}
			seen[name] = f.Snake
		}
	}
	// A file flag shares the mutation verbs' flag set with the field flags.
	for _, f := range ent.Uploads {
		if !token.IsIdentifier(appspec.ToCamelCase(f.Flag)) {
			return cliEntity{}, fmt.Errorf("entity %q: field %s does not derive a usable CLI flag; rename it to letters, digits and underscores starting with a letter", decl.Name, f.Snake)
		}
		if cliReservedFlags[f.Flag] {
			return cliEntity{}, fmt.Errorf("entity %q: field %q derives CLI flag --%s, which is reserved. Rename the field, or exclude the entity with --exclude=%s", decl.Name, f.Snake, f.Flag, decl.Name)
		}
		if prev, dup := seen[f.Flag]; dup {
			return cliEntity{}, fmt.Errorf("entity %q: fields %q and %q both derive CLI flag --%s: rename one", decl.Name, prev, f.Snake, f.Flag)
		}
		seen[f.Flag] = f.Snake
	}
	if err := validateDeclarationStates(decl); err != nil {
		return cliEntity{}, fmt.Errorf("entity %q: %w", decl.Name, err)
	}
	// An endpoint's flags share the command's flag set with --url, --json
	// and the flag package's own -h/--help. Inputs are unique by wire
	// name, which has no dash, so two never derive one flag.
	for _, ep := range ent.Endpoints {
		for _, f := range ep.Fields {
			if cliEndpointReservedFlags[f.Flag] {
				return cliEntity{}, fmt.Errorf("entity %q: endpoint %s: input %q derives CLI flag --%s, which is reserved; rename the input", decl.Name, ep.Command, f.Wire, f.Flag)
			}
		}
	}
	return ent, nil
}

// validateDeclarationStates refuses a declaration whose states block the
// app would refuse at boot. `gofastr gen cli/sdk` read hand-written
// entities/*.go through packReadEntities — files that never passed
// entity.Define — so the generators re-run the ONE boot check (the move-key
// grammar, the reserved-name set, duplicates, the guarded columns) from
// framework/entity instead of re-implementing any of it here. The boot
// check runs Define first, so the guards see the injected
// framework-managed columns the app sees.
func validateDeclarationStates(decl framework.EntityDeclaration) error {
	if decl.States == nil {
		return nil
	}
	cfg, err := decl.Config()
	if err != nil {
		return err
	}
	return entity.ValidateStates(decl.Name, cfg)
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

// RenderCLIFiles renders the CLI tree. Every file but custom.go is
// derived and named *.gen.go, so `ls` shows which files a run rebuilds.
func RenderCLIFiles(spec cliSpec) []GeneratedFile {
	files := []GeneratedFile{
		{Name: "main.gen.go", Content: renderCLIMain(spec)},
		{Name: "config.gen.go", Content: renderCLIConfig(spec)},
		{Name: "auth.gen.go", Content: renderCLIAuth(spec)},
		{Name: "output.gen.go", Content: renderCLIOutput(spec)},
		{Name: cliCustomFile, Content: renderCLICustom(spec)},
	}
	// One shared verb-body file per CLI (pruned to the selected verbs);
	// the per-entity files keep one-line run wrappers and per-entity
	// flag/field tables over these bodies.
	if cliAnyVerb(spec, "list") || cliAnyVerb(spec, "get") || cliAnyVerb(spec, "delete") ||
		cliAnyVerb(spec, "create") || cliAnyVerb(spec, "update") || cliAnyVerb(spec, "patch") ||
		cliAnyVerb(spec, "batch-create") || cliAnyVerb(spec, "batch-update") ||
		cliAnyVerb(spec, "batch-delete") || cliAnyVerb(spec, "watch") || cliAnyTransition(spec) ||
		cliAnyEndpoint(spec) {
		files = append(files, GeneratedFile{
			Name:    "verbs.gen.go",
			Content: renderCLIVerbsFile(spec),
		})
	}
	for _, ent := range spec.Entities {
		if len(ent.Verbs) == 0 && len(ent.Transitions) == 0 && len(ent.Endpoints) == 0 {
			continue
		}
		files = append(files, GeneratedFile{
			Name:    ent.Command + ".gen.go",
			Content: renderCLIEntityFile(spec, ent),
		})
	}
	if spec.SelfClient {
		files = append(files, GeneratedFile{
			Name:    "internal/client/client.gen.go",
			Content: renderCLISelfClient(spec),
		})
	}
	if len(spec.Ops) > 0 {
		files = append(files, GeneratedFile{
			Name:    "operations.gen.go",
			Content: renderCLIOpsFile(spec),
		})
	}
	for i, f := range files {
		if f.Name != cliCustomFile {
			files[i].Content = cliGeneratedLine + f.Content
		}
	}
	return files
}

func renderCLIMain(spec cliSpec) string {
	source := "the current entity set"
	if len(spec.Ops) > 0 {
		source = "the OpenAPI document"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, `// %s is a terminal client for your app's HTTP API, generated by
// `+"`gofastr generate cli`"+` from `+source+`. Every run rebuilds the
// *.gen.go files; custom.go, your extension seam, is never overwritten.
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

// commandMap merges customCommands() over the generated set: a custom
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
`, spec.Binary, spec.Binary, spec.EnvPrefix, spec.APIPrefix, exampleCommandName(spec))
	for _, ent := range spec.Entities {
		if len(ent.Verbs) == 0 && len(ent.Transitions) == 0 && len(ent.Endpoints) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "\tcmds = append(cmds, %sCommands()...)\n", lowerFirst(ent.Struct))
	}
	if len(spec.Ops) > 0 {
		sb.WriteString("\tcmds = append(cmds, operationCommands()...)\n")
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
	fmt.Printf("\nConnection: --url flag, %s_URL/%s_TOKEN env vars, or ` + "`%s login`" + ` (no --token flag: tokens never ride argv).\n", envPrefix, envPrefix, binaryName)
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

// exampleCommandName picks a real command for the doc comment.
func exampleCommandName(spec cliSpec) string {
	for _, ent := range spec.Entities {
		if len(ent.Verbs) > 0 {
			return ent.Command + " " + ent.Verbs[0]
		}
	}
	if len(spec.Ops) > 0 {
		return spec.Ops[0].Command
	}
	return "entity list"
}

func renderCLIConfig(spec cliSpec) string {
	return `package main

import (
	"encoding/json"
	"fmt"
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
// none: a missing or unreadable file is not an error, it just means the
// caller falls through to flags/env. A CORRUPT file is refused loudly
// instead: the zero value would march on as data, silently dropping the
// stored token so every call runs unauthenticated against the
// operator's assumption.
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
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "%s: config file %s is corrupt (%v); delete it or run '%s login' again\n", binaryName, path, err, binaryName)
		os.Exit(1)
	}
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
	// 0600 on CREATE and OVERWRITE alike: os.WriteFile applies its mode
	// only when creating, so a pre-existing 0644 config.json (operator
	// chmod, restored backup, dotfiles manager) would be truncated and
	// refilled with the bearer token while still world-readable. Open,
	// chmod the handle, then write — the credential bytes land only
	// after the mode is fixed.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return "", err
	}
	return path, f.Close()
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

// defaultServerURL is the spec's servers[0].url (--from-openapi mode);
// empty otherwise. Flags, env, and stored config all win over it.
const defaultServerURL = ` + fmt.Sprintf("%q", spec.DefaultURL) + `

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
		fmt.Fprintf(os.Stderr, "Paste an API token minted in the app (input will echo, so prefer --with-token via a pipe): ")
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
	fmt.Printf("Logged in to %s. Config stored at %s\n", cfg.URL, path)
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
	fmt.Println("Logged out. Stored token removed. Revoke it in the app to invalidate it server-side.")
	return 0
}
`
}

func renderCLIOutput(spec cliSpec) string {
	return `package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"

	client "` + spec.ClientImport + `"
)

// global carries the per-invocation context and the authenticated API
// client. parseGlobals builds it after the verb registered its own flags.
type global struct {
	ctx    context.Context
	client *client.Client
	stop   context.CancelFunc
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(binaryName+" "+name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

// parseGlobals registers the connection flags, parses args, and builds the
// client. Resolution order: env > stored config (the API token never
// rides argv: there is deliberately no --token flag, the credential
// would sit in ps/procfs world-readable state for the whole call;
// ` + "`login --with-token`" + ` reads it from stdin instead). The stored
// token goes only to the origin login stored it for: a --url or $<APP>_URL
// naming another host gets no credential. A nil *global means don't
// proceed: exit 0 for --help, 2 for usage/resolution failures.
func parseGlobals(fs *flag.FlagSet, args []string) (*global, int) {
	urlF := fs.String("url", "", "server URL (default $"+envPrefix+"_URL, then stored config)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, 0
		}
		return nil, 2
	}
	cfg := loadConfig()
	base := firstNonEmpty(*urlF, os.Getenv(envPrefix+"_URL"), cfg.URL, defaultServerURL)
	if base == "" {
		fmt.Fprintf(os.Stderr, "no server URL: pass --url, set %s_URL, or run ` + "`%s login`" + `\n", envPrefix, binaryName)
		return nil, 2
	}
	// $<APP>_TOKEN is the caller's explicit choice and goes wherever
	// the call does. The stored token is tied to the URL it was stored
	// with: sending it to whatever --url names would hand a production
	// credential to a local or hostile server.
	token := os.Getenv(envPrefix + "_TOKEN")
	if token == "" && cfg.Token != "" {
		if sameOrigin(base, cfg.URL) {
			token = cfg.Token
		} else {
			fmt.Fprintf(os.Stderr, "%s: not sending the stored token: it was stored for %s, and this call goes to %s; run ` + "`%s login --url <url>`" + ` or set %s_TOKEN\n", binaryName, originOf(cfg.URL), originOf(base), binaryName, envPrefix)
		}
	}
	c := client.NewClient(apiBase(base), nil)
	c.Token = token
	configureClient(c)
	// Every verb runs under a cancellable ctx: Ctrl-C cancels the
	// in-flight request instead of raw process death mid-write. stop is
	// held on the global; the process exits when the verb returns, which
	// is the signal goroutine's whole lifetime.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	return &global{ctx: ctx, client: c, stop: stop}, 0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// apiBase is base with the API prefix joined on. A base whose path
// already ends in the prefix is used as it is: the CLI generated from an
// OpenAPI document once kept the server URL's path in its default and
// in what login stored, https://pets.example/api/v3 say, and appending
// the prefix again would request /api/v3/api/v3/....
func apiBase(base string) string {
	base = strings.TrimRight(base, "/")
	if apiPrefix == "" {
		return base
	}
	if u, err := url.Parse(base); err == nil && strings.HasSuffix(u.EscapedPath(), apiPrefix) {
		return base
	}
	return base + apiPrefix
}

// originOf is raw's scheme://host:port, the port spelled out (80 for
// http, 443 for https when raw names none), or "" when raw is not an
// absolute http(s) URL. The host folds ASCII case only, and a host with
// any other byte has no origin, so the stored token goes nowhere: Go's
// Unicode case folding is not the IDNA mapping net/http dials through
// (strings.ToLower folds U+0130 to "i", while net/http sends that host
// to xn--i-9bb), so folding it would let a token stored for i.test reach
// another host. An IPv6 zone is compared as written.
func originOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	port := u.Port()
	switch {
	case port != "":
	case scheme == "http":
		port = "80"
	case scheme == "https":
		port = "443"
	default:
		return ""
	}
	host, zone, _ := strings.Cut(u.Hostname(), "%")
	folded := []byte(host)
	for i, c := range folded {
		switch {
		case c >= 0x80:
			return ""
		case 'A' <= c && c <= 'Z':
			folded[i] = c + ('a' - 'A')
		}
	}
	if zone != "" {
		zone = "%" + zone
	}
	host = string(folded) + zone
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host + ":" + port
}

// sameOrigin reports whether a and b name the same scheme, host and port.
func sameOrigin(a, b string) bool {
	oa := originOf(a)
	return oa != "" && oa == originOf(b)
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
				cells[i] = scrubTerminal(fmt.Sprintf("%v", v))
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
		fmt.Fprintln(os.Stderr, scrubTerminal(apiErr.Error()))
		if apiErr.Status == 401 || apiErr.Status == 403 {
			fmt.Fprintf(os.Stderr, "auth failed: mint a token in the app and run ` + "`%s login`" + `\n", binaryName)
			return 4
		}
		return 1
	}
	fmt.Fprintln(os.Stderr, scrubTerminal(err.Error()))
	return 1
}

// scrubTerminal drops what a terminal acts on instead of printing: C0
// controls but tab, DEL, and C1 controls (0x9b is a one-byte CSI), with
// an invalid byte read as U+FFFD. Record text and server error bodies
// pass through it before a human-format line: a stored title
// "\x1b[2J..." cleared the screen of whoever ran list -o table. JSON
// output needs no scrub: its encoder escapes control bytes.
func scrubTerminal(s string) string {
	return strings.Map(func(r rune) rune {
		if (r < 0x20 && r != '\t') || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
}

// decodeJSON is json.Unmarshal with UseNumber: a number landing in an
// any keeps its digits as a json.Number. Decoded as a float64,
// 9007199254740993 went to the server as ...992. Like json.Unmarshal, it
// refuses bytes after the value.
func decodeJSON(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("invalid data after the JSON value")
	}
	return nil
}

// takeKey pops the record's key: one leading positional argument per key
// column, <verb> <id> [flags] or <verb> <a> <b> [flags]. It returns the
// record's path tail, one escaped segment per value, and the id the
// server knows the record by: the value itself for a one-column key, the
// path tail for a longer one.
func takeKey(verb string, keys, args []string) (tail, id string, rest []string, ok bool) {
	short := len(args) < len(keys)
	for i := 0; !short && i < len(keys); i++ {
		short = strings.HasPrefix(args[i], "-")
	}
	if short {
		names := make([]string, len(keys))
		for i, k := range keys {
			names[i] = "<" + k + ">"
		}
		fmt.Fprintf(os.Stderr, "usage: %s %s %s [flags]\n", binaryName, verb, strings.Join(names, " "))
		return "", "", nil, false
	}
	segs := make([]string, len(keys))
	for i := range keys {
		segs[i] = url.PathEscape(args[i])
	}
	tail = strings.Join(segs, "/")
	if id = tail; len(keys) == 1 {
		id = args[0]
	}
	return tail, id, args[len(keys):], true
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
		case "url", "json", "o":
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
		if err := decodeJSON(raw, &body); err != nil {
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
// decoded and returned as a response; printBatch then surfaces it on
// stdout and exit code 1, rather than treated as a transport error.
func doBatch(g *global, method, path string, body any) (client.BatchResponse, int) {
	var resp client.BatchResponse
	if err := g.client.Do(g.ctx, method, path, body, &resp); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 400 {
			if decodeJSON(apiErr.Body, &resp) == nil && len(resp.Results) > 0 {
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

// paramFlags collects repeatable --param key=value pairs: the escape hatch
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

// This file is yours: ` + "`gofastr generate cli`" + ` creates it once and never
// overwrites it. The *.gen.go files beside it are rebuilt on every run.

// customCommands is merged OVER the generated command set: an entry whose
// name matches a generated command ("` + exampleCommandName(spec) + `") replaces it, and new
// names become new commands. The built-in login and logout can be replaced
// the same way. To wrap rather than replace a generated verb, call its run
// function (runPostsList-style) from your own.
func customCommands() []command {
	return nil
}

// configureClient runs on every request-bearing command right after the
// client is built: set default headers via a custom c.HTTP transport,
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

	hasMutation := has("create") || has("update") || has("patch")
	hasWrapper := has("list") || has("get") || has("delete") || has("batch-create") ||
		has("batch-update") || has("batch-delete") || has("watch") || len(ent.Transitions) > 0 ||
		len(ent.Endpoints) > 0
	// Import needs shrink with the shared verb bodies (verbs.go): this
	// file holds the command table, the one-line wrappers, and the
	// per-entity flag/field tables. Only the batch wrappers reference
	// http method identifiers directly; list/get/delete/watch and the
	// create/update/patch plumbing are verbs.go's.
	needsHTTP := has("batch-create") || has("batch-update")
	needsClient := has("watch") // wrapper passes the typed Watch method expression

	var imports []string
	if needsHTTP {
		imports = append(imports, "\t\"net/http\"")
	}
	if needsClient {
		imports = append(imports, "\n\tclient `"+spec.ClientImport+"`")
	}
	if len(imports) > 0 {
		sb.WriteString("package main\n\nimport (\n" + strings.Join(imports, "\n") + "\n)\n\n")
	} else {
		sb.WriteString("package main\n\n")
	}
	// base is the pre-escaped collection path every wrapper binds to.
	base := "/" + url.PathEscape(ent.Table)

	// Command table. The bare entity command prints its subcommand list.
	fmt.Fprintf(&sb, "func %sCommands() []command {\n\treturn []command{\n", lowerFirst(ent.Struct))
	fmt.Fprintf(&sb, "\t\t{name: %q, summary: %q, run: func(args []string) int { return groupUsage(%q, args) }},\n",
		ent.Command, "manage "+ent.Table+" (run for subcommands)", ent.Command)
	keyWords, batchIDNote := "id", ""
	if len(ent.Keys) > 1 || ent.Keys[0] != "id" {
		keyWords = "key (" + strings.Join(ent.Keys, ", ") + ")"
	}
	if len(ent.Keys) > 1 {
		batchIDNote = ", each " + strings.Join(ent.Keys, "/") + " path-escaped"
	}
	summaries := map[string]string{
		"list":         "list " + ent.Table + " (filters, sort, pagination)",
		"get":          "fetch one record by " + keyWords,
		"create":       "create a record from field flags or --json",
		"update":       "update fields on a record (PUT)",
		"patch":        "patch fields on a record (PATCH)",
		"delete":       "delete a record by " + keyWords,
		"batch-create": "create up to 100 records atomically (--json array)",
		"batch-update": "patch up to 100 records atomically (--json array)",
		"batch-delete": "delete ids atomically (positional ids" + batchIDNote + ")",
		"watch":        "stream live create/update/delete events (SSE)",
	}
	for _, verb := range cliVerbs {
		if !has(verb) {
			continue
		}
		fmt.Fprintf(&sb, "\t\t{name: %q, summary: %q, run: run%s%s},\n",
			ent.Command+" "+verb, summaries[verb], ent.Struct, verbFuncSuffix(verb))
	}
	for _, tr := range ent.Transitions {
		fmt.Fprintf(&sb, "\t\t{name: %q, summary: %q, run: run%s%s},\n",
			ent.Command+" "+tr.Key, tr.Summary, ent.Struct, appspec.ToCamelCase(tr.Key))
	}
	for _, ep := range ent.Endpoints {
		fmt.Fprintf(&sb, "\t\t{name: %q, summary: %q, run: run%s%s},\n",
			ent.Command+" "+ep.Command, ep.Summary, ent.Struct, ep.Func)
	}
	sb.WriteString("\t}\n}\n\n")
	if hasWrapper {
		fmt.Fprintf(&sb, "// Verb wrappers: each binds this entity's command names and pre-escaped\n// base path %q to the shared verb bodies in verbs.go.\n\n", base)
	}

	// The key columns are literals, like every other declaration-derived
	// string here.
	keys := "[]string{" + quoteList(ent.Keys) + "}"
	if has("list") {
		renderCLIListTables(&sb, ent)
		fmt.Fprintf(&sb, "func run%sList(args []string) int {\n\treturn runListVerb(%q, %q, %sListFilters, %sListHeaders, %sListKeys, args)\n}\n\n",
			ent.Struct, ent.Command+" list", base, lowerFirst(ent.Struct), lowerFirst(ent.Struct), lowerFirst(ent.Struct))
	}
	if has("get") {
		fmt.Fprintf(&sb, "func run%sGet(args []string) int {\n\treturn runGetVerb(%q, %q, %s, args)\n}\n\n",
			ent.Struct, ent.Command+" get", base, keys)
	}
	changeFields := lowerFirst(ent.Struct) + "MutationFields"
	if hasMutation {
		renderCLIMutationFields(&sb, ent, false)
		// A declared key's columns are fields, set on create. The path
		// names the record on update and patch, and the server never
		// writes a key column there, so those verbs offer no flag for one.
		if (has("update") || has("patch")) && slices.ContainsFunc(ent.Fields, func(f cliField) bool { return slices.Contains(ent.Keys, f.Snake) }) {
			renderCLIMutationFields(&sb, ent, true)
			changeFields = lowerFirst(ent.Struct) + "ChangeFields"
		}
	}
	if has("create") {
		fmt.Fprintf(&sb, "func run%sCreate(args []string) int {\n\treturn runCreateVerb(%q, %q, %sMutationFields, args)\n}\n\n",
			ent.Struct, ent.Command+" create", base, lowerFirst(ent.Struct))
	}
	if has("update") {
		fmt.Fprintf(&sb, "func run%sUpdate(args []string) int {\n\treturn runUpdateVerb(%q, %q, %s, %s, args)\n}\n\n",
			ent.Struct, ent.Command+" update", base, keys, changeFields)
	}
	if has("patch") {
		fmt.Fprintf(&sb, "func run%sPatch(args []string) int {\n\treturn runPatchVerb(%q, %q, %s, %s, args)\n}\n\n",
			ent.Struct, ent.Command+" patch", base, keys, changeFields)
	}
	if has("delete") {
		fmt.Fprintf(&sb, "func run%sDelete(args []string) int {\n\treturn runDeleteVerb(%q, %q, %s, args)\n}\n\n",
			ent.Struct, ent.Command+" delete", base, keys)
	}
	if has("batch-create") {
		fmt.Fprintf(&sb, "func run%sBatchCreate(args []string) int {\n\treturn runBatchJSONVerb(%q, %q, http.MethodPost, args)\n}\n\n",
			ent.Struct, ent.Command+" batch-create", base)
	}
	if has("batch-update") {
		fmt.Fprintf(&sb, "func run%sBatchUpdate(args []string) int {\n\treturn runBatchJSONVerb(%q, %q, http.MethodPatch, args)\n}\n\n",
			ent.Struct, ent.Command+" batch-update", base)
	}
	if has("batch-delete") {
		fmt.Fprintf(&sb, "func run%sBatchDelete(args []string) int {\n\treturn runBatchDeleteVerb(%q, %q, args)\n}\n\n",
			ent.Struct, ent.Command+" batch-delete", base)
	}
	if has("watch") {
		fmt.Fprintf(&sb, "func run%sWatch(args []string) int {\n\treturn runWatchVerb(%q, (*client.Client).Watch%s, args)\n}\n\n",
			ent.Struct, ent.Command+" watch", ent.Struct)
	}
	for _, tr := range ent.Transitions {
		fmt.Fprintf(&sb, "func run%s%s(args []string) int {\n\treturn runTransitionVerb(%q, %q, %q, %s, args)\n}\n\n",
			ent.Struct, appspec.ToCamelCase(tr.Key), ent.Command+" "+tr.Key, base, tr.Key, keys)
	}
	for _, ep := range ent.Endpoints {
		renderCLIEndpoint(&sb, ent, ep, base)
	}
	return sb.String()
}

// renderCLIEndpoint emits one endpoint's call table and its wrapper over
// runEndpointVerb. Every declaration-derived string is a literal.
func renderCLIEndpoint(sb *strings.Builder, ent cliEntity, ep cliEndpoint, base string) {
	v := lowerFirst(ent.Struct) + ep.Func + "Call"
	if !project.IsGoIdentifier(v) {
		// The settings decoder holds names to snake_case, so this never
		// fires; the generated build failing on the missing run func is
		// the loud backstop, never a non-identifier in a declaration.
		return
	}
	fmt.Fprintf(sb, "// %s is the route and flag table behind `%s %s`.\n", v, goCommentSafe(ent.Command), goCommentSafe(ep.Command))
	fmt.Fprintf(sb, "var %s = endpointCall{\n\tmethod: %q,\n\tsegs:   []string{%s},\n", v, ep.Method, quoteList(ep.Segs))
	if ep.Query {
		sb.WriteString("\tquery:  true,\n")
	}
	if len(ep.Fields) > 0 {
		sb.WriteString("\tfields: []mutationField{\n")
		for _, f := range ep.Fields {
			fmt.Fprintf(sb, "\t\t{flag: %q, wire: %q, kind: %s, usage: %q", f.Flag, f.Wire, f.Kind, f.Usage)
			if f.Required {
				sb.WriteString(", required: true")
			}
			sb.WriteString("},\n")
		}
		sb.WriteString("\t},\n")
	}
	sb.WriteString("}\n\n")
	fmt.Fprintf(sb, "func run%s%s(args []string) int {\n\treturn runEndpointVerb(%q, %q, %s, args)\n}\n\n",
		ent.Struct, ep.Func, ent.Command+" "+ep.Command, base, v)
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

// renderCLIListTables emits the per-entity list data: the filter-flag
// table (one entry per flag, in the order the former inline body
// declared them) and the -o table columns. The mechanics live once in
// verbs.go (runListVerb); this is the part that genuinely varies.
func renderCLIListTables(sb *strings.Builder, ent cliEntity) {
	p := lowerFirst(ent.Struct)
	fmt.Fprintf(sb, "// %sListFilters is the filter-flag table behind `%s list`: one entry\n// per flag, in help order, each bound to the query param it sets.\n", p, goCommentSafe(ent.Command))
	fmt.Fprintf(sb, "var %sListFilters = []filterFlag{\n", p)
	if ent.Search {
		fmt.Fprintf(sb, "\t{flag: %q, param: %q, help: %q},\n", "q", "q", "free-text search over the declared search fields")
	}
	if ent.SoftDelete {
		fmt.Fprintf(sb, "\t{flag: %q, param: %q, help: %q, isBool: true},\n", "trashed", "trashed", "include soft-deleted rows")
	}
	for _, f := range ent.Fields {
		if f.NoQuery || f.NoEq {
			continue
		}
		help := "filter: " + f.Snake + " equals"
		if len(f.Values) > 0 {
			help += " [" + strings.Join(f.Values, "|") + "]"
		}
		fmt.Fprintf(sb, "\t{flag: %q, param: %q, help: %q},\n", f.Flag, f.Snake, help)
		// One row per operator the field's type accepts, from the same
		// FilterOps derivation: the table cannot name a param the server
		// refuses, and a new operator in filter.FilterSuffixes lands
		// here without a second list.
		for _, op := range f.FilterOps {
			fmt.Fprintf(sb, "\t{flag: %q, param: %q, help: %q},\n", f.Flag+op.Flag, f.Snake+op.Param, "filter: "+f.Snake+" "+op.Help)
		}
	}
	sb.WriteString("}\n\n")
	var headers, keys []string
	if slices.Equal(ent.Keys, []string{"id"}) {
		headers, keys = []string{"id"}, []string{"id"}
	}
	for _, f := range ent.Fields {
		headers = append(headers, f.Snake)
		keys = append(keys, f.Wire)
	}
	fmt.Fprintf(sb, "// Table columns for `%s list -o table`: %sListHeaders are the display\n// titles, %sListKeys the JSON wire keys each column reads.\n", goCommentSafe(ent.Command), p, p)
	fmt.Fprintf(sb, "var (\n\t%sListHeaders = []string{%s}\n\t%sListKeys    = []string{%s}\n)\n\n", p, quoteList(headers), p, quoteList(keys))
}

// renderCLIMutationFields emits the per-entity field table behind the
// mutation verbs: one entry per writable field, in declaration order,
// each binding the CLI flag to the JSON wire key it sets. The mechanics
// live once in verbs.go (runMutationVerb); this is the part that
// genuinely varies. Field names are string literals here, never
// identifiers. With change set, it emits <entity>ChangeFields instead:
// the table less the key columns, behind update and patch.
func renderCLIMutationFields(sb *strings.Builder, ent cliEntity, change bool) {
	p := lowerFirst(ent.Struct)
	if change {
		fmt.Fprintf(sb, "// %sChangeFields is %sMutationFields less the key columns, behind\n// `%s update/patch`: the path names the record.\n", p, p, goCommentSafe(ent.Command))
		fmt.Fprintf(sb, "var %sChangeFields = []mutationField{\n", p)
	} else {
		fmt.Fprintf(sb, "// %sMutationFields is the field-flag table behind `%s create/update/patch`:\n// one entry per writable field, each bound to the JSON wire key it sets.\n", p, goCommentSafe(ent.Command))
		if len(ent.Uploads) > 0 {
			sb.WriteString("// A file flag sends the write as multipart, where every part goes by its\n// column.\n")
		}
		fmt.Fprintf(sb, "var %sMutationFields = []mutationField{\n", p)
	}
	for _, f := range ent.Fields {
		if f.ReadOnly || (change && slices.Contains(ent.Keys, f.Snake)) {
			continue
		}
		help := f.Snake + " (" + f.Type + ")"
		if len(f.Values) > 0 {
			help += " [" + strings.Join(f.Values, "|") + "]"
		}
		if len(ent.Uploads) > 0 {
			fmt.Fprintf(sb, "\t{flag: %q, wire: %q, column: %q, kind: %s, usage: %q},\n",
				f.Flag, f.Wire, f.Snake, cliFieldKindConst(f.GoType), help)
			continue
		}
		fmt.Fprintf(sb, "\t{flag: %q, wire: %q, kind: %s, usage: %q},\n",
			f.Flag, f.Wire, cliFieldKindConst(f.GoType), help)
	}
	for _, f := range ent.Uploads {
		fmt.Fprintf(sb, "\t{flag: %q, wire: %q, column: %q, kind: fieldFile, usage: %q},\n",
			f.Flag, f.Wire, f.Snake, f.Snake+" ("+f.Type+"): a file path, or - for stdin")
	}
	sb.WriteString("}\n\n")
}

// cliFieldKindConst maps the derived Go type to the fieldKind constant
// the shared mutation body switches on. Fixed vocabulary: no declaration
// string reaches an identifier slot.
func cliFieldKindConst(goType string) string {
	switch goType {
	case "int":
		return "fieldInt"
	case "float64":
		return "fieldFloat"
	case "bool":
		return "fieldBool"
	case "any":
		return "fieldJSON"
	default:
		return "fieldString"
	}
}

// cliAnyTransition reports whether any selected entity holds a non-system
// move; it gates the shared transition body in verbs.go.
func cliAnyTransition(spec cliSpec) bool {
	for _, ent := range spec.Entities {
		if len(ent.Transitions) > 0 {
			return true
		}
	}
	return false
}

// cliAnyEndpoint reports whether any selected entity has a settings
// endpoint; it gates the shared endpoint body in verbs.go.
func cliAnyEndpoint(spec cliSpec) bool {
	for _, ent := range spec.Entities {
		if len(ent.Endpoints) > 0 {
			return true
		}
	}
	return false
}

// cliAnyUpload reports whether any selected entity takes a file on a
// mutation verb; it gates the multipart body in verbs.go.
func cliAnyUpload(spec cliSpec) bool {
	for _, ent := range spec.Entities {
		if len(ent.Uploads) > 0 && (slices.Contains(ent.Verbs, "create") || slices.Contains(ent.Verbs, "update") || slices.Contains(ent.Verbs, "patch")) {
			return true
		}
	}
	return false
}

// cliAnyVerb reports whether any selected entity kept the verb; it gates
// which shared bodies verbs.go needs.
func cliAnyVerb(spec cliSpec, verb string) bool {
	for _, ent := range spec.Entities {
		for _, v := range ent.Verbs {
			if v == verb {
				return true
			}
		}
	}
	return false
}

// fileFlagConst is the fieldKind of a file flag, declared only for a
// selection with file fields.
func fileFlagConst(uploads bool) string {
	if !uploads {
		return ""
	}
	return "\tfieldFile\n"
}

// fileFlagArm is fieldFlags' case for a file flag, which stores the path
// as an uploadPath; only a selection with file fields has one.
func fileFlagArm(uploads bool) string {
	if !uploads {
		return ""
	}
	return `			case fieldFile:
				body[f.wire] = uploadPath(*strVals[i])
`
}

// mutationSend is the send step of runMutationVerb: a body holding a file
// flag goes out as multipart when the selection has file fields.
func mutationSend(uploads bool) string {
	if !uploads {
		return `	if err := g.client.Do(g.ctx, method, path, body, &out); err != nil {
		return apiFail(err)
	}
`
	}
	return `	var err error
	if hasUpload(body) {
		err = sendMultipart(g.ctx, g.client, method, path, fields, body, &out)
	} else {
		err = g.client.Do(g.ctx, method, path, body, &out)
	}
	if err != nil {
		return apiFail(err)
	}
`
}

// cliUploadHelpers is the multipart half of the mutation verbs, emitted
// when a selected entity has an Image or File field.
const cliUploadHelpers = `// uploadPath is a file flag's value in a mutation body: a path, or - for
// stdin. A body holding one goes out as multipart.
type uploadPath string

// hasUpload reports whether body holds a file flag.
func hasUpload(body map[string]any) bool {
	for _, v := range body {
		if _, ok := v.(uploadPath); ok {
			return true
		}
	}
	return false
}

// sendMultipart sends body as one multipart request, each value under its
// column: a file flag as the file it names, any other value as text. The
// client refuses a file over the server's limit before sending it.
func sendMultipart(ctx context.Context, c *client.Client, method, path string, fields []mutationField, body map[string]any, out any) error {
	values := map[string]any{}
	files := map[string]client.FilePart{}
	stdin := ""
	for _, f := range fields {
		v, ok := body[f.wire]
		if !ok {
			continue
		}
		p, isFile := v.(uploadPath)
		if !isFile {
			values[f.column] = v
			continue
		}
		if p == "-" {
			if stdin != "" {
				return fmt.Errorf("--%s and --%s both read stdin; pass one of them a file path", stdin, f.flag)
			}
			stdin = f.flag
			files[f.column] = client.FilePart{Name: f.flag, Body: os.Stdin}
			continue
		}
		file, err := os.Open(string(p))
		if err != nil {
			return fmt.Errorf("--%s: %w", f.flag, err)
		}
		defer file.Close()
		files[f.column] = client.FilePart{Name: filepath.Base(string(p)), Body: file}
	}
	return c.DoMultipart(ctx, method, path, values, files, out)
}

`

// renderCLIVerbsFile emits verbs.go, the shared verb bodies. The former
// per-entity copies (run<Ent>List/Get/Create/Update/Patch/Delete/
// BatchCreate/BatchUpdate/BatchDelete/Watch inside every <entity>.go)
// differed only in the command name, the pre-escaped base path, and
// per-entity flag/column/field tables, so each verb now has ONE body here
// and the entity files keep one-line wrappers binding theirs plus the
// tables those bodies walk (issue #417 moved create/update/patch over:
// their per-field flag declarations became rows in a mutationField
// table). Emitted only when some entity kept a verb, pruned to the
// selected verbs.
func renderCLIVerbsFile(spec cliSpec) string {
	list := cliAnyVerb(spec, "list")
	get := cliAnyVerb(spec, "get")
	del := cliAnyVerb(spec, "delete")
	mutation := cliAnyVerb(spec, "create") || cliAnyVerb(spec, "update") || cliAnyVerb(spec, "patch")
	batchJSON := cliAnyVerb(spec, "batch-create") || cliAnyVerb(spec, "batch-update")
	batchDel := cliAnyVerb(spec, "batch-delete")
	watch := cliAnyVerb(spec, "watch")
	transition := cliAnyTransition(spec)
	endpoint := cliAnyEndpoint(spec)
	fields := mutation || endpoint // the field-flag table and its parser
	uploads := cliAnyUpload(spec)

	var imports []string
	if watch || uploads {
		imports = append(imports, "\t\"context\"")
	}
	if fields || list {
		imports = append(imports, "\t\"flag\"") // list visits its flags for a given --cursor
	}
	if list || del || batchDel || watch || fields {
		imports = append(imports, "\t\"fmt\"")
	}
	if list || get || del || batchDel || mutation || transition {
		imports = append(imports, "\t\"net/http\"")
	}
	if list || transition || endpoint {
		imports = append(imports, "\t\"net/url\"")
	}
	if batchDel || endpoint || uploads {
		imports = append(imports, "\t\"os\"") // usage errors go to stderr; files open from disk
	}
	if uploads {
		imports = append(imports, "\t\"path/filepath\"")
	}
	if endpoint {
		imports = append(imports, "\t\"strings\"")
	}
	if watch || uploads {
		imports = append(imports, "\n\tclient `"+spec.ClientImport+"`")
	}
	var sb strings.Builder
	sb.WriteString("package main\n\nimport (\n" + strings.Join(imports, "\n") + "\n)\n\n")

	if list {
		sb.WriteString(`// filterFlag binds one list flag to the query param it sets. String
// filters (the default) only set their param when non-empty; isBool
// filters (soft-delete trashed) set theirs to "true" when given.
type filterFlag struct {
	flag   string
	param  string
	help   string
	isBool bool
}

// runListVerb is the shared list body: the pagination/output flags, the
// entity's filter table (filters), the query string, and the JSON or
// table print.
func runListVerb(cmd, base string, filters []filterFlag, headers, keys []string, args []string) int {
	fs := newFlagSet(cmd)
	sortF := fs.String("sort", "", "sort field(s), comma-separated, - prefix for desc")
	page := fs.String("page", "", "page number (offset pagination)")
	limit := fs.String("limit", "", "page size")
	cursor := fs.String("cursor", "", "keyset cursor (from a prior response; --cursor \"\" starts at the first page)")
	include := fs.String("include", "", "relations to eager-load (comma, dots for nesting)")
	fieldsF := fs.String("fields", "", "sparse field projection (comma-separated)")
	outF := fs.String("o", "json", "output format: json|table")
	var params paramFlags
	fs.Var(&params, "param", "extra query param key=value (repeatable)")
	strVals := make([]*string, len(filters))
	boolVals := make([]*bool, len(filters))
	for i, f := range filters {
		if f.isBool {
			boolVals[i] = fs.Bool(f.flag, false, f.help)
		} else {
			strVals[i] = fs.String(f.flag, "", f.help)
		}
	}
	g, code := parseGlobals(fs, args)
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
	// The server enters keyset mode on the cursor key's presence, so a
	// given --cursor goes even when empty: --cursor "" fetches page one.
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "cursor" {
			q.Set("cursor", *cursor)
		}
	})
	set("include", *include)
	set("fields", *fieldsF)
	for i, f := range filters {
		if f.isBool {
			if *boolVals[i] {
				q.Set(f.param, "true")
			}
		} else {
			set(f.param, *strVals[i])
		}
	}
	for _, kv := range params.pairs {
		q.Set(kv[0], kv[1])
	}
	path := base
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var resp listResponse
	if err := g.client.Do(g.ctx, http.MethodGet, path, nil, &resp); err != nil {
		return apiFail(err)
	}
	if *outF == "table" {
		printListTable(headers, keys, resp.Data)
		if resp.Cursor != "" || resp.HasMore {
			fmt.Printf("%d rows, next cursor: %s\n", len(resp.Data), scrubTerminal(resp.Cursor))
		} else {
			fmt.Printf("page %d/%d, %d total\n", resp.Page, resp.TotalPages, resp.Total)
		}
		return 0
	}
	return printJSON(resp)
}

`)
	}
	if get {
		sb.WriteString(`// runGetVerb is the shared get body: take the positional key, GET
// base/{key}, print the record.
func runGetVerb(cmd, base string, keys, args []string) int {
	tail, _, rest, ok := takeKey(cmd, keys, args)
	if !ok {
		return 2
	}
	fs := newFlagSet(cmd)
	g, code := parseGlobals(fs, rest)
	if g == nil {
		return code
	}
	var out singleResponse
	if err := g.client.Do(g.ctx, http.MethodGet, base+"/"+tail, nil, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

`)
	}
	if fields {
		sb.WriteString(`// fieldKind is the flag type a mutation field declares.
type fieldKind int

const (
	fieldString fieldKind = iota
	fieldInt
	fieldFloat
	fieldBool
	fieldJSON
` + fileFlagConst(uploads) + `)

// mutationField describes one field flag behind create/update/patch or an
// endpoint command: the CLI flag, the JSON wire key it sets, the column a
// multipart write names it by (entities with file fields only), the flag
// type, the usage line, and (endpoints only) whether the call needs it.
// Field names are data bound at generate time (string literals), never
// identifiers.
type mutationField struct {
	flag     string
	wire     string
	column   string
	kind     fieldKind
	usage    string
	required bool
}

// fieldFlags registers one flag per field on fs and returns the func
// buildBody calls for each flag the caller set, which writes the flag's
// value under its wire key.
func fieldFlags(fs *flag.FlagSet, fields []mutationField) func(name string, body map[string]any) error {
	strVals := make([]*string, len(fields))
	intVals := make([]*int, len(fields))
	floatVals := make([]*float64, len(fields))
	boolVals := make([]*bool, len(fields))
	for i, f := range fields {
		switch f.kind {
		case fieldInt:
			intVals[i] = fs.Int(f.flag, 0, f.usage)
		case fieldFloat:
			floatVals[i] = fs.Float64(f.flag, 0, f.usage)
		case fieldBool:
			boolVals[i] = fs.Bool(f.flag, false, f.usage)
		default: // string and json-typed fields both arrive as strings
			strVals[i] = fs.String(f.flag, "", f.usage)
		}
	}
	return func(name string, body map[string]any) error {
		for i, f := range fields {
			if f.flag != name {
				continue
			}
			switch f.kind {
			case fieldInt:
				body[f.wire] = *intVals[i]
			case fieldFloat:
				body[f.wire] = *floatVals[i]
			case fieldBool:
				body[f.wire] = *boolVals[i]
			case fieldJSON:
				var v any
				if err := decodeJSON([]byte(*strVals[i]), &v); err != nil {
					return fmt.Errorf("--%s: %w", f.flag, err)
				}
				body[f.wire] = v
` + fileFlagArm(uploads) + `			default:
				body[f.wire] = *strVals[i]
			}
		}
		return nil
	}
}

`)
	}
	if mutation {
		sb.WriteString(`// runCreateVerb is the shared create body over the entity's field table.
func runCreateVerb(cmd, base string, fields []mutationField, args []string) int {
	return runMutationVerb(cmd, http.MethodPost, nil, base, fields, args)
}

// runUpdateVerb is the shared update body over the entity's field table.
func runUpdateVerb(cmd, base string, keys []string, fields []mutationField, args []string) int {
	return runMutationVerb(cmd, http.MethodPut, keys, base, fields, args)
}

// runPatchVerb is the shared patch body over the entity's field table.
func runPatchVerb(cmd, base string, keys []string, fields []mutationField, args []string) int {
	return runMutationVerb(cmd, http.MethodPatch, keys, base, fields, args)
}

// runMutationVerb is the shared create/update/patch body: per-field flags
// from the entity's field table OR --json, sent as a presence-faithful map
// so explicit zero values survive. keys selects the item form:
// update/patch pop the positional key first and address base/{key};
// create passes none and posts the collection itself.
func runMutationVerb(cmd, method string, keys []string, base string, fields []mutationField, args []string) int {
	path := base
	if keys != nil {
		tail, _, rest, ok := takeKey(cmd, keys, args)
		if !ok {
			return 2
		}
		path += "/" + tail
		args = rest
	}
	fs := newFlagSet(cmd)
	jsonBody := fs.String("json", "", "raw JSON body: inline, @file, or - for stdin")
	apply := fieldFlags(fs, fields)
	g, code := parseGlobals(fs, args)
	if g == nil {
		return code
	}
	body, code := buildBody(fs, *jsonBody, apply)
	if code != 0 {
		return code
	}
	var out singleResponse
` + mutationSend(uploads) + `	return printJSON(out.Data)
}

`)
		if uploads {
			sb.WriteString(cliUploadHelpers)
		}
	}
	if del {
		sb.WriteString(`// runDeleteVerb is the shared delete body: take the positional key,
// DELETE base/{key}, confirm on stdout.
func runDeleteVerb(cmd, base string, keys, args []string) int {
	tail, id, rest, ok := takeKey(cmd, keys, args)
	if !ok {
		return 2
	}
	fs := newFlagSet(cmd)
	g, code := parseGlobals(fs, rest)
	if g == nil {
		return code
	}
	if err := g.client.Do(g.ctx, http.MethodDelete, base+"/"+tail, nil, nil); err != nil {
		return apiFail(err)
	}
	fmt.Printf("deleted %s\n", id)
	return 0
}

`)
	}
	if transition {
		sb.WriteString(`// runTransitionVerb is the shared move body: take the positional key,
// POST base/{key}/transitions/{move}, print the moved record. The route
// takes no payload but requires the JSON content type (its cross-site
// gate), so the request carries an empty JSON body.
func runTransitionVerb(cmd, base, key string, keys, args []string) int {
	tail, _, rest, ok := takeKey(cmd, keys, args)
	if !ok {
		return 2
	}
	fs := newFlagSet(cmd)
	g, code := parseGlobals(fs, rest)
	if g == nil {
		return code
	}
	var out singleResponse
	path := base + "/" + tail + "/transitions/" + url.PathEscape(key)
	if err := g.client.Do(g.ctx, http.MethodPost, path, map[string]any{}, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out.Data)
}

`)
	}
	if endpoint {
		sb.WriteString(`// endpointCall is one gofastr.settings.yml endpoint behind its command:
// the method, the route segments ("{name}" takes the next positional
// argument), and the flag table. query sends the flags as the query
// string; otherwise they, or --json, are the JSON body.
type endpointCall struct {
	method string
	segs   []string
	query  bool
	fields []mutationField
}

// runEndpointVerb is the shared endpoint body: fill the path values from
// the positional arguments, send the flags, print the JSON response.
func runEndpointVerb(cmd, base string, call endpointCall, args []string) int {
	var names []string
	for _, seg := range call.segs {
		if strings.HasPrefix(seg, "{") {
			names = append(names, "<"+strings.Trim(seg, "{}")+">")
		}
	}
	path := base
	for _, seg := range call.segs {
		if !strings.HasPrefix(seg, "{") {
			path += "/" + seg
			continue
		}
		if len(args) == 0 || strings.HasPrefix(args[0], "-") {
			fmt.Fprintf(os.Stderr, "usage: %s %s %s [flags]\n", binaryName, cmd, strings.Join(names, " "))
			return 2
		}
		path += "/" + url.PathEscape(args[0])
		args = args[1:]
	}
	fs := newFlagSet(cmd)
	jsonBody := new(string)
	if !call.query {
		jsonBody = fs.String("json", "", "raw JSON body: inline, @file, or - for stdin")
	}
	apply := fieldFlags(fs, call.fields)
	g, code := parseGlobals(fs, args)
	if g == nil {
		return code
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	anyField := false
	for _, f := range call.fields {
		if given[f.flag] {
			anyField = true
		} else if f.required && *jsonBody == "" {
			fmt.Fprintf(os.Stderr, "%s %s: --%s is required\n", binaryName, cmd, f.flag)
			return 2
		}
	}
	var body any
	switch {
	case call.query:
		values := map[string]any{}
		q := url.Values{}
		for _, f := range call.fields {
			if !given[f.flag] {
				continue
			}
			if err := apply(f.flag, values); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			q.Set(f.wire, fmt.Sprint(values[f.wire]))
		}
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
	case !anyField && *jsonBody == "":
		// Every field is optional and none was given. buildBody would
		// refuse that ("nothing to send"), which suits update but not an
		// endpoint. Send an empty JSON object rather than no body: a write
		// with no Content-Type counts as form-shaped
		// (handler.IsForgeableRequest), and a CSRF gate in front of the
		// route would demand a token for it.
		body = map[string]any{}
	default:
		b, code := buildBody(fs, *jsonBody, apply)
		if code != 0 {
			return code
		}
		body = b
	}
	var out any
	if err := g.client.Do(g.ctx, call.method, path, body, &out); err != nil {
		return apiFail(err)
	}
	return printJSON(out)
}

`)
	}
	if batchJSON {
		sb.WriteString(`// runBatchJSONVerb is the shared batch-create/batch-update body: send a
// --json array through the atomic _batch route wrapped into the
// {items: [...]} envelope. A rolled-back batch prints its
// {committed, results[]} envelope and exits 1.
func runBatchJSONVerb(cmd, base, method string, args []string) int {
	fs := newFlagSet(cmd)
	jsonBody := fs.String("json", "", "JSON array of items: inline, @file, or - for stdin")
	g, code := parseGlobals(fs, args)
	if g == nil {
		return code
	}
	items, code := readJSONArrayArg(*jsonBody)
	if code != 0 {
		return code
	}
	resp, code := doBatch(g, method, base+"/_batch", map[string]any{"items": items})
	if code != 0 {
		return code
	}
	return printBatch(resp)
}

`)
	}
	if batchDel {
		sb.WriteString(`// runBatchDeleteVerb deletes the positional ids in one transaction. Ids
// may appear before or after flags: flag.Parse stops at the first
// positional, so the trailing ones are collected from fs.Args().
func runBatchDeleteVerb(cmd, base string, args []string) int {
	var ids []string
	for len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		ids = append(ids, args[0])
		args = args[1:]
	}
	fs := newFlagSet(cmd)
	g, code := parseGlobals(fs, args)
	if g == nil {
		return code
	}
	for _, id := range fs.Args() {
		if id != "" && id[0] == '-' {
			fmt.Fprintln(os.Stderr, binaryName+" "+cmd+": flags must precede trailing ids (got "+id+" after an id)")
			return 2
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		fmt.Fprintln(os.Stderr, "usage: "+binaryName+" "+cmd+" <id> [id...]")
		return 2
	}
	resp, code := doBatch(g, http.MethodDelete, base+"/_batch", map[string]any{"ids": ids})
	if code != 0 {
		return code
	}
	return printBatch(resp)
}

`)
	}
	if watch {
		sb.WriteString(`// runWatchVerb is the shared watch body: stream the entity's live event
// feed until interrupted; each event is one JSON line on stdout. watch is
// the typed client method expression ((*client.Client).Watch<Entity>),
// bound by each entity's wrapper.
func runWatchVerb(cmd string, watch func(c *client.Client, ctx context.Context, fn func(event string, data []byte) error) error, args []string) int {
	fs := newFlagSet(cmd)
	g, code := parseGlobals(fs, args)
	if g == nil {
		return code
	}
	// g.ctx is already signal-cancellable: parseGlobals built it with
	// signal.NotifyContext, so Ctrl-C cancels the stream here too.
	err := watch(g.client, g.ctx, func(event string, data []byte) error {
		fmt.Printf("{\"event\":%q,\"data\":%s}\n", event, data)
		return nil
	})
	if err != nil && g.ctx.Err() == nil {
		return apiFail(err)
	}
	return 0
}

`)
	}
	return sb.String()
}

func quoteList(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(quoted, ", ")
}
