package devloop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	if len(got.Events) != 2 || got.Events[0].Command != "gofastr dev" || !got.Events[0].Background || got.Events[1].Name != "Edit" {
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
