package cliplugin

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// PluginSource indicates where a plugin entry originated.
type PluginSource string

const (
	SourceGlobal  PluginSource = "global"
	SourceProject PluginSource = "project"
)

// PluginStatus summarizes whether a plugin can currently execute.
type PluginStatus string

const (
	StatusReady        PluginStatus = "ready"
	StatusMissing      PluginStatus = "missing"
	StatusUntrusted    PluginStatus = "untrusted"
	StatusIncompatible PluginStatus = "incompatible"
	StatusConflict     PluginStatus = "conflict"
	StatusInvalid      PluginStatus = "invalid"
)

// ResolvedPlugin is the merged, validated view of one configured plugin.
type ResolvedPlugin struct {
	Name                string       `json:"name"`
	Source              PluginSource `json:"source"`
	Status              PluginStatus `json:"status"`
	Summary             string       `json:"summary,omitempty"`
	InstallHint         string       `json:"install_hint,omitempty"`
	Command             []string     `json:"command,omitempty"`
	ProjectDir          string       `json:"project_dir,omitempty"`
	ProjectLocalBin     bool         `json:"project_local_bin,omitempty"`
	SHA256              string       `json:"sha256,omitempty"`
	AllowEnv            []string     `json:"allow_env,omitempty"`
	AllowEnvFlags       []string     `json:"allow_env_flags,omitempty"`
	AllowHooks          []string     `json:"allow_hooks,omitempty"`
	AllowOverrides      []string     `json:"allow_overrides,omitempty"`
	ConflictedOverrides []string     `json:"conflicted_overrides,omitempty"`
	Err                 error        `json:"-"`
	ErrorMessage        string       `json:"error,omitempty"`
}

// Diagnostic is the full health check result produced by Diagnose for one
// plugin (combining static resolution with a live manifest handshake).
type Diagnostic struct {
	Name                string        `json:"name"`
	Source              PluginSource  `json:"source"`
	Status              PluginStatus  `json:"status"`
	Version             string        `json:"version,omitempty"`
	ProtocolVersion     int           `json:"protocol_version,omitempty"`
	Summary             string        `json:"summary,omitempty"`
	InstallHint         string        `json:"install_hint,omitempty"`
	Command             []string      `json:"command,omitempty"`
	ProjectDir          string        `json:"project_dir,omitempty"`
	Subcommands         []CommandInfo `json:"subcommands,omitempty"`
	AllowEnv            []string      `json:"allow_env,omitempty"`
	AllowEnvFlags       []string      `json:"allow_env_flags,omitempty"`
	AllowHooks          []string      `json:"allow_hooks,omitempty"`
	AllowOverrides      []string      `json:"allow_overrides,omitempty"`
	ConflictedOverrides []string      `json:"conflicted_overrides,omitempty"`
	DeclaredHooks       []HookSpec    `json:"declared_hooks,omitempty"`
	DeclaredOverr       []string      `json:"declared_overrides,omitempty"`
	Error               string        `json:"error,omitempty"`
}

// ResolveOptions configures plugin resolution.
type ResolveOptions struct {
	UserConfig    UserConfig
	ProjectConfig ProjectConfig
	// Builtins maps every built-in command name and alias to its canonical
	// built-in command name (e.g. "m" -> "migrate", "build" -> "build").
	Builtins map[string]string
	// SkipDigests skips hashing project executables on disk (used for
	// fast help rendering where no binary is executed).
	SkipDigests bool
}

// Resolve merges global user registrations and project declarations without
// running any external binary. Global entries are never overwritten or
// disabled by conflicting project declarations; conflicting project entries
// are reported separately alongside the intact global entry.
func Resolve(opts ResolveOptions) []ResolvedPlugin {
	globalByName := map[string]ResolvedPlugin{}
	var projectEntries []ResolvedPlugin

	// 1. Global registrations from ~/.config/gofastr/plugins.yml.
	for _, gp := range opts.UserConfig.Plugins {
		rp := ResolvedPlugin{
			Name:           gp.Name,
			Source:         SourceGlobal,
			Status:         StatusReady,
			Summary:        gp.Summary,
			Command:        slices.Clone(gp.Command),
			AllowEnv:       slices.Clone(gp.AllowEnv),
			AllowEnvFlags:  slices.Clone(gp.AllowEnvFlags),
			AllowHooks:     slices.Clone(gp.AllowHooks),
			AllowOverrides: slices.Clone(gp.AllowOverrides),
		}
		if canonical, collides := opts.Builtins[gp.Name]; collides {
			rp.setError(StatusConflict, fmt.Errorf("%w: %q collides with built-in command %q", ErrBuiltinCollision, gp.Name, canonical))
		} else if err := validateGrantsAgainstBuiltins(gp.AllowHooks, gp.AllowOverrides, opts.Builtins); err != nil {
			rp.setError(StatusInvalid, err)
		} else {
			resolved, _, err := ResolveExecutable(gp.Command, "", false)
			if err != nil {
				rp.setError(StatusInvalid, err)
			} else {
				rp.Command = resolved
			}
		}
		globalByName[gp.Name] = rp
	}

	// 2. Project declarations from gofastr.settings.yml.
	if opts.ProjectConfig.Found {
		canonProjDir := canonicalDir(opts.ProjectConfig.ProjectDir)
		trustMap, ambiguousTrust := indexTrust(opts.UserConfig.TrustedProjects)

		for _, pp := range opts.ProjectConfig.Plugins {
			// Declaration-only entry (documents expected global plugin + install hint).
			if len(pp.Command) == 0 {
				if existing, ok := globalByName[pp.Name]; ok {
					if existing.Summary == "" {
						existing.Summary = pp.Description
					}
					if existing.InstallHint == "" {
						existing.InstallHint = pp.Install
					}
					globalByName[pp.Name] = existing
					continue
				}
				rp := ResolvedPlugin{
					Name:        pp.Name,
					Source:      SourceProject,
					Status:      StatusMissing,
					Summary:     pp.Description,
					InstallHint: pp.Install,
					ProjectDir:  canonProjDir,
				}
				if canonical, collides := opts.Builtins[pp.Name]; collides {
					rp.setError(StatusConflict, fmt.Errorf("%w: %q collides with built-in command %q", ErrBuiltinCollision, pp.Name, canonical))
				} else {
					msg := fmt.Sprintf("plugin %q is declared by this project but not registered", pp.Name)
					if pp.Install != "" {
						msg += "; install with: " + pp.Install
					}
					rp.Err = fmt.Errorf("%w: %s", ErrPluginNotFound, msg)
					rp.ErrorMessage = rp.Err.Error()
				}
				projectEntries = append(projectEntries, rp)
				continue
			}

			// Project-defined command entry.
			rp := ResolvedPlugin{
				Name:        pp.Name,
				Source:      SourceProject,
				Status:      StatusUntrusted,
				Summary:     pp.Description,
				InstallHint: pp.Install,
				Command:     slices.Clone(pp.Command),
				ProjectDir:  canonProjDir,
			}

			if canonical, collides := opts.Builtins[pp.Name]; collides {
				rp.setError(StatusConflict, fmt.Errorf("%w: %q collides with built-in command %q", ErrBuiltinCollision, pp.Name, canonical))
				projectEntries = append(projectEntries, rp)
				continue
			}
			if _, existsGlobal := globalByName[pp.Name]; existsGlobal {
				// Keep the user's global entry intact so an untrusted repo can
				// never disable a user-approved global hook or override; report
				// the project conflict separately.
				rp.setError(StatusConflict, fmt.Errorf("%w: project plugin %q conflicts with an existing global registration of the same name", ErrDuplicatePlugin, pp.Name))
				projectEntries = append(projectEntries, rp)
				continue
			}
			if err := validateGrantsAgainstBuiltins(pp.Hooks, pp.Overrides, opts.Builtins); err != nil {
				rp.setError(StatusInvalid, err)
				projectEntries = append(projectEntries, rp)
				continue
			}

			resolvedCmd, isLocal, err := ResolveExecutable(pp.Command, opts.ProjectConfig.ProjectDir, true)
			if err != nil {
				rp.setError(StatusInvalid, err)
				projectEntries = append(projectEntries, rp)
				continue
			}
			rp.Command = resolvedCmd
			rp.ProjectLocalBin = isLocal

			trustKey := projectPluginKey{projectDir: canonProjDir, name: pp.Name}
			if ambiguousTrust[trustKey] {
				rp.setError(StatusConflict, fmt.Errorf("%w: multiple trust records resolve to project directory %q for plugin %q; run `gofastr plugin untrust %s` and re-trust",
					ErrDuplicatePlugin, canonProjDir, pp.Name, pp.Name))
				projectEntries = append(projectEntries, rp)
				continue
			}
			trust, ok := trustMap[trustKey]
			if !ok {
				rp.setError(StatusUntrusted, formatUntrustedError(pp))
				projectEntries = append(projectEntries, rp)
				continue
			}

			if err := verifyProjectTrust(trust, pp, resolvedCmd, !opts.SkipDigests); err != nil {
				rp.setError(StatusUntrusted, err)
				projectEntries = append(projectEntries, rp)
				continue
			}

			rp.Status = StatusReady
			rp.SHA256 = trust.SHA256
			if trust.Summary != "" && rp.Summary == "" {
				rp.Summary = trust.Summary
			}
			rp.AllowEnv = slices.Clone(trust.AllowEnv)
			rp.AllowEnvFlags = slices.Clone(trust.AllowEnvFlags)
			rp.AllowHooks = slices.Clone(trust.AllowHooks)
			rp.AllowOverrides = slices.Clone(trust.AllowOverrides)
			projectEntries = append(projectEntries, rp)
		}
	}

	out := make([]ResolvedPlugin, 0, len(globalByName)+len(projectEntries))
	for _, name := range slices.Sorted(maps.Keys(globalByName)) {
		out = append(out, globalByName[name])
	}
	slices.SortFunc(projectEntries, func(a, b ResolvedPlugin) int {
		return cmp.Compare(a.Name, b.Name)
	})
	out = append(out, projectEntries...)

	// 3. Record duplicate built-in override conflicts scoped per canonical
	// command so an unrelated override, hook, or namespace on the same plugin
	// stays usable while the conflicting command itself fails closed.
	overrideOwners := map[string][]int{}
	for i := range out {
		if out[i].Status != StatusReady {
			continue
		}
		for _, ov := range out[i].AllowOverrides {
			overrideOwners[ov] = append(overrideOwners[ov], i)
		}
	}
	for _, cmdName := range slices.Sorted(maps.Keys(overrideOwners)) {
		idxs := overrideOwners[cmdName]
		if len(idxs) > 1 {
			for _, idx := range idxs {
				out[idx].ConflictedOverrides = append(out[idx].ConflictedOverrides, cmdName)
			}
		}
	}
	for i := range out {
		if len(out[i].ConflictedOverrides) > 0 {
			out[i].ConflictedOverrides = sortedUnique(out[i].ConflictedOverrides)
		}
	}

	slices.SortFunc(out, func(a, b ResolvedPlugin) int {
		if d := cmp.Compare(a.Name, b.Name); d != 0 {
			return d
		}
		return cmp.Compare(string(a.Source), string(b.Source))
	})
	return out
}

func formatUntrustedError(pp ProjectPlugin) error {
	var req []string
	if len(pp.AllowEnv) > 0 {
		req = append(req, "allow_env="+strings.Join(pp.AllowEnv, ","))
	}
	if len(pp.AllowEnvFlags) > 0 {
		req = append(req, "allow_env_flags="+strings.Join(pp.AllowEnvFlags, ","))
	}
	if len(pp.Hooks) > 0 {
		req = append(req, "hooks="+strings.Join(pp.Hooks, ","))
	}
	if len(pp.Overrides) > 0 {
		req = append(req, "overrides="+strings.Join(pp.Overrides, ","))
	}
	detail := ""
	if len(req) > 0 {
		detail = " [requests: " + strings.Join(req, "; ") + "]"
	}
	return fmt.Errorf("%w: project plugin %q (%s)%s is not trusted; inspect and run `gofastr plugin trust %s` to approve it",
		ErrUntrustedPlugin, pp.Name, strings.Join(pp.Command, " "), detail, pp.Name)
}

func (r *ResolvedPlugin) setError(status PluginStatus, err error) {
	r.Status = status
	r.Err = err
	if err != nil {
		r.ErrorMessage = err.Error()
	}
}

// FindResolved looks up one plugin by namespace name. When a project entry
// conflicts with a global entry of the same name, the conflict is surfaced for
// namespace invocation so ambiguity is never resolved silently.
func FindResolved(plugins []ResolvedPlugin, name string) (ResolvedPlugin, bool) {
	var found ResolvedPlugin
	var has bool
	for _, p := range plugins {
		if p.Name != name {
			continue
		}
		if p.Status == StatusConflict {
			return p, true
		}
		if !has {
			found = p
			has = true
		}
	}
	return found, has
}

// ResolveOverride returns the single ready plugin explicitly authorized to
// override canonical built-in command name, or (nil, nil) if none is active.
func ResolveOverride(plugins []ResolvedPlugin, canonicalBuiltin string) (*ResolvedPlugin, error) {
	if IsProtectedBuiltin(canonicalBuiltin) {
		return nil, nil
	}
	var matches []ResolvedPlugin
	for _, p := range plugins {
		if p.Status != StatusReady || !slices.Contains(p.AllowOverrides, canonicalBuiltin) {
			continue
		}
		matches = append(matches, p)
	}
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Name
		}
		slices.Sort(names)
		return nil, fmt.Errorf("%w: built-in command %q is overridden by multiple plugins (%s)",
			ErrDuplicateOverride, canonicalBuiltin, strings.Join(names, ", "))
	}
	if slices.Contains(matches[0].ConflictedOverrides, canonicalBuiltin) {
		return nil, fmt.Errorf("%w: built-in command %q is overridden by multiple plugins", ErrDuplicateOverride, canonicalBuiltin)
	}
	chosen := matches[0]
	return &chosen, nil
}

// ResolveHooks returns every ready plugin explicitly authorized to hook
// (phase, canonicalBuiltin), sorted deterministically by plugin name.
func ResolveHooks(plugins []ResolvedPlugin, phase HookPhase, canonicalBuiltin string) []ResolvedPlugin {
	if IsProtectedBuiltin(canonicalBuiltin) {
		return nil
	}
	target := string(phase) + ":" + canonicalBuiltin
	var out []ResolvedPlugin
	for _, p := range plugins {
		if p.Status == StatusReady && slices.Contains(p.AllowHooks, target) {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b ResolvedPlugin) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return out
}

// VerifyLiveCommand probes an already-prepared command vector and checks CLI
// version compatibility.
func VerifyLiveCommand(ctx context.Context, command []string, cliVersion, workDir string) (Manifest, error) {
	m, err := Probe(ctx, command, workDir)
	if err != nil {
		return Manifest{}, err
	}
	if err := m.CheckCLIVersion(cliVersion); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// VerifyLiveManifest prepares a verified execution snapshot (for project
// plugins), probes its manifest, and checks protocol and CLI version
// compatibility.
func VerifyLiveManifest(ctx context.Context, rp ResolvedPlugin, cliVersion string, workDir string) (Manifest, error) {
	cmd, cleanup, err := PrepareVerifiedExec(rp)
	if err != nil {
		return Manifest{}, err
	}
	defer cleanup()
	return VerifyLiveCommand(ctx, cmd, cliVersion, workDir)
}

// EffectiveEnvGrants computes the intersection of what the operator approved
// and what the plugin's live manifest declares (plus any operator-explicit
// AllowEnv keys) so a binary update can never silently widen env access.
func EffectiveEnvGrants(rp ResolvedPlugin, m Manifest) (allowEnv []string, allowEnvFlags []string) {
	for _, pat := range rp.AllowEnv {
		if ValidateEnvPattern(pat) == nil {
			allowEnv = append(allowEnv, pat)
		}
	}
	manifestFlags := map[string]bool{}
	for _, f := range m.EnvFromFlags {
		manifestFlags[f] = true
	}
	for _, f := range rp.AllowEnvFlags {
		if manifestFlags[f] && ValidateEnvFlag(f) == nil {
			allowEnvFlags = append(allowEnvFlags, f)
		}
	}
	return sortedUnique(allowEnv), sortedUnique(allowEnvFlags)
}

// ManifestSupportsHook reports whether m declares hook target ("pre:cmd" or "post:cmd").
func ManifestSupportsHook(m Manifest, target string) bool {
	for _, h := range m.Hooks {
		if h.Target() == target {
			return true
		}
	}
	return false
}

// ManifestSupportsOverride reports whether m declares override capability for canonicalBuiltin.
func ManifestSupportsOverride(m Manifest, canonicalBuiltin string) bool {
	return slices.Contains(m.Overrides, canonicalBuiltin)
}

// Diagnose runs Resolve plus a live manifest probe on every ready plugin (or
// only filterName when non-empty).
func Diagnose(ctx context.Context, opts ResolveOptions, cliVersion, workDir, filterName string) []Diagnostic {
	resolved := Resolve(opts)
	var out []Diagnostic
	for _, rp := range resolved {
		if filterName != "" && rp.Name != filterName {
			continue
		}
		d := Diagnostic{
			Name:                rp.Name,
			Source:              rp.Source,
			Status:              rp.Status,
			Summary:             rp.Summary,
			InstallHint:         rp.InstallHint,
			Command:             rp.Command,
			ProjectDir:          rp.ProjectDir,
			AllowEnv:            rp.AllowEnv,
			AllowEnvFlags:       rp.AllowEnvFlags,
			AllowHooks:          rp.AllowHooks,
			AllowOverrides:      rp.AllowOverrides,
			ConflictedOverrides: slices.Clone(rp.ConflictedOverrides),
			Error:               rp.ErrorMessage,
		}
		if rp.Status == StatusReady {
			m, err := VerifyLiveManifest(ctx, rp, cliVersion, workDir)
			if err != nil {
				if errors.Is(err, ErrProtocolMismatch) || errors.Is(err, ErrIncompatibleVersion) {
					d.Status = StatusIncompatible
				} else if errors.Is(err, ErrTrustMismatch) {
					d.Status = StatusUntrusted
				} else {
					d.Status = StatusInvalid
				}
				d.Error = err.Error()
			} else {
				d.Version = m.Version
				d.ProtocolVersion = m.ProtocolVersion
				d.Summary = m.Summary
				d.Subcommands = m.Subcommands
				d.DeclaredHooks = m.Hooks
				d.DeclaredOverr = m.Overrides
				if len(rp.ConflictedOverrides) > 0 {
					d.Status = StatusConflict
					d.Error = fmt.Sprintf("%s: built-in command(s) %s overridden by multiple plugins",
						ErrDuplicateOverride, strings.Join(rp.ConflictedOverrides, ", "))
				}
			}
		}
		out = append(out, d)
	}
	return out
}

func canonicalDir(dir string) string {
	if dir == "" {
		return ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

// CanonicalProjectDir returns the canonical (absolute, symlink-resolved) path
// for a project directory so trust records match consistently across symlinks.
func CanonicalProjectDir(dir string) string {
	return canonicalDir(dir)
}

type projectPluginKey struct {
	projectDir string
	name       string
}

func indexTrust(list []TrustedProjectPlugin) (map[projectPluginKey]TrustedProjectPlugin, map[projectPluginKey]bool) {
	out := make(map[projectPluginKey]TrustedProjectPlugin, len(list))
	conflicts := map[projectPluginKey]bool{}
	for _, tp := range list {
		k := projectPluginKey{projectDir: canonicalDir(tp.ProjectDir), name: tp.Name}
		if _, exists := out[k]; exists {
			conflicts[k] = true
			continue
		}
		out[k] = tp
	}
	return out, conflicts
}

func verifyProjectTrust(trust TrustedProjectPlugin, pp ProjectPlugin, resolvedCmd []string, checkDigest bool) error {
	if !slices.Equal(trust.Command, resolvedCmd) {
		return fmt.Errorf("%w: project plugin %q command changed (trusted %q, now %q); run `gofastr plugin trust %s` to re-approve",
			ErrTrustMismatch, pp.Name, strings.Join(trust.Command, " "), strings.Join(resolvedCmd, " "), pp.Name)
	}
	if trust.SHA256 == "" {
		return fmt.Errorf("%w: project plugin %q trust record is missing its SHA-256 digest; re-run `gofastr plugin trust %s`",
			ErrTrustMismatch, pp.Name, pp.Name)
	}
	if checkDigest {
		currentHash, err := HashFile(resolvedCmd[0])
		if err != nil {
			return fmt.Errorf("%w: hash %s: %v", ErrInvalidPath, textsafe.ScrubTerminal(resolvedCmd[0]), err)
		}
		if !strings.EqualFold(trust.SHA256, currentHash) {
			return fmt.Errorf("%w: project plugin %q executable changed since it was trusted; run `gofastr plugin trust %s` to approve the updated binary",
				ErrTrustMismatch, pp.Name, pp.Name)
		}
	}
	if missing := diffStrings(pp.AllowEnv, trust.AllowEnv); len(missing) > 0 {
		return fmt.Errorf("%w: project plugin %q requests unapproved env vars (%s); run `gofastr plugin trust %s` to review",
			ErrTrustMismatch, pp.Name, strings.Join(missing, ", "), pp.Name)
	}
	if missing := diffStrings(pp.AllowEnvFlags, trust.AllowEnvFlags); len(missing) > 0 {
		return fmt.Errorf("%w: project plugin %q requests unapproved env flags (%s); run `gofastr plugin trust %s` to review",
			ErrTrustMismatch, pp.Name, strings.Join(missing, ", "), pp.Name)
	}
	if missing := diffStrings(pp.Hooks, trust.AllowHooks); len(missing) > 0 {
		return fmt.Errorf("%w: project plugin %q requests unapproved hooks (%s); run `gofastr plugin trust %s` to review",
			ErrTrustMismatch, pp.Name, strings.Join(missing, ", "), pp.Name)
	}
	if missing := diffStrings(pp.Overrides, trust.AllowOverrides); len(missing) > 0 {
		return fmt.Errorf("%w: project plugin %q requests unapproved overrides (%s); run `gofastr plugin trust %s` to review",
			ErrTrustMismatch, pp.Name, strings.Join(missing, ", "), pp.Name)
	}
	return nil
}

func diffStrings(requested, approved []string) []string {
	if len(requested) == 0 {
		return nil
	}
	set := make(map[string]bool, len(approved))
	for _, a := range approved {
		set[a] = true
	}
	var missing []string
	for _, r := range requested {
		if !set[r] {
			missing = append(missing, r)
		}
	}
	slices.Sort(missing)
	return slices.Compact(missing)
}

func validateGrantsAgainstBuiltins(hooks, overrides []string, builtins map[string]string) error {
	if len(builtins) == 0 {
		return nil
	}
	canonicalSet := map[string]bool{}
	for _, canonical := range builtins {
		canonicalSet[canonical] = true
	}
	for _, h := range hooks {
		spec, err := ParseHookTarget(h)
		if err != nil {
			return err
		}
		if !canonicalSet[spec.Command] {
			return fmt.Errorf("%w: hook target %q does not name a canonical built-in command", ErrInvalidManifest, h)
		}
	}
	for _, ov := range overrides {
		if IsProtectedBuiltin(ov) {
			return fmt.Errorf("%w: %q", ErrProtectedBuiltin, ov)
		}
		if !canonicalSet[ov] {
			return fmt.Errorf("%w: override target %q does not name a canonical built-in command", ErrInvalidManifest, ov)
		}
	}
	return nil
}
