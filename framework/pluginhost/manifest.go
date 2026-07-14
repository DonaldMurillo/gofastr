package pluginhost

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

// Isolation constants. v1 has a single fixpoint: the plugin client runs in an
// opaque-origin sandboxed iframe (sandbox="allow-scripts" WITHOUT
// allow-same-origin), served same-origin. See protocol-v1.md §1.
const (
	IsolationSandboxOpaque = "sandbox-iframe-opaque"

	// DefaultSandbox is the v1 sandbox policy: scripts only. The same-origin
	// token is NEVER added — that would de-opaque the frame and collapse the
	// isolation guarantee.
	DefaultSandbox = "allow-scripts"
)

// Manifest is the declarative description of a plugin's client module. It is
// generalised from the wysiwyg plugin's Phase-0 manifest (protocol-v1.md §1/§5)
// and doubles as the JSON blob the generic host broker reads to build the
// sandboxed iframe for each mount marker.
//
// Fields are kept minimal and forward-compatible: unknown params on the wire are
// ignored by both sides (protocol-v1.md §3 envelope is frozen; method param
// payloads may grow).
type Manifest struct {
	// Entry is the frame document URL the broker loads into the iframe, e.g.
	// "/__gofastr/plugin/wysiwyg/editor.html". Required.
	Entry string `json:"entry"`

	// ScriptHash is an optional bundle hash used for cache-busting / SRI. v1
	// does not enforce it; the broker appends its own cache-buster.
	ScriptHash string `json:"scriptHash,omitempty"`

	// Isolation is the isolation model identifier. The v1 fixpoint is
	// [IsolationSandboxOpaque]. Empty defaults to it; any other value is
	// rejected by [Manifest.Validate].
	Isolation string `json:"isolation"`

	// Sandbox is the iframe sandbox token list. MUST contain "allow-scripts" and
	// MUST NOT contain "allow-same-origin" (enforced by [Manifest.Validate]).
	// Defaults to ["allow-scripts"] when empty.
	Sandbox []string `json:"sandbox"`

	// Capabilities is the default resource:verb grant set advertised to the
	// client in init.capabilities when the mount marker does not override it.
	Capabilities []string `json:"capabilities,omitempty"`

	// MinHeight is the initial iframe height before the first resize event.
	// Defaults to "240px" when empty.
	MinHeight string `json:"minHeight,omitempty"`

	// Schema is the interchange schema version bridged in init.schemaVersion
	// (e.g. "wysiwyg-v1").
	Schema string `json:"schema"`

	// Title is the iframe title attribute (accessibility). Defaults to
	// "Plugin" when empty.
	Title string `json:"title,omitempty"`
}

// Validate enforces the v1 isolation invariants, failing loudly at
// registration on a mis-configured manifest. It is called by
// [NewClientModule]. Note the frame's actual sandbox attribute is derived by
// [Manifest.SandboxString] / the broker's sandboxFor, both of which are
// authoritative (they strip allow-same-origin regardless), so Validate is a
// fail-fast nicety, not the sole line of defense. It does not mutate the
// receiver.
func (m Manifest) Validate() error {
	if m.Entry == "" {
		return errors.New("pluginhost: manifest entry is required")
	}
	if m.Isolation != "" && m.Isolation != IsolationSandboxOpaque {
		return fmt.Errorf("pluginhost: unsupported isolation %q (v1 supports only %q)",
			m.Isolation, IsolationSandboxOpaque)
	}
	// Normalise the same way the browser tokenises the attribute (lowercase,
	// whitespace-split) so a case/whitespace variant can't dodge the check.
	var norm []string
	for _, raw := range m.Sandbox {
		for _, token := range strings.Fields(strings.ToLower(raw)) {
			if token == "allow-same-origin" {
				return errors.New("pluginhost: sandbox \"allow-same-origin\" is forbidden — it breaks opaque-origin isolation (protocol-v1.md §1)")
			}
			norm = append(norm, token)
		}
	}
	// An empty sandbox is normalised to allow-scripts by the broker; but if the
	// caller specified tokens they MUST include allow-scripts or the frame can
	// never boot its JS.
	if len(norm) > 0 && !slices.Contains(norm, "allow-scripts") {
		return errors.New("pluginhost: sandbox must include \"allow-scripts\"")
	}
	return nil
}

// SandboxString returns the iframe `sandbox` attribute value. It is
// AUTHORITATIVE, not advisory: it always includes "allow-scripts" and always
// strips "allow-same-origin" (and any other same-origin-collapsing token),
// regardless of what the manifest carries. A mis-configured or tampered
// manifest therefore cannot produce a de-opaqued frame — the isolation
// invariant does not depend on anyone having called [Manifest.Validate].
func (m Manifest) SandboxString() string {
	return sanitizeSandboxTokens(m.Sandbox)
}

// sameOriginCollapsingTokens are iframe sandbox tokens that would give the
// framed document access back to the host's origin (DOM, cookies, storage),
// collapsing the opaque-origin isolation. They are stripped unconditionally.
var sameOriginCollapsingTokens = map[string]bool{
	"allow-same-origin": true,
}

// sanitizeSandboxTokens returns a normalised sandbox token string: the
// same-origin-collapsing tokens removed, "allow-scripts" guaranteed present,
// duplicates dropped, order preserved. Empty input yields [DefaultSandbox].
//
// The HTML `sandbox` attribute is ASCII-case-insensitive and whitespace-
// separated, so each input element is lowercased AND split on whitespace
// before filtering — otherwise "Allow-Same-Origin" (honoured as
// allow-same-origin by the browser) or a single element like
// "x allow-same-origin" (tokenised into two) would slip an effective
// same-origin grant past the filter.
func sanitizeSandboxTokens(tokens []string) string {
	seen := map[string]bool{}
	out := make([]string, 0, len(tokens)+1)
	for _, raw := range tokens {
		for _, tok := range strings.Fields(strings.ToLower(raw)) {
			if sameOriginCollapsingTokens[tok] || seen[tok] {
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

// ClientModule bundles a plugin name with its [Manifest] and the embedded
// asset filesystem the [AssetServer] serves. It is the unit a plugin registers
// with the platform. Worker G builds one of these per plugin.
type ClientModule struct {
	// Name is the plugin name, also the data-fui-plugin attribute value the
	// mount marker carries and the generic broker dispatches on.
	Name string

	// Manifest describes the client module.
	Manifest Manifest

	// Assets is the (sub)filesystem holding the framed client assets
	// (editor.html / editor.js / editor.css). May be nil if the plugin serves
	// its assets itself.
	Assets fs.FS
}

// NewClientModule is the validating constructor for a [ClientModule]: it runs
// [Manifest.Validate] so a mis-configured plugin fails loudly at registration
// instead of silently mounting a bad frame. Plugins should build their module
// through this rather than a struct literal.
func NewClientModule(name string, m Manifest, assets fs.FS) (ClientModule, error) {
	if name == "" {
		return ClientModule{}, errors.New("pluginhost: client module name is required")
	}
	if err := m.Validate(); err != nil {
		return ClientModule{}, err
	}
	return ClientModule{Name: name, Manifest: m, Assets: assets}, nil
}
