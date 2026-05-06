package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func runTest(args []string) {
	fmt.Printf("\n  %s\n\n", bold("Running tests..."))

	// Build the go test command
	testArgs := []string{"test", "./...", "-v"}

	// Additional flags
	extraTestArgs := []string{}
	runPattern := ""
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--run="):
			runPattern = strings.TrimPrefix(a, "--run=")
		case strings.HasPrefix(a, "--bench="):
			extraTestArgs = append(extraTestArgs, "-bench="+strings.TrimPrefix(a, "--bench="))
		case a == "--race":
			extraTestArgs = append(extraTestArgs, "-race")
		case a == "--cover":
			extraTestArgs = append(extraTestArgs, "-cover")
		case a == "--short":
			extraTestArgs = append(extraTestArgs, "-short")
		case strings.HasPrefix(a, "--timeout="):
			extraTestArgs = append(extraTestArgs, "-timeout="+strings.TrimPrefix(a, "--timeout="))
		}
	}

	if runPattern != "" {
		extraTestArgs = append(extraTestArgs, "-run", runPattern)
	}

	testArgs = append(testArgs, extraTestArgs...)

	cmd := exec.Command("go", testArgs...)
	cmd.Stdout = nil // We'll pipe and colorize
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "GOFASTR_TEST=1")

	// Capture stdout and colorize
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fail("Failed to pipe stdout: %v", err)
		// Fallback: run without colorization
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fail("Tests failed")
			os.Exit(1)
		}
		success("All tests passed!")
		return
	}

	if err := cmd.Start(); err != nil {
		fail("Failed to start tests: %v", err)
		os.Exit(1)
	}

	// Colorize test output
	passed := 0
	failed := 0
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		colored := colorizeTestLine(line, &passed, &failed)
		fmt.Println(colored)
	}

	if err := cmd.Wait(); err != nil {
		fmt.Println()
		fail("%d test(s) passed, %d test(s) failed", passed, failed)
		info("Run 'gofastr test --run=TestName' to run a specific test")
		os.Exit(1)
	}

	fmt.Println()
	success("All tests passed! (%d passed, %d failed)", passed, failed)
}

// colorizeTestLine adds color to go test -v output lines.
func colorizeTestLine(line string, passed, failed *int) string {
	switch {
	case strings.HasPrefix(line, "=== RUN"):
		return "  " + bold(line)
	case strings.HasPrefix(line, "--- PASS"):
		*passed++
		return green("  ✓ " + strings.TrimPrefix(line, "--- PASS:   "))
	case strings.HasPrefix(line, "--- FAIL"):
		*failed++
		return red("  ✗ " + strings.TrimPrefix(line, "--- FAIL:   "))
	case strings.HasPrefix(line, "ok"):
		return green("  " + line)
	case strings.HasPrefix(line, "FAIL"):
		return red("  " + line)
	case strings.HasPrefix(line, "PASS"):
		return green("  " + line)
	default:
		return "  " + line
	}
}
