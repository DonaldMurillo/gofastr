package bounded

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

func boundLateArgs(ctx context.Context, prog string) error {
	cmd := exec.CommandContext(ctx, prog)
	var out bytes.Buffer
	cmd.Stdout = &out
	prepare(ctx, cmd)
	return cmd.Run()
}

func prepare(ctx context.Context, cmd *exec.Cmd) {
	cmd.WaitDelay = time.Second
}

func BoundLateArgs(ctx context.Context, program string) error { return boundLateArgs(ctx, program) }
