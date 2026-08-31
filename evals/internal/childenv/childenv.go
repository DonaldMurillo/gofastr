// Package childenv decides what environment an agent-driven child
// process may see.
//
// It exists because the rule was hand-copied into two eval runners and
// they had already drifted: the NUGET_ and TWILIO_ prefixes existed in
// one copy only, and neither recognised a bare *_TOKEN name, which is
// how HF_TOKEN, VERCEL_TOKEN, and CLOUDFLARE_API_TOKEN rode through. A
// denylist maintained in two places is a denylist that protects one of
// them.
package childenv

import (
	"os"
	"strings"
)

// exactNames are credential-bearing variables whose names carry no
// generic marker at all.
var exactNames = map[string]bool{
	"SSH_AUTH_SOCK":  true,
	"GPG_AGENT_INFO": true,
	"DATABASE_URL":   true,
}

// vendorPrefixes name a cloud or service account whose variables are
// credentials by convention, whatever the rest of the name says.
var vendorPrefixes = []string{
	"AWS_", "AZURE_", "GCP_", "GOOGLE_", "GH_", "GITHUB_",
	"NPM_", "NUGET_", "DOCKER_", "SLACK_", "STRIPE_", "TWILIO_",
	"CLOUDFLARE_", "VERCEL_", "NETLIFY_", "FLY_", "HEROKU_",
	"HF_", "REPLICATE_", "OPENAI_", "ANTHROPIC_",
}

// secretFragments appear somewhere inside a credential-bearing name.
// TOKEN is here rather than as a suffix rule so PREFIX_TOKEN,
// TOKEN_VALUE, and API_TOKEN_V2 all match; the cost is that a variable
// legitimately containing "TOKEN" is withheld, which is the safe
// direction for a list whose job is to keep secrets out of a child
// process nobody is watching.
var secretFragments = []string{
	"API_KEY", "ACCESS_KEY", "AUTH_TOKEN", "CREDENTIAL", "PASSWORD",
	"PRIVATE_KEY", "SECRET", "SIGNING_KEY", "TOKEN", "PASSPHRASE",
	"SESSION_ID", "ACCOUNT_SID",
}

// LooksCredentialBearing reports whether an environment variable name
// should be withheld from an agent-driven child.
func LooksCredentialBearing(name string) bool {
	if exactNames[name] {
		return true
	}
	upper := strings.ToUpper(name)
	for _, prefix := range vendorPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	for _, fragment := range secretFragments {
		if strings.Contains(upper, fragment) {
			return true
		}
	}
	return false
}

// allowedNames is the environment an agent-built candidate server needs
// to compile and run, and nothing else.
//
// An allowlist rather than a denylist, because the two runners started
// from opposite ends and only one arrived: ui-quality passed its
// candidate an allowlisted environment while backend-adoption handed its
// candidate os.Environ() whole. That candidate is code an unsupervised
// agent wrote; it should not be reading the operator's cloud keys, SCM
// tokens, or DATABASE_URL, and a denylist can only withhold the shapes
// someone thought of.
var allowedNames = map[string]bool{
	"APPDATA": true, "CC": true, "CGO_ENABLED": true, "COMSPEC": true,
	"CXX": true, "GOARCH": true, "GOCACHE": true,
	"GOEXPERIMENT": true, "GOMODCACHE": true, "GOOS": true, "GOPATH": true,
	"GOROOT": true, "HOME": true, "HOMEDRIVE": true, "HOMEPATH": true,
	"LANG": true, "LANGUAGE": true, "LC_ALL": true, "LOCALAPPDATA": true,
	"LOGNAME": true, "NUMBER_OF_PROCESSORS": true, "OS": true, "PATH": true,
	"PATHEXT": true, "PKG_CONFIG_PATH": true, "PROCESSOR_ARCHITECTURE": true,
	"PROCESSOR_IDENTIFIER": true, "PROGRAMDATA": true, "PROGRAMFILES": true,
	"PROGRAMFILES(X86)": true, "PROGRAMW6432": true, "SHELL": true,
	"SSL_CERT_DIR": true, "SSL_CERT_FILE": true, "SYSTEMDRIVE": true,
	"SYSTEMROOT": true, "TEMP": true, "TMP": true, "TMPDIR": true,
	"TZ": true, "USER": true, "USERPROFILE": true, "WINDIR": true,
}

// Allowed reports whether an environment variable name may be passed to
// an agent-driven child.
func Allowed(name string) bool { return allowedNames[strings.ToUpper(name)] }

// Allowlisted returns the entries of os.Environ() that Allowed accepts,
// in "NAME=value" form.
func Allowlisted() []string {
	var out []string
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if ok && Allowed(name) {
			out = append(out, entry)
		}
	}
	return out
}
