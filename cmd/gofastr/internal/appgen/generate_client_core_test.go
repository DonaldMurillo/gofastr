package appgen

import (
	"strings"
	"testing"
)

// The SDK's default client has no Timeout (Watch holds an SSE stream
// open) and says why; the --from-openapi CLI's default has a 30s one.
// Both refuse redirects.
func TestClientCoreDefaults(t *testing.T) {
	sdk, cli := sdkClientCore.render(), cliSelfClientCore.render()
	if strings.Contains(sdk, "Timeout:") || !strings.Contains(sdk, "//gofastr:allow(clienttimeout)") {
		t.Errorf("SDK default client: want no Timeout and the clienttimeout allow:\n%s", sdk)
	}
	if !strings.Contains(cli, "Timeout: 30 * time.Second,") || strings.Contains(cli, "gofastr:allow") {
		t.Errorf("CLI default client: want a 30s Timeout and no allow:\n%s", cli)
	}
	for name, src := range map[string]string{"sdk": sdk, "cli": cli} {
		if !strings.Contains(src, "return http.ErrUseLastResponse") {
			t.Errorf("%s default client follows redirects:\n%s", name, src)
		}
	}
}
