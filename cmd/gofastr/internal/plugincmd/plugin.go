package plugincmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/core/cliplugin"
	"github.com/DonaldMurillo/gofastr/core/settings"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

const pluginUsage = `Usage:
  gofastr plugin <subcommand> [arguments]

Subcommands:
  list [--json]                   List globally registered and project-declared plugins
  add <name> <command...> [flags] Register a trusted global CLI plugin after probing its manifest
  remove <name>                   Remove a globally registered plugin (alias: rm)
  trust <name> [flags]            Inspect and approve a project-declared plugin from gofastr.settings.yml
  untrust <name>                  Revoke trust for a project-declared plugin in this project
  doctor [name] [--json]          Verify plugin paths, trust digests, protocol version, and compatibility

Flags (add, trust):
  --allow-env <VAR|PREFIX_*>      Approve an environment variable or prefix (repeatable)
  --allow-env-flag <--flag>       Approve a flag whose value names an env var to forward (repeatable)
  --allow-hook <pre:cmd|post:cmd> Opt into a plugin-declared pre/post hook on a built-in command (repeatable)
  --allow-override <cmd>          Opt into a plugin-declared override of a built-in command (repeatable)
  --no-manifest-env               Do not auto-approve env_passthrough / env_from_flags declared in the manifest

Examples:
  gofastr plugin add native gofastr-native
  gofastr plugin add native /usr/local/bin/gofastr-native
  gofastr plugin trust release-helper --allow-hook pre:release
  gofastr plugin list
  gofastr plugin doctor --json
  gofastr plugin remove native
`

// RunPlugin dispatches `gofastr plugin ...`.
func RunPlugin(args []string) {
	jsonMode := cli.HasFlagBeforeTerminator(args, "--json")
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		cli.Print(pluginUsage)
		return
	}
	if cli.HasHelpFlagBeforeTerminator(args) {
		cli.Print(pluginUsage)
		return
	}

	sub, rest := args[0], args[1:]
	switch sub {
	case "list", "ls":
		runPluginList(rest, jsonMode)
	case "add":
		if jsonMode {
			pluginUsageFail(true, "--json is only supported on `gofastr plugin list` and `gofastr plugin doctor`")
			return
		}
		runPluginAdd(rest)
	case "remove", "rm":
		if jsonMode {
			pluginUsageFail(true, "--json is only supported on `gofastr plugin list` and `gofastr plugin doctor`")
			return
		}
		runPluginRemove(rest)
	case "trust":
		if jsonMode {
			pluginUsageFail(true, "--json is only supported on `gofastr plugin list` and `gofastr plugin doctor`")
			return
		}
		runPluginTrust(rest)
	case "untrust":
		if jsonMode {
			pluginUsageFail(true, "--json is only supported on `gofastr plugin list` and `gofastr plugin doctor`")
			return
		}
		runPluginUntrust(rest)
	case "doctor":
		runPluginDoctor(rest, jsonMode)
	default:
		pluginUsageFail(jsonMode, "Unknown plugin subcommand %q (try: list, add, remove, trust, untrust, doctor).", sub)
	}
}

func pluginUsageFail(jsonMode bool, format string, args ...any) {
	if jsonMode {
		msg := textsafe.ScrubTerminal(fmt.Sprintf(format, args...))
		_ = cli.EmitJSON(map[string]string{"error": msg})
		cli.OsExit(2)
		return
	}
	cli.UsageFail(format, args...)
}

func pluginRuntimeFail(jsonMode bool, format string, args ...any) {
	if jsonMode {
		msg := textsafe.ScrubTerminal(fmt.Sprintf(format, args...))
		_ = cli.EmitJSON(map[string]string{"error": msg})
		cli.OsExit(1)
		return
	}
	cli.Fail(format, args...)
	cli.OsExit(1)
}

func runPluginList(args []string, jsonMode bool) {
	var wantJSON bool
	_, err := cli.ParseFlags(args, nil, map[string]*bool{"--json": &wantJSON})
	if err != nil {
		pluginUsageFail(jsonMode, "%v", err)
		return
	}

	cfgPath, err := cliplugin.DefaultUserConfigPath()
	if err != nil {
		pluginRuntimeFail(jsonMode, "%v", err)
		return
	}
	resolved, _, userErr, projErr := loadUserAndProject(false)
	if userErr != nil {
		pluginRuntimeFail(jsonMode, "%v", userErr)
		return
	}

	if jsonMode {
		doc := map[string]any{
			"config_path": cfgPath,
			"plugins":     resolved,
		}
		if projErr != nil {
			doc["project_error"] = projErr.Error()
		}
		if err := cli.EmitJSON(doc); err != nil {
			cli.Fail("emit JSON: %v", err)
			cli.OsExit(1)
		}
		return
	}

	if projErr != nil {
		cli.Warn("Project settings error (showing global plugins): %v", projErr)
	}
	if len(resolved) == 0 {
		cli.Info("No CLI plugins registered.")
		cli.Info("Register one with: gofastr plugin add <name> <executable>")
		return
	}
	cli.Println("")
	cli.Printf("  %-14s  %-8s  %-12s  %s\n", "NAME", "SOURCE", "STATUS", "SUMMARY")
	cli.Printf("  %-14s  %-8s  %-12s  %s\n", "----", "------", "------", "-------")
	for _, p := range resolved {
		summary := p.Summary
		if summary == "" && p.ErrorMessage != "" {
			summary = p.ErrorMessage
		}
		cli.Printf("  %-14s  %-8s  %-12s  %s\n", p.Name, string(p.Source), string(p.Status), summary)
		if len(p.AllowHooks) > 0 {
			cli.Printf("  %-14s  hooks: %s\n", "", strings.Join(p.AllowHooks, ", "))
		}
		if len(p.AllowOverrides) > 0 {
			cli.Printf("  %-14s  overrides: %s\n", "", strings.Join(p.AllowOverrides, ", "))
		}
		if len(p.ConflictedOverrides) > 0 {
			cli.Printf("  %-14s  conflicted overrides: %s\n", "", strings.Join(p.ConflictedOverrides, ", "))
		}
	}
	cli.Println("")
}

type grantFlags struct {
	allowEnv      []string
	allowEnvFlags []string
	allowHooks    []string
	allowOverride []string
	noManifestEnv bool
}

func parseGrantFlagsAndPositionals(args []string) (positionals []string, g grantFlags, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}
		name, val, hasEq := strings.Cut(arg, "=")
		switch name {
		case "--no-manifest-env":
			if hasEq {
				return nil, g, fmt.Errorf("%s takes no value", name)
			}
			g.noManifestEnv = true
		case "--allow-env", "--allow-env-flag", "--allow-hook", "--allow-override":
			if !hasEq {
				if i+1 >= len(args) {
					return nil, g, cli.NeedsValue(name)
				}
				next := args[i+1]
				if name == "--allow-env-flag" {
					if isPluginGrantFlag(next) {
						return nil, g, cli.NeedsValue(name)
					}
				} else if cli.IsFlagToken(next) {
					return nil, g, cli.NeedsValue(name)
				}
				i++
				val = next
			}
			switch name {
			case "--allow-env":
				g.allowEnv = append(g.allowEnv, val)
			case "--allow-env-flag":
				g.allowEnvFlags = append(g.allowEnvFlags, val)
			case "--allow-hook":
				g.allowHooks = append(g.allowHooks, val)
			case "--allow-override":
				g.allowOverride = append(g.allowOverride, val)
			}
		default:
			return nil, g, fmt.Errorf("unknown flag %s", name)
		}
	}
	return positionals, g, nil
}

func isPluginGrantFlag(tok string) bool {
	name, _, _ := strings.Cut(tok, "=")
	switch name {
	case "--allow-env", "--allow-env-flag", "--allow-hook", "--allow-override", "--no-manifest-env", "--help", "-h", "--json":
		return true
	}
	return false
}

func runPluginAdd(args []string) {
	pos, grants, err := parseGrantFlagsAndPositionals(args)
	if err != nil {
		cli.UsageFail("%v", err)
		return
	}
	if len(pos) < 2 {
		cli.UsageFail("Usage: gofastr plugin add <name> <command...> [--allow-env ...] [--allow-hook ...] [--allow-override ...]")
		return
	}
	name := pos[0]
	rawCmd := pos[1:]

	if err := cliplugin.ValidateName(name); err != nil {
		cli.UsageFail("%v", err)
		return
	}
	builtins := getHostBuiltins()
	if canonical, collides := builtins[name]; collides {
		cli.Fail("Cannot register plugin %q: collides with built-in command %q.", name, canonical)
		cli.OsExit(1)
		return
	}

	wd, _ := os.Getwd()
	resolvedCmd, _, err := cliplugin.ResolveExecutable(rawCmd, wd, false)
	if err != nil {
		cli.Fail("Invalid plugin command: %v", err)
		cli.OsExit(1)
		return
	}

	manifest, err := cliplugin.Probe(context.Background(), resolvedCmd, wd)
	if err != nil {
		cli.Fail("Plugin %q failed manifest handshake: %v", name, err)
		cli.OsExit(1)
		return
	}
	if err := manifest.CheckCLIVersion(project.CliVersion()); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}

	allowEnv, allowEnvFlags, allowHooks, allowOverrides, err := computeApprovedGrants(manifest, grants, builtins)
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}

	cfgPath, err := cliplugin.DefaultUserConfigPath()
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}

	err = cliplugin.UpdateUserConfig(cfgPath, func(cfg *cliplugin.UserConfig) error {
		for _, existing := range cfg.Plugins {
			if existing.Name == name {
				return fmt.Errorf("plugin %q is already registered; remove it first with `gofastr plugin remove %s`", name, name)
			}
			for _, ov := range allowOverrides {
				if slices.Contains(existing.AllowOverrides, ov) {
					return fmt.Errorf("built-in command %q is already overridden by plugin %q", ov, existing.Name)
				}
			}
		}
		cfg.Plugins = append(cfg.Plugins, cliplugin.RegisteredPlugin{
			Name:           name,
			Command:        resolvedCmd,
			Summary:        manifest.Summary,
			AllowEnv:       allowEnv,
			AllowEnvFlags:  allowEnvFlags,
			AllowHooks:     allowHooks,
			AllowOverrides: allowOverrides,
		})
		return nil
	})
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}

	cli.Success("Registered plugin %s (%s) -> %s", name, manifest.Version, strings.Join(resolvedCmd, " "))
	printGrantedCapabilities(allowEnv, allowEnvFlags, allowHooks, allowOverrides)
	printAvailableInactiveCapabilities(manifest, allowHooks, allowOverrides)
}

func runPluginRemove(args []string) {
	if len(args) != 1 || cli.IsFlagToken(args[0]) {
		cli.UsageFail("Usage: gofastr plugin remove <name>")
		return
	}
	name := args[0]

	cfgPath, err := cliplugin.DefaultUserConfigPath()
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}

	err = cliplugin.UpdateUserConfig(cfgPath, func(cfg *cliplugin.UserConfig) error {
		idx := -1
		for i, p := range cfg.Plugins {
			if p.Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("plugin %q is not registered globally", name)
		}
		cfg.Plugins = slices.Delete(cfg.Plugins, idx, idx+1)
		return nil
	})
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	cli.Success("Removed plugin %s", name)
}

func runPluginTrust(args []string) {
	pos, grants, err := parseGrantFlagsAndPositionals(args)
	if err != nil {
		cli.UsageFail("%v", err)
		return
	}
	if len(pos) != 1 {
		cli.UsageFail("Usage: gofastr plugin trust <name> [--allow-env ...] [--allow-hook ...] [--allow-override ...]")
		return
	}
	name := pos[0]

	wd, err := os.Getwd()
	if err != nil {
		cli.Fail("Working directory: %v", err)
		cli.OsExit(1)
		return
	}
	projCfg, err := cliplugin.DiscoverProjectConfig(wd)
	if err != nil {
		cli.Fail("Read project settings: %v", err)
		cli.OsExit(1)
		return
	}
	if !projCfg.Found {
		cli.Fail("No `plugins:` section found in gofastr.settings.yml.")
		cli.OsExit(1)
		return
	}

	var pp *cliplugin.ProjectPlugin
	for i := range projCfg.Plugins {
		if projCfg.Plugins[i].Name == name {
			pp = &projCfg.Plugins[i]
			break
		}
	}
	if pp == nil {
		cli.Fail("Project plugin %q is not declared in %s.", name, projCfg.Path)
		cli.OsExit(1)
		return
	}
	if len(pp.Command) == 0 {
		cli.Fail("Project plugin %q declares no local command; install and register it globally with `gofastr plugin add %s <executable>`.", name, name)
		cli.OsExit(1)
		return
	}

	builtins := getHostBuiltins()
	if canonical, collides := builtins[name]; collides {
		cli.Fail("Cannot trust plugin %q: collides with built-in command %q.", name, canonical)
		cli.OsExit(1)
		return
	}

	// Verify that every capability requested in gofastr.settings.yml was
	// explicitly passed on the command line BEFORE executing the manifest probe.
	if missingFlags := missingExplicitTrustFlags(*pp, grants); len(missingFlags) > 0 {
		cli.Fail("Project plugin %q (%s) requests capabilities that must be approved explicitly:", name, strings.Join(pp.Command, " "))
		cli.Info("Re-run with: gofastr plugin trust %s %s", name, strings.Join(missingFlags, " "))
		cli.OsExit(1)
		return
	}

	var resolvedCmd []string
	var source string
	cleanBuild := func() {}
	if pp.GoPackage != "" {
		source, cleanBuild, err = buildProjectPlugin(context.Background(), projCfg.ProjectDir, pp.GoPackage)
		if err != nil {
			cli.Fail("%v", err)
			cli.OsExit(1)
			return
		}
	} else {
		resolvedCmd, _, err = cliplugin.ResolveExecutable(pp.Command, projCfg.ProjectDir, true)
		if err != nil {
			cli.Fail("Invalid project plugin command: %v", err)
			cli.OsExit(1)
			return
		}
		source = resolvedCmd[0]
	}

	// Snapshot and hash the executable in one pass and probe the private
	// snapshot so the probed binary and recorded SHA-256 are identical.
	snapPath, sha, cleanup, err := cliplugin.SnapshotExecutable(source)
	cleanBuild()
	if err != nil {
		cli.Fail("Snapshot project plugin binary: %v", err)
		cli.OsExit(1)
		return
	}
	defer cleanup()

	manifest, err := cliplugin.Probe(context.Background(), []string{snapPath}, projCfg.ProjectDir)
	if err != nil {
		cli.Fail("Project plugin %q failed manifest handshake: %v", name, err)
		cli.OsExit(1)
		return
	}
	if err := manifest.CheckCLIVersion(project.CliVersion()); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}

	// Project plugins never auto-grant manifest env_passthrough/env_from_flags;
	// only capabilities explicitly passed on `trust` are approved.
	explicitGrants := grants
	explicitGrants.noManifestEnv = true

	allowEnv, allowEnvFlags, allowHooks, allowOverrides, err := computeApprovedGrants(manifest, explicitGrants, builtins)
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}

	cfgPath, err := cliplugin.DefaultUserConfigPath()
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}

	canonProj := cliplugin.CanonicalProjectDir(projCfg.ProjectDir)
	record := cliplugin.TrustedProjectPlugin{
		ProjectDir:     canonProj,
		Name:           name,
		Command:        resolvedCmd,
		SHA256:         sha,
		Summary:        manifest.Summary,
		AllowEnv:       allowEnv,
		AllowEnvFlags:  allowEnvFlags,
		AllowHooks:     allowHooks,
		AllowOverrides: allowOverrides,
	}

	err = cliplugin.UpdateUserConfig(cfgPath, func(cfg *cliplugin.UserConfig) error {
		for _, existing := range cfg.Plugins {
			if existing.Name == name {
				return fmt.Errorf("project plugin %q conflicts with an existing global registration of the same name", name)
			}
			for _, ov := range allowOverrides {
				if slices.Contains(existing.AllowOverrides, ov) {
					return fmt.Errorf("built-in command %q is already overridden by global plugin %q", ov, existing.Name)
				}
			}
		}
		for _, existing := range cfg.TrustedProjects {
			if cliplugin.CanonicalProjectDir(existing.ProjectDir) == canonProj && existing.Name != name {
				for _, ov := range allowOverrides {
					if slices.Contains(existing.AllowOverrides, ov) {
						return fmt.Errorf("built-in command %q is already overridden by project plugin %q", ov, existing.Name)
					}
				}
			}
		}
		if pp.GoPackage != "" {
			if err := installProjectPlugin(canonProj, pp.Command[0], snapPath); err != nil {
				return fmt.Errorf("install project plugin: %w", err)
			}
			resolvedCmd, _, err = cliplugin.ResolveExecutable(pp.Command, canonProj, true)
			if err != nil {
				return err
			}
			record.Command = resolvedCmd
		}
		replaced := false
		for i, existing := range cfg.TrustedProjects {
			if cliplugin.CanonicalProjectDir(existing.ProjectDir) == canonProj && existing.Name == name {
				cfg.TrustedProjects[i] = record
				replaced = true
				break
			}
		}
		if !replaced {
			cfg.TrustedProjects = append(cfg.TrustedProjects, record)
		}
		return nil
	})
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}

	cli.Success("Trusted project plugin %s (%s) in %s", name, manifest.Version, canonProj)
	cli.Info("Command: %s (sha256: %s)", strings.Join(resolvedCmd, " "), sha[:12])
	printGrantedCapabilities(allowEnv, allowEnvFlags, allowHooks, allowOverrides)
	printAvailableInactiveCapabilities(manifest, allowHooks, allowOverrides)
}

func missingExplicitTrustFlags(pp cliplugin.ProjectPlugin, g grantFlags) []string {
	var flags []string
	for _, v := range pp.AllowEnv {
		if !slices.Contains(g.allowEnv, v) {
			flags = append(flags, "--allow-env "+v)
		}
	}
	for _, v := range pp.AllowEnvFlags {
		if !slices.Contains(g.allowEnvFlags, v) {
			flags = append(flags, "--allow-env-flag "+v)
		}
	}
	for _, v := range pp.Hooks {
		if !slices.Contains(g.allowHooks, v) {
			flags = append(flags, "--allow-hook "+v)
		}
	}
	for _, v := range pp.Overrides {
		if !slices.Contains(g.allowOverride, v) {
			flags = append(flags, "--allow-override "+v)
		}
	}
	return flags
}

// settingsDir is the nearest directory from wd up to the project root that
// holds the settings file, readable or not, else wd.
func settingsDir(wd string) string {
	dirs, _ := settings.SearchDirs(wd)
	for _, d := range dirs {
		if _, err := os.Stat(filepath.Join(d, settings.FileName)); err == nil {
			return d
		}
	}
	return wd
}

func runPluginUntrust(args []string) {
	if len(args) != 1 || cli.IsFlagToken(args[0]) {
		cli.UsageFail("Usage: gofastr plugin untrust <name>")
		return
	}
	name := args[0]

	wd, err := os.Getwd()
	if err != nil {
		cli.Fail("Working directory: %v", err)
		cli.OsExit(1)
		return
	}
	projCfg, _ := cliplugin.DiscoverProjectConfig(wd)
	projDir := projCfg.ProjectDir
	if projDir == "" {
		projDir = settingsDir(wd)
	}
	canonProj := cliplugin.CanonicalProjectDir(projDir)

	cfgPath, err := cliplugin.DefaultUserConfigPath()
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}

	err = cliplugin.UpdateUserConfig(cfgPath, func(cfg *cliplugin.UserConfig) error {
		before := len(cfg.TrustedProjects)
		cfg.TrustedProjects = slices.DeleteFunc(cfg.TrustedProjects, func(tp cliplugin.TrustedProjectPlugin) bool {
			return cliplugin.CanonicalProjectDir(tp.ProjectDir) == canonProj && tp.Name == name
		})
		if len(cfg.TrustedProjects) == before {
			return fmt.Errorf("no trust record found for project plugin %q in %s", name, canonProj)
		}
		return nil
	})
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	cli.Success("Revoked trust for project plugin %s in %s", name, canonProj)
}

func runPluginDoctor(args []string, jsonMode bool) {
	var filterName string
	var wantJSON bool
	for _, a := range args {
		if a == "--json" {
			wantJSON = true
			continue
		}
		if cli.IsFlagToken(a) {
			pluginUsageFail(jsonMode, "unknown flag %s", a)
			return
		}
		if filterName != "" {
			pluginUsageFail(jsonMode, "unexpected argument %q", a)
			return
		}
		filterName = a
	}
	_ = wantJSON

	userCfg, projCfg, cfgPath, wd, userErr, projErr := loadConfigs()
	if userErr != nil {
		pluginRuntimeFail(jsonMode, "%v", userErr)
		return
	}
	diags := cliplugin.Diagnose(context.Background(), cliplugin.ResolveOptions{
		UserConfig:    userCfg,
		ProjectConfig: projCfg,
		Builtins:      getHostBuiltins(),
	}, project.CliVersion(), wd, filterName)

	if filterName != "" && len(diags) == 0 {
		if projErr != nil {
			pluginRuntimeFail(jsonMode, "Plugin %q not found in global registry and project settings failed to load: %v", filterName, projErr)
			return
		}
		pluginRuntimeFail(jsonMode, "Plugin %q is not registered or declared in this project.", filterName)
		return
	}

	hasError := projErr != nil
	for _, d := range diags {
		if d.Status != cliplugin.StatusReady {
			hasError = true
		}
	}

	if jsonMode {
		doc := map[string]any{
			"config_path":      cfgPath,
			"protocol_version": cliplugin.ProtocolVersion,
			"cli_version":      project.CliVersion(),
			"ok":               !hasError,
			"plugins":          diags,
		}
		if projErr != nil {
			doc["project_error"] = projErr.Error()
		}
		if err := cli.EmitJSON(doc); err != nil {
			cli.Fail("emit JSON: %v", err)
			cli.OsExit(1)
			return
		}
		if hasError {
			cli.OsExit(1)
		}
		return
	}

	if projErr != nil {
		cli.Fail("Project settings error (global plugins checked below): %v", projErr)
	}
	if len(diags) == 0 && projErr == nil {
		cli.Info("No CLI plugins configured (registry: %s).", cfgPath)
		return
	}

	for _, d := range diags {
		switch d.Status {
		case cliplugin.StatusReady:
			cli.Success("%s (%s, protocol v%d, %s): %s", d.Name, d.Version, d.ProtocolVersion, string(d.Source), d.Summary)
			if len(d.Subcommands) > 0 {
				names := make([]string, len(d.Subcommands))
				for i, sub := range d.Subcommands {
					names[i] = sub.Name
				}
				cli.Info("  subcommands: %s", strings.Join(names, ", "))
			}
			if len(d.AllowEnv) > 0 {
				cli.Info("  env passthrough: %s", strings.Join(d.AllowEnv, ", "))
			}
			if len(d.AllowEnvFlags) > 0 {
				cli.Info("  env flags: %s", strings.Join(d.AllowEnvFlags, ", "))
			}
			if len(d.AllowHooks) > 0 {
				cli.Info("  active hooks: %s", strings.Join(d.AllowHooks, ", "))
			}
			if len(d.AllowOverrides) > 0 {
				cli.Info("  active overrides: %s", strings.Join(d.AllowOverrides, ", "))
			}
		default:
			cli.Fail("%s (%s, %s): %s", d.Name, string(d.Status), string(d.Source), d.Error)
			if d.InstallHint != "" {
				cli.Info("  install: %s", d.InstallHint)
			}
		}
	}
	if hasError {
		cli.OsExit(1)
	}
}

func computeApprovedGrants(m cliplugin.Manifest, g grantFlags, builtins map[string]string) (allowEnv, allowEnvFlags, allowHooks, allowOverrides []string, err error) {
	if !g.noManifestEnv {
		allowEnv = append(allowEnv, m.EnvPassthrough...)
		allowEnvFlags = append(allowEnvFlags, m.EnvFromFlags...)
	}
	allowEnv = append(allowEnv, g.allowEnv...)
	allowEnvFlags = append(allowEnvFlags, g.allowEnvFlags...)

	for _, pat := range allowEnv {
		if err := cliplugin.ValidateEnvPattern(pat); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("invalid --allow-env %q: %w", pat, err)
		}
	}
	for _, flagName := range allowEnvFlags {
		if err := cliplugin.ValidateEnvFlag(flagName); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("invalid --allow-env-flag %q: %w", flagName, err)
		}
	}

	canonicalSet := map[string]bool{}
	for _, c := range builtins {
		canonicalSet[c] = true
	}

	for _, h := range g.allowHooks {
		spec, err := cliplugin.ParseHookTarget(h)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("invalid --allow-hook %q: %w", h, err)
		}
		if len(canonicalSet) > 0 && !canonicalSet[spec.Command] {
			return nil, nil, nil, nil, fmt.Errorf("cannot grant hook %q: %q is not a built-in command", h, spec.Command)
		}
		if !cliplugin.ManifestSupportsHook(m, spec.Target()) {
			return nil, nil, nil, nil, fmt.Errorf("plugin %q manifest does not declare hook %q", m.Name, spec.Target())
		}
		allowHooks = append(allowHooks, spec.Target())
	}

	for _, ov := range g.allowOverride {
		if cliplugin.IsProtectedBuiltin(ov) {
			return nil, nil, nil, nil, fmt.Errorf("%w: %q", cliplugin.ErrProtectedBuiltin, ov)
		}
		if len(canonicalSet) > 0 && !canonicalSet[ov] {
			return nil, nil, nil, nil, fmt.Errorf("cannot grant override %q: not a built-in command", ov)
		}
		if !cliplugin.ManifestSupportsOverride(m, ov) {
			return nil, nil, nil, nil, fmt.Errorf("plugin %q manifest does not declare override %q", m.Name, ov)
		}
		allowOverrides = append(allowOverrides, ov)
	}

	return dedupeSorted(allowEnv), dedupeSorted(allowEnvFlags), dedupeSorted(allowHooks), dedupeSorted(allowOverrides), nil
}

func dedupeSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := slices.Clone(in)
	slices.Sort(out)
	return slices.Compact(out)
}

func printGrantedCapabilities(allowEnv, allowEnvFlags, allowHooks, allowOverrides []string) {
	if len(allowEnv) > 0 {
		cli.Info("Approved env passthrough: %s", strings.Join(allowEnv, ", "))
	}
	if len(allowEnvFlags) > 0 {
		cli.Info("Approved env flags: %s", strings.Join(allowEnvFlags, ", "))
	}
	if len(allowHooks) > 0 {
		cli.Info("Approved hooks: %s", strings.Join(allowHooks, ", "))
	}
	if len(allowOverrides) > 0 {
		cli.Info("Approved overrides: %s", strings.Join(allowOverrides, ", "))
	}
}

func printAvailableInactiveCapabilities(m cliplugin.Manifest, activeHooks, activeOverrides []string) {
	var inactiveHooks []string
	for _, h := range m.Hooks {
		if !slices.Contains(activeHooks, h.Target()) {
			inactiveHooks = append(inactiveHooks, h.Target())
		}
	}
	var inactiveOverrides []string
	for _, ov := range m.Overrides {
		if !slices.Contains(activeOverrides, ov) {
			inactiveOverrides = append(inactiveOverrides, ov)
		}
	}
	if len(inactiveHooks) > 0 {
		cli.Info("Optional hooks declared (inactive unless opted in with --allow-hook): %s", strings.Join(inactiveHooks, ", "))
	}
	if len(inactiveOverrides) > 0 {
		cli.Info("Optional overrides declared (inactive unless opted in with --allow-override): %s", strings.Join(inactiveOverrides, ", "))
	}
}

func loadConfigs() (cliplugin.UserConfig, cliplugin.ProjectConfig, string, string, error, error) {
	cfgPath, err := cliplugin.DefaultUserConfigPath()
	if err != nil {
		return cliplugin.UserConfig{}, cliplugin.ProjectConfig{}, "", "", err, nil
	}
	userCfg, err := cliplugin.LoadUserConfig(cfgPath)
	if err != nil {
		return cliplugin.UserConfig{}, cliplugin.ProjectConfig{}, cfgPath, "", err, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return userCfg, cliplugin.ProjectConfig{}, cfgPath, "", err, nil
	}
	projCfg, projErr := cliplugin.DiscoverProjectConfig(wd)
	if projErr != nil {
		return userCfg, cliplugin.ProjectConfig{}, cfgPath, wd, nil, projErr
	}
	return userCfg, projCfg, cfgPath, wd, nil, nil
}

// CompanionHint describes a known first-party companion CLI plugin so users
// invoking `gofastr native ...` before installing it get a targeted diagnostic.
type CompanionHint struct {
	Name        string
	Description string
	Install     string
	Register    string
}

var knownCompanions = map[string]CompanionHint{
	"native": {
		Name:        "native",
		Description: "Build and run GoFastr mobile (iOS/Android) apps via gofastr-native (requires a CLI-plugin protocol v1 release)",
		Install:     "go install github.com/DonaldMurillo/gofastr-native/cmd/gofastr-native@latest",
		Register:    "gofastr plugin add native gofastr-native",
	},
}

// LookupCompanionHint returns install/register guidance for a known companion
// module (such as "native") when it is not registered.
func LookupCompanionHint(name string) (CompanionHint, bool) {
	h, ok := knownCompanions[name]
	return h, ok
}

// errorsIsMissing checks whether an error represents an uninstalled plugin.
func errorsIsMissing(err error) bool {
	return errors.Is(err, cliplugin.ErrPluginNotFound)
}
