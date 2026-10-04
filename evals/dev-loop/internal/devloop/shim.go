package devloop

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// callerEnv carries who launched a shimmed tool. The agent's own shell
// leaves it unset; the gofastr shim exports "gofastr-<subcommand>" before
// exec'ing the real binary, so the `go build` that `gofastr dev` runs to
// rebuild the app is attributed to the dev loop, not counted as the
// agent building and launching a binary by hand.
const callerEnv = "DEVLOOP_CALLER"

// installShims writes `gofastr` and `go` wrappers into dir. Each appends
// one line per invocation to logPath, "<caller>\t<tool>\t<argv>", then
// execs the real binary. Prepending dir to the agent's PATH also pins the
// snapshot's own gofastr CLI, so a globally installed one cannot leak a
// different framework version into the run.
//
// One invocation yields exactly one line: newlines, carriage returns and
// tabs in argv are flattened to spaces, so an argument cannot forge extra
// records or shift the caller/tool columns.
//
// The gofastr shim also pins `gofastr dev` to devAddr when the agent
// names no address (--addr, -p), so each trial serves on its own port
// and never meets another process on :8080. The log keeps the agent's
// argv as typed.
func installShims(dir, realGofastr, realGo, logPath, devAddr string) error {
	for _, p := range []string{dir, realGofastr, realGo, logPath, devAddr} {
		if strings.ContainsAny(p, "'\n") {
			return fmt.Errorf("shim path %q cannot be single-quoted in sh", p)
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	record := func(tool string) string {
		return "printf '%s\\t%s\\t%s\\n' \"${" + callerEnv + ":-agent}\" '" + tool + "' " +
			"\"$(printf '%s' \"$*\" | tr '\\n\\r\\t' '   ')\" >> '" + logPath + "'\n"
	}
	gofastr := "#!/bin/sh\n" + record("gofastr") +
		callerEnv + "=\"gofastr-${1:-none}\"\nexport " + callerEnv + "\n" +
		"if [ \"$1\" = dev ]; then\n" +
		"  pin=1\n" +
		"  for a in \"$@\"; do\n" +
		"    case \"$a\" in --addr|--addr=*|-p|-p=*) pin=0 ;; esac\n" +
		"  done\n" +
		"  if [ \"$pin\" = 1 ]; then\n" +
		"    shift\n" +
		"    exec '" + realGofastr + "' dev --addr '" + devAddr + "' \"$@\"\n" +
		"  fi\n" +
		"fi\n" +
		"exec '" + realGofastr + "' \"$@\"\n"
	goShim := "#!/bin/sh\n" + record("go") +
		"exec '" + realGo + "' \"$@\"\n"
	for _, shim := range []struct{ name, body string }{{"gofastr", gofastr}, {"go", goShim}} {
		path := filepath.Join(dir, shim.name)
		if err := fileperm.WriteOwnerOnly(path, []byte(shim.body)); err != nil {
			return err
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// shimCall is one parsed line of the shim log.
type shimCall struct {
	Caller string   `json:"caller"`
	Tool   string   `json:"tool"`
	Args   []string `json:"args"`
}

// FromAgent reports whether the agent's shell ran the command, as opposed
// to a gofastr subcommand running it on the agent's behalf.
func (c shimCall) FromAgent() bool { return c.Caller == "agent" }

func readShimLog(path string) ([]shimCall, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var calls []shimCall
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		caller, rest, ok := strings.Cut(scanner.Text(), "\t")
		if !ok {
			continue
		}
		tool, argv, _ := strings.Cut(rest, "\t")
		calls = append(calls, shimCall{Caller: caller, Tool: tool, Args: strings.Fields(argv)})
	}
	return calls, scanner.Err()
}
