package testcmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
)

func RunTest(args []string) {
	if cli.HasHelpFlag(args) {
		printTestUsage()
		return
	}
	fmt.Printf("\n  %s\n\n", cli.Bold("Running tests..."))

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
		cli.Fail("Failed to pipe stdout: %v", err)
		// Fallback: run without colorization
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			cli.Fail("Tests failed")
			cli.OsExit(1)
		}
		cli.Success("All tests passed!")
		return
	}

	if err := cmd.Start(); err != nil {
		cli.Fail("Failed to start tests: %v", err)
		cli.OsExit(1)
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

	if err := scanner.Err(); err != nil {
		// Output stopped early, so the tally below counted only part of
		// the run. Saying so beats printing a confident wrong number.
		fmt.Println()
		cli.Fail("test output truncated after %d passed / %d failed: %v", passed, failed, err)
		cli.OsExit(1)
	}

	if err := cmd.Wait(); err != nil {
		fmt.Println()
		cli.Fail("%d test(s) passed, %d test(s) failed", passed, failed)
		cli.Info("Run 'gofastr test --run=TestName' to run a specific test")
		cli.OsExit(1)
	}

	fmt.Println()
	cli.Success("All tests passed! (%d passed, %d failed)", passed, failed)
}

// colorizeTestLine adds color to go test -v output lines.
func colorizeTestLine(line string, passed, failed *int) string {
	switch {
	case strings.HasPrefix(line, "=== RUN"):
		return "  " + cli.Bold(line)
	case strings.HasPrefix(line, "--- PASS"):
		*passed++
		return cli.Green("  ✓ " + strings.TrimPrefix(line, "--- PASS:   "))
	case strings.HasPrefix(line, "--- FAIL"):
		*failed++
		return cli.Red("  ✗ " + strings.TrimPrefix(line, "--- FAIL:   "))
	case strings.HasPrefix(line, "ok"):
		return cli.Green("  " + line)
	case strings.HasPrefix(line, "FAIL"):
		return cli.Red("  " + line)
	case strings.HasPrefix(line, "PASS"):
		return cli.Green("  " + line)
	default:
		return "  " + line
	}
}
