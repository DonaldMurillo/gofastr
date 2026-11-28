package cliplugin

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/settings"
	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/internal/filelock"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// ConfigVersion is the version of the user plugins.yml format.
const ConfigVersion = 1

const maxConfigBytes = 1 << 20 // 1 MiB cap on plugins.yml

// UserConfig represents the user-owned global plugin registry stored at
// ~/.config/gofastr/plugins.yml (honoring GOFASTR_PLUGIN_CONFIG and
// XDG_CONFIG_HOME).
type UserConfig struct {
	Version         int                    `json:"version"`
	Plugins         []RegisteredPlugin     `json:"plugins,omitempty"`
	TrustedProjects []TrustedProjectPlugin `json:"trusted_projects,omitempty"`
}

// RegisteredPlugin is a globally registered, operator-trusted CLI plugin.
type RegisteredPlugin struct {
	Name           string   `json:"name"`
	Command        []string `json:"command"`
	Summary        string   `json:"summary,omitempty"`
	AllowEnv       []string `json:"allow_env,omitempty"`
	AllowEnvFlags  []string `json:"allow_env_flags,omitempty"`
	AllowHooks     []string `json:"allow_hooks,omitempty"`
	AllowOverrides []string `json:"allow_overrides,omitempty"`
}

// TrustedProjectPlugin records explicit operator approval for one
// project-declared plugin command in a specific repository.
type TrustedProjectPlugin struct {
	ProjectDir     string   `json:"project_dir"`
	Name           string   `json:"name"`
	Command        []string `json:"command"`
	SHA256         string   `json:"sha256,omitempty"`
	Summary        string   `json:"summary,omitempty"`
	AllowEnv       []string `json:"allow_env,omitempty"`
	AllowEnvFlags  []string `json:"allow_env_flags,omitempty"`
	AllowHooks     []string `json:"allow_hooks,omitempty"`
	AllowOverrides []string `json:"allow_overrides,omitempty"`
}

// ProjectConfig holds the decoded `plugins:` section from gofastr.settings.yml.
type ProjectConfig struct {
	Path       string
	ProjectDir string
	Found      bool
	Plugins    []ProjectPlugin
}

// ProjectPlugin declares a plugin expected or provided by a repository.
//
// An entry without Command documents an expected global plugin (for example
// `native` with an install hint). An entry with Command declares a repo-scoped
// executable that never runs until explicitly trusted in UserConfig.
type ProjectPlugin struct {
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Install       string   `json:"install,omitempty"`
	Command       []string `json:"command,omitempty"`
	GoPackage     string   `json:"go_package,omitempty"`
	AllowEnv      []string `json:"allow_env,omitempty"`
	AllowEnvFlags []string `json:"allow_env_flags,omitempty"`
	Hooks         []string `json:"hooks,omitempty"`
	Overrides     []string `json:"overrides,omitempty"`
}

// DefaultUserConfigPath returns the path of the user plugin registry:
//  1. $GOFASTR_PLUGIN_CONFIG if set
//  2. $XDG_CONFIG_HOME/gofastr/plugins.yml if XDG_CONFIG_HOME is set
//  3. ~/.config/gofastr/plugins.yml
func DefaultUserConfigPath() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv(EnvConfigPath)); explicit != "" {
		return explicit, nil
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "gofastr", "plugins.yml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "gofastr", "plugins.yml"), nil
}

// LoadUserConfig reads and validates the user plugin registry at path.
// A missing file returns an empty version-1 UserConfig with no error.
// Symlinks, non-regular files, files exceeding maxConfigBytes, and
// group/world-writable registry files or directories are refused.
func LoadUserConfig(path string) (UserConfig, error) {
	if err := rejectDotDotConfigPath(path); err != nil {
		return UserConfig{}, err
	}
	if err := checkUserConfigReadSafety(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return UserConfig{Version: ConfigVersion}, nil
		}
		return UserConfig{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return UserConfig{Version: ConfigVersion}, nil
		}
		return UserConfig{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return UserConfig{}, err
	}
	if !info.Mode().IsRegular() {
		return UserConfig{}, fmt.Errorf("%s: plugin config must be a regular file", path)
	}
	if err := checkOpenConfigSafety(path, f, info); err != nil {
		return UserConfig{}, err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return UserConfig{}, err
	}
	if len(data) > maxConfigBytes {
		return UserConfig{}, fmt.Errorf("%s: plugin config exceeds %d byte limit", path, maxConfigBytes)
	}
	return ParseUserConfig(path, data)
}

// ParseUserConfig decodes and validates a user plugins.yml document.
func ParseUserConfig(path string, data []byte) (UserConfig, error) {
	node, err := coreyaml.Parse(string(data))
	if err != nil {
		return UserConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	root, err := coreyaml.ExpectMap(node, "plugins config")
	if err != nil {
		return UserConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	allowedRoot := map[string]bool{
		"version":          true,
		"plugins":          true,
		"trusted_projects": true,
	}
	if err := rejectUnknownYAMLKeys(root, allowedRoot, "plugins config"); err != nil {
		return UserConfig{}, fmt.Errorf("%s: %w", path, err)
	}

	cfg := UserConfig{Version: ConfigVersion}
	if v := root["version"]; v != nil {
		n, ok := v.Value.(int64)
		if v.Kind != coreyaml.Scalar || !ok || n != ConfigVersion {
			return UserConfig{}, fmt.Errorf("%s: version %v is not supported (expected %d)", path, v.Value, ConfigVersion)
		}
		cfg.Version = int(n)
	}

	if pNode := root["plugins"]; pNode != nil {
		list, err := expectYAMLList(pNode, "plugins")
		if err != nil {
			return UserConfig{}, fmt.Errorf("%s: %w", path, err)
		}
		seen := map[string]bool{}
		for i, item := range list {
			label := fmt.Sprintf("plugins[%d]", i)
			p, err := decodeRegisteredPlugin(item, label)
			if err != nil {
				return UserConfig{}, fmt.Errorf("%s: %w", path, err)
			}
			if seen[p.Name] {
				return UserConfig{}, fmt.Errorf("%s: %w: %q", path, ErrDuplicatePlugin, p.Name)
			}
			seen[p.Name] = true
			cfg.Plugins = append(cfg.Plugins, p)
		}
	}

	if tNode := root["trusted_projects"]; tNode != nil {
		list, err := expectYAMLList(tNode, "trusted_projects")
		if err != nil {
			return UserConfig{}, fmt.Errorf("%s: %w", path, err)
		}
		seen := map[projectPluginKey]bool{}
		for i, item := range list {
			label := fmt.Sprintf("trusted_projects[%d]", i)
			tp, err := decodeTrustedProjectPlugin(item, label)
			if err != nil {
				return UserConfig{}, fmt.Errorf("%s: %w", path, err)
			}
			key := projectPluginKey{projectDir: cleanProjectDirKey(tp.ProjectDir), name: tp.Name}
			if seen[key] {
				return UserConfig{}, fmt.Errorf("%s: %w: project trust entry %q in %q", path, ErrDuplicatePlugin, tp.Name, tp.ProjectDir)
			}
			seen[key] = true
			cfg.TrustedProjects = append(cfg.TrustedProjects, tp)
		}
	}

	return cfg, nil
}

// SaveUserConfig atomically writes cfg to path with owner-only (0600)
// permissions under an exclusive advisory lock. Refuses symlinks at path or
// its lockfile and refuses group/world-writable parent directories.
func SaveUserConfig(path string, cfg UserConfig) error {
	return UpdateUserConfig(path, func(dst *UserConfig) error {
		*dst = cfg
		return nil
	})
}

// UpdateUserConfig runs an atomic read-modify-write transaction on the user
// plugin registry at path, holding an exclusive advisory lock across load,
// mutation, and owner-only atomic replacement so concurrent add/remove/trust
// operations cannot lose updates or resurrect revoked trust.
func UpdateUserConfig(path string, fn func(*UserConfig) error) error {
	if err := rejectDotDotConfigPath(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create plugin config dir: %w", err)
	}
	if err := checkUserConfigDirSafety(dir); err != nil {
		return err
	}
	if err := refuseSymlinkIfPresent(path, "plugin config"); err != nil {
		return err
	}

	lockPath := path + ".lock"
	if err := refuseSymlinkIfPresent(lockPath, "plugin config lock"); err != nil {
		return err
	}
	lockFile, err := openLockFileNoFollow(lockPath)
	if err != nil {
		return fmt.Errorf("open plugin config lock: %w", err)
	}
	defer lockFile.Close()
	if err := filelock.Lock(lockFile); err != nil {
		return fmt.Errorf("lock plugin config: %w", err)
	}

	// Re-check target after acquiring the lock.
	if err := refuseSymlinkIfPresent(path, "plugin config"); err != nil {
		return err
	}
	cfg, err := LoadUserConfig(path)
	if err != nil {
		return err
	}
	if err := fn(&cfg); err != nil {
		return err
	}
	body := FormatUserConfig(cfg)
	if len(body) > maxConfigBytes {
		return fmt.Errorf("%s: plugin config exceeds %d byte limit", path, maxConfigBytes)
	}
	if err := fileperm.ReplaceOwnerOnly(path, body); err != nil {
		return fmt.Errorf("write plugin config: %w", err)
	}
	return nil
}

func rejectDotDotConfigPath(path string) error {
	for _, seg := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return fmt.Errorf("%s: plugin config path must not contain '..' segments", path)
		}
	}
	return nil
}

func cleanProjectDirKey(dir string) string {
	if dir == "" {
		return ""
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return filepath.Clean(dir)
}

func refuseSymlinkIfPresent(path, label string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink %s at %s", label, path)
	}
	return nil
}

// FormatUserConfig renders cfg as deterministic YAML sorted by plugin name and
// project directory.
func FormatUserConfig(cfg UserConfig) []byte {
	var b bytes.Buffer
	b.WriteString("version: 1\n")

	plugins := slices.Clone(cfg.Plugins)
	slices.SortFunc(plugins, func(a, c RegisteredPlugin) int {
		return cmp.Compare(a.Name, c.Name)
	})
	if len(plugins) > 0 {
		b.WriteString("plugins:\n")
		for _, p := range plugins {
			writeYAMLField(&b, "  - name", p.Name)
			writeYAMLStringList(&b, "    command", p.Command)
			if p.Summary != "" {
				writeYAMLField(&b, "    summary", p.Summary)
			}
			if len(p.AllowEnv) > 0 {
				writeYAMLStringList(&b, "    allow_env", sortedUnique(p.AllowEnv))
			}
			if len(p.AllowEnvFlags) > 0 {
				writeYAMLStringList(&b, "    allow_env_flags", sortedUnique(p.AllowEnvFlags))
			}
			if len(p.AllowHooks) > 0 {
				writeYAMLStringList(&b, "    allow_hooks", sortedUnique(p.AllowHooks))
			}
			if len(p.AllowOverrides) > 0 {
				writeYAMLStringList(&b, "    allow_overrides", sortedUnique(p.AllowOverrides))
			}
		}
	}

	trusted := slices.Clone(cfg.TrustedProjects)
	slices.SortFunc(trusted, func(a, c TrustedProjectPlugin) int {
		if d := cmp.Compare(a.ProjectDir, c.ProjectDir); d != 0 {
			return d
		}
		return cmp.Compare(a.Name, c.Name)
	})
	if len(trusted) > 0 {
		b.WriteString("trusted_projects:\n")
		for _, tp := range trusted {
			writeYAMLField(&b, "  - project_dir", tp.ProjectDir)
			writeYAMLField(&b, "    name", tp.Name)
			writeYAMLStringList(&b, "    command", tp.Command)
			if tp.SHA256 != "" {
				writeYAMLField(&b, "    sha256", tp.SHA256)
			}
			if tp.Summary != "" {
				writeYAMLField(&b, "    summary", tp.Summary)
			}
			if len(tp.AllowEnv) > 0 {
				writeYAMLStringList(&b, "    allow_env", sortedUnique(tp.AllowEnv))
			}
			if len(tp.AllowEnvFlags) > 0 {
				writeYAMLStringList(&b, "    allow_env_flags", sortedUnique(tp.AllowEnvFlags))
			}
			if len(tp.AllowHooks) > 0 {
				writeYAMLStringList(&b, "    allow_hooks", sortedUnique(tp.AllowHooks))
			}
			if len(tp.AllowOverrides) > 0 {
				writeYAMLStringList(&b, "    allow_overrides", sortedUnique(tp.AllowOverrides))
			}
		}
	}

	return b.Bytes()
}

func writeYAMLField(b *bytes.Buffer, key, val string) {
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(strconv.Quote(val))
	b.WriteByte('\n')
}

func writeYAMLStringList(b *bytes.Buffer, key string, items []string) {
	b.WriteString(key)
	b.WriteString(":\n")
	indent := strings.Repeat(" ", len(key)-len(strings.TrimLeft(key, " "))+2)
	for _, item := range items {
		b.WriteString(indent)
		b.WriteString("- ")
		b.WriteString(strconv.Quote(item))
		b.WriteByte('\n')
	}
}

func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := slices.Clone(in)
	slices.Sort(out)
	return slices.Compact(out)
}

// DiscoverProjectConfig searches for a `plugins:` section in
// gofastr.settings.yml bounded to the enclosing project root. If the section
// fails to decode, the returned config still identifies the settings file and
// its directory.
func DiscoverProjectConfig(startDir string) (ProjectConfig, error) {
	f, ok, err := settings.Find(startDir, "plugins")
	if err != nil || !ok {
		return ProjectConfig{}, err
	}
	sec := f.Section("plugins")
	if sec == nil {
		return ProjectConfig{Path: f.Path, ProjectDir: f.Dir}, nil
	}
	plugins, err := DecodeProjectPlugins(sec)
	if err != nil {
		return ProjectConfig{Path: f.Path, ProjectDir: f.Dir}, fmt.Errorf("%s: %w", f.Path, err)
	}
	return ProjectConfig{
		Path:       f.Path,
		ProjectDir: f.Dir,
		Found:      true,
		Plugins:    plugins,
	}, nil
}

// DecodeProjectPlugins decodes the `plugins:` node from a gofastr.settings.yml
// document.
func DecodeProjectPlugins(node *coreyaml.Node) ([]ProjectPlugin, error) {
	list, err := expectYAMLList(node, "plugins")
	if err != nil {
		return nil, err
	}
	out := make([]ProjectPlugin, 0, len(list))
	seen := map[string]bool{}
	for i, item := range list {
		label := fmt.Sprintf("plugins[%d]", i)
		m, err := coreyaml.ExpectMap(item, label)
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{
			"name":            true,
			"description":     true,
			"install":         true,
			"command":         true,
			"go_package":      true,
			"config":          true,
			"allow_env":       true,
			"allow_env_flags": true,
			"hooks":           true,
			"overrides":       true,
		}
		if err := rejectUnknownYAMLKeys(m, allowed, label); err != nil {
			return nil, err
		}
		name, err := requiredYAMLString(m["name"], label+".name")
		if err != nil {
			return nil, err
		}
		if err := ValidateName(name); err != nil {
			return nil, fmt.Errorf("%s.name: %w", label, err)
		}
		if seen[name] {
			return nil, fmt.Errorf("%w: %q in project settings", ErrDuplicatePlugin, name)
		}
		seen[name] = true

		desc, err := optionalYAMLString(m["description"], label+".description")
		if err != nil {
			return nil, err
		}
		install, err := optionalYAMLString(m["install"], label+".install")
		if err != nil {
			return nil, err
		}
		cmd, err := optionalYAMLStringList(m["command"], label+".command")
		if err != nil {
			return nil, err
		}
		goPackage, err := optionalYAMLString(m["go_package"], label+".go_package")
		if err != nil {
			return nil, err
		}
		if m["go_package"] != nil {
			if m["command"] != nil || !validGoPackage(goPackage) {
				return nil, fmt.Errorf("%s.go_package requires one concrete Go import path and cannot be combined with command", label)
			}
			cmd = []string{"./" + filepath.ToSlash(projectGoBinary(name))}
		}
		if m["config"] != nil {
			if _, err := coreyaml.ExpectMap(m["config"], label+".config"); err != nil {
				return nil, err
			}
		}
		allowEnv, err := optionalYAMLStringList(m["allow_env"], label+".allow_env")
		if err != nil {
			return nil, err
		}
		for _, pat := range allowEnv {
			if err := ValidateEnvPattern(pat); err != nil {
				return nil, fmt.Errorf("%s.allow_env: %w", label, err)
			}
		}
		allowEnvFlags, err := optionalYAMLStringList(m["allow_env_flags"], label+".allow_env_flags")
		if err != nil {
			return nil, err
		}
		for _, flagName := range allowEnvFlags {
			if err := ValidateEnvFlag(flagName); err != nil {
				return nil, fmt.Errorf("%s.allow_env_flags: %w", label, err)
			}
		}
		hooks, err := optionalYAMLStringList(m["hooks"], label+".hooks")
		if err != nil {
			return nil, err
		}
		for _, h := range hooks {
			if _, err := ParseHookTarget(h); err != nil {
				return nil, fmt.Errorf("%s.hooks: %w", label, err)
			}
		}
		overrides, err := optionalYAMLStringList(m["overrides"], label+".overrides")
		if err != nil {
			return nil, err
		}
		for _, ov := range overrides {
			if !commandNameRE.MatchString(ov) {
				return nil, fmt.Errorf("%s.overrides: %w: invalid command %q", label, ErrInvalidManifest, ov)
			}
			if IsProtectedBuiltin(ov) {
				return nil, fmt.Errorf("%s.overrides: %w: %q", label, ErrProtectedBuiltin, ov)
			}
		}

		out = append(out, ProjectPlugin{
			Name:          name,
			Description:   desc,
			Install:       install,
			Command:       cmd,
			GoPackage:     goPackage,
			AllowEnv:      allowEnv,
			AllowEnvFlags: allowEnvFlags,
			Hooks:         hooks,
			Overrides:     overrides,
		})
	}
	return out, nil
}

func decodeRegisteredPlugin(node *coreyaml.Node, label string) (RegisteredPlugin, error) {
	m, err := coreyaml.ExpectMap(node, label)
	if err != nil {
		return RegisteredPlugin{}, err
	}
	allowed := map[string]bool{
		"name":            true,
		"command":         true,
		"summary":         true,
		"allow_env":       true,
		"allow_env_flags": true,
		"allow_hooks":     true,
		"allow_overrides": true,
	}
	if err := rejectUnknownYAMLKeys(m, allowed, label); err != nil {
		return RegisteredPlugin{}, err
	}
	name, err := requiredYAMLString(m["name"], label+".name")
	if err != nil {
		return RegisteredPlugin{}, err
	}
	if err := ValidateName(name); err != nil {
		return RegisteredPlugin{}, fmt.Errorf("%s.name: %w", label, err)
	}
	cmd, err := requiredYAMLStringList(m["command"], label+".command")
	if err != nil {
		return RegisteredPlugin{}, err
	}
	summary, err := optionalYAMLString(m["summary"], label+".summary")
	if err != nil {
		return RegisteredPlugin{}, err
	}
	allowEnv, err := optionalYAMLStringList(m["allow_env"], label+".allow_env")
	if err != nil {
		return RegisteredPlugin{}, err
	}
	for _, pat := range allowEnv {
		if err := ValidateEnvPattern(pat); err != nil {
			return RegisteredPlugin{}, fmt.Errorf("%s.allow_env: %w", label, err)
		}
	}
	allowEnvFlags, err := optionalYAMLStringList(m["allow_env_flags"], label+".allow_env_flags")
	if err != nil {
		return RegisteredPlugin{}, err
	}
	for _, flagName := range allowEnvFlags {
		if err := ValidateEnvFlag(flagName); err != nil {
			return RegisteredPlugin{}, fmt.Errorf("%s.allow_env_flags: %w", label, err)
		}
	}
	allowHooks, err := optionalYAMLStringList(m["allow_hooks"], label+".allow_hooks")
	if err != nil {
		return RegisteredPlugin{}, err
	}
	for _, h := range allowHooks {
		if _, err := ParseHookTarget(h); err != nil {
			return RegisteredPlugin{}, fmt.Errorf("%s.allow_hooks: %w", label, err)
		}
	}
	allowOverrides, err := optionalYAMLStringList(m["allow_overrides"], label+".allow_overrides")
	if err != nil {
		return RegisteredPlugin{}, err
	}
	for _, ov := range allowOverrides {
		if !commandNameRE.MatchString(ov) {
			return RegisteredPlugin{}, fmt.Errorf("%s.allow_overrides: %w: invalid command %q", label, ErrInvalidManifest, ov)
		}
		if IsProtectedBuiltin(ov) {
			return RegisteredPlugin{}, fmt.Errorf("%s.allow_overrides: %w: %q", label, ErrProtectedBuiltin, ov)
		}
	}
	return RegisteredPlugin{
		Name:           name,
		Command:        cmd,
		Summary:        summary,
		AllowEnv:       allowEnv,
		AllowEnvFlags:  allowEnvFlags,
		AllowHooks:     allowHooks,
		AllowOverrides: allowOverrides,
	}, nil
}

func decodeTrustedProjectPlugin(node *coreyaml.Node, label string) (TrustedProjectPlugin, error) {
	m, err := coreyaml.ExpectMap(node, label)
	if err != nil {
		return TrustedProjectPlugin{}, err
	}
	allowed := map[string]bool{
		"project_dir":     true,
		"name":            true,
		"command":         true,
		"sha256":          true,
		"summary":         true,
		"allow_env":       true,
		"allow_env_flags": true,
		"allow_hooks":     true,
		"allow_overrides": true,
	}
	if err := rejectUnknownYAMLKeys(m, allowed, label); err != nil {
		return TrustedProjectPlugin{}, err
	}
	projectDir, err := requiredYAMLString(m["project_dir"], label+".project_dir")
	if err != nil {
		return TrustedProjectPlugin{}, err
	}
	name, err := requiredYAMLString(m["name"], label+".name")
	if err != nil {
		return TrustedProjectPlugin{}, err
	}
	if err := ValidateName(name); err != nil {
		return TrustedProjectPlugin{}, fmt.Errorf("%s.name: %w", label, err)
	}
	cmd, err := requiredYAMLStringList(m["command"], label+".command")
	if err != nil {
		return TrustedProjectPlugin{}, err
	}
	sha, err := optionalYAMLString(m["sha256"], label+".sha256")
	if err != nil {
		return TrustedProjectPlugin{}, err
	}
	summary, err := optionalYAMLString(m["summary"], label+".summary")
	if err != nil {
		return TrustedProjectPlugin{}, err
	}
	allowEnv, err := optionalYAMLStringList(m["allow_env"], label+".allow_env")
	if err != nil {
		return TrustedProjectPlugin{}, err
	}
	for _, pat := range allowEnv {
		if err := ValidateEnvPattern(pat); err != nil {
			return TrustedProjectPlugin{}, fmt.Errorf("%s.allow_env: %w", label, err)
		}
	}
	allowEnvFlags, err := optionalYAMLStringList(m["allow_env_flags"], label+".allow_env_flags")
	if err != nil {
		return TrustedProjectPlugin{}, err
	}
	for _, flagName := range allowEnvFlags {
		if err := ValidateEnvFlag(flagName); err != nil {
			return TrustedProjectPlugin{}, fmt.Errorf("%s.allow_env_flags: %w", label, err)
		}
	}
	allowHooks, err := optionalYAMLStringList(m["allow_hooks"], label+".allow_hooks")
	if err != nil {
		return TrustedProjectPlugin{}, err
	}
	for _, h := range allowHooks {
		if _, err := ParseHookTarget(h); err != nil {
			return TrustedProjectPlugin{}, fmt.Errorf("%s.allow_hooks: %w", label, err)
		}
	}
	allowOverrides, err := optionalYAMLStringList(m["allow_overrides"], label+".allow_overrides")
	if err != nil {
		return TrustedProjectPlugin{}, err
	}
	for _, ov := range allowOverrides {
		if !commandNameRE.MatchString(ov) {
			return TrustedProjectPlugin{}, fmt.Errorf("%s.allow_overrides: %w: invalid command %q", label, ErrInvalidManifest, ov)
		}
		if IsProtectedBuiltin(ov) {
			return TrustedProjectPlugin{}, fmt.Errorf("%s.allow_overrides: %w: %q", label, ErrProtectedBuiltin, ov)
		}
	}
	return TrustedProjectPlugin{
		ProjectDir:     projectDir,
		Name:           name,
		Command:        cmd,
		SHA256:         sha,
		Summary:        summary,
		AllowEnv:       allowEnv,
		AllowEnvFlags:  allowEnvFlags,
		AllowHooks:     allowHooks,
		AllowOverrides: allowOverrides,
	}, nil
}

func rejectUnknownYAMLKeys(m map[string]*coreyaml.Node, allowed map[string]bool, label string) error {
	for _, k := range slices.Sorted(maps.Keys(m)) {
		if !allowed[k] {
			return fmt.Errorf("unknown key %q in %s", k, label)
		}
	}
	return nil
}

func expectYAMLList(node *coreyaml.Node, label string) ([]*coreyaml.Node, error) {
	if node == nil || node.Kind != coreyaml.List {
		return nil, fmt.Errorf("%s must be a list", label)
	}
	return node.List, nil
}

func requiredYAMLString(node *coreyaml.Node, label string) (string, error) {
	v, err := optionalYAMLString(node, label)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	return v, nil
}

func optionalYAMLString(node *coreyaml.Node, label string) (string, error) {
	if node == nil {
		return "", nil
	}
	if node.Kind != coreyaml.Scalar {
		return "", fmt.Errorf("%s must be a string", label)
	}
	v, ok := node.Value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", label)
	}
	return v, nil
}

func requiredYAMLStringList(node *coreyaml.Node, label string) ([]string, error) {
	list, err := optionalYAMLStringList(node, label)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("%s must be a non-empty list of strings", label)
	}
	return list, nil
}

func optionalYAMLStringList(node *coreyaml.Node, label string) ([]string, error) {
	if node == nil {
		return nil, nil
	}
	if node.Kind != coreyaml.List {
		return nil, fmt.Errorf("%s must be a list of strings", label)
	}
	out := make([]string, 0, len(node.List))
	for i, item := range node.List {
		if item.Kind != coreyaml.Scalar {
			return nil, fmt.Errorf("%s[%d] must be a string", label, i)
		}
		v, ok := item.Value.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a string", label, i)
		}
		out = append(out, v)
	}
	return out, nil
}
