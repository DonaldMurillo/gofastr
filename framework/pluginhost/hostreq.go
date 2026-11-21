package pluginhost

import (
	"fmt"
	"log/slog"
	"strings"
)

// HostRequirementPrefix starts a host requirement: "permissions-policy:camera".
const HostRequirementPrefix = "permissions-policy:"

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

const defaultPermissionsPolicy = "geolocation=(), microphone=(), camera=()"

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

func hostRequirementFeature(token string) (feature string, ok bool) {
	t := strings.ToLower(strings.TrimSpace(token))
	if !strings.HasPrefix(t, HostRequirementPrefix) {
		return "", false
	}
	f := strings.TrimSpace(strings.TrimPrefix(t, HostRequirementPrefix))
	if !allowedPolicyFeatures[f] {
		// Return the feature anyway: the caller's error names what was
		// declared, so a typo says which token to fix.
		return f, false
	}
	return f, true
}

// CheckHostRequirements warns about module requirements the policy denies.
func CheckHostRequirements(log *slog.Logger, permissionsPolicy string, modules ...ClientModule) {
	if log == nil {
		log = slog.Default()
	}
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
			return false
		}
	}
	return named
}

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
