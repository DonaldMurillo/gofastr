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
	// token is NEVER added, that would de-opaque the frame and collapse the
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

	// CSP lists opt-in Content-Security-Policy keywords appended to the framed
	// policy's script-src. Declaring it alone changes nothing: the host that
	// builds the plugin's [AssetServer] must pass it through
	// [AssetServer.WithCSP], or the manifest validates, the frame still
	// refuses WebAssembly, and nothing reports why. The allowlist
	// is closed and has exactly one member: 'wasm-unsafe-eval', which lets a
	// plugin compile WebAssembly inside the sandboxed frame without granting
	// string eval ('unsafe-eval' stays forbidden) and without touching any
	// other directive — the frame keeps its opaque origin, sandbox
	// allow-scripts, and connect-src 'none', so a wasm engine still exchanges
	// data only over the postMessage bridge. Anything outside the allowlist
	// (a host source, 'unsafe-inline', 'unsafe-eval', '*', or a token
	// carrying ';', whitespace, or mismatched quotes — these values are
	// interpolated into a response header, where ';' could splice an
	// arbitrary directive such as re-enabled connect-src) is rejected by
	// [Manifest.Validate] and dropped at header assembly. Matching is EXACT,
	// byte-for-byte: unlike the HTML sandbox attribute the CSP header neither
	// case-folds nor whitespace-tokenises source expressions, so a variant
	// like 'WASM-UNSAFE-EVAL' grants nothing — and exact match rejects every
	// smuggle shape with the one comparison.
	CSP []string `json:"csp,omitempty"`

	// Capabilities is the default resource:verb grant set advertised to the
	// client in init.capabilities when the mount marker does not override it.
	Capabilities []string `json:"capabilities,omitempty"`

	// HostRequirements names browser features the HOST PAGE around the plugin
	// must be allowed to use, as "permissions-policy:<feature>" tokens (e.g.
	// "permissions-policy:camera" for a scanner whose host page captures and
	// whose sandboxed frame decodes). The frame itself is opaque-origin and
	// can never hold these permissions; this declares what the page embedding
	// it needs, so [CheckHostRequirements] can turn an unsatisfied token into
	// a boot-time warning instead of the runtime console error a user would
	// otherwise hit first. Validated against a closed feature registry.
	HostRequirements []string `json:"hostRequirements,omitempty"`

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
	if err := validateEntry(m.Entry); err != nil {
		return err
	}
	if m.Isolation != "" && m.Isolation != IsolationSandboxOpaque {
		return fmt.Errorf("pluginhost: unsupported isolation %q (v1 supports only %q)",
			m.Isolation, IsolationSandboxOpaque)
	}
	// Normalise the same way the browser tokenises the attribute (lowercase,
	// whitespace-split) so a case/whitespace variant can't dodge the check.
	var norm []string
	for _, raw := range m.Sandbox {
		for token := range strings.FieldsSeq(strings.ToLower(raw)) {
			if token == "allow-same-origin" {
				return errors.New("pluginhost: sandbox \"allow-same-origin\" is forbidden: it breaks opaque-origin isolation (protocol-v1.md §1)")
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
	// CSP keywords are matched EXACTLY (byte-for-byte) against a closed
	// allowlist. The sandbox check above normalises case and whitespace
	// because the HTML sandbox attribute — its sink — tokenises that way, so
	// a variant there is a live grant. The CSP header does not normalise
	// source expressions: a case/whitespace variant is an unrecognised,
	// inert source, not an evasion. Exact match is also what rejects ';'
	// (directive splicing into the response header), embedded whitespace,
	// and quote mismatches outright: none of those shapes equals the
	// allowlisted keyword.
	for _, kw := range m.CSP {
		if !allowedCSPKeywords[kw] {
			return fmt.Errorf("pluginhost: manifest csp token %q is not in the allowlist (only 'wasm-unsafe-eval' is permitted)", kw)
		}
	}
	// Host-page requirements: a closed grammar, rejected loudly at
	// registration so a typo'd or invented feature never becomes a silently
	// unsatisfiable requirement (see allowedPolicyFeatures).
	for _, token := range m.HostRequirements {
		if err := validateHostRequirement(token); err != nil {
			return err
		}
	}
	return nil
}

// SandboxString returns the iframe `sandbox` attribute value. It is
// AUTHORITATIVE, not advisory: it always includes "allow-scripts" and always
// strips "allow-same-origin" (and any other same-origin-collapsing token),
// regardless of what the manifest carries. A mis-configured or tampered
// manifest therefore cannot produce a de-opaqued frame, the isolation
// invariant does not depend on anyone having called [Manifest.Validate].
func (m Manifest) SandboxString() string {
	return sanitizeSandboxTokens(m.Sandbox)
}

// validateEntry requires the frame document URL to be a same-origin absolute
// path.
//
// Entry ships with the third-party plugin, so it is attacker-influenced by
// construction. The opaque-origin guarantee has two carriers: the iframe
// sandbox attribute, and the `Content-Security-Policy: sandbox allow-scripts`
// header [AssetServer] emits for the assets IT serves. A cross-origin or
// scheme-bearing Entry escapes the second one entirely, nothing the host
// controls is emitting headers for someone else's document.
//
// Rejected: any scheme (`https:`, `javascript:`, `data:`), protocol-relative
// `//host/…`, the backslash forms browsers normalise to an authority
// (`\\host\…`, `/\host/…`), relative paths (they resolve against whatever page
// mounted the plugin, which is not knowable here), and control characters
// (header/attribute smuggling).
func validateEntry(entry string) error {
	for _, r := range entry {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("pluginhost: manifest entry %q contains a control character", entry)
		}
	}
	// Normalise backslashes the way a browser does before deciding what the
	// authority component is: "/\evil.example/x" is //evil.example/x.
	norm := strings.ReplaceAll(entry, "\\", "/")
	if !strings.HasPrefix(norm, "/") {
		return fmt.Errorf("pluginhost: manifest entry %q must be a same-origin absolute path beginning with \"/\" (a scheme or relative path escapes the host origin, and with it the framed-CSP sandbox header)", entry)
	}
	if strings.HasPrefix(norm, "//") {
		return fmt.Errorf("pluginhost: manifest entry %q is protocol-relative, which resolves to another origin", entry)
	}
	return nil
}

// allowedSandboxTokens is the set of iframe sandbox capabilities a plugin
// frame may be granted.
//
// An allow-list, because the deny-list shape has to enumerate every way
// out of the box and loses the moment the HTML spec adds one. It already
// had: stripping only allow-same-origin left
// `allow-popups-to-escape-sandbox` (a popup the plugin opens is fully
// unsandboxed AND same-origin, window.open('/admin/...') is then an
// ordinary cookie-bearing document), `allow-top-navigation` (retarget the
// whole tab) and `allow-downloads` (write to the user's disk) all
// passing. The manifest ships with the third-party plugin, so it is
// attacker-influenced by construction, this is not a wrong-layer check.
//
// What is here is what a UI plugin actually needs to render and interact.
// Adding to this list is a deliberate act; drifting into it is not
// possible.
var allowedSandboxTokens = map[string]bool{
	"allow-scripts":          true,
	"allow-forms":            true,
	"allow-modals":           true,
	"allow-popups":           true, // a popup, still sandboxed. See the escape token below
	"allow-pointer-lock":     true,
	"allow-orientation-lock": true,
	"allow-presentation":     true,
}

// sanitizeSandboxTokens returns a normalised sandbox token string:
// anything outside [allowedSandboxTokens] removed, "allow-scripts"
// guaranteed present, duplicates dropped, order preserved. Empty input
// yields [DefaultSandbox].
//
// The HTML `sandbox` attribute is ASCII-case-insensitive and whitespace-
// separated, so each input element is lowercased AND split on whitespace
// before filtering, otherwise "Allow-Same-Origin" (honoured as
// allow-same-origin by the browser) or a single element like
// "x allow-same-origin" (tokenised into two) would slip an effective
// same-origin grant past the filter.
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

// allowedCSPKeywords is the closed set of Content-Security-Policy source
// keywords a manifest may append to the framed policy's script-src.
//
// Exactly one member. 'wasm-unsafe-eval' permits WebAssembly compilation
// within script-src and nothing else: not string eval ('unsafe-eval'), not
// inline markup ('unsafe-inline'), not host sources, not wildcards. A plugin
// built on a wasm engine (a SQL notebook, a barcode scanner, an ONNX
// classifier) needs exactly this and no more — data still arrives over the
// postMessage bridge and leaves the same way, because connect-src stays
// 'none' and the frame keeps its opaque origin.
//
// Like [allowedSandboxTokens] this is an allow-list because the manifest
// ships with the third-party plugin and is attacker-influenced by
// construction. The tokens are interpolated into a response header, so
// anything carrying ';', whitespace, or mismatched quotes could splice a new
// directive (re-enabling connect-src, the exfiltration guard); exact
// matching against this set rejects all of those shapes. Adding a member is
// a deliberate act with a security review behind it; drifting into it is not
// possible.
var allowedCSPKeywords = map[string]bool{
	"'wasm-unsafe-eval'": true,
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
