package evalrunner

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// installCLIShim writes a `gofastr` wrapper into dir that appends each
// invocation's arguments to logPath and then execs realBin. Prepending dir to
// the builder's PATH gives every candidate the SNAPSHOT's CLI (guidance tells
// agents to run `gofastr docs` / `gofastr dev`; a globally installed gofastr
// would leak a different framework version into the treatment) and records a
// precise, non-deterministic funnel signal: the harness prompt names no
// command, so any `gofastr dev` in the log was discovered from the generated
// guidance alone. Transcript grepping cannot provide this signal — an agent
// merely READING the guidance echoes the command into its transcript.
func installCLIShim(dir, realBin, logPath string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		shim := "@echo off\r\n" +
			">> \"" + logPath + "\" echo %*\r\n" +
			"\"" + realBin + "\" %*\r\n"
		return os.WriteFile(filepath.Join(dir, "gofastr.cmd"), []byte(shim), 0o755)
	}
	shim := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"" + logPath + "\"\n" +
		"exec \"" + realBin + "\" \"$@\"\n"
	return os.WriteFile(filepath.Join(dir, "gofastr"), []byte(shim), 0o755)
}

// cliInvocationStats reads a shim log and reports how many times the builder
// invoked the gofastr CLI and whether any invocation was the dev server.
// A missing log means zero invocations, not an error — the builder simply
// never reached for the CLI.
func cliInvocationStats(logPath string) (calls int, usedDev bool) {
	f, err := os.Open(logPath)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	reader := bufio.NewReader(f)
	for {
		line, readErr := reader.ReadString('\n')
		if fields := strings.Fields(line); len(fields) > 0 {
			calls++
			if fields[0] == "dev" {
				usedDev = true
			}
		}
		if readErr == io.EOF {
			return calls, usedDev
		}
		if readErr != nil {
			return calls, usedDev
		}
	}
}

type cliDocumentationStats struct {
	Calls             int
	Searches          []string
	Topics            []string
	UsedCapabilityMap bool
}

// cliDocsInvocationStats extracts documentation-discovery evidence from the
// same PATH-shim log as cliInvocationStats. The builder prompt names no docs
// command or topic, so these are behavioral funnel signals rather than prompt
// compliance. Searches and topics are unique and sorted for stable artifacts.
func cliDocsInvocationStats(logPath string) cliDocumentationStats {
	f, err := os.Open(logPath)
	if err != nil {
		return cliDocumentationStats{}
	}
	defer f.Close()

	stats := cliDocumentationStats{}
	searches := map[string]bool{}
	topics := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "docs" {
			continue
		}
		stats.Calls++
		rest := strings.TrimSpace(strings.TrimPrefix(line, "docs"))
		switch {
		case strings.HasPrefix(rest, "--grep="):
			query := strings.Trim(strings.TrimSpace(strings.TrimPrefix(rest, "--grep=")), `"'`)
			if query != "" {
				searches[query] = true
			}
		case strings.HasPrefix(rest, "--grep"):
			query := strings.Trim(strings.TrimSpace(strings.TrimPrefix(rest, "--grep")), `"'`)
			if query != "" {
				searches[query] = true
			}
		default:
			args := strings.Fields(rest)
			if len(args) == 0 {
				continue
			}
			topic := strings.Trim(args[0], `"'`)
			if topic == "" || strings.HasPrefix(topic, "-") {
				continue
			}
			topics[topic] = true
			if topic == "ui-capability-map" {
				stats.UsedCapabilityMap = true
			}
		}
	}
	for query := range searches {
		stats.Searches = append(stats.Searches, query)
	}
	for topic := range topics {
		stats.Topics = append(stats.Topics, topic)
	}
	sort.Strings(stats.Searches)
	sort.Strings(stats.Topics)
	return stats
}
