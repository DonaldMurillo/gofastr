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

// TrustTier selects the runner a process module is launched under. It comes from
// the operator-approved descriptor, never from the child.
type TrustTier int

const (
	// TrustUntrusted runs only under a SandboxRunner whose backend passed every
	// conformance probe; Register fails with [UntrustedNoSandboxError] otherwise.
	TrustUntrusted TrustTier = iota

	// TrustTrusted runs under [TrustedProcessRunner]: crash isolation, no confinement.
	TrustTrusted
)

// String returns "untrusted", "trusted" or trust-tier(N).
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

// RouteDeclaration is one operator-approved HTTP route a process module exposes.
type RouteDeclaration struct {
	// ID is the descriptor-local route id the host sends as
	// [moduleproto.HTTPRequestParams.RouteID].
	ID     string
	Method string

	// Path is the route pattern, such as "/items/:id"; it begins with one "/".
	Path string
}

// ToolDigest pins one MCP tool the module may expose.
type ToolDigest struct {
	ID string

	// SHA256 is the hex SHA-256 of the tool's canonical JSON ([ModuleToolDigest]).
	SHA256 string
}

// ModuleLimits narrows the host's per-child limits. Zero takes the host default;
// a value above it is refused at validation.
type ModuleLimits struct {
	// Deadline is the per-call ceiling for module.* requests (host default 10s).
	Deadline time.Duration

	// FrameBytes is the max frame size (host default [moduleproto.DefaultMaxFrameBytes]).
	FrameBytes int

	// Inflight caps concurrent module.* requests (host default [moduleproto.DefaultMaxInflight]).
	Inflight int
}

const maxModuleCallDeadline = 10 * time.Second

// ProcessModuleDescriptor is the operator-approved, content-addressed description
// of a process module. The child's handshake only cross-checks its digests.
type ProcessModuleDescriptor struct {
	// Name is the module's unique name, a letter followed by letters, digits, _ or -.
	Name    string
	Version string

	// ArtifactPath is the approved executable, hashed against ArtifactSHA256 before exec.
	ArtifactPath   string
	ArtifactSHA256 string

	// SurfaceSHA256 is [ComputeSurfaceSHA256] of this descriptor.
	SurfaceSHA256 string
	Routes        []RouteDeclaration
	Tools         []ToolDigest

	// RequestedGrants is the resource:verb list the operator reviews, at most 32.
	RequestedGrants []access.Permission
	TrustTier       TrustTier

	// MigrationGroup names the migration group the module owns, if any.
	MigrationGroup string
	Limits         ModuleLimits
}

// ApprovedGrants is the operator-approved set of scopes; effective grants are the
// requested grants it matches.
type ApprovedGrants []access.Permission

// DescriptorValidationError names the first descriptor field that failed validation.
type DescriptorValidationError struct {
	Field string // descriptor field or "grants[N]"
	Rule  string // short rule id, e.g. "empty", "non_hex", "non_grantable"
	msg   string
}

// Error returns the validation message.
func (e *DescriptorValidationError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return e.msg
}

func descErr(field, rule, msg string) *DescriptorValidationError {
	return &DescriptorValidationError{Field: field, Rule: rule, msg: msg}
}

const moduleIdentPattern = "^[A-Za-z][A-Za-z0-9_-]*$"

const idPattern = "^[A-Za-z][A-Za-z0-9_-]*$"

const maxModuleGrants = 32

const hexSHA256Len = 64

// ValidateProcessModuleDescriptor checks d at install time and returns the effective
// grants: the requested grants that approved matches. It returns a
// *[DescriptorValidationError] for the first failing check and never mutates d.
func ValidateProcessModuleDescriptor(d ProcessModuleDescriptor, approved ApprovedGrants) ([]access.Permission, error) {
	if d.Name == "" {
		return nil, descErr("name", "empty", "descriptor: name is required")
	}
	if !matchIdent(d.Name) {
		return nil, descErr("name", "pattern", fmt.Sprintf("descriptor: name %q must match %s", d.Name, moduleIdentPattern))
	}
	if d.Version == "" {
		return nil, descErr("version", "empty", "descriptor: version is required")
	}

	if d.ArtifactPath == "" {
		return nil, descErr("artifact_path", "empty", "descriptor: artifact_path is required")
	}
	if err := validateHexSHA256("artifact_sha256", d.ArtifactSHA256); err != nil {
		return nil, err
	}
	if err := validateHexSHA256("surface_sha256", d.SurfaceSHA256); err != nil {
		return nil, err
	}

	seenRoute := make(map[string]string, len(d.Routes))
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
			return nil, descErr(prefix+".path", "pattern", fmt.Sprintf("descriptor: module %q route %q path %q must begin with '/'", d.Name, r.ID, r.Path))
		}
		if len(r.Path) >= 2 && r.Path[0] == '/' && (r.Path[1] == '/' || r.Path[1] == '\\') {
			return nil, descErr(prefix+".path", "origin", fmt.Sprintf("descriptor: module %q route %q path %q is protocol-relative — name a root-relative path (/prefix/route), not an origin", d.Name, r.ID, r.Path))
		}
	}

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

	if len(d.RequestedGrants) > maxModuleGrants {
		return nil, descErr("requested_grants", "cap", fmt.Sprintf("descriptor: %d requested grants exceeds cap %d", len(d.RequestedGrants), maxModuleGrants))
	}
	for i, g := range d.RequestedGrants {
		prefix := fmt.Sprintf("requested_grants[%d]", i)
		gs := string(g)
		if gs == "" {
			return nil, descErr(prefix, "empty", "descriptor: empty grant")
		}
		if reason := nonGrantableReason(g); reason != "" {
			return nil, descErr(prefix, "non_grantable", fmt.Sprintf("descriptor: grant %q is %s (design §5 carve-out: a module cannot escape owner/tenant scoping)", gs, reason))
		}
		if !access.ValidScope(gs) {
			return nil, descErr(prefix, "scope", fmt.Sprintf("descriptor: grant %q is not a valid resource:verb scope", gs))
		}
	}

	effective := intersectGrants(d.RequestedGrants, approved)

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

	if d.MigrationGroup != "" && !matchIdent(d.MigrationGroup) {
		return nil, descErr("migration_group", "pattern", fmt.Sprintf("descriptor: migration_group %q must match %s", d.MigrationGroup, moduleIdentPattern))
	}

	return effective, nil
}

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

func matchID(s string) bool { return matchIdent(s) }

func isAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

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
		return ""
	}
	if res == "*" && (verb == "*" || verb == "read") {
		return "a wildcard broad enough to subsume cross-owner reads"
	}
	return ""
}

func splitResourceVerb(s string) (resource, verb string, ok bool) {
	idx := strings.IndexByte(s, ':')
	if idx <= 0 || idx == len(s)-1 {
		return "", "", false
	}
	return s[:idx], s[idx+1:], true
}

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

// ComputeSurfaceSHA256 returns the hex SHA-256 of d's canonical surface JSON (name,
// version, routes, tools, grants, trust tier, migration group), the digest the
// child's handshake must echo.
func ComputeSurfaceSHA256(d ProcessModuleDescriptor) (string, error) {
	canon, err := surfaceCanonical(d)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:]), nil
}

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
	return json.Marshal(c)
}

// Is matches a target with the same Field and Rule.
func (e *DescriptorValidationError) Is(target error) bool {
	if dv, ok := errors.AsType[*DescriptorValidationError](target); ok {
		return e.Field == dv.Field && e.Rule == dv.Rule
	}
	return false
}
