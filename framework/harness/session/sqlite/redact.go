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
}
