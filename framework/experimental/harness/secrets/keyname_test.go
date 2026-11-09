package secrets

import "testing"

// #669: isProviderCredentialKey matched only the _API_KEY/_TOKEN suffix,
// so a name that is not an environment variable name ("\x1b…ZAI_API_KEY",
// "ZAI-API-KEY", a Turkish dotted capital) counted as a credential the
// file may deliver — and the CLI had to unset such keys after the fact
// when no prompt ran. The whole name is checked here now, at the source:
// a key that is not ^[A-Z][A-Z0-9_]*$ never reaches the pending list, so
// it is never set, never prompted, never unset.
func TestIsProviderCredentialKeyChecksWholeName(t *testing.T) {
	for k, want := range map[string]bool{
		"ZAI_API_KEY": true, "OPENROUTER_API_KEY": true, "GITHUB_TOKEN": true, "A1_TOKEN": true,
		"": false, "1_API_KEY": false, "_API_KEY": false, "zai_api_key": false,
		"ZAI-API_KEY": false, "ZAi_API_KEY": false,
		"\x1b[2K\r\x1b]52;c;cHduZWQ=\aZAI_API_KEY": false,
		"ZAI_API_KEY\u202e":                        false, "ZAİ_API_KEY": false,
		"ZAI_API_KEY\n": false, "ZAI API_KEY": false,
		// Not credentials at all, whatever their shape.
		"GOFASTR_HARNESS_PASSPHRASE": false, "PATH": false, "MY_HOME_TOKEN": true,
	} {
		if got := isProviderCredentialKey(k); got != want {
			t.Errorf("isProviderCredentialKey(%q) = %v, want %v", k, got, want)
		}
	}
}
