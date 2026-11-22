package plugincmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/core/cliplugin"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

var (
	builtinsMu   sync.RWMutex
	hostBuiltins map[string]string
)

// SetHostBuiltins registers the host CLI's built-in command names and aliases
// (mapped to their canonical command name) so plugin resolution, collision
// checks, and hook/override validation share one source of truth with main.
func SetHostBuiltins(builtins map[string]string) {
	builtinsMu.Lock()
	defer builtinsMu.Unlock()
	copied := make(map[string]string, len(builtins))
	for k, v := range builtins {
		copied[k] = v
	}
	hostBuiltins = copied
}

func getHostBuiltins() map[string]string {
	builtinsMu.RLock()
	defer builtinsMu.RUnlock()
	copied := make(map[string]string, len(hostBuiltins))
	for k, v := range hostBuiltins {
		copied[k] = v
	}
	return copied
}

// loadUserAndProject loads UserConfig and ProjectConfig independently so a
// malformed project gofastr.settings.yml can never hide or disable user-approved
// global plugins, hooks, or overrides.
func loadUserAndProject(skipDigests bool) (resolved []cliplugin.ResolvedPlugin, projectRoot string, userErr error, projErr error) {
	cfgPath, err := cliplugin.DefaultUserConfigPath()
	if err != nil {
		return nil, "", err, nil
	}
	userCfg, err := cliplugin.LoadUserConfig(cfgPath)
	if err != nil {
		return nil, "", err, nil
	}
	wd, _ := os.Getwd()
	projCfg, pErr := cliplugin.DiscoverProjectConfig(wd)
	if pErr != nil {
		projErr = pErr
		projCfg = cliplugin.ProjectConfig{}
	}
	resolved = cliplugin.Resolve(cliplugin.ResolveOptions{
		UserConfig:    userCfg,
		ProjectConfig: projCfg,
		Builtins:      getHostBuiltins(),
		SkipDigests:   skipDigests,
	})
	projectRoot = projCfg.ProjectDir
	if projectRoot == "" && wd != "" {
		_, root := project.FindEnclosingGoMod(wd)
		projectRoot = root
	}
	if projectRoot != "" {
		if abs, err := filepath.Abs(projectRoot); err == nil {
			projectRoot = abs
		}
	}
	return resolved, projectRoot, nil, projErr
}

// HasUserConfigError reports whether the user plugin registry failed to load
// (for example, malformed YAML, symlink, or unsafe permissions).
func HasUserConfigError() bool {
	_, _, userErr, _ := loadUserAndProject(true)
	return userErr != nil
}

// KnownCommand reports whether name corresponds to a registered plugin, a
// project-declared plugin, or a known companion module (such as "native").
// It never executes any external binary or hashes executables.
func KnownCommand(name string) bool {
	if _, ok := LookupCompanionHint(name); ok {
		return true
	}
	resolved, _, userErr, _ := loadUserAndProject(true)
	if userErr != nil {
		return false
	}
	_, ok := cliplugin.FindResolved(resolved, name)
	return ok
}

// RegisteredNames returns the sorted namespace names of configured plugins for
// fuzzy "Did you mean" suggestions on unknown commands.
func RegisteredNames() []string {
	resolved, _, userErr, _ := loadUserAndProject(true)
	if userErr != nil {
		return nil
	}
	names := make([]string, 0, len(resolved))
	for _, p := range resolved {
		if !slices.Contains(names, p.Name) {
			names = append(names, p.Name)
		}
	}
	return names
}

// HelpSummaryLines returns formatted lines describing configured plugins for
// `gofastr help`. It reads config only, skips executable hashing, and never
// executes plugin binaries.
func HelpSummaryLines() []string {
	resolved, _, userErr, _ := loadUserAndProject(true)
	if userErr != nil || len(resolved) == 0 {
		return nil
	}
	lines := make([]string, 0, len(resolved))
	for _, p := range resolved {
		summary := textsafe.ScrubTerminal(p.Summary)
		switch p.Status {
		case cliplugin.StatusReady:
			if summary == "" {
				summary = "External CLI plugin"
			}
		case cliplugin.StatusMissing:
			if summary == "" {
				summary = "Project plugin (not installed)"
			} else {
				summary += " [not installed]"
			}
		case cliplugin.StatusUntrusted:
			if summary == "" {
				summary = "Project plugin [untrusted: run `gofastr plugin trust " + p.Name + "`]"
			} else {
				summary += " [untrusted]"
			}
		default:
			summary = fmt.Sprintf("[%s] %s", p.Status, textsafe.ScrubTerminal(p.ErrorMessage))
		}
		lines = append(lines, fmt.Sprintf("  %-21s %s", textsafe.ScrubTerminal(p.Name), summary))
	}
	return lines
}

// DispatchNamespace runs a plugin namespace command (`gofastr <name> [args...]`).
// It returns true if name was recognized as a configured plugin, known
// companion module, or blocked by a registry error; false if name is unknown.
func DispatchNamespace(name string, args []string) bool {
	resolved, projectRoot, userErr, projErr := loadUserAndProject(false)
	if userErr != nil {
		cli.Fail("Load CLI plugin registry: %v", userErr)
		cli.OsExit(1)
		return true
	}

	rp, found := cliplugin.FindResolved(resolved, name)
	if !found {
		if projErr != nil {
			cli.Fail("Load project plugin settings: %v", projErr)
			cli.OsExit(1)
			return true
		}
		if hint, ok := LookupCompanionHint(name); ok {
			cli.Fail("Plugin %q is not installed or registered.", name)
			cli.Info("%s", hint.Description)
			cli.Info("Install:  %s", hint.Install)
			cli.Info("Register: %s", hint.Register)
			cli.OsExit(1)
			return true
		}
		return false
	}

	if rp.Status != cliplugin.StatusReady {
		if errorsIsMissing(rp.Err) {
			cli.Fail("Plugin %q is declared by this project but not registered.", rp.Name)
			if rp.Summary != "" {
				cli.Info("%s", rp.Summary)
			}
			if rp.InstallHint != "" {
				cli.Info("Install:  %s", rp.InstallHint)
			}
			cli.Info("Register: gofastr plugin add %s <executable>", rp.Name)
			cli.OsExit(1)
			return true
		}
		cli.Fail("%v", rp.Err)
		cli.OsExit(1)
		return true
	}

	wd, _ := os.Getwd()
	if code := runVerifiedNamespace(rp, args, wd, projectRoot); code != 0 {
		cli.OsExit(code)
	}
	return true
}

func runVerifiedNamespace(rp cliplugin.ResolvedPlugin, args []string, wd, projectRoot string) int {
	execCmd, cleanup, err := cliplugin.PrepareVerifiedExec(rp)
	if err != nil {
		cli.Fail("Plugin %q cannot run: %v", rp.Name, err)
		return 1
	}
	defer cleanup()

	manifest, err := cliplugin.VerifyLiveCommand(context.Background(), execCmd, project.CliVersion(), wd)
	if err != nil {
		cli.Fail("Plugin %q cannot run: %v", rp.Name, err)
		return 1
	}

	allowEnv, allowEnvFlags := cliplugin.EffectiveEnvGrants(rp, manifest)
	cliBin, _ := os.Executable()
	code, err := cliplugin.Run(context.Background(), cliplugin.ExecRequest{
		PluginName:    rp.Name,
		Command:       execCmd,
		Args:          args,
		WorkDir:       wd,
		CLIVersion:    project.CliVersion(),
		CLIBin:        cliBin,
		ProjectRoot:   projectRoot,
		Mode:          cliplugin.ModeNamespace,
		AllowEnv:      allowEnv,
		AllowEnvFlags: allowEnvFlags,
		Stdin:         os.Stdin,
		Stdout:        os.Stdout,
		Stderr:        os.Stderr,
	})
	if err != nil {
		cli.Fail("%v", err)
		return 1
	}
	return code
}

func failBuiltinSeam(code int, format string, args ...any) {
	if cli.InJSONSession() {
		msg := textsafe.ScrubTerminal(fmt.Sprintf(format, args...))
		_ = cli.EmitJSON(map[string]any{"error": msg, "exit_code": code})
	} else {
		cli.Fail(format, args...)
	}
	cli.OsExit(code)
}

// DispatchBuiltin runs a built-in command with opt-in plugin overrides and
// pre/post hooks applied unless bypass is true, the command is protected, or
// the user asked for help. A malformed or conflicting project settings file
// never disables user-approved global hooks or overrides.
func DispatchBuiltin(canonical string, args []string, bypass bool, runBuiltin func([]string)) {
	if bypass ||
		os.Getenv(cliplugin.EnvBypassPlugins) == "1" ||
		cliplugin.IsProtectedBuiltin(canonical) ||
		cli.HasHelpFlagBeforeTerminator(args) {
		runBuiltin(args)
		return
	}

	resolved, projectRoot, userErr, projErr := loadUserAndProject(false)
	if userErr != nil {
		failBuiltinSeam(1, "Load CLI plugin registry (refusing to bypass configured plugins): %v", userErr)
		return
	}
	if projErr != nil {
		cli.Fprintf(os.Stderr, cli.Yellow("warning:")+" project plugin settings error (global plugins still apply): %v\n", projErr)
	}
	if len(resolved) == 0 {
		runBuiltin(args)
		return
	}

	wd, _ := os.Getwd()
	cliBin, _ := os.Executable()

	// 1. Check for an explicitly authorized built-in command override.
	override, err := cliplugin.ResolveOverride(resolved, canonical)
	if err != nil {
		failBuiltinSeam(1, "%v", err)
		return
	}
	if override != nil {
		if code, errMsg := runVerifiedOverride(*override, canonical, args, wd, cliBin, projectRoot); code != 0 {
			if errMsg != "" {
				failBuiltinSeam(code, "%s", errMsg)
				return
			}
			cli.OsExit(code)
		}
		return
	}

	// 2. Run explicitly authorized pre-hooks.
	preHooks := cliplugin.ResolveHooks(resolved, cliplugin.HookPre, canonical)
	for _, hp := range preHooks {
		if code := runHook(hp, cliplugin.HookPre, canonical, args, wd, cliBin, projectRoot); code != 0 {
			if cli.InJSONSession() {
				_ = cli.EmitJSON(map[string]any{
					"error":     fmt.Sprintf("pre:%s hook plugin %q exited with status %d", canonical, textsafe.ScrubTerminal(hp.Name), code),
					"exit_code": code,
				})
			}
			cli.OsExit(code)
			return
		}
	}

	// 3. Run the built-in command itself.
	runBuiltin(args)

	// 4. Run explicitly authorized post-hooks on successful completion.
	// PrepareVerifiedExec snapshots & verifies project binaries fresh here so
	// anything the built-in wrote inside the checkout cannot tamper with them.
	postHooks := cliplugin.ResolveHooks(resolved, cliplugin.HookPost, canonical)
	for _, hp := range postHooks {
		if code := runHook(hp, cliplugin.HookPost, canonical, args, wd, cliBin, projectRoot); code != 0 {
			cli.OsExit(code)
			return
		}
	}
}

func runVerifiedOverride(override cliplugin.ResolvedPlugin, canonical string, args []string, wd, cliBin, projectRoot string) (int, string) {
	execCmd, cleanup, err := cliplugin.PrepareVerifiedExec(override)
	if err != nil {
		return 1, fmt.Sprintf("Override plugin %q for %q cannot run: %v", override.Name, canonical, err)
	}
	defer cleanup()

	manifest, err := cliplugin.VerifyLiveCommand(context.Background(), execCmd, project.CliVersion(), wd)
	if err != nil {
		return 1, fmt.Sprintf("Override plugin %q for %q cannot run: %v", override.Name, canonical, err)
	}
	if !cliplugin.ManifestSupportsOverride(manifest, canonical) {
		return 1, fmt.Sprintf("Override plugin %q no longer declares override %q; remove or re-register it.", override.Name, canonical)
	}
	allowEnv, allowEnvFlags := cliplugin.EffectiveEnvGrants(override, manifest)
	// Overrides own machine output on `--json` commands, so give them the
	// reserved stdout handle instead of BeginJSON's redirected stderr.
	code, err := cliplugin.Run(context.Background(), cliplugin.ExecRequest{
		PluginName:    override.Name,
		Command:       execCmd,
		Args:          args,
		WorkDir:       wd,
		CLIVersion:    project.CliVersion(),
		CLIBin:        cliBin,
		ProjectRoot:   projectRoot,
		Mode:          cliplugin.ModeOverride,
		TargetCommand: canonical,
		AllowEnv:      allowEnv,
		AllowEnvFlags: allowEnvFlags,
		Stdin:         os.Stdin,
		Stdout:        cli.JSONWriter(),
		Stderr:        os.Stderr,
	})
	if err != nil {
		return 1, err.Error()
	}
	return code, ""
}

func runHook(rp cliplugin.ResolvedPlugin, phase cliplugin.HookPhase, canonical string, args []string, wd, cliBin, projectRoot string) int {
	target := string(phase) + ":" + canonical
	execCmd, cleanup, err := cliplugin.PrepareVerifiedExec(rp)
	if err != nil {
		cli.Fail("Hook plugin %q (%s) cannot run: %v", rp.Name, target, err)
		return 1
	}
	defer cleanup()

	manifest, err := cliplugin.VerifyLiveCommand(context.Background(), execCmd, project.CliVersion(), wd)
	if err != nil {
		cli.Fail("Hook plugin %q (%s) cannot run: %v", rp.Name, target, err)
		return 1
	}
	if !cliplugin.ManifestSupportsHook(manifest, target) {
		cli.Fail("Hook plugin %q no longer declares hook %q.", rp.Name, target)
		return 1
	}
	allowEnv, allowEnvFlags := cliplugin.EffectiveEnvGrants(rp, manifest)
	code, err := cliplugin.Run(context.Background(), cliplugin.ExecRequest{
		PluginName:    rp.Name,
		Command:       execCmd,
		Args:          args,
		WorkDir:       wd,
		CLIVersion:    project.CliVersion(),
		CLIBin:        cliBin,
		ProjectRoot:   projectRoot,
		Mode:          cliplugin.ModeHook,
		HookPhase:     phase,
		TargetCommand: canonical,
		AllowEnv:      allowEnv,
		AllowEnvFlags: allowEnvFlags,
		Stdin:         os.Stdin,
		Stdout:        os.Stdout, // redirected to stderr in JSON mode so hooks never corrupt JSON stdout
		Stderr:        os.Stderr,
	})
	if err != nil {
		cli.Fail("%v", err)
		return 1
	}
	return code
}

// FuzzyCandidates appends configured plugin names to built-in names without
// duplicates, preserving deterministic order.
func FuzzyCandidates(builtinNames []string) []string {
	out := slices.Clone(builtinNames)
	for _, name := range RegisteredNames() {
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

// DescribeArgsForLog formats args safely (scrubbed). Not executed via a shell.
func DescribeArgsForLog(args []string) string {
	return textsafe.ScrubTerminal(strings.Join(args, " "))
}
