package framework

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// TrustTier selects the runner a process module is launched under (design §6,
// decision C). It comes from the operator-approved descriptor, never a child
// self-claim.
type TrustTier int

const (
	// TrustUntrusted requires a probe-passing SandboxRunner (design §6
	// decision C). At Register, [SelectRunner] maps it to the
	// supervisor's configured *SandboxRunner iff that runner's backend
	// passes the P1–P7 conformance probe on this host; otherwise
	// Register fails with [UntrustedNoSandboxError] and the module
	// never reaches Ready (fail-closed — never a silent downgrade to
	// TrustedProcessRunner).
	TrustUntrusted TrustTier = iota

	// TrustTrusted runs under TrustedProcessRunner: crash isolation only,
	// baseline hygiene. First-party / dev; never auto-selected for
	// untrusted.
	TrustTrusted
)

// String makes TrustTier log-friendly and operator-readable.
func (t TrustTier) String() string {
	switch t {
	case TrustUntrusted:
		return "untrusted"
	case TrustTrusted:
		return "trusted"
	default:
		return fmt.Sprintf("trust-tier(%d)", int(t))
	}
}

// RouteDeclaration is one operator-approved HTTP route a process module
// exposes. The descriptor is the source of truth (design §3 decision B); the
// child cannot add, rename, or reshape routes at runtime — the handshake
// cross-checks surface_sha256, and the host proxies only RouteIDs present
// here.
type RouteDeclaration struct {
	// ID is the stable, descriptor-local route identifier the host places
	// in [moduleproto.HTTPRequestParams.RouteID]. It lets the child
	// dispatch by an opaque key without re-deriving method+path. IDs must
	// be unique within a descriptor and match [idPattern].
	ID string

	// Method is the HTTP method (GET, POST, …). The host marshals it into
	// [moduleproto.HTTPRequestParams.Method].
	Method string

	// Path is the route pattern (e.g. "/items/:id"). The host parses path
	// parameters and forwards them in
	// [moduleproto.HTTPRequestParams.PathParams]. Path must be non-empty
	// and begin with "/".
	Path string
}

// ToolDigest is the digest of one MCP tool the module may expose (design §5.1,
// optional surface). The host registers tools from the descriptor and
// byte-compares against module.tool.list at handshake; the child cannot add,
// rename, or reshape at runtime.
type ToolDigest struct {
	// ID is the tool's descriptor-local identifier, namespaced under
	// "module.<name>." when registered into the host's MCP server.
	ID string

	// SHA256 is the hex-encoded SHA-256 of the tool's canonical JSON
	// (id+name+description+input_schema). Mismatch with module.tool.list
	// quarantines the module (terminal Failed this wave).
	SHA256 string
}

// ModuleLimits is the per-child resource envelope an operator may narrow from
// the v1 defaults (design §4.4 / §8). The descriptor may LOWER a ceiling but
// never RAISE it — a value of 0 means "host default applies"; a non-zero
// value greater than the host default is rejected at install time.
type ModuleLimits struct {
	// Deadline is the per-call ceiling for module.* requests. Default 10s;
	// the descriptor may lower it.
	Deadline time.Duration

	// FrameBytes is the negotiated max_frame_bytes. Default 1 MiB; the
	// descriptor may lower it. Must be ≤ [moduleproto.scannerMaxCap].
	FrameBytes int

	// Inflight is the maximum simultaneously in-flight module.* requests
	// the host will issue. Default 32; the descriptor may lower it.
	Inflight int
}

// maxModuleCallDeadline is the v1 host ceiling on per-call deadlines. A
// descriptor value above this is rejected at install — modules cannot widen
// the host's call budget (design §8).
const maxModuleCallDeadline = 10 * time.Second

// ProcessModuleDescriptor is the content-addressed, operator-approved
// description of a process-isolated third-party module (design §3 decision B).
// Every field here is authoritative at runtime: the child's handshake only
// *cross-checks* digests, never supplies values. A mismatch on digest /
// identity / extra grant is terminal Failed (no restart loop).
type ProcessModuleDescriptor struct {
	// Name is the module's operator-approved unique name. Must match
	// [moduleIdentPattern]; same namespace as in-process modules.
	Name string

	// Version is the operator-approved semantic version (informational;
	// also round-tripped at handshake).
	Version string

	// ArtifactPath is the filesystem path to the approved executable. The
	// runner verifies the executable's SHA-256 equals ArtifactSHA256
	// BEFORE exec (verify-then-exec, design §4.6).
	ArtifactPath string

	// ArtifactSHA256 is the hex-encoded SHA-256 of the executable at
	// ArtifactPath. Non-empty hex; verified before exec and never re-read
	// from the child.
	ArtifactSHA256 string

	// SurfaceSHA256 is the hex-encoded SHA-256 of the canonical surface
	// descriptor (routes + tool list + requested permissions). The child's
	// handshake result echoes this; mismatch is terminal.
	SurfaceSHA256 string

	// Routes is the declared HTTP surface, host-registered behind the
	// existing module route gate (Enabled → 404, indistinguishable from
	// uninstalled).
	Routes []RouteDeclaration

	// Tools is the optional MCP tool surface (design §5.1). Empty means
	// the module exposes no tools.
	Tools []ToolDigest

	// RequestedGrants is the verbatim resource:verb list the operator
	// reviews at install (design §5). Effective grants =
	// RequestedGrants ∩ ApprovedGrants, with the non-grantable carve-out
	// applied. Capped at 32 entries.
	RequestedGrants []access.Permission

	// TrustTier selects the runner. TrustUntrusted requires a probe-
	// passing *SandboxRunner (else Register fails closed).
	TrustTier TrustTier

	// MigrationGroup is the #33 migration group name this module owns
	// (informational this wave; the migration coordinator lands later).
	MigrationGroup string

	// Limits narrows the host defaults. Zero values mean "default applies".
	Limits ModuleLimits
}

// ApprovedGrants is the operator-approved subset of a descriptor's
// RequestedGrants. It is a parameter to [ValidateProcessModuleDescriptor], not
// a UI — this wave treats approval as an explicit set the caller supplies
// (design §5: "approval is a parameter, not a UI").
type ApprovedGrants []access.Permission

// DescriptorValidationError is returned by
// [ValidateProcessModuleDescriptor] when a descriptor fails install-time
// validation. The error names the FIRST failing field/check so an operator
// sees an actionable cause; the [Field] and [Rule] let a UI map it to a form.
type DescriptorValidationError struct {
	Field string // descriptor field or "grants[N]"
	Rule  string // short rule id, e.g. "empty", "non_hex", "non_grantable"
	msg   string
}

func (e *DescriptorValidationError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return e.msg
}

// descErr is a small constructor so call sites stay one-line.
func descErr(field, rule, msg string) *DescriptorValidationError {
	return &DescriptorValidationError{Field: field, Rule: rule, msg: msg}
}

// moduleIdentPattern is the legal module-name class (mirrors the in-process
// module convention: letters, digits, underscore, hyphen).
const moduleIdentPattern = "^[A-Za-z][A-Za-z0-9_-]*$"

// idPattern is the legal RouteDeclaration.ID / ToolDigest.ID class: letters,
// digits, underscore, hyphen — no slashes/colons so it survives a URL or a
// method-qualified tool name unchanged.
const idPattern = "^[A-Za-z][A-Za-z0-9_-]*$"

// maxModuleGrants caps the per-module grant set at 32 (design §5, mirroring
// the token-scope cap of battery/auth's scopePattern).
const maxModuleGrants = 32

// hexSHA256Pattern is a non-canonical check that a string is 64 hex chars.
// We use encoding/hex.DecodeString for the rigorous check and this only as
// a fast pre-filter / message formatter.
const hexSHA256Len = 64

// ValidateProcessModuleDescriptor is the pure install-time validation for a
// process module descriptor (design §3 decision B + §5). It checks:
//
//   - name/version present and well-formed;
//   - artifact path present; ArtifactSHA256 + SurfaceSHA256 are non-empty
//     hex SHA-256;
//   - route IDs unique and well-formed; methods upper-case HTTP verbs; paths
//     begin with "/";
//   - tool IDs unique and digests are hex SHA-256;
//   - every requested grant passes [access.ValidScope] and the non-grantable
//     carve-out (CrossOwnerRead + broad wildcards); ≤32 grants;
//   - effective grants = requested ∩ approved, computed and returned;
//   - limits do not RAISE any host default (deadline ≤ 10s, frame bytes ≤
//     scanner cap, inflight ≤ host default);
//   - migration group, if set, matches [moduleIdentPattern].
//
// It returns the computed effective-grant set on success. The descriptor is
// NOT mutated.
func ValidateProcessModuleDescriptor(d ProcessModuleDescriptor, approved ApprovedGrants) ([]access.Permission, error) {
	// --- identity ---
	if d.Name == "" {
		return nil, descErr("name", "empty", "descriptor: name is required")
	}
	if !matchIdent(d.Name) {
		return nil, descErr("name", "pattern", fmt.Sprintf("descriptor: name %q must match %s", d.Name, moduleIdentPattern))
	}
	if d.Version == "" {
		return nil, descErr("version", "empty", "descriptor: version is required")
	}

	// --- artifact + digests ---
	if d.ArtifactPath == "" {
		return nil, descErr("artifact_path", "empty", "descriptor: artifact_path is required")
	}
	if err := validateHexSHA256("artifact_sha256", d.ArtifactSHA256); err != nil {
		return nil, err
	}
	if err := validateHexSHA256("surface_sha256", d.SurfaceSHA256); err != nil {
		return nil, err
	}

	// --- routes ---
	seenRoute := make(map[string]string, len(d.Routes)) // id → method+path (dedup msg)
	for i, r := range d.Routes {
		prefix := fmt.Sprintf("routes[%d]", i)
		if !matchID(r.ID) {
			return nil, descErr(prefix+".id", "pattern", fmt.Sprintf("descriptor: route id %q must match %s", r.ID, idPattern))
		}
		if _, dup := seenRoute[r.ID]; dup {
			return nil, descErr(prefix+".id", "duplicate", fmt.Sprintf("descriptor: duplicate route id %q", r.ID))
		}
		seenRoute[r.ID] = r.Method + " " + r.Path
		if r.Method == "" || strings.ToUpper(r.Method) != r.Method {
			return nil, descErr(prefix+".method", "pattern", fmt.Sprintf("descriptor: route %q method %q must be an upper-case HTTP verb", r.ID, r.Method))
		}
		if r.Path == "" || !strings.HasPrefix(r.Path, "/") {
			return nil, descErr(prefix+".path", "pattern", fmt.Sprintf("descriptor: route %q path %q must begin with '/'", r.ID, r.Path))
		}
	}

	// --- tools ---
	seenTool := make(map[string]struct{}, len(d.Tools))
	for i, t := range d.Tools {
		prefix := fmt.Sprintf("tools[%d]", i)
		if !matchID(t.ID) {
			return nil, descErr(prefix+".id", "pattern", fmt.Sprintf("descriptor: tool id %q must match %s", t.ID, idPattern))
		}
		if _, dup := seenTool[t.ID]; dup {
			return nil, descErr(prefix+".id", "duplicate", fmt.Sprintf("descriptor: duplicate tool id %q", t.ID))
		}
		seenTool[t.ID] = struct{}{}
		if err := validateHexSHA256(prefix+".sha256", t.SHA256); err != nil {
			return nil, err
		}
	}

	// --- grants: scope validity + carve-out + cap ---
	if len(d.RequestedGrants) > maxModuleGrants {
		return nil, descErr("requested_grants", "cap", fmt.Sprintf("descriptor: %d requested grants exceeds cap %d", len(d.RequestedGrants), maxModuleGrants))
	}
	for i, g := range d.RequestedGrants {
		prefix := fmt.Sprintf("requested_grants[%d]", i)
		gs := string(g)
		if gs == "" {
			return nil, descErr(prefix, "empty", "descriptor: empty grant")
		}
		// Carve-out FIRST (design §5): the non-grantable check must run
		// before ValidScope because the carve-out's named tokens
		// ("CrossOwnerRead", bare "*") are not resource:verb scopes —
		// ValidScope would reject them as "scope" instead of naming the
		// security-critical reason. Belt-and-suspenders on the broker
		// path is the second layer.
		if reason := nonGrantableReason(g); reason != "" {
			return nil, descErr(prefix, "non_grantable", fmt.Sprintf("descriptor: grant %q is %s (design §5 carve-out: a module cannot escape owner/tenant scoping)", gs, reason))
		}
		if !access.ValidScope(gs) {
			return nil, descErr(prefix, "scope", fmt.Sprintf("descriptor: grant %q is not a valid resource:verb scope", gs))
		}
	}

	// --- effective grants: requested ∩ approved ---
	effective := intersectGrants(d.RequestedGrants, approved)

	// --- limits: descriptor may LOWER, never RAISE ---
	if d.Limits.Deadline < 0 {
		return nil, descErr("limits.deadline", "negative", "descriptor: limits.deadline must be >= 0")
	}
	if d.Limits.Deadline > maxModuleCallDeadline {
		return nil, descErr("limits.deadline", "ceiling", fmt.Sprintf("descriptor: limits.deadline %s exceeds host ceiling %s", d.Limits.Deadline, maxModuleCallDeadline))
	}
	if d.Limits.FrameBytes < 0 {
		return nil, descErr("limits.frame_bytes", "negative", "descriptor: limits.frame_bytes must be >= 0")
	}
	if d.Limits.FrameBytes > moduleproto.DefaultMaxFrameBytes {
		return nil, descErr("limits.frame_bytes", "ceiling", fmt.Sprintf("descriptor: limits.frame_bytes %d exceeds host default %d", d.Limits.FrameBytes, moduleproto.DefaultMaxFrameBytes))
	}
	if d.Limits.Inflight < 0 {
		return nil, descErr("limits.inflight", "negative", "descriptor: limits.inflight must be >= 0")
	}
	if d.Limits.Inflight > moduleproto.DefaultMaxInflight {
		return nil, descErr("limits.inflight", "ceiling", fmt.Sprintf("descriptor: limits.inflight %d exceeds host default %d", d.Limits.Inflight, moduleproto.DefaultMaxInflight))
	}

	// --- migration group ---
	if d.MigrationGroup != "" && !matchIdent(d.MigrationGroup) {
		return nil, descErr("migration_group", "pattern", fmt.Sprintf("descriptor: migration_group %q must match %s", d.MigrationGroup, moduleIdentPattern))
	}

	return effective, nil
}

// validateHexSHA256 checks s is 64 hex chars (a SHA-256 digest).
func validateHexSHA256(field, s string) error {
	if s == "" {
		return descErr(field, "empty", "descriptor: "+field+" is required")
	}
	if len(s) != hexSHA256Len {
		return descErr(field, "length", fmt.Sprintf("descriptor: %s must be a 64-char hex SHA-256 (got %d)", field, len(s)))
	}
	if _, err := hex.DecodeString(s); err != nil {
		return descErr(field, "hex", fmt.Sprintf("descriptor: %s must be hex: %v", field, err))
	}
	return nil
}

// matchIdent reports whether s is a legal module / migration-group name.
func matchIdent(s string) bool {
	if s == "" || !isAlpha(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !(isAlpha(c) || isDigit(c) || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// matchID reports whether s is a legal route/tool identifier.
func matchID(s string) bool { return matchIdent(s) }

func isAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// nonGrantableReason reports why p is in the §5 non-grantable carve-out, or ""
// if it is grantable. The carve-out is necessarily heuristic because no closed
// permission catalog exists (#5); the rules below are the concrete
// enforcement of "CrossOwnerRead, any cross-tenant scope, and any wildcard
// broad enough to subsume them" (design §5).
//
// Concretely:
//   - the literal "CrossOwnerRead" (the design's named carve-out marker);
//   - bare "*" (also fails [access.ValidScope], but defense-in-depth);
//   - "*:*" (subsumes every scope);
//   - "*:read" (subsumes any read, which includes any cross-owner read verb).
//
// Resource-scoped wildcards ("articles:*") are GRANTABLE: they cannot subsume
// a cross-owner verb because cross-owner verbs are namespaced per resource
// (e.g. "tickets:read:all") and do not collide with a same-resource wildcard.
func nonGrantableReason(p access.Permission) string {
	s := string(p)
	if s == "CrossOwnerRead" {
		return "the CrossOwnerRead carve-out"
	}
	if s == "*" {
		return "a bare wildcard (subsumes every scope)"
	}
	res, verb, ok := splitResourceVerb(s)
	if !ok {
		return "" // malformed; ValidScope rejects separately
	}
	if res == "*" && (verb == "*" || verb == "read") {
		return "a wildcard broad enough to subsume cross-owner reads"
	}
	return ""
}

// splitResourceVerb splits "resource:verb" at the first colon. Local copy so
// this file does not depend on an unexported helper in framework/access.
func splitResourceVerb(s string) (resource, verb string, ok bool) {
	idx := strings.IndexByte(s, ':')
	if idx <= 0 || idx == len(s)-1 {
		return "", "", false
	}
	return s[:idx], s[idx+1:], true
}

// intersectGrants returns the ordered intersection of requested and approved,
// preserving requested order, with duplicates dropped. Membership in approved
// uses [access.ScopeMatch] so an approved "articles:*" satisfies a requested
// "articles:read" (the operator approves a superset).
func intersectGrants(requested []access.Permission, approved ApprovedGrants) []access.Permission {
	if len(requested) == 0 || len(approved) == 0 {
		return nil
	}
	out := make([]access.Permission, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, r := range requested {
		if _, dup := seen[string(r)]; dup {
			continue
		}
		if access.ScopeMatch(approved, r) {
			seen[string(r)] = struct{}{}
			out = append(out, r)
		}
	}
	return out
}

// ComputeSurfaceSHA256 is the canonicalizer the install path uses to mint a
// [ProcessModuleDescriptor.SurfaceSHA256] from the surface fields (design §3
// decision B). The exact bytes are part of the contract: the child must
// produce the SAME digest from the same surface bytes at handshake. The
// canonical form is the JSON encoding of surfaceCanonical below, sorted for
// determinism.
//
// This is exported so the install tool (a later wave) and tests can mint a
// descriptor's surface digest from the same source the validator reads.
func ComputeSurfaceSHA256(d ProcessModuleDescriptor) (string, error) {
	canon, err := surfaceCanonical(d)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:]), nil
}

// surfaceCanonical is the deterministic JSON encoding of the surface fields
// the handshake cross-checks. Keys are sorted; route/tool arrays preserve
// descriptor order (the descriptor is the source of truth).
func surfaceCanonical(d ProcessModuleDescriptor) ([]byte, error) {
	type routeJSON struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		Path   string `json:"path"`
	}
	type toolJSON struct {
		ID     string `json:"id"`
		SHA256 string `json:"sha256"`
	}
	type canon struct {
		Name            string      `json:"name"`
		Version         string      `json:"version"`
		Routes          []routeJSON `json:"routes"`
		Tools           []toolJSON  `json:"tools,omitempty"`
		RequestedGrants []string    `json:"requested_grants"`
		TrustTier       string      `json:"trust_tier"`
		MigrationGroup  string      `json:"migration_group,omitempty"`
	}
	c := canon{
		Name:            d.Name,
		Version:         d.Version,
		TrustTier:       d.TrustTier.String(),
		MigrationGroup:  d.MigrationGroup,
		RequestedGrants: make([]string, len(d.RequestedGrants)),
	}
	for i, g := range d.RequestedGrants {
		c.RequestedGrants[i] = string(g)
	}
	c.Routes = make([]routeJSON, len(d.Routes))
	for i, r := range d.Routes {
		c.Routes[i] = routeJSON{ID: r.ID, Method: r.Method, Path: r.Path}
	}
	if len(d.Tools) > 0 {
		c.Tools = make([]toolJSON, len(d.Tools))
		for i, t := range d.Tools {
			c.Tools[i] = toolJSON{ID: t.ID, SHA256: t.SHA256}
		}
	}
	// json.Marshal on a struct preserves field order, which is what we want
	// (the struct field order IS the canonical order). Sort is not needed.
	return json.Marshal(c)
}

// Is lets callers errors.Is against DescriptorValidationError if they wrap it.
func (e *DescriptorValidationError) Is(target error) bool {
	var dv *DescriptorValidationError
	if errors.As(target, &dv) {
		return e.Field == dv.Field && e.Rule == dv.Rule
	}
	return false
}
