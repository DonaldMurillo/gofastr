package testcmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

func RunTest(args []string) {
	if cli.HasHelpFlag(args) {
		printTestUsage()
		return
	}
	fmt.Printf("\n  %s\n\n", cli.Bold("Running tests..."))

	testArgs, err := goTestArgs(args)
	if err != nil {
		cli.Fail("%v", err)
		cli.Info("Run 'gofastr test --help' for the flags it takes")
		cli.OsExit(2)
		return
	}

	cmd := exec.Command("go", testArgs...)
	stdout, output := io.Pipe()
	defer stdout.Close()
	defer output.Close()
	cmd.Stdout = output
	stderr := &scrubWriter{out: os.Stderr}
	cmd.Stderr = stderr
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "GOFASTR_TEST=1")

	if err := cmd.Start(); err != nil {
		cli.Fail("Failed to start tests: %v", err)
		cli.OsExit(1)
		return
	}

	passed := 0
	failed := 0
	scanDone := make(chan error, 1)
	go func() {
		defer stdout.Close()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			fmt.Println(colorizeTestLine(scanner.Text(), &passed, &failed))
		}
		if scanner.Err() != nil {
			_ = cmd.Process.Kill()
		}
		scanDone <- scanner.Err()
	}()
	waitErr := cmd.Wait()
	_ = output.Close()
	scanErr := <-scanDone
	stderrErr := stderr.flush()

	if scanErr != nil {
		fmt.Println()
		cli.Fail("test output truncated after %d passed / %d failed: %v", passed, failed, scanErr)
		cli.OsExit(1)
		return
	}

	if waitErr != nil || stderrErr != nil {
		fmt.Println()
		cli.Fail("%d test(s) passed, %d test(s) failed", passed, failed)
		cli.Info("Run 'gofastr test --run=TestName' to run a specific test")
		cli.OsExit(1)
		return
	}

	fmt.Println()
	cli.Success("All tests passed! (%d passed, %d failed)", passed, failed)
}

func colorizeTestLine(line string, passed, failed *int) string {
	line = textsafe.ScrubTerminal(line)
	switch {
	case strings.HasPrefix(line, "=== RUN"):
		return "  " + cli.Bold(line)
	case strings.HasPrefix(line, "--- PASS"):
		*passed++
		return cli.Green("  ✓ " + strings.TrimLeft(strings.TrimPrefix(line, "--- PASS:"), " "))
	case strings.HasPrefix(line, "--- FAIL"):
		*failed++
		return cli.Red("  ✗ " + strings.TrimLeft(strings.TrimPrefix(line, "--- FAIL:"), " "))
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

type scrubWriter struct {
	out io.Writer
	buf []byte
}

func (w *scrubWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		if _, err := fmt.Fprintln(w.out, textsafe.ScrubTerminal(line)); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

func (w *scrubWriter) flush() error {
	if len(w.buf) == 0 {
		return nil
	}
	remainder := textsafe.ScrubTerminal(string(w.buf))
	w.buf = nil
	_, err := io.WriteString(w.out, remainder)
	return err
}

func goTestArgs(args []string) ([]string, error) {
	valued := map[string]string{"--run": "-run", "--bench": "-bench", "--timeout": "-timeout"}
	bare := map[string]string{"--race": "-race", "--cover": "-cover", "--short": "-short"}
	var flags, pkgs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			pkgs = append(pkgs, a)
			continue
		}
		if f, ok := bare[a]; ok {
			flags = append(flags, f)
			continue
		}
		name, value, hasValue := strings.Cut(a, "=")
		f, ok := valued[name]
		if !ok {
			return nil, fmt.Errorf("unknown flag %s", name)
		}
		if !hasValue {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return nil, fmt.Errorf("%s needs a value (write %s=<value> for one that starts with a dash)", name, name)
			}
			i++
			value = args[i]
		}
		flags = append(flags, f, value)
	}
	if len(pkgs) == 0 {
		pkgs = []string{"./..."}
	}
	return append(append(append([]string{"test"}, pkgs...), "-v"), flags...), nil
}
