package project

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// PackageName asks Go for the package name in dir, with caller-supplied
// subprocess bounds. It passes -buildvcs=false, so it never runs git.
func PackageName(ctx context.Context, dir, pkg string, waitDelay time.Duration) (string, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-buildvcs=false", "-f={{.Name}}", pkg)
	cmd.Dir = dir
	cmd.WaitDelay = waitDelay
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			if detail := strings.TrimSpace(string(exitErr.Stderr)); detail != "" {
				return "", errors.New(detail)
			}
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func ValidateBuildTarget(ctx context.Context, pkg string) error {
	name, err := PackageName(ctx, "", pkg, 0)
	if err != nil {
		return err
	}
	if name != "main" {
		return fmt.Errorf("package name is %q, want \"main\"", name)
	}
	return nil
}
