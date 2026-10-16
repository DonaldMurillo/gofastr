package config

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

type scopeRecursiveConfig struct {
	Value string
	Next  *scopeRecursiveConfig
}

func TestRecursiveConfigTerminates(t *testing.T) {
	if os.Getenv("GOFASTR_SCOPE_CONFIG_CHILD") == "1" {
		var cfg scopeRecursiveConfig
		if Load(&cfg, MapSource{}) == nil {
			t.Fatal("expected unsupported recursive shape error")
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRecursiveConfigTerminates$")
	cmd.Env = append(os.Environ(), "GORACE=atexit_sleep_ms=0", "GOFASTR_SCOPE_CONFIG_CHILD=1")
	if e := cmd.Run(); e != nil {
		t.Fatalf("recursive config did not safely refuse: %v", e)
	}
}
