package sqlite

import "regexp"

type redactionSub struct {
	regex *regexp.Regexp
	repl  string
}

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
		regex: regexp.MustCompile(`(?s)-----BEGIN [A-Z ]+PRIVATE KEY-----.*?-----END [A-Z ]+PRIVATE KEY-----`),
		repl:  "-----BEGIN «redacted:private-key»-----",
	},
	{
		regex: regexp.MustCompile(`-----BEGIN [A-Z ]+PRIVATE KEY-----`),
		repl:  "-----BEGIN «redacted:private-key»-----",
	},
	{
		regex: regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}`),
		repl:  "«redacted:jwt»",
	},
	{
		regex: regexp.MustCompile(`sk-(or-|ant-|proj-)?[A-Za-z0-9][A-Za-z0-9_\-]{15,}`),
		repl:  "«redacted:provider-key»",
	},
	{
		regex: regexp.MustCompile(`([A-Z][A-Z0-9]*_(?:` + secretNameSuffixes + `)=)['"]?[^\s'"]{6,}['"]?`),
		repl:  "${1}«redacted:secret-assignment»",
	},
}

const secretNameSuffixes = `API_KEY|TOKEN|SECRET|PASSWORD|PASSPHRASE|ACCESS_KEY|MACHINE_KEY`

var secretMemberName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*_(?:` + secretNameSuffixes + `)$`)

func redactSecretMember(key, value string) (string, bool) {
	if key == "" || len(value) < 6 || !secretMemberName.MatchString(key) {
		return value, false
	}
	return "«redacted:secret-assignment»", true
}
