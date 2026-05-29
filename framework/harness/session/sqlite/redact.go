package sqlite

import "regexp"

// redactionSub is one regex → replacement pair.
type redactionSub struct {
	regex *regexp.Regexp
	repl  string
}

// redactionSubs is the default set of secret patterns. Kept tight to
// minimize false-positive damage to legitimate event payloads.
//
// Coverage:
//
//   - AWS access keys (AKIA…, ASIA…)
//   - GitHub PATs (ghp_, gho_, github_pat_)
//   - Bearer tokens in headers
//   - PEM-headered private keys
//   - JWT-shaped triple-segment base64 strings of plausible length
//   - provider API keys (sk-, sk-or-, sk-ant-, sk-proj-) the harness resolves
//   - *_API_KEY / *_TOKEN / *_SECRET = <value> environment assignments
//
// The provider-key + assignment patterns close the gap noted in
// framework/docs/content/harness-architecture.md: the on-write middleware
// must cover the cloud-provider key prefixes the harness itself handles
// (OPENROUTER_API_KEY, ZAI_API_KEY, …) so a tool result that echoes the
// environment (e.g. "env | grep KEY") never lands in events.payload
// plaintext. The assignment matcher catches provider keys whose value is
// not an sk- token (ZAI's <id>.<secret> form) without clobbering benign
// assignments like PATH=… — it only fires when the variable name ends in
// API_KEY / TOKEN / SECRET / PASSWORD / ACCESS_KEY.
var redactionSubs = []redactionSub{
	{
		regex: regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		repl:  "«redacted:aws-access-key»",
	},
	{
		regex: regexp.MustCompile(`ASIA[0-9A-Z]{16}`),
		repl:  "«redacted:aws-session-key»",
	},
	{
		regex: regexp.MustCompile(`ghp_[A-Za-z0-9]{36,}`),
		repl:  "«redacted:github-pat»",
	},
	{
		regex: regexp.MustCompile(`gho_[A-Za-z0-9]{30,}`),
		repl:  "«redacted:github-oauth»",
	},
	{
		regex: regexp.MustCompile(`github_pat_[A-Za-z0-9_]{60,}`),
		repl:  "«redacted:github-pat»",
	},
	{
		regex: regexp.MustCompile(`Bearer\s+[A-Za-z0-9\-_\.=]{20,}`),
		repl:  "Bearer «redacted:bearer»",
	},
	{
		regex: regexp.MustCompile(`-----BEGIN [A-Z ]+PRIVATE KEY-----`),
		repl:  "-----BEGIN «redacted:private-key»-----",
	},
	{
		regex: regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}`),
		repl:  "«redacted:jwt»",
	},
	// Provider API keys: sk-, sk-or-(v1-), sk-ant-(api03-), sk-proj-, etc.
	// Matches the bare token wherever it appears (assignment, Bearer-less
	// echo, JSON value). 16+ trailing chars avoids matching short prose.
	{
		regex: regexp.MustCompile(`sk-(or-|ant-|proj-)?[A-Za-z0-9][A-Za-z0-9_\-]{15,}`),
		repl:  "«redacted:provider-key»",
	},
	// *_API_KEY / *_TOKEN / *_SECRET / *_PASSWORD / *_ACCESS_KEY = <value>.
	// Anchored on the secret-suffixed variable name so benign assignments
	// (PATH=…, HOME=…) are untouched. Catches non-sk provider keys such as
	// ZAI's <id>.<secret> form. The variable name is preserved; only the
	// value (optionally quoted) is redacted.
	{
		regex: regexp.MustCompile(`([A-Z][A-Z0-9]*_(?:API_KEY|TOKEN|SECRET|PASSWORD|ACCESS_KEY)=)['"]?[^\s'"]{6,}['"]?`),
		repl:  "${1}«redacted:secret-assignment»",
	},
}
