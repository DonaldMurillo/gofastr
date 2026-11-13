package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestJSONSessionOwnsStdout(t *testing.T) {
	stderr, err := os.CreateTemp(t.TempDir(), "stderr-*")
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	previousStderr := os.Stderr
	os.Stderr = stderr
	defer func() { os.Stderr = previousStderr }()
	output := clitest.CovT_capStdout(t, func() {
		stdout := os.Stdout
		end := cli.BeginJSON()
		defer end()
		nestedEnd := cli.BeginJSON()
		cli.Success("success")
		cli.Fail("failure")
		cli.Info("info")
		cli.Warn("warning")
		cli.Printf("printf\n")
		cli.Print("print\n")
		cli.Println("println")
		fmt.Print("raw output\n")
		nestedEnd()
		if os.Stdout != stderr {
			t.Fatal("nested end restored stdout before the outer session ended")
		}
		if err := cli.EmitJSON(map[string]string{"path": "a\x1b\x7f\u0085\u202e\u200b"}); err != nil {
			t.Fatal(err)
		}
		if err := cli.EmitRawJSON([]byte(`{"second":true}`)); err == nil {
			t.Fatal("accepted a second document")
		}
		end()
		end()
		if os.Stdout != stdout {
			t.Fatal("end did not restore stdout")
		}
	})
	var document map[string]string
	if err := json.Unmarshal([]byte(output), &document); err != nil || document["path"] != "a\x1b\x7f\u0085\u202e\u200b" {
		t.Fatalf("document = %q, error = %v", output, err)
	}
	if strings.ContainsAny(output, "\x1b\x7f\u0085\u202e\u200b") {
		t.Fatalf("terminal controls escaped the document: %q", output)
	}
	data, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	for _, prose := range []string{"success", "failure", "info", "warning", "printf", "print", "println", "raw output"} {
		if !strings.Contains(string(data), prose) {
			t.Errorf("stderr is missing %q: %q", prose, data)
		}
	}
}

func TestJSONValidationBeforeWriting(t *testing.T) {
	output := clitest.CovT_capStdout(t, func() {
		defer cli.BeginJSON()()
		for _, invalid := range []string{"", "not JSON", "{} {}"} {
			if err := cli.EmitRawJSON([]byte(invalid)); err == nil {
				t.Errorf("accepted %q", invalid)
			}
		}
		if err := cli.EmitJSON(math.NaN()); err == nil {
			t.Fatal("accepted an unencodable value")
		}
		raw := []byte(" \n{\"ok\":true}\n \n")
		before := bytes.Clone(raw)
		if err := cli.EmitRawJSON(raw); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(raw, before) {
			t.Fatal("EmitRawJSON modified its input")
		}
	})
	if output != "{\"ok\":true}\n" {
		t.Fatalf("output = %q", output)
	}
}

func TestJSONSessionRestoresOnExit(t *testing.T) {
	clitest.CovT_capStdout(t, func() {
		stdout := os.Stdout
		code := clitest.CovT_capExit(t, func() {
			defer cli.BeginJSON()()
			cli.OsExit(1)
		})
		if code != 1 || os.Stdout != stdout {
			t.Fatalf("exit = %d, stdout restored = %v", code, os.Stdout == stdout)
		}
	})
}

func TestJSONWithoutSession(t *testing.T) {
	for _, value := range []any{nil, true, 3, "text", []int{1, 2}, map[string]int{"key": 1}} {
		output := clitest.CovT_capStdout(t, func() {
			if err := cli.EmitJSON(value); err != nil {
				t.Fatal(err)
			}
		})
		if !json.Valid([]byte(output)) || !strings.HasSuffix(output, "\n") {
			t.Fatalf("output = %q", output)
		}
	}
}

func TestJSONWriterFailure(t *testing.T) {
	closed, err := os.CreateTemp(t.TempDir(), "closed-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = closed
	defer func() { os.Stdout = stdout }()
	defer cli.BeginJSON()()
	if err := cli.EmitJSON(true); err == nil {
		t.Fatal("ignored a document write failure")
	}
	if err := cli.EmitJSON(false); err == nil || !strings.Contains(err.Error(), "already emitted") {
		t.Fatalf("second write = %v", err)
	}
}
