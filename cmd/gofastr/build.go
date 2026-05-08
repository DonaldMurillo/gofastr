package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func runBuild(args []string) {
	fmt.Printf("\n  %s Building project...\n\n", bold("GoFastr"))

	start := time.Now()

	output := "bin/server"
	noGenerate := false
	for _, arg := range args {
		switch {
		case arg == "--no-generate":
			noGenerate = true
		case strings.HasPrefix(arg, "-o="):
			output = strings.TrimPrefix(arg, "-o=")
		case strings.HasPrefix(arg, "--output="):
			output = strings.TrimPrefix(arg, "--output=")
		}
	}

	// Step 1: generate code when entity declarations are present.
	if !noGenerate {
		if _, err := os.Stat("entities"); err == nil {
			info("Generating code...")
			generateProject([]string{"--clean"})
		}
	}

	// Step 2: go vet
	info("Running go vet...")
	vetCmd := exec.Command("go", "vet", "./...")
	vetCmd.Stdout = os.Stdout
	vetCmd.Stderr = os.Stderr
	if err := vetCmd.Run(); err != nil {
		fail("go vet found issues")
		os.Exit(1)
	}
	success("go vet passed")

	// Step 3: go build
	info("Compiling...")
	buildCmd := exec.Command("go", "build", "-o", output, ".")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fail("Build failed")
		os.Exit(1)
	}

	elapsed := time.Since(start)
	success("Build completed in %s", elapsed.Round(time.Millisecond))
	fmt.Printf("  Binary: %s\n", bold(output))
}
