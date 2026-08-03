package analyzers

import (
	"bufio"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"github.com/DonaldMurillo/gofastr/framework/semcov"
)

func init() {
	contracts.Register(&contracts.Analyzer{
		Name: "testing",
		Doc:  "Semantic coverage: which routes, permissions, roles, entity operations, lifecycle hooks, and event subscribers a test run actually exercised.",
		Rules: []string{
			contracts.RuleRouteNotExercised,
			contracts.RulePermissionNotExercised,
			contracts.RuleEntityNotExercised,
			contracts.RuleCoverageBelowMinimum,
			contracts.RuleDisabledTest,
			contracts.RuleNoCoverageManifest,
			contracts.RuleCoverageManifestBroken,
			contracts.RuleHookNotFired,
			contracts.RuleEventNotEmitted,
			contracts.RuleRoleNotExercised,
		},
		Run: runTesting,
	})
}

func runTesting(p *contracts.Pass) ([]contracts.Diagnostic, error) {
	out := disabledTests(p)
	out = append(out, lineCoverage(p)...)
	out = append(out, semanticCoverage(p)...)
	return out, nil
}

// ----------------------------------------------------------------------
// GOFASTR1106 / 1101 / 1102 / 1103 — the semantic-coverage manifest.
// ----------------------------------------------------------------------

func semanticCoverage(p *contracts.Pass) []contracts.Diagnostic {
	cfg := p.Config.Coverage
	if !cfg.Routes && !cfg.Permissions && !cfg.Entities {
		return nil
	}
	table := Routes(p)
	entities := Entities(p)
	hooks := Hooks(p)
	subs := EventSubs(p)
	roles := Roles(p)
	// Nothing discoverable to demand coverage of. Hooks and subscriptions
	// count here too: a package that registers them but whose routes are
	// wired elsewhere still has a surface worth checking, and leaving them
	// out of the guard meant they were never examined.
	if !table.Registered && len(entities) == 0 && len(hooks) == 0 &&
		len(subs) == 0 && len(roles) == 0 {
		return nil
	}

	manifest, err := semcov.Read(p.Root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Absence and drift are different failures, treated
			// differently on purpose. A fresh clone has never run its
			// tests; walling the first verify off behind a full test run
			// would teach people that verify is something to skip.
			return []contracts.Diagnostic{{
				RuleID:  contracts.RuleNoCoverageManifest,
				File:    semcov.FileName,
				Message: "no semantic-coverage manifest — the route, permission, and entity coverage checks did not run",
			}}
		}
		// A manifest that exists but cannot be read is corruption, not
		// absence. Relaxing enforcement exactly when the record is
		// untrustworthy would invert the guarantee.
		// Its own rule, not a severity bump on the absence rule: Run
		// assigns severity from the catalog and config by design, so an
		// analyzer-set Severity here was silently discarded and corruption
		// reported at the same `info` as absence.
		return []contracts.Diagnostic{{
			RuleID:     contracts.RuleCoverageManifestBroken,
			File:       semcov.FileName,
			Message:    "semantic-coverage manifest is unreadable: " + err.Error(),
			Suggestion: "delete " + semcov.FileName + " and re-run `go test ./...`",
		}}
	}

	var out []contracts.Diagnostic
	if cfg.Routes {
		for _, r := range table.Routes {
			if manifest.CoveredRoute(r.Method, r.Pattern) {
				continue
			}
			d := diag(p, contracts.RuleRouteNotExercised, r.File, r.Pos,
				fmt.Sprintf("no recorded test request reached %s %s", r.Method, r.Pattern))
			d.Evidence = map[string]string{"method": r.Method, "pattern": r.Pattern}
			out = append(out, d)
		}
	}
	if cfg.Entities {
		for _, e := range entities {
			if !e.Exposed() || manifest.CoveredEntity(e.Name) {
				continue
			}
			d := diag(p, contracts.RuleEntityNotExercised, e.File, e.Pos,
				fmt.Sprintf("no recorded test exercised any CRUD operation on entity %q", e.Name))
			d.Evidence = map[string]string{"entity": e.Name}
			out = append(out, d)
		}
		// Hooks ride with the entity demand: they are entity behaviour,
		// and an app that has opted out of entity coverage has opted out
		// of this too.
		for _, h := range hooks {
			if manifest.CoveredHook(h.Entity, h.Type) {
				continue
			}
			d := diag(p, contracts.RuleHookNotFired, h.File, h.Pos, fmt.Sprintf(
				"the %s hook on %q never ran during the recorded test run", h.Type, h.Entity))
			d.Evidence = map[string]string{"entity": h.Entity, "hook": h.Type}
			out = append(out, d)
		}
	}
	if cfg.Entities {
		// Subscriptions ride with the entity demand for the same reason
		// hooks do: they are background behaviour that only a real
		// operation can trigger.
		for _, s := range subs {
			if manifest.CoveredEvent(s.Type) {
				continue
			}
			d := diag(p, contracts.RuleEventNotEmitted, s.File, s.Pos, fmt.Sprintf(
				"nothing published %q during the recorded test run, so this subscriber never ran", s.Type))
			d.Evidence = map[string]string{"event": s.Type}
			out = append(out, d)
		}
	}
	if cfg.Permissions {
		out = append(out, uncoveredPermissions(p, manifest)...)
		for _, r := range roles {
			if manifest.CoveredRole(r.Role) {
				continue
			}
			d := diag(p, contracts.RuleRoleNotExercised, r.File, r.Pos, fmt.Sprintf(
				"no recorded test request carried the %q role", r.Role))
			d.Evidence = map[string]string{"role": r.Role}
			out = append(out, d)
		}
	}
	return out
}

// uncoveredPermissions compares the permission strings declared in source
// against the ones the manifest saw evaluated.
func uncoveredPermissions(p *contracts.Pass, manifest *semcov.Manifest) []contracts.Diagnostic {
	declared := declaredPermissions(p)
	names := make([]string, 0, len(declared))
	for name := range declared {
		names = append(names, name)
	}
	sort.Strings(names)

	var out []contracts.Diagnostic
	for _, name := range names {
		if manifest.CoveredPermission(name) {
			continue
		}
		site := declared[name]
		d := diag(p, contracts.RulePermissionNotExercised, site.File, site.Pos,
			fmt.Sprintf("permission %q was never evaluated during the recorded test run", name))
		d.Evidence = map[string]string{"permission": name}
		out = append(out, d)
	}
	return out
}

// permSite is where a permission string was declared, so the finding can
// point at the declaration rather than at nothing.
type permSite struct {
	File string
	Pos  token.Pos
}

// declaredPermissions finds permission strings in entity Access blocks
// and in `access.Require("…")`-shaped calls. A permission is recognised
// by its `resource:verb` shape, which is the framework's convention and
// specific enough that ordinary strings do not collide with it.
func declaredPermissions(p *contracts.Pass) map[string]permSite {
	out := map[string]permSite{}
	for _, f := range p.AppFiles() {
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			_, method, call, isCall := selectorCall(n)
			if !isCall {
				return true
			}
			// Only permission-shaped call sites, so an arbitrary
			// "scheme:value" string elsewhere is not mistaken for one.
			switch method {
			case "Require", "RequirePermission", "Can", "HasScope", "RequireAPIScopes":
			default:
				return true
			}
			for _, arg := range call.Args {
				s, litOK := stringLit(arg)
				if !litOK || !looksLikePermission(s) {
					continue
				}
				if _, seen := out[s]; !seen {
					out[s] = permSite{File: f.Rel, Pos: call.Pos()}
				}
			}
			return true
		})
	}
	// Entity access blocks declare permissions too, and they are the ones
	// most likely to go unexercised — nothing in the app calls them
	// explicitly, so only a request can prove them.
	for _, e := range Entities(p) {
		for _, perm := range e.AccessPermissions {
			if _, seen := out[perm]; !seen {
				out[perm] = permSite{File: e.File, Pos: e.Pos}
			}
		}
	}
	return out
}

func looksLikePermission(s string) bool {
	i := strings.Index(s, ":")
	if i <= 0 || i == len(s)-1 {
		return false
	}
	if strings.ContainsAny(s, " /\\") {
		return false
	}
	return true
}

// ----------------------------------------------------------------------
// GOFASTR1105 — disabled tests.
// ----------------------------------------------------------------------

// laneBoundaryPhrases mark a skip that describes an executable boundary
// rather than missing coverage: a capability the machine lacks, a lane
// the run opted out of. The reason is printed in test output, so the
// absence is visible rather than hidden — which is the whole distinction
// this rule draws.
var laneBoundaryPhrases = []string{
	"not set", "unavailable", "not available", "requires /bin/sh",
	"requires docker", "no usable chromium", "no sandbox backend",
	"not supported on", "only runs on", "live agent tests",
	"short mode", "-short",
}

// debtPhrases mark a skip that is hiding work.
var debtPhrases = []string{
	"not yet", "todo", "temporarily disabled", "restore this test",
	"restore once", "being reimplemented", "no session cookie",
	"flaky", "fixme", "broken",
}

func disabledTests(p *contracts.Pass) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, f := range p.TestFiles() {
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		lines := p.Lines(f.Rel)
		ast.Inspect(file, func(n ast.Node) bool {
			recv, method, _, isCall := selectorCall(n)
			if !isCall {
				return true
			}
			if method != "Skip" && method != "Skipf" && method != "SkipNow" {
				return true
			}
			if id, isIdent := recv.(*ast.Ident); !isIdent || (id.Name != "t" && id.Name != "b") {
				return true
			}
			pos := p.Position(n.Pos())
			if hasLegacyAnnotation(lines, pos.Line, "allow-skip:") {
				return true
			}
			line := strings.ToLower(p.Line(f.Rel, pos.Line))
			for _, phrase := range laneBoundaryPhrases {
				if strings.Contains(line, phrase) {
					return true
				}
			}
			// A `testing.Short()` guard just above is the canonical lane
			// boundary even when the message does not say so.
			if guardedByShort(lines, pos.Line) {
				return true
			}
			// Only report skips that read as deferred work. A skip with
			// an unfamiliar reason is left alone: the alternative is
			// flagging every conditional skip in the suite, which turns
			// the rule into noise and gets it switched off.
			isDebt := false
			for _, phrase := range debtPhrases {
				if strings.Contains(line, phrase) {
					isDebt = true
					break
				}
			}
			if !isDebt {
				return true
			}
			out = append(out, diag(p, contracts.RuleDisabledTest, f.Rel, n.Pos(),
				"this skip defers work rather than marking a lane boundary — the suite reports it as a pass"))
			return true
		})
	}
	return out
}

func guardedByShort(lines []string, lineNo int) bool {
	for i, seen := lineNo-2, 0; i >= 0 && seen < 4; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		seen++
		if strings.Contains(trimmed, "testing.Short()") {
			return true
		}
		if trimmed == "}" || strings.HasPrefix(trimmed, "func ") {
			return false
		}
	}
	return false
}

// ----------------------------------------------------------------------
// GOFASTR1104 — line coverage floor.
// ----------------------------------------------------------------------

// coverageProfiles are the conventional locations a `go test
// -coverprofile` output lands in, tried in order when none is configured.
var coverageProfiles = []string{"coverage.out", ".gofastr/coverage.out", "cover.out", "coverage.txt"}

func lineCoverage(p *contracts.Pass) []contracts.Diagnostic {
	cfg := p.Config.Coverage
	if !cfg.MinimumSet {
		return nil
	}
	profile := cfg.Profile
	if profile == "" {
		for _, candidate := range coverageProfiles {
			if _, err := os.Stat(filepath.Join(p.Root, candidate)); err == nil {
				profile = candidate
				break
			}
		}
	}
	if profile == "" {
		return []contracts.Diagnostic{{
			RuleID:     contracts.RuleCoverageBelowMinimum,
			File:       "coverage.out",
			Message:    fmt.Sprintf("a coverage floor of %.1f%% is configured but no coverage profile was found", cfg.Minimum),
			Suggestion: "run `go test ./... -coverprofile=coverage.out`, or set contracts.coverage.profile to where yours lands",
		}}
	}
	pct, err := readCoverageProfile(filepath.Join(p.Root, profile))
	if err != nil {
		return []contracts.Diagnostic{{
			RuleID:  contracts.RuleCoverageBelowMinimum,
			File:    profile,
			Message: "coverage profile could not be read: " + err.Error(),
		}}
	}
	if pct >= cfg.Minimum {
		return nil
	}
	return []contracts.Diagnostic{{
		RuleID: contracts.RuleCoverageBelowMinimum,
		File:   profile,
		Message: fmt.Sprintf("line coverage is %.1f%%, below the configured floor of %.1f%%",
			pct, cfg.Minimum),
		Evidence: map[string]string{
			"actual":  fmt.Sprintf("%.2f", pct),
			"minimum": fmt.Sprintf("%.2f", cfg.Minimum),
		},
	}}
}

// readCoverageProfile computes the statement-weighted percentage from a
// `go test -coverprofile` file. Weighting by statement count is what `go
// tool cover -func` reports; averaging the per-block percentages instead
// would over-weight tiny blocks and quietly inflate the number.
func readCoverageProfile(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var total, covered int
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<22)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		// name.go:line.col,line.col numberOfStatements count
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		statements, err1 := strconv.Atoi(fields[len(fields)-2])
		count, err2 := strconv.Atoi(fields[len(fields)-1])
		if err1 != nil || err2 != nil {
			continue
		}
		total += statements
		if count > 0 {
			covered += statements
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	if total == 0 {
		return 0, fmt.Errorf("profile records no statements")
	}
	return float64(covered) / float64(total) * 100, nil
}
