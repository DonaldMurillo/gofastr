package devloop

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func bash(cmd string) toolEvent { return toolEvent{Name: "Bash", Command: cmd} }

func TestShimAttributesDevRebuildToGofastr(t *testing.T) {
	if !supported {
		t.Skip("sh shims")
	}
	dir := t.TempDir()
	shimDir := filepath.Join(dir, "shims")
	logPath := filepath.Join(dir, "cli.log")
	// The fake gofastr runs the toolchain through PATH the way its
	// subcommands do: a rebuild, then a code-generation `go run`. Both
	// belong to gofastr, not the agent, so neither is a bypass.
	fakeGofastr := filepath.Join(dir, "real-gofastr")
	fakeGo := filepath.Join(dir, "real-go")
	if err := os.WriteFile(fakeGofastr, []byte("#!/bin/sh\ngo build -o /x/server . && go run ./internal/gen\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fakeGo, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := installShims(shimDir, fakeGofastr, fakeGo, logPath, "127.0.0.1:7001"); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "PATH="+shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, argv := range [][]string{{"gofastr", "dev"}, {"go", "run", ".\nagent\tgofastr\tdev"}} {
		cmd := exec.Command(filepath.Join(shimDir, argv[0]), argv[1:]...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", argv, err, out)
		}
	}

	calls, err := readShimLog(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 {
		t.Fatalf("want 4 log lines (dev, its build, its go run, agent go run), got %d: %+v", len(calls), calls)
	}
	for _, c := range calls[1:3] {
		if c.Caller != "gofastr-dev" || c.Tool != "go" {
			t.Errorf("gofastr's own go call logged as %+v, want caller gofastr-dev", c)
		}
	}
	s := collectSignals(calls, nil, dir, module)
	if s.DevLaunches != 1 {
		t.Errorf("DevLaunches = %d, want 1; an argv newline must not forge a second dev launch", s.DevLaunches)
	}
	if len(s.GoRuns) != 1 {
		t.Errorf("GoRuns = %v, want the agent's one go run", s.GoRuns)
	}
}

func TestTranscriptLaunchShapes(t *testing.T) {
	ws := t.TempDir()
	cases := []struct {
		cmd           string
		dev, goRun    int
		binaryLaunchs int
	}{
		{"gofastr dev", 1, 0, 0},
		{"nohup gofastr dev -p 9000 > dev.log 2>&1 &", 1, 0, 0},
		{"cd app && GOFASTR_DEV_MCP=0 gofastr dev", 1, 0, 0},
		{"go run github.com/DonaldMurillo/gofastr/cmd/gofastr dev", 1, 0, 0},
		{"/opt/bin/gofastr dev", 1, 0, 0},
		{"go run . &", 0, 1, 0},
		{"PORT=:9000 go run main.go home.go", 0, 1, 0},
		{"go build -o bin/server . && ./bin/server &", 0, 0, 1},
		{"go build . && PORT=:9000 ./devloop", 0, 0, 1},
		{"go build ./... && go test ./...", 0, 0, 0},
		{"gofastr docs dev-livereload", 0, 0, 0},
		{"curl -s localhost:8080/ | grep Harbor", 0, 0, 0},
		{"./scripts/check.sh", 0, 0, 0},
		{"gofastr dev --help", 0, 0, 0},
		{`grep -n -i "dev server\|gofastr dev\|livereload" agents/framework.md`, 0, 0, 0},
		{"echo 'run gofastr dev; go run .'", 0, 0, 0},
		{"# later; gofastr dev\ncurl localhost:8080", 0, 0, 0},
		{"pkill -f \"gofastr dev\"", 0, 0, 0},
		{"(gofastr dev > /tmp/dev.log 2>&1 &) ; sleep 3", 1, 0, 0},
		// A system tool by absolute path is not the app's binary.
		{"/usr/bin/curl -s http://127.0.0.1:1/", 0, 0, 0},
		// Only a `go run` of the app's own main package is a bypass.
		{"go run -race .", 0, 1, 0},
		{"go run -tags dev ./", 0, 1, 0},
		{"go run eval.local/devloop", 0, 1, 0},
		{"go run " + ws, 0, 1, 0},
		{"go run ./scripts/seed", 0, 0, 0},
		{"go run golang.org/x/tools/cmd/goimports@latest -w .", 0, 0, 0},
		{"go run github.com/DonaldMurillo/gofastr/cmd/gofastr docs ui", 0, 0, 0},
		// Heredoc bodies are data, not commands, and an apostrophe in one
		// must not swallow what follows the terminator.
		{"cat > notes.txt <<'EOF'\nthe shop's page\nEOF\ngo run .", 0, 1, 0},
		{"cat > README.md <<EOF\nRun it:\ngo run .\ngofastr dev\nEOF", 0, 0, 0},
		// The CLI treats only --help and -h as help; -help serves.
		{"gofastr dev -help", 1, 0, 0},
		{"cat <<-EOF > x.txt\n\tgo run .\n\tEOF\ngofastr dev &", 1, 0, 0},
		{"grep -c x <<< 'go run .'", 0, 0, 0},
		// A `sh -c` payload is shell code too.
		{"sh -c '/usr/local/go/bin/go run .'", 0, 1, 0},
		{`bash -c "gofastr dev > dev.log 2>&1 &"`, 1, 0, 0},
	}
	for _, c := range cases {
		s := collectSignals(nil, []toolEvent{bash(c.cmd)}, ws, module)
		if s.DevLaunches != c.dev || len(s.GoRuns) != c.goRun || len(s.BinaryLaunches) != c.binaryLaunchs {
			t.Errorf("%q: dev=%d goRun=%d binary=%d, want %d/%d/%d",
				c.cmd, s.DevLaunches, len(s.GoRuns), len(s.BinaryLaunches), c.dev, c.goRun, c.binaryLaunchs)
		}
	}
}

func TestBinaryHeaderMarksLaunch(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "srv"), []byte{0xcf, 0xfa, 0xed, 0xfe, 0, 0}, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "run.sh"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := collectSignals(nil, []toolEvent{bash("./srv &"), bash("./run.sh")}, ws, module)
	if len(s.BinaryLaunches) != 1 || !strings.HasPrefix(s.BinaryLaunches[0], "./srv") {
		t.Errorf("BinaryLaunches = %v, want only ./srv", s.BinaryLaunches)
	}
	// A binary the agent built, run by bare name through PATH.
	s = collectSignals(nil, []toolEvent{bash("go build -o srv . && PATH=.:$PATH srv")}, ws, module)
	if len(s.BinaryLaunches) != 1 {
		t.Errorf("bare-name launch of a built binary: BinaryLaunches = %v, want 1", s.BinaryLaunches)
	}
}

func TestShimAndTranscriptLaunchesAdd(t *testing.T) {
	// Two launches only the shim saw (from a script) and two only the
	// transcript saw (absolute path, which skips the shim) are four
	// launches, not two.
	launch := shimCall{Caller: "agent", Tool: "gofastr", Args: []string{"dev"}}
	events := []toolEvent{bash("./start.sh"), bash("/opt/bin/gofastr dev &"), bash("./start.sh"), bash("/opt/bin/gofastr dev &")}
	if s := collectSignals([]shimCall{launch, launch}, events, t.TempDir(), module); s.DevLaunches != 4 {
		t.Errorf("DevLaunches = %d, want 4", s.DevLaunches)
	}
	// A bare `gofastr dev` shows up in both logs: one launch, not two.
	if s := collectSignals([]shimCall{launch}, []toolEvent{bash("gofastr dev &")}, t.TempDir(), module); s.DevLaunches != 1 {
		t.Errorf("DevLaunches = %d for one bare launch, want 1", s.DevLaunches)
	}
}

func TestRebuildsExcludeFirstBuild(t *testing.T) {
	build := func(caller, out string) shimCall {
		return shimCall{Caller: caller, Tool: "go", Args: []string{"build", "-o", out, "."}}
	}
	devBin := "/tmp/gofastr-dev-server-1/server"
	calls := []shimCall{
		{Caller: "agent", Tool: "gofastr", Args: []string{"dev"}},
		build("gofastr-dev", devBin), // first build at launch
		build("gofastr-dev", devBin),
		build("agent", devBin), // dev launched through a path that skipped the shim
		build("agent", "bin/app"),
	}
	if s := collectSignals(calls, nil, t.TempDir(), module); s.DevRebuilds != 2 {
		t.Errorf("DevRebuilds = %d, want 2", s.DevRebuilds)
	}
}

func TestRebuildsCountPerLaunch(t *testing.T) {
	devBuild := shimCall{Caller: "gofastr-dev", Tool: "go", Args: []string{"build", "-o", "/tmp/gofastr-dev-server-1/server", "."}}
	launch := shimCall{Caller: "agent", Tool: "gofastr", Args: []string{"dev"}}
	// The second launch died before building (a port clash caught early),
	// so its launch must not swallow one of the watcher's rebuilds.
	calls := []shimCall{launch, launch, devBuild, devBuild, devBuild}
	if s := collectSignals(calls, nil, t.TempDir(), module); s.DevRebuilds != 2 {
		t.Errorf("DevRebuilds = %d, want 2: one initial build, two rebuilds", s.DevRebuilds)
	}
	// A relaunch's own first build is its startup, not a rebuild.
	calls = []shimCall{launch, devBuild, devBuild, launch, devBuild, devBuild}
	if s := collectSignals(calls, nil, t.TempDir(), module); s.DevRebuilds != 2 {
		t.Errorf("DevRebuilds = %d, want 2: one rebuild under each launch", s.DevRebuilds)
	}
}

func TestLookupsBeforeFirstDevLaunch(t *testing.T) {
	events := []toolEvent{
		{Name: "Read", Path: "/w/AGENTS.md"},
		{Name: "Read", Path: "/w/agents/framework.md"},
		bash("gofastr --help"),
		bash(`grep -n "gofastr dev" agents/framework.md`),
		bash("gofastr dev --help"),
		bash("gofastr dev &"),
		{Name: "Read", Path: "/w/agents/ui.md"},
	}
	s := collectSignals(nil, events, t.TempDir(), module)
	if s.CallsBeforeDev != 5 {
		t.Errorf("CallsBeforeDev = %d, want 5", s.CallsBeforeDev)
	}
	// Not the prompted AGENTS.md, not reads after launch, and not
	// `gofastr dev --help`: an agent asking for dev's flags has already
	// chosen the command.
	if len(s.Lookups) != 3 {
		t.Errorf("Lookups = %v, want the framework.md read, gofastr --help and the grep", s.Lookups)
	}
	if s := collectSignals(nil, events[:2], t.TempDir(), module); s.CallsBeforeDev != -1 {
		t.Errorf("CallsBeforeDev = %d with no launch, want -1", s.CallsBeforeDev)
	}
}

func TestLookupLabelNamesTheCommand(t *testing.T) {
	long := "cd /Users/someone/" + strings.Repeat("deep/", 30) + "app; "
	for cmd, want := range map[string]string{
		long + "cat go.mod; gofastr --help 2>&1 | head -30": "Bash gofastr --help 2>&1",
		long + "ls; grep -n serve agents/framework.md":      "Bash grep -n serve agents/framework.md",
	} {
		if got := lookup(bash(cmd)); got != want {
			t.Errorf("lookup(%q) = %q, want %q", cmd, got, want)
		}
	}
}

func TestIssuesFailTheTrial(t *testing.T) {
	dir := t.TempDir()
	log := "agent\tgofastr\tdev\n" + strings.Repeat("gofastr-dev\tgo\tbuild -o /tmp/gofastr-dev-server-1/server .\n", 3)
	if err := os.WriteFile(filepath.Join(dir, "cli.log"), []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	writeTranscript := func(result string) {
		if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte(result+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeTranscript(`{"type":"result","result":"ok","is_error":false}`)
	if tr := gradeTrial(context.Background(), dir, 1, "go", nil); !tr.Verdict.Pass {
		t.Fatalf("control: clean trial failed: %+v", tr.Verdict)
	}
	// An agent that timed out after working under the watcher is not a pass.
	if tr := gradeTrial(context.Background(), dir, 1, "go", []string{"agent: timed out after 20m0s"}); tr.Verdict.Pass {
		t.Errorf("agent issue left Pass=true: %+v", tr)
	}
	// Neither is a Claude run whose result record says it errored.
	writeTranscript(`{"type":"result","result":"API Error: overloaded","is_error":true}`)
	if tr := gradeTrial(context.Background(), dir, 1, "go", nil); tr.Verdict.Pass || len(tr.Issues) == 0 {
		t.Errorf("errored agent run graded as Pass=%t issues=%v", tr.Verdict.Pass, tr.Issues)
	}
}

func TestZeroRunsIsAnError(t *testing.T) {
	if !supported {
		t.Skip("sh shims")
	}
	_, err := Run(context.Background(), Options{RunDir: t.TempDir(), Runs: 0})
	if err == nil || !strings.Contains(err.Error(), "runs must be at least 1") {
		t.Errorf("Run with Runs=0: err = %v, want the runs check to refuse before anything builds", err)
	}
}

func TestRegradeCarriesOnlyAgentIssues(t *testing.T) {
	got := carriedIssues([]string{"agent: timed out after 20m0s", "read transcript: bad line", "agent run ended in an error: x", "agent: exit status 1"})
	want := []string{"agent: timed out after 20m0s", "agent: exit status 1"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("carried %v, want %v: grade-time issues are re-derived, not copied", got, want)
	}
}

func TestReapKillsDetachedServer(t *testing.T) {
	if !supported {
		t.Skip("unix process tools")
	}
	dir := t.TempDir()
	// A server the agent detached with setsid sits outside the agent's
	// process group; the reaper finds it by working directory instead.
	cmd := exec.Command("sleep", "60")
	cmd.Dir = dir
	detach(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	reapUnder(dir)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("detached process under the trial dir survived reapUnder")
	}
}

func TestJudgeVerdicts(t *testing.T) {
	good := Signals{DevLaunches: 1, DevRebuilds: 3}
	cases := []struct {
		name       string
		s          Signals
		task       []string
		pass, done bool
	}{
		{"dev loop used", good, nil, true, true},
		{"one relaunch allowed", Signals{DevLaunches: 2, DevRebuilds: 3}, nil, true, true},
		{"never ran dev", Signals{DevRebuilds: 3}, nil, false, true},
		{"go run bypass", Signals{DevLaunches: 1, DevRebuilds: 3, GoRuns: []string{"go run ."}}, nil, false, true},
		{"binary bypass", Signals{DevLaunches: 1, DevRebuilds: 3, BinaryLaunches: []string{"./app"}}, nil, false, true},
		{"restart per edit", Signals{DevLaunches: 3, DevRebuilds: 3}, nil, false, true},
		{"dev only at the end", Signals{DevLaunches: 1, DevRebuilds: 1}, nil, false, true},
		// A broken edit is the model's coding, not its dev loop: it
		// fails the task without failing the dev-loop verdict.
		{"task not done", good, []string{"task: /about missing"}, true, false},
	}
	for _, c := range cases {
		v := judge(c.s, c.task)
		if v.Pass != c.pass || v.TaskDone != c.done {
			t.Errorf("%s: pass=%t done=%t, want %t %t (failures %v, task %v)", c.name, v.Pass, v.TaskDone, c.pass, c.done, v.Failures, v.TaskFailures)
		}
	}
}

func TestShimPinsDevAddr(t *testing.T) {
	if !supported {
		t.Skip("sh shims")
	}
	dir := t.TempDir()
	shimDir := filepath.Join(dir, "shims")
	argsLog := filepath.Join(dir, "args.log")
	fakeGofastr := filepath.Join(dir, "real-gofastr")
	if err := os.WriteFile(fakeGofastr, []byte("#!/bin/sh\necho \"$*\" >> '"+argsLog+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := installShims(shimDir, fakeGofastr, "/bin/true", filepath.Join(dir, "cli.log"), "127.0.0.1:7001"); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{
		{"dev"},
		{"dev", "--dir", "."},
		{"dev", "--addr", "127.0.0.1:9000"},
		{"dev", "--addr=:9000"},
		{"dev", "-p", "9001"},
		{"dev", "-p=9002"},
		{"init", "x"},
	} {
		if out, err := exec.Command(filepath.Join(shimDir, "gofastr"), argv...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", argv, err, out)
		}
	}
	data, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatal(err)
	}
	want := "dev --addr 127.0.0.1:7001\n" +
		"dev --addr 127.0.0.1:7001 --dir .\n" +
		"dev --addr 127.0.0.1:9000\n" +
		"dev --addr=:9000\n" +
		"dev -p 9001\n" +
		"dev -p=9002\n" +
		"init x\n"
	if string(data) != want {
		t.Errorf("real gofastr saw:\n%s\nwant:\n%s", data, want)
	}
}

func TestTaskNamesTrialURL(t *testing.T) {
	got := renderTask("127.0.0.1:7001")
	if !strings.Contains(got, "http://127.0.0.1:7001/") {
		t.Errorf("task does not name the trial URL:\n%s", got)
	}
	if strings.Contains(got, "{{") {
		t.Errorf("task keeps a template marker:\n%s", got)
	}
}

func TestReadTranscriptStreamJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	stream := `{"type":"system","subtype":"init"}
{"type":"assistant","message":{"content":[{"type":"text","text":"hi"},{"type":"tool_use","name":"Bash","input":{"command":" gofastr dev ","run_in_background":true}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"ok"}]}}
not json
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"home.go"}}]}}
{"type":"result","result":"done","total_cost_usd":0.42,"is_error":false}
`
	if err := os.WriteFile(path, []byte(stream), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 2 || got.Events[0].Command != "gofastr dev" || got.Events[1].Name != "Edit" || got.Events[1].Path != "home.go" {
		t.Errorf("events = %+v", got.Events)
	}
	if got.Final != "done" || got.CostUSD != 0.42 {
		t.Errorf("final=%q cost=%v", got.Final, got.CostUSD)
	}
}

func TestReportTalliesPassesForCaller(t *testing.T) {
	r := Report{Trials: []Trial{{Verdict: Verdict{Pass: true}}, {Verdict: Verdict{TaskDone: true}}, {}}}
	if err := writeReport(t.TempDir(), &r); err != nil {
		t.Fatal(err)
	}
	if r.Passed != 1 {
		t.Errorf("Passed = %d, want 1: the CLI's exit code reads it", r.Passed)
	}
	if r.TasksDone != 1 {
		t.Errorf("TasksDone = %d, want 1", r.TasksDone)
	}
}

func TestFindingSummaryMedian(t *testing.T) {
	trial := func(calls int, lookups ...string) Trial {
		return Trial{Signals: Signals{CallsBeforeDev: calls, Lookups: lookups}}
	}
	got := findingSummary([]Trial{trial(2), trial(5, "Read agents/framework.md"), trial(3), trial(-1), trial(9)})
	for _, want := range []string{"median 4 tool calls", "4 of 5 trials launched it", "1 of 5 looked the command up"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	if got := findingSummary([]Trial{trial(9), trial(2), trial(5)}); !strings.Contains(got, "median 5 tool calls") {
		t.Errorf("odd-length summary %q, want median 5", got)
	}
}

func TestRegradeRewritesTrialGrade(t *testing.T) {
	runDir := t.TempDir()
	dir := trialDir(runDir, 1)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"gofastr dev &"}}]}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(dir, "grade.json"), Trial{Index: 1, Signals: Signals{CallsBeforeDev: 99}}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(runDir, "results.json"), Report{Model: "m", Trials: []Trial{{Index: 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Regrade(context.Background(), runDir); err != nil {
		t.Fatal(err)
	}
	var got Trial
	if err := readJSON(filepath.Join(dir, "grade.json"), &got); err != nil {
		t.Fatal(err)
	}
	if got.Signals.CallsBeforeDev != 0 {
		t.Errorf("grade.json CallsBeforeDev = %d after regrade, want 0: the per-trial file must match RESULTS.md", got.Signals.CallsBeforeDev)
	}
}
