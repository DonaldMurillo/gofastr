package pluginhost

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

// The v1 isolation model and its default sandbox.
const (
	IsolationSandboxOpaque = "sandbox-iframe-opaque"

	DefaultSandbox = "allow-scripts"
)

// Manifest describes a plugin's client module; the broker reads it as JSON.
type Manifest struct {
	// Entry is the frame document's same-origin absolute path. Required.
	Entry string `json:"entry"`

	// ScriptHash is an optional bundle hash, not enforced.
	ScriptHash string `json:"scriptHash,omitempty"`

	// Isolation is the isolation model; empty or [IsolationSandboxOpaque].
	Isolation string `json:"isolation"`

	// Sandbox is the iframe sandbox token list; empty means [DefaultSandbox].
	Sandbox []string `json:"sandbox"`

	// CSP holds framed script-src keywords; see [ClientModule.AssetServer].
	CSP []string `json:"csp,omitempty"`

	// Capabilities is the default resource:verb grant set.
	Capabilities []string `json:"capabilities,omitempty"`

	// HostRequirements are features the host page needs; see [HostRequirementPrefix].
	HostRequirements []string `json:"hostRequirements,omitempty"`

	// MinHeight is the initial iframe height; empty means "240px".
	MinHeight string `json:"minHeight,omitempty"`

	// Schema is the interchange schema version, such as "wysiwyg-v1".
	Schema string `json:"schema"`

	// Title is the iframe title; empty means "Plugin".
	Title string `json:"title,omitempty"`
}

// Validate reports the first v1 isolation rule the manifest breaks.
func (m Manifest) Validate() error {
	if m.Entry == "" {
		return errors.New("pluginhost: manifest entry is required")
	}
	if err := validateEntry(m.Entry); err != nil {
		return err
	}
	if m.Isolation != "" && m.Isolation != IsolationSandboxOpaque {
		return fmt.Errorf("pluginhost: unsupported isolation %q (v1 supports only %q)",
			m.Isolation, IsolationSandboxOpaque)
	}
	var norm []string
	for _, raw := range m.Sandbox {
		for token := range strings.FieldsSeq(strings.ToLower(raw)) {
			if token == "allow-same-origin" {
				return errors.New("pluginhost: sandbox \"allow-same-origin\" is forbidden: it breaks opaque-origin isolation (protocol-v1.md §1)")
			}
			norm = append(norm, token)
		}
	}
	if len(norm) > 0 && !slices.Contains(norm, "allow-scripts") {
		return errors.New("pluginhost: sandbox must include \"allow-scripts\"")
	}
	for _, kw := range m.CSP {
		if !allowedCSPKeywords[kw] {
			return fmt.Errorf("pluginhost: manifest csp token %q is not in the allowlist (only 'wasm-unsafe-eval' is permitted)", kw)
		}
	}
	for _, token := range m.HostRequirements {
		if err := validateHostRequirement(token); err != nil {
			return err
		}
	}
	return nil
}

// SandboxString returns the sanitised iframe sandbox attribute.
func (m Manifest) SandboxString() string {
	return sanitizeSandboxTokens(m.Sandbox)
}

func validateEntry(entry string) error {
	for _, r := range entry {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("pluginhost: manifest entry %q contains a control character", entry)
		}
	}
	norm := strings.ReplaceAll(entry, "\\", "/")
	if !strings.HasPrefix(norm, "/") {
		return fmt.Errorf("pluginhost: manifest entry %q must be a same-origin absolute path beginning with \"/\" (a scheme or relative path escapes the host origin, and with it the framed-CSP sandbox header)", entry)
	}
	if strings.HasPrefix(norm, "//") {
		return fmt.Errorf("pluginhost: manifest entry %q is protocol-relative, which resolves to another origin", entry)
	}
	return nil
}

var allowedSandboxTokens = map[string]bool{
	"allow-scripts":          true,
	"allow-forms":            true,
	"allow-modals":           true,
	"allow-popups":           true,
	"allow-pointer-lock":     true,
	"allow-orientation-lock": true,
	"allow-presentation":     true,
}

func sanitizeSandboxTokens(tokens []string) string {
	seen := map[string]bool{}
	out := make([]string, 0, len(tokens)+1)
	for _, raw := range tokens {
		for tok := range strings.FieldsSeq(strings.ToLower(raw)) {
			if !allowedSandboxTokens[tok] || seen[tok] {
				continue
			}
			seen[tok] = true
			out = append(out, tok)
		}
	}
	if !seen["allow-scripts"] {
		out = append([]string{"allow-scripts"}, out...)
	}
	return strings.Join(out, " ")
}

var allowedCSPKeywords = map[string]bool{
	"'wasm-unsafe-eval'": true,
}

// ClientModule is a plugin name, its [Manifest] and its asset filesystem.
type ClientModule struct {
	// Name is the plugin name, the data-fui-plugin value.
	Name string

	// Manifest describes the client module.
	Manifest Manifest

	// Assets may be nil when the plugin serves its assets itself.
	Assets fs.FS
}

// AssetServer builds the module's [AssetServer] with the manifest's CSP.
func (m ClientModule) AssetServer(prefix string, specs []AssetSpec) *AssetServer {
	return NewAssetServer(m.Assets, prefix, specs).WithCSP(m.Manifest.CSP)
}

// NewClientModule validates name and m and returns the module.
func NewClientModule(name string, m Manifest, assets fs.FS) (ClientModule, error) {
	if name == "" {
		return ClientModule{}, errors.New("pluginhost: client module name is required")
	}
	if err := m.Validate(); err != nil {
		return ClientModule{}, err
	}
	return ClientModule{Name: name, Manifest: m, Assets: assets}, nil
}
