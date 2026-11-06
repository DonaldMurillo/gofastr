package generatecmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestWatchJSONFlagErrors(t *testing.T) {
	command := exec.Command(clitest.BuildGofastrBinary(t), "generate", "--watch", "--json", "--unknown")
	output, err := command.Output()
	if err == nil || !json.Valid(output) {
		t.Fatalf("invalid watch flags must produce one JSON error: %v\n%s", err, output)
	}
}

func TestWatchJSONCyclesStayReadable(t *testing.T) {
	binary := clitest.BuildGofastrBinary(t)
	root := t.TempDir()
	extension := clitest.WriteReportExtension(t, root, "")
	config := "version: 1\ncodegen:\n  output: gen\n  generators:\n    - name: custom/report\n      extension: report\n      source:\n        type: json_file\n        path: input.json\n  extensions:\n    - name: report\n      command: " + extension + "\n"
	clitest.WriteSettings(t, root, config)
	input := filepath.Join(root, "input.json")
	if err := os.WriteFile(input, []byte(`{"save":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "generate", "--watch", "--json", "--config=gofastr.settings.yml")
	command.Dir = root
	var stderr clitest.SyncBuffer
	command.Stderr = &stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = command.Wait()
	})
	type cycle struct {
		Errors []json.RawMessage `json:"errors"`
		Files  []json.RawMessage `json:"files"`
	}
	results := make(chan cycle)
	failures := make(chan error, 1)
	go func() {
		decoder := json.NewDecoder(stdout)
		for {
			var result cycle
			if err := decoder.Decode(&result); err != nil {
				failures <- err
				return
			}
			select {
			case results <- result:
			case <-ctx.Done():
				return
			}
		}
	}()
	for index, content := range []string{`{"save":1}`, `{"save":2}`, `invalid json`, `{"save":3}`} {
		if index > 0 {
			if err := os.WriteFile(input, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		select {
		case result := <-results:
			if (len(result.Errors) > 0) != (index == 2) {
				t.Fatalf("cycle %d has unexpected errors: %s", index, fmt.Sprint(result.Errors))
			}
			if index != 2 && len(result.Files) == 0 {
				t.Fatalf("cycle %d has no generated files", index)
			}
		case err := <-failures:
			t.Fatalf("cycle %d corrupted stdout: %v; stderr: %s", index, err, stderr.String())
		case <-ctx.Done():
			t.Fatalf("cycle %d did not finish: %s", index, stderr.String())
		}
		time.Sleep(350 * time.Millisecond)
	}
}
