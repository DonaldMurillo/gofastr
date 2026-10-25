package devcmd

import (
	"strings"
	"testing"
)

func TestDesktopRunEnvConstruction(t *testing.T) {
	env := BuildDevChildEnv([]string{"GOFASTR_DEV=0", "HOME=/x"}, "")
	if len(env) != 2 || env[0] != "GOFASTR_DEV=1" || env[1] != "HOME=/x" {
		t.Fatalf("env = %v", env)
	}
	if strings.HasPrefix(env[len(env)-1], "PORT=") {
		t.Fatal("desktop run must not inject PORT: the host picks its own port")
	}
}
