package cliplugin

import (
	"fmt"
	"maps"
	"os"
	"regexp"
	"runtime"
	"slices"
	"strings"
)

var (
	envExactRE  = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	envPrefixRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{1,}\*$`)
	envFlagRE   = regexp.MustCompile(`^--[a-zA-Z0-9][a-zA-Z0-9_-]*$`)
)

// probeEnvAllowlist is the minimal environment passed during manifest probes:
// OS paths, temp/home dirs, and locale only. Toolchain variables that can hold
// credentials (such as authenticated GOPROXY URLs) are deliberately excluded.
var probeEnvAllowlist = map[string]bool{
	"PATH": true, "HOME": true, "TMPDIR": true, "TMP": true, "TEMP": true,
	"USER": true, "LOGNAME": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true,
	"XDG_CACHE_HOME": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "XDG_STATE_HOME": true,
}

// baseEnvAllowlist lists the minimal environment variables every CLI plugin
// needs to run and render cleanly in a terminal (paths, home/temp dirs,
// locale, color/TTY hints, XDG dirs, and the Go toolchain).
var baseEnvAllowlist = map[string]bool{
	"PATH": true, "HOME": true, "TMPDIR": true, "TMP": true, "TEMP": true,
	"USER": true, "LOGNAME": true, "SHELL": true,
	"LANG": true, "LC_ALL": true, "LC_CTYPE": true, "LC_MESSAGES": true,
	"TERM": true, "COLORTERM": true, "NO_COLOR": true, "CLICOLOR": true,
	"CLICOLOR_FORCE": true, "FORCE_COLOR": true, "CI": true,
	"XDG_CACHE_HOME": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "XDG_STATE_HOME": true,
	// Go toolchain
	"GOROOT": true, "GOPATH": true, "GOBIN": true, "GOCACHE": true,
	"GOMODCACHE": true, "GOTMPDIR": true, "GOFLAGS": true, "GOPROXY": true,
	"GOPRIVATE": true, "GONOPROXY": true, "GONOSUMDB": true, "GOSUMDB": true,
	"GONOSUMCHECK": true, "GOOS": true, "GOARCH": true, "GOARM": true,
	"GOAMD64": true, "GOEXPERIMENT": true, "GOTOOLCHAIN": true, "GOWORK": true,
	"GOENV": true, "GODEBUG": true, "CGO_ENABLED": true, "CC": true, "CXX": true,
}

var baseEnvAllowlistWindows = map[string]bool{
	"SYSTEMROOT": true, "SYSTEMDRIVE": true, "WINDIR": true, "PATHEXT": true,
	"COMSPEC": true, "USERPROFILE": true, "APPDATA": true, "LOCALAPPDATA": true,
	"PROCESSOR_ARCHITECTURE": true, "NUMBER_OF_PROCESSORS": true,
	"HOMEDRIVE": true, "HOMEPATH": true,
}

// blockedSecretKeys are GoFastr and common ambient secrets that are never
// forwarded implicitly, never matched by wildcard patterns, and never allowed
// via env_from_flags.
var blockedSecretKeys = map[string]bool{
	"GOFASTR_SECRET":              true,
	"GOFASTR_HARNESS_PASSPHRASE":  true,
	"GOFASTR_HARNESS_MACHINE_KEY": true,
	"GOFASTR_HARNESS_SECRET":      true,
	"GOFASTR_AUDIT_PASSWORD":      true,
	"DATABASE_URL":                true,
	"AWS_SECRET_ACCESS_KEY":       true,
	"AWS_SESSION_TOKEN":           true,
	"GITHUB_TOKEN":                true,
	"GH_TOKEN":                    true,
	"OPENAI_API_KEY":              true,
	"ANTHROPIC_API_KEY":           true,
	"OPENROUTER_API_KEY":          true,
	"GEMINI_API_KEY":              true,
}

// blockedSecretPrefixes are prefixes reserved for GoFastr secrets and control
// variables that plugins must not pull in via wildcards.
var blockedSecretPrefixes = []string{
	"GOFASTR_HARNESS_",
	"GOFASTR_SECRET",
}

// protocolEnvKeys are owned by the host and always overwritten cleanly by
// BuildExecEnv / BuildProbeEnv so stale parent values cannot spoof context.
var protocolEnvKeys = map[string]bool{
	EnvManifestRequest: true,
	EnvProtocolVersion: true,
	EnvCLIVersion:      true,
	EnvCLIBin:          true,
	EnvPluginName:      true,
	EnvProjectRoot:     true,
	EnvHookPhase:       true,
	EnvHookCommand:     true,
	EnvOverrideCommand: true,
	EnvBypassPlugins:   true,
	EnvConfigPath:      true,
	EnvFilteredDB:      true,
}

// IsBlockedEnvKey reports whether key is a protected secret that must never be
// forwarded via wildcards, defaults, or env_from_flags.
func IsBlockedEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	if blockedSecretKeys[upper] {
		return true
	}
	for _, prefix := range blockedSecretPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

// ValidateEnvPattern validates an env passthrough entry: either an exact
// variable name (JAVA_HOME) or a prefix wildcard with at least 2 characters
// before '*' (ANDROID_*). Blocked secret keys and overly broad prefixes such
// as "*" or "GOFASTR_*" are refused.
func ValidateEnvPattern(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("env pattern must not be empty")
	}
	if strings.HasSuffix(pattern, "*") {
		if !envPrefixRE.MatchString(pattern) {
			return fmt.Errorf("invalid env prefix pattern %q (must match ^[A-Z_][A-Z0-9_]+\\*$)", pattern)
		}
		prefix := strings.TrimSuffix(pattern, "*")
		// Refuse prefixes that would cover blocked GoFastr secrets.
		for _, k := range slices.Sorted(maps.Keys(blockedSecretKeys)) {
			if strings.HasPrefix(k, prefix) {
				return fmt.Errorf("env pattern %q would match blocked secret %s", pattern, k)
			}
		}
		for _, bp := range blockedSecretPrefixes {
			if strings.HasPrefix(bp, prefix) || strings.HasPrefix(prefix, bp) {
				return fmt.Errorf("env pattern %q overlaps blocked secret prefix %s", pattern, bp)
			}
		}
		return nil
	}
	if !envExactRE.MatchString(pattern) {
		return fmt.Errorf("invalid env key %q (must match ^[A-Z_][A-Z0-9_]*$)", pattern)
	}
	if IsBlockedEnvKey(pattern) {
		return fmt.Errorf("env key %q is a protected secret and cannot be requested in env_passthrough", pattern)
	}
	if protocolEnvKeys[pattern] {
		return fmt.Errorf("env key %q is reserved by the gofastr plugin protocol", pattern)
	}
	return nil
}

// ValidateEnvFlag validates an env_from_flags entry (e.g. "--keystore-pass-env").
func ValidateEnvFlag(flagName string) error {
	if !envFlagRE.MatchString(flagName) {
		return fmt.Errorf("invalid env_from_flags entry %q (must match ^--[a-zA-Z0-9][a-zA-Z0-9_-]*$)", flagName)
	}
	return nil
}

// EnvPatternMatches reports whether env key matches pattern (exact or PREFIX_*).
// Blocked secret keys and protocol-reserved keys never match.
func EnvPatternMatches(pattern, key string) bool {
	return envPatternMatches(pattern, key, runtime.GOOS == "windows")
}

func envPatternMatches(pattern, key string, caseInsensitive bool) bool {
	if IsBlockedEnvKey(key) || isProtocolEnvKey(key, caseInsensitive) {
		return false
	}
	matchKey := key
	if caseInsensitive {
		matchKey = strings.ToUpper(key)
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return len(prefix) >= 2 && strings.HasPrefix(matchKey, prefix)
	}
	return matchKey == pattern
}

// EnvOptions configures environment construction for a plugin invocation.
type EnvOptions struct {
	Environ      []string          // source environment; defaults to os.Environ()
	AllowEnv     []string          // approved exact keys or PREFIX_* patterns
	AllowEnvFlag []string          // approved flag names whose values name env vars to forward
	Args         []string          // invocation argv scanned for AllowEnvFlag before "--"
	ProtocolVars map[string]string // host-injected GOFASTR_* protocol vars
}

// BuildProbeEnv constructs the minimal, secret-free environment used for the
// manifest handshake probe. It excludes Go toolchain variables (such as
// GOPROXY) that may embed credentials.
func BuildProbeEnv(environ []string) []string {
	return buildProbeEnv(environ, runtime.GOOS == "windows")
}

func buildProbeEnv(environ []string, caseInsensitive bool) []string {
	src := environ
	if src == nil {
		src = os.Environ()
	}
	selected := map[string]string{}
	for _, kv := range src {
		key, val, ok := strings.Cut(kv, "=")
		if !ok || key == "" || isProtocolEnvKey(key, caseInsensitive) || IsBlockedEnvKey(key) {
			continue
		}
		if isProbeAllowedEnv(key, caseInsensitive) {
			selected[key] = val
		}
	}
	selected[EnvManifestRequest] = "1"
	selected[EnvProtocolVersion] = fmt.Sprintf("%d", ProtocolVersion)

	keys := slices.Sorted(maps.Keys(selected))
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+selected[k])
	}
	return out
}

// BuildExecEnv builds the filtered child environment for a plugin execution.
// Only base allowlisted variables, operator-approved AllowEnv matches,
// non-blocked env vars referenced by approved AllowEnvFlag arguments, and
// host-owned ProtocolVars are included.
func BuildExecEnv(opts EnvOptions) []string {
	return buildExecEnv(opts, runtime.GOOS == "windows")
}

func buildExecEnv(opts EnvOptions, caseInsensitive bool) []string {
	src := opts.Environ
	if src == nil {
		src = os.Environ()
	}

	flagVars := extractEnvVarsFromFlags(opts.Args, opts.AllowEnvFlag)

	selected := map[string]string{}
	for _, kv := range src {
		key, val, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			continue
		}
		if isProtocolEnvKey(key, caseInsensitive) {
			continue
		}
		flagMatch := flagVars[key] || (caseInsensitive && flagVars[strings.ToUpper(key)])
		if isBaseAllowedEnv(key, caseInsensitive) || matchesAnyPattern(opts.AllowEnv, key, caseInsensitive) || flagMatch {
			if !IsBlockedEnvKey(key) {
				selected[key] = val
			}
		}
	}

	for _, k := range slices.Sorted(maps.Keys(opts.ProtocolVars)) {
		if v := opts.ProtocolVars[k]; v != "" {
			selected[k] = v
		}
	}

	keys := slices.Sorted(maps.Keys(selected))
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+selected[k])
	}
	return out
}

func isProtocolEnvKey(key string, caseInsensitive bool) bool {
	if protocolEnvKeys[key] {
		return true
	}
	return caseInsensitive && protocolEnvKeys[strings.ToUpper(key)]
}

func isProbeAllowedEnv(key string, caseInsensitive bool) bool {
	if probeEnvAllowlist[key] {
		return true
	}
	if caseInsensitive {
		upper := strings.ToUpper(key)
		return probeEnvAllowlist[upper] || baseEnvAllowlistWindows[upper]
	}
	return false
}

func isBaseAllowedEnv(key string, caseInsensitive bool) bool {
	if baseEnvAllowlist[key] {
		return true
	}
	if caseInsensitive {
		upper := strings.ToUpper(key)
		return baseEnvAllowlist[upper] || baseEnvAllowlistWindows[upper]
	}
	return false
}

func matchesAnyPattern(patterns []string, key string, caseInsensitive bool) bool {
	for _, p := range patterns {
		if envPatternMatches(p, key, caseInsensitive) {
			return true
		}
	}
	return false
}

// extractEnvVarsFromFlags scans args before the first "--" for any approved
// flag name (in "--flag VALUE" or "--flag=VALUE" form) and returns the set of
// valid, non-blocked environment variable names referenced by those flags.
func extractEnvVarsFromFlags(args, allowedFlags []string) map[string]bool {
	if len(allowedFlags) == 0 || len(args) == 0 {
		return nil
	}
	if end := slices.Index(args, "--"); end >= 0 {
		args = args[:end]
	}
	allowed := make(map[string]bool, len(allowedFlags))
	for _, f := range allowedFlags {
		allowed[f] = true
	}
	result := map[string]bool{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var varName string
		if name, val, hasEq := strings.Cut(arg, "="); hasEq {
			if allowed[name] {
				varName = val
			}
		} else if allowed[arg] && i+1 < len(args) {
			i++
			varName = args[i]
		}
		if varName != "" && envExactRE.MatchString(varName) && !IsBlockedEnvKey(varName) && !protocolEnvKeys[varName] {
			result[varName] = true
		}
	}
	return result
}
