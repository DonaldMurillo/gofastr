package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func validateBuildTarget(pkg string) error {
	out, err := exec.Command("go", "list", "-f={{.Name}}", pkg).CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("%s", detail)
	}
	if name := strings.TrimSpace(string(out)); name != "main" {
		return fmt.Errorf("package name is %q, want \"main\"", name)
	}
	return nil
}
