package appgen

import (
	"cmp"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/codegen"
)

// entryConfig reads one built-in generator's settings entry strictly: an
// unknown key, a value of the wrong type, or a comma string where a list
// belongs is an error naming the entry, never a value dropped on the floor.
type entryConfig struct {
	index int
	name  string
	cfg   map[string]any
}

func (e entryConfig) errorf(format string, args ...any) error {
	if e.index < 0 {
		return fmt.Errorf("codegen.generators (%s): %s", e.name, fmt.Sprintf(format, args...))
	}
	return fmt.Errorf("codegen.generators[%d] (%s): %s", e.index, e.name, fmt.Sprintf(format, args...))
}

// known refuses a config key outside keys.
func (e entryConfig) known(keys ...string) error {
	for _, key := range slices.Sorted(maps.Keys(e.cfg)) {
		if !slices.Contains(keys, key) {
			return e.errorf("unknown config key %q (one of %s)", key, strings.Join(slices.Sorted(slices.Values(keys)), ", "))
		}
	}
	return nil
}

// str reads a string key. ok is false when the key is absent. Only a key in
// emptyOK may hold "".
func (e entryConfig) str(key string, emptyOK bool) (v string, ok bool, err error) {
	raw, present := e.cfg[key]
	if !present {
		return "", false, nil
	}
	s, isString := raw.(string)
	s = strings.TrimSpace(s)
	if !isString || (s == "" && !emptyOK) {
		return "", false, e.errorf("config.%s must be a non-empty string", key)
	}
	return s, true, nil
}

// list reads a list of non-empty strings. A string value is refused with
// the list spelling, so the comma form older releases read fails loudly.
func (e entryConfig) list(key string) ([]string, error) {
	raw, present := e.cfg[key]
	if !present {
		return nil, nil
	}
	if s, isString := raw.(string); isString {
		return nil, e.errorf("config.%s must be a YAML list, not a string: write %s: %s", key, key, yamlList(splitCommaList(s)))
	}
	items, isList := raw.([]any)
	if !isList || len(items) == 0 {
		return nil, e.errorf("config.%s must be a non-empty list of names", key)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, isString := item.(string)
		s = strings.TrimSpace(s)
		if !isString || s == "" || strings.Contains(s, ",") {
			return nil, e.errorf("config.%s entries must be single names, one per list item", key)
		}
		out = append(out, s)
	}
	return out, nil
}

// verbs reads the cli entry's verbs: a list for every entity, or a map from
// entity to its list. It returns the --verbs spelling parseVerbSelection
// reads, so both sources share one validator.
func (e entryConfig) verbs() (string, error) {
	raw, present := e.cfg["verbs"]
	if !present {
		return "", nil
	}
	if m, isMap := raw.(map[string]any); isMap {
		if len(m) == 0 {
			return "", e.errorf("config.verbs must name at least one entity")
		}
		pairs := make([]string, 0, len(m))
		for _, entity := range slices.Sorted(maps.Keys(m)) {
			if strings.ContainsAny(entity, "=;,") || strings.TrimSpace(entity) == "" {
				return "", e.errorf("config.verbs key %q is not an entity name", entity)
			}
			if s, isString := m[entity].(string); isString {
				return "", e.errorf("config.verbs.%s must be a YAML list, not a string: write %s: %s under verbs", yamlScalar(entity), yamlScalar(entity), yamlList(splitCommaList(s)))
			}
			sub := entryConfig{index: e.index, name: e.name, cfg: map[string]any{"verbs." + entity: m[entity]}}
			list, err := sub.list("verbs." + entity)
			if err != nil {
				return "", err
			}
			if err := e.singleVerbs(list); err != nil {
				return "", err
			}
			pairs = append(pairs, entity+"="+strings.Join(list, ","))
		}
		return strings.Join(pairs, ";"), nil
	}
	list, err := e.list("verbs")
	if _, isString := raw.(string); isString {
		return "", err // the list spelling for the string it read
	}
	if err != nil {
		return "", e.errorf("config.verbs must be a list of verbs, or a map from entity to its list")
	}
	if err := e.singleVerbs(list); err != nil {
		return "", err
	}
	return strings.Join(list, ","), nil
}

// singleVerbs refuses a list item carrying --verbs syntax: joined, it would
// turn a global list into per-entity pairs.
func (e entryConfig) singleVerbs(list []string) error {
	for _, v := range list {
		if strings.ContainsAny(v, "=;") {
			return e.errorf("config.verbs entries must be single verbs, not %q; name the entity as a key under verbs", v)
		}
	}
	return nil
}

// decodeCLIEntry turns a `cli` settings entry into the options the flags
// would set. output is relative to the project, like --out.
func decodeCLIEntry(index int, gen codegen.GeneratorConfig) (CliOptions, error) {
	e := entryConfig{index: index, name: "cli", cfg: gen.Config}
	opts := CliOptions{ApiPrefix: "api", entryIndex: index}
	if gen.Source.Type != "" {
		return opts, e.errorf("source does not apply; the CLI reads entities/, or the document named by config.from_openapi")
	}
	if err := e.known("binary", "api_prefix", "only", "exclude", "verbs", "from_openapi"); err != nil {
		return opts, err
	}
	opts.OutDir = strings.TrimSpace(gen.Output)
	var err error
	if opts.Binary, _, err = e.str("binary", false); err != nil {
		return opts, err
	}
	if v, ok, err := e.str("api_prefix", true); err != nil {
		return opts, err
	} else if ok {
		opts.ApiPrefix = v
		opts.apiPrefixSet = true
	}
	if opts.only, err = e.list("only"); err != nil {
		return opts, err
	}
	if opts.exclude, err = e.list("exclude"); err != nil {
		return opts, err
	}
	if opts.verbs, err = e.verbs(); err != nil {
		return opts, err
	}
	if _, _, err := parseVerbSelection(opts.verbs); err != nil {
		return opts, e.errorf("config.verbs: %s", strings.TrimPrefix(err.Error(), "--verbs: "))
	}
	if opts.FromOpenAPI, _, err = e.str("from_openapi", false); err != nil {
		return opts, err
	}
	if strings.Contains(opts.FromOpenAPI, "://") {
		return opts, e.errorf("config.from_openapi must be a file in the project, not a URL: every build would fetch it. Save the document and name the file")
	}
	if err := checkCLIOptions(opts); err != nil {
		return opts, e.errorf("%v", err)
	}
	return opts, nil
}

// decodeSDKEntry turns an `sdk` settings entry into SDK options. output is
// left to the caller: the engine and the direct command root it
// differently.
func decodeSDKEntry(index int, gen codegen.GeneratorConfig) (SdkOptions, error) {
	e := entryConfig{index: index, name: "sdk", cfg: gen.Config}
	var opts SdkOptions
	if err := e.known("name", "api_prefix", "module", "base_url", "sdk_version", "targets", "only", "exclude"); err != nil {
		return opts, err
	}
	for _, field := range []struct {
		key string
		dst *string
	}{
		{"name", &opts.name},
		{"module", &opts.module},
		{"base_url", &opts.baseURL},
		{"sdk_version", &opts.sdkVersion},
	} {
		v, _, err := e.str(field.key, false)
		if err != nil {
			return opts, err
		}
		*field.dst = v
	}
	var err error
	if opts.apiPrefix, _, err = e.str("api_prefix", true); err != nil {
		return opts, err
	}
	if opts.targets, err = e.list("targets"); err != nil {
		return opts, err
	}
	if opts.only, err = e.list("only"); err != nil {
		return opts, err
	}
	if opts.exclude, err = e.list("exclude"); err != nil {
		return opts, err
	}
	return opts, nil
}

// ApplyCLIConfigDefaults fills what the flags left unset from a cli entry in
// the project's gofastr.settings.yml, so `gofastr generate cli` and `gofastr
// build` produce the same CLI. With --out or --binary it uses the entry
// that names the same directory or binary; without, the only entry (two or
// more need --out to pick). It reports whether an entry was used.
func ApplyCLIConfigDefaults(opts *CliOptions) (bool, error) {
	discovery, err := codegen.DiscoverConfig(".")
	if err != nil {
		return false, err
	}
	if !discovery.Found || !sameDir(discovery.ProjectDir, ".") {
		return false, nil
	}
	type entry struct {
		index int
		opts  CliOptions
	}
	var entries []entry
	for i, gen := range discovery.Config.Codegen.Generators {
		if gen.Name != "cli" || gen.Extension != "" {
			continue
		}
		decoded, err := decodeCLIEntry(i, gen)
		if err != nil {
			return false, err
		}
		entries = append(entries, entry{i, decoded})
	}
	abs, err := filepath.Abs(".")
	if err != nil {
		return false, err
	}
	// An entry without binary or output takes the defaults a run would.
	defaultBinary := strings.ToLower(filepath.Base(abs))
	var picked *CliOptions
	switch {
	case opts.OutDir != "" || opts.Binary != "":
		for i := range entries {
			e := &entries[i].opts
			binary := cmp.Or(e.Binary, defaultBinary)
			outDir := cmp.Or(e.OutDir, filepath.Join("cmd", binary))
			if (opts.OutDir != "" && filepath.Clean(opts.OutDir) == filepath.Clean(outDir)) ||
				(opts.OutDir == "" && opts.Binary == binary) {
				picked = e
				break
			}
		}
	case len(entries) == 1:
		picked = &entries[0].opts
	case len(entries) > 1:
		return false, fmt.Errorf("gofastr.settings.yml lists %d cli entries; pass --out or --binary to pick one", len(entries))
	}
	if picked == nil {
		return false, nil
	}
	if opts.OutDir == "" {
		opts.OutDir = picked.OutDir
	}
	if opts.Binary == "" {
		opts.Binary = picked.Binary
	}
	opts.entryIndex = picked.entryIndex
	if !opts.flagSet["from-openapi"] {
		opts.FromOpenAPI = picked.FromOpenAPI
	} else if opts.FromOpenAPI != "" {
		// --from-openapi is spec-driven: the entry's entity selection does
		// not apply, and copying it would refuse flags nobody typed. An
		// entry for the same document lends its api_prefix, so the flag
		// builds the CLI a settings run does.
		if !opts.apiPrefixSet && picked.apiPrefixSet && sameOpenAPISource(picked.FromOpenAPI, opts.FromOpenAPI) {
			opts.ApiPrefix, opts.apiPrefixSet = picked.ApiPrefix, true
		}
		return true, checkCLIOptions(*opts)
	}
	if !opts.apiPrefixSet && picked.apiPrefixSet {
		opts.ApiPrefix, opts.apiPrefixSet = picked.ApiPrefix, true
	}
	// A flag given empty (--only=) still wins: it clears the entry's value.
	if !opts.flagSet["only"] {
		opts.only = picked.only
	}
	if !opts.flagSet["exclude"] {
		opts.exclude = picked.exclude
	}
	if !opts.flagSet["verbs"] {
		opts.verbs = picked.verbs
	}
	return true, checkCLIOptions(*opts)
}

// sameOpenAPISource reports whether two --from-openapi values name the
// same document: equal URLs, or local paths equal once cleaned.
func sameOpenAPISource(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if strings.Contains(a, "://") || strings.Contains(b, "://") {
		return a == b
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func sameDir(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	return errA == nil && errB == nil && absA == absB
}

// cliEntryYAML renders the cli entry that reproduces a run, for the next
// steps of a run no entry drove. It refuses a value that is not UTF-8:
// yamlScalar's strconv.Quote writes such a byte as \xff, which YAML 1.2
// reads as U+00FF, so the entry would name a different path.
func cliEntryYAML(opts CliOptions, binary string) (string, error) {
	values := append([]string{opts.OutDir, binary, opts.FromOpenAPI, opts.ApiPrefix, opts.verbs}, opts.only...)
	for _, v := range append(values, opts.exclude...) {
		if !utf8.ValidString(v) {
			return "", fmt.Errorf("%q is not valid UTF-8, which a gofastr.settings.yml entry cannot carry", v)
		}
	}
	var b strings.Builder
	b.WriteString("codegen:\n  generators:\n    - name: cli\n")
	fmt.Fprintf(&b, "      output: %s\n", yamlScalar(opts.OutDir))
	b.WriteString("      config:\n")
	fmt.Fprintf(&b, "        binary: %s\n", yamlScalar(binary))
	if opts.FromOpenAPI != "" {
		if strings.Contains(opts.FromOpenAPI, "://") {
			b.WriteString("        from_openapi: openapi.json  # save the document in the project; an entry never fetches a URL\n")
		} else {
			fmt.Fprintf(&b, "        from_openapi: %s\n", yamlScalar(opts.FromOpenAPI))
		}
		if opts.apiPrefixSet {
			fmt.Fprintf(&b, "        api_prefix: %s\n", yamlScalar(opts.ApiPrefix))
		}
		return b.String(), nil
	}
	if opts.ApiPrefix != "api" {
		fmt.Fprintf(&b, "        api_prefix: %s\n", yamlScalar(opts.ApiPrefix))
	}
	for _, list := range []struct {
		key   string
		items []string
	}{{"only", opts.only}, {"exclude", opts.exclude}} {
		if len(list.items) > 0 {
			fmt.Fprintf(&b, "        %s: %s\n", list.key, yamlList(list.items))
		}
	}
	// The parsed selection, not the flag text: a repeated entity in --verbs
	// would be a duplicate YAML key.
	global, per, err := parseVerbSelection(opts.verbs)
	switch {
	case err != nil || (global == nil && per == nil):
	case per == nil:
		fmt.Fprintf(&b, "        verbs: %s\n", yamlList(global))
	default:
		b.WriteString("        verbs:\n")
		for _, entity := range slices.Sorted(maps.Keys(per)) {
			verbs := per[entity]
			if slices.Equal(verbs, cliVerbs) {
				verbs = []string{"*"}
			}
			fmt.Fprintf(&b, "          %s: %s\n", yamlScalar(entity), yamlList(verbs))
		}
	}
	return b.String(), nil
}

// yamlList renders a flow list; "*" is quoted, since bare it is a YAML
// alias.
func yamlList(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = yamlScalar(item)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// yamlScalar quotes a value unless YAML reads it plain as the same string:
// a name or path that starts with a letter and is no boolean or null word.
func yamlScalar(v string) string {
	plain := v != "" && (v[0] >= 'a' && v[0] <= 'z' || v[0] >= 'A' && v[0] <= 'Z') &&
		strings.IndexFunc(v, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_./-", r))
		}) < 0 &&
		!slices.Contains([]string{"true", "false", "yes", "no", "on", "off", "y", "n", "null"}, strings.ToLower(v))
	if plain {
		return v
	}
	return strconv.Quote(v)
}
