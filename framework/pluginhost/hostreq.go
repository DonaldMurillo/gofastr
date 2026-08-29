package pluginhost

import (
	"fmt"
	"log/slog"
	"strings"
)

// HostRequirementPrefix is the grammar of a Manifest.HostRequirements token:
// "permissions-policy:<feature>", e.g. "permissions-policy:camera".
//
// The prefix names WHERE the requirement lives — the app's Permissions-Policy
// response header — so the vocabulary can grow to other host-page surfaces
// later without repurposing bare words (a bare "camera" token would read as
// a frame capability, the opposite of what it is).
const HostRequirementPrefix = "permissions-policy:"

// allowedPolicyFeatures is the closed registry of Permissions-Policy
// policy-controlled features a host requirement may name.
//
// Why a closed allow-list rather than the normalising shape used for sandbox
// tokens: [Manifest.SandboxString] must be authoritative (it derives the real
// iframe attribute, so an unknown token there is silently DROPPED and the
// guarantee holds regardless). HostRequirements has no authoritative sink —
// the tokens are declarative metadata the host reads, and a normaliser that
// quietly dropped a typo'd or invented feature would produce a permanently
// unsatisfiable requirement: the plugin stays broken and the boot check has
// nothing confident to compare, so nobody is ever told. Rejecting at
// registration ([Manifest.Validate], run by [NewClientModule]) turns that
// into a build-time error instead. The feature names come from the
// Permissions-Policy spec's registry of policy-controlled features; adding
// one is a deliberate act (and a docs edit), not a drift.
var allowedPolicyFeatures = map[string]bool{
	"accelerometer":                true,
	"ambient-light-sensor":         true,
	"attribution-reporting":        true,
	"autoplay":                     true,
	"bluetooth":                    true,
	"browsing-topics":              true,
	"camera":                       true,
	"clipboard-read":               true,
	"clipboard-write":              true,
	"compute-pressure":             true,
	"cross-origin-isolated":        true,
	"display-capture":              true,
	"document-domain":              true,
	"encrypted-media":              true,
	"fullscreen":                   true,
	"gamepad":                      true,
	"geolocation":                  true,
	"gyroscope":                    true,
	"hid":                          true,
	"identity-credentials-get":     true,
	"idle-detection":               true,
	"local-fonts":                  true,
	"magnetometer":                 true,
	"microphone":                   true,
	"midi":                         true,
	"otp-credentials":              true,
	"payment":                      true,
	"picture-in-picture":           true,
	"private-state-token-issuance": true,
	"publickey-credentials-get":    true,
	"run-ad-auction":               true,
	"screen-wake-lock":             true,
	"serial":                       true,
	"shared-storage":               true,
	"shared-storage-select-url":    true,
	"storage-access":               true,
	"usb":                          true,
	"web-share":                    true,
	"window-management":            true,
	"xr-spatial-tracking":          true,
}

// defaultPermissionsPolicy mirrors the header core/middleware.SecurityHeaders
// sends when SecurityHeadersConfig.PermissionsPolicy is empty. Mirrored as a
// constant rather than imported: pluginhost needs no other middleware
// coupling for one string, and TestCheckHostRequirementsEmptyMeansDefault
// pins the mirror to the middleware's real output so drift fails CI.
const defaultPermissionsPolicy = "geolocation=(), microphone=(), camera=()"

// validateHostRequirement enforces the token grammar for one
// Manifest.HostRequirements entry. Tokens are ASCII-case-insensitive and
// outer-whitespace-tolerant (matching how the header itself is parsed), but
// the feature must be exactly one registry entry: anything else — an
// embedded space, a ";", a parenthesised grant, a typo — fails the closed
// registry lookup, so header syntax can never be smuggled through a token.
func validateHostRequirement(token string) error {
	t := strings.ToLower(strings.TrimSpace(token))
	if !strings.HasPrefix(t, HostRequirementPrefix) {
		return fmt.Errorf("pluginhost: host requirement %q: unknown grammar, expected %q<feature>",
			token, HostRequirementPrefix)
	}
	if feature, ok := hostRequirementFeature(token); !ok {
		return fmt.Errorf("pluginhost: host requirement %q: %q is not a known permissions-policy feature",
			token, feature)
	}
	return nil
}

// hostRequirementFeature extracts the normalised feature name from a host
// requirement token, reporting ok=false when the token is not a well-formed
// "permissions-policy:<known-feature>".
func hostRequirementFeature(token string) (feature string, ok bool) {
	t := strings.ToLower(strings.TrimSpace(token))
	if !strings.HasPrefix(t, HostRequirementPrefix) {
		return "", false
	}
	f := strings.TrimSpace(strings.TrimPrefix(t, HostRequirementPrefix))
	if !allowedPolicyFeatures[f] {
		return "", false
	}
	return f, true
}

// CheckHostRequirements is the boot-time check for [Manifest.HostRequirements].
// Pass the Permissions-Policy the app configures (the PermissionsPolicy field
// of core/middleware.SecurityHeadersConfig); an empty value is treated as the
// framework default, which denies camera, microphone and geolocation. There
// is no central ClientModule registry to hang this on — an app that mounts
// plugins calls it once at startup with its modules:
//
//	pluginhost.CheckHostRequirements(slog.Default(), secCfg.PermissionsPolicy, scanner, charts)
//
// It LOGS and never fails: a plugin declaring a requirement the host has not
// satisfied is a developer-facing warning, and a plugin must not be able to
// take an app down by declaring something. It never returns an error and
// never panics for any input; tokens that do not parse (a module built as a
// struct literal, bypassing [NewClientModule]) are skipped silently —
// [Manifest.Validate] is the loud gate for those.
//
// The "not satisfied" rule is deliberately narrow: warn only when EVERY
// directive naming the feature carries the empty allowlist "()" — the one
// Permissions-Policy shape that unambiguously denies the feature to every
// context, including the host page itself, and the shape GoFastr's default
// header uses. Anything else stays silent: "(self)" and "*" grant the page,
// a directive that does not name the feature leaves it at its default
// allowlist, and an origin list "(https://a.example)" cannot be decided at
// boot at all (it depends on the app's own origin, which no request has
// supplied yet). A warning that fired on grants would train developers to
// ignore the check; missing an exotic denial costs one console error, which
// is the status quo this check improves on.
func CheckHostRequirements(log *slog.Logger, permissionsPolicy string, modules ...ClientModule) {
	if log == nil {
		log = slog.Default()
	}
	// Exactly empty, matching core/middleware.SecurityHeaders: it
	// substitutes its default only for "". A whitespace-only value is
	// emitted verbatim, names no feature, and must not be read as the
	// default policy - that would warn about a denial never sent.
	if permissionsPolicy == "" {
		permissionsPolicy = defaultPermissionsPolicy
	}
	for _, m := range modules {
		for _, token := range m.Manifest.HostRequirements {
			feature, ok := hostRequirementFeature(token)
			if !ok {
				continue
			}
			if policyDeniesFeature(permissionsPolicy, feature) {
				log.Warn("plugin requires a host-page permission the Permissions-Policy denies",
					"plugin", m.Name,
					"requirement", token,
					"policy", permissionsPolicy,
					"fix", fmt.Sprintf("allow it on the host page, e.g. %s=(self), or unset the empty allowlist %s=()", feature, feature),
				)
			}
		}
	}
}

// policyDeniesFeature reports whether the Permissions-Policy header value
// provably denies `feature` to the host page: the feature is named, and every
// directive naming it carries the empty allowlist "()". See
// [CheckHostRequirements] for why narrower is the contract.
func policyDeniesFeature(policy, feature string) bool {
	policy = strings.ToLower(policy)
	named := false
	for _, directive := range splitPolicyDirectives(policy) {
		name, allowlist, ok := strings.Cut(strings.TrimSpace(directive), "=")
		if !ok || strings.TrimSpace(name) != feature {
			continue
		}
		named = true
		if strings.TrimSpace(allowlist) != "()" {
			return false // some directive grants (or partially grants) the feature
		}
	}
	return named
}

// splitPolicyDirectives splits a Permissions-Policy header on the commas that
// separate directives, ignoring commas inside allowlist parentheses
// ("camera=(self, https://a.example), microphone=()" is two directives).
// Unbalanced parens simply stop depth tracking, so no input panics.
func splitPolicyDirectives(policy string) []string {
	var out []string
	depth, start := 0, 0
	for i, c := range policy {
		switch c {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, policy[start:i])
				start = i + 1
			}
		}
	}
	return append(out, policy[start:])
}
