package cliplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"golang.org/x/mod/semver"
)

// ProtocolVersion is the CLI command-extension wire contract version.
const ProtocolVersion = 1

// HandshakeFlag is passed to a plugin executable to request its JSON manifest.
const HandshakeFlag = "--gofastr-plugin-manifest"

// Environment variables forming the host<->plugin contract.
const (
	EnvManifestRequest = "GOFASTR_PLUGIN_MANIFEST"
	EnvProtocolVersion = "GOFASTR_CLI_PROTOCOL"
	EnvCLIVersion      = "GOFASTR_CLI_VERSION"
	EnvCLIBin          = "GOFASTR_CLI_BIN"
	EnvPluginName      = "GOFASTR_PLUGIN_NAME"
	EnvProjectRoot     = "GOFASTR_PROJECT_ROOT"
	EnvHookPhase       = "GOFASTR_HOOK_PHASE"
	EnvHookCommand     = "GOFASTR_HOOK_COMMAND"
	EnvOverrideCommand = "GOFASTR_OVERRIDE_COMMAND"
	EnvBypassPlugins   = "GOFASTR_NO_PLUGINS"
	EnvConfigPath      = "GOFASTR_PLUGIN_CONFIG"
	EnvFilteredDB      = "GOFASTR_FILTERED_DATABASE_URL"
)

// Sentinel errors returned by protocol, registry, trust, and resolver checks.
var (
	ErrProtocolMismatch    = errors.New("cliplugin: protocol version mismatch")
	ErrIncompatibleVersion = errors.New("cliplugin: incompatible gofastr CLI version")
	ErrInvalidManifest     = errors.New("cliplugin: invalid plugin manifest")
	ErrBuiltinCollision    = errors.New("cliplugin: plugin name collides with a built-in command")
	ErrDuplicatePlugin     = errors.New("cliplugin: duplicate plugin registration")
	ErrDuplicateOverride   = errors.New("cliplugin: duplicate built-in command override")
	ErrUntrustedPlugin     = errors.New("cliplugin: project plugin is not trusted")
	ErrTrustMismatch       = errors.New("cliplugin: project plugin changed since it was trusted")
	ErrInvalidPath         = errors.New("cliplugin: invalid plugin executable path")
	ErrPluginNotFound      = errors.New("cliplugin: plugin not found")
	ErrProtectedBuiltin    = errors.New("cliplugin: built-in command cannot be hooked or overridden")
)

// HookPhase identifies when a command hook runs around a built-in command.
type HookPhase string

const (
	HookPre  HookPhase = "pre"
	HookPost HookPhase = "post"
)

// InvocationMode describes how a plugin executable was launched.
type InvocationMode string

const (
	ModeStandalone InvocationMode = "standalone"
	ModeNamespace  InvocationMode = "namespace"
	ModeHook       InvocationMode = "hook"
	ModeOverride   InvocationMode = "override"
)

// Manifest is the JSON document a plugin prints when invoked with HandshakeFlag.
type Manifest struct {
	ProtocolVersion int           `json:"protocol_version"`
	Name            string        `json:"name"`
	Version         string        `json:"version"`
	Summary         string        `json:"summary"`
	MinCLIVersion   string        `json:"min_gofastr_version,omitempty"`
	MaxCLIVersion   string        `json:"max_gofastr_version,omitempty"`
	Subcommands     []CommandInfo `json:"subcommands,omitempty"`
	SupportsJSON    bool          `json:"supports_json,omitempty"`
	EnvPassthrough  []string      `json:"env_passthrough,omitempty"`
	EnvFromFlags    []string      `json:"env_from_flags,omitempty"`
	Hooks           []HookSpec    `json:"hooks,omitempty"`
	Overrides       []string      `json:"overrides,omitempty"`
}

// CommandInfo describes one subcommand exposed by a plugin.
type CommandInfo struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

// HookSpec declares an optional hook a plugin can run around a built-in command.
// Hooks remain inactive until the operator explicitly grants them.
type HookSpec struct {
	Phase   HookPhase `json:"phase"`
	Command string    `json:"command"`
	Summary string    `json:"summary,omitempty"`
}

// Target returns the canonical "phase:command" identifier (e.g. "pre:build").
func (h HookSpec) Target() string {
	return string(h.Phase) + ":" + h.Command
}

// Invocation holds the host metadata injected into a plugin process.
type Invocation struct {
	Mode          InvocationMode
	Protocol      string
	CLIVersion    string
	CLIBin        string
	PluginName    string
	ProjectRoot   string
	HookPhase     HookPhase
	TargetCommand string
}

var (
	pluginNameRE  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	commandNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
)

// ProtectedBuiltins are meta-commands that can never be hooked or overridden,
// ensuring the operator always retains access to help, version, and plugin
// management.
var protectedBuiltins = []string{"help", "plugin", "version"}

// IsProtectedBuiltin reports whether canonical command name is protected from
// hooks and overrides.
func IsProtectedBuiltin(name string) bool {
	return slices.Contains(protectedBuiltins, name)
}

// ValidateName checks that a plugin namespace name is lowercase ASCII
// letters, digits, or hyphens, starting with a letter, at most 32 bytes.
func ValidateName(name string) error {
	if !pluginNameRE.MatchString(name) {
		return fmt.Errorf("%w: invalid name %q (must match ^[a-z][a-z0-9-]{0,31}$)", ErrInvalidManifest, name)
	}
	return nil
}

// ParseHookTarget parses a "pre:<cmd>" or "post:<cmd>" hook target.
func ParseHookTarget(raw string) (HookSpec, error) {
	phaseStr, cmd, ok := strings.Cut(raw, ":")
	if !ok {
		return HookSpec{}, fmt.Errorf("%w: hook target %q must be pre:<command> or post:<command>", ErrInvalidManifest, raw)
	}
	spec := HookSpec{Phase: HookPhase(phaseStr), Command: cmd}
	if err := ValidateHookSpec(spec); err != nil {
		return HookSpec{}, err
	}
	return spec, nil
}

// ValidateHookSpec checks that a hook spec has a valid phase and target command.
func ValidateHookSpec(h HookSpec) error {
	if h.Phase != HookPre && h.Phase != HookPost {
		return fmt.Errorf("%w: hook phase %q must be %q or %q", ErrInvalidManifest, h.Phase, HookPre, HookPost)
	}
	if !commandNameRE.MatchString(h.Command) {
		return fmt.Errorf("%w: hook command %q is not a valid command name", ErrInvalidManifest, h.Command)
	}
	if IsProtectedBuiltin(h.Command) {
		return fmt.Errorf("%w: %q", ErrProtectedBuiltin, h.Command)
	}
	return nil
}

// Validate checks every field of m against protocol v1 rules.
func (m Manifest) Validate() error {
	if m.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("%w: plugin reports protocol_version %d, host supports %d",
			ErrProtocolMismatch, m.ProtocolVersion, ProtocolVersion)
	}
	if err := ValidateName(m.Name); err != nil {
		return err
	}
	if strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("%w: version is required", ErrInvalidManifest)
	}
	if strings.ContainsAny(m.Version, "\r\n") {
		return fmt.Errorf("%w: version must be single-line", ErrInvalidManifest)
	}
	if strings.TrimSpace(m.Summary) == "" {
		return fmt.Errorf("%w: summary is required", ErrInvalidManifest)
	}
	if strings.ContainsAny(m.Summary, "\r\n") {
		return fmt.Errorf("%w: summary must be single-line", ErrInvalidManifest)
	}
	if m.MinCLIVersion != "" && !semver.IsValid(m.MinCLIVersion) {
		return fmt.Errorf("%w: min_gofastr_version %q must be valid semver (e.g. v0.98.0)", ErrInvalidManifest, m.MinCLIVersion)
	}
	if m.MaxCLIVersion != "" && !semver.IsValid(m.MaxCLIVersion) {
		return fmt.Errorf("%w: max_gofastr_version %q must be valid semver (e.g. v1.0.0)", ErrInvalidManifest, m.MaxCLIVersion)
	}
	if m.MinCLIVersion != "" && m.MaxCLIVersion != "" && semver.Compare(m.MinCLIVersion, m.MaxCLIVersion) > 0 {
		return fmt.Errorf("%w: min_gofastr_version %s exceeds max_gofastr_version %s",
			ErrInvalidManifest, m.MinCLIVersion, m.MaxCLIVersion)
	}
	seenSub := map[string]bool{}
	for i, sub := range m.Subcommands {
		if !commandNameRE.MatchString(sub.Name) {
			return fmt.Errorf("%w: subcommands[%d].name %q is invalid", ErrInvalidManifest, i, sub.Name)
		}
		if seenSub[sub.Name] {
			return fmt.Errorf("%w: duplicate subcommand %q", ErrInvalidManifest, sub.Name)
		}
		seenSub[sub.Name] = true
		if strings.ContainsAny(sub.Summary, "\r\n") {
			return fmt.Errorf("%w: subcommands[%d].summary must be single-line", ErrInvalidManifest, i)
		}
	}
	for _, pat := range m.EnvPassthrough {
		if err := ValidateEnvPattern(pat); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidManifest, err)
		}
	}
	for _, flagName := range m.EnvFromFlags {
		if err := ValidateEnvFlag(flagName); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidManifest, err)
		}
	}
	seenHooks := map[string]bool{}
	for _, h := range m.Hooks {
		if err := ValidateHookSpec(h); err != nil {
			return err
		}
		key := h.Target()
		if seenHooks[key] {
			return fmt.Errorf("%w: duplicate hook %q", ErrInvalidManifest, key)
		}
		seenHooks[key] = true
	}
	seenOverrides := map[string]bool{}
	for _, ov := range m.Overrides {
		if !commandNameRE.MatchString(ov) {
			return fmt.Errorf("%w: override command %q is not a valid command name", ErrInvalidManifest, ov)
		}
		if IsProtectedBuiltin(ov) {
			return fmt.Errorf("%w: %q", ErrProtectedBuiltin, ov)
		}
		if seenOverrides[ov] {
			return fmt.Errorf("%w: duplicate override %q", ErrInvalidManifest, ov)
		}
		seenOverrides[ov] = true
	}
	return nil
}

// CheckCLIVersion verifies that cliVersion satisfies m.MinCLIVersion and
// m.MaxCLIVersion. Non-semver dev builds (e.g. "dev") are allowed so local
// checkouts can always develop against plugins.
func (m Manifest) CheckCLIVersion(cliVersion string) error {
	v := normalizeSemver(cliVersion)
	if v == "" {
		return nil
	}
	if m.MinCLIVersion != "" && semver.Compare(v, m.MinCLIVersion) < 0 {
		return fmt.Errorf("%w: plugin %q (%s) requires gofastr >= %s, host is %s",
			ErrIncompatibleVersion, textsafe.ScrubTerminal(m.Name), textsafe.ScrubTerminal(m.Version), m.MinCLIVersion, v)
	}
	if m.MaxCLIVersion != "" && semver.Compare(v, m.MaxCLIVersion) > 0 {
		return fmt.Errorf("%w: plugin %q (%s) supports gofastr <= %s, host is %s",
			ErrIncompatibleVersion, textsafe.ScrubTerminal(m.Name), textsafe.ScrubTerminal(m.Version), m.MaxCLIVersion, v)
	}
	return nil
}

func normalizeSemver(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || s == "dev" {
		return ""
	}
	if !strings.HasPrefix(s, "v") {
		s = "v" + s
	}
	if !semver.IsValid(s) {
		return ""
	}
	return s
}

// MaybeServeManifest checks whether args requests the plugin manifest. It
// triggers when HandshakeFlag is the sole argument (standalone or direct binary)
// or when EnvManifestRequest is "1" and HandshakeFlag is the trailing argument
// (supporting registered commands with fixed prefix arguments). When triggered,
// MaybeServeManifest validates m, encodes it as JSON to w, and returns (true, err).
func MaybeServeManifest(args []string, w io.Writer, m Manifest) (bool, error) {
	direct := len(args) == 1 && args[0] == HandshakeFlag
	prefixed := os.Getenv(EnvManifestRequest) == "1" && len(args) > 0 && args[len(args)-1] == HandshakeFlag
	if !direct && !prefixed {
		return false, nil
	}
	if m.ProtocolVersion == 0 {
		m.ProtocolVersion = ProtocolVersion
	}
	if err := m.Validate(); err != nil {
		return true, err
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return true, fmt.Errorf("encode plugin manifest: %w", err)
	}
	return true, nil
}

// InvocationFromEnv inspects the current process environment to tell a plugin
// whether it was launched standalone or via the gofastr CLI seam.
func InvocationFromEnv() Invocation {
	inv := Invocation{
		Mode:        ModeStandalone,
		Protocol:    os.Getenv(EnvProtocolVersion),
		CLIVersion:  os.Getenv(EnvCLIVersion),
		CLIBin:      os.Getenv(EnvCLIBin),
		PluginName:  os.Getenv(EnvPluginName),
		ProjectRoot: os.Getenv(EnvProjectRoot),
	}
	if inv.Protocol == "" {
		return inv
	}
	if ov := os.Getenv(EnvOverrideCommand); ov != "" {
		inv.Mode = ModeOverride
		inv.TargetCommand = ov
		return inv
	}
	if phase := os.Getenv(EnvHookPhase); phase != "" {
		inv.Mode = ModeHook
		inv.HookPhase = HookPhase(phase)
		inv.TargetCommand = os.Getenv(EnvHookCommand)
		return inv
	}
	inv.Mode = ModeNamespace
	return inv
}

// BuiltinCommand constructs an *exec.Cmd that calls back into the host gofastr
// binary with plugins bypassed (`--builtin <args...>` and GOFASTR_NO_PLUGINS=1).
// Override/wrapper plugins use this to delegate to the real built-in command
// without infinite recursion.
func BuiltinCommand(ctx context.Context, args ...string) (*exec.Cmd, error) {
	bin := os.Getenv(EnvCLIBin)
	if bin == "" {
		return nil, errors.New("cliplugin: GOFASTR_CLI_BIN is not set (not invoked by gofastr)")
	}
	fullArgs := append([]string{"--builtin"}, args...)
	cmd := exec.CommandContext(ctx, bin, fullArgs...)
	cmd.Env = append(os.Environ(), EnvBypassPlugins+"=1")
	return cmd, nil
}
