package main

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestTourFreshBootHasDemoData(t *testing.T) {
	if os.Getenv("GOFASTR_TOUR_BOOT_TEST") == "1" {
		main()
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	output, err := os.Create(filepath.Join(root, "boot.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = output.Close() })
	command := exec.Command(binary, "-test.run=^TestTourFreshBootHasDemoData$")
	command.Env = append(os.Environ(), "GOFASTR_TOUR_BOOT_TEST=1", "GOFASTR_ISOLATION=off", "PORT="+address, "DATABASE_URL="+filepath.Join(root, "tour.db"), "TMPDIR="+root)
	command.Stdout, command.Stderr = output, output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(20 * time.Second)
	for {
		response, err := client.Get("http://" + address + "/users")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			logs, _ := os.ReadFile(output.Name())
			t.Fatalf("tour did not start: %v\n%s", err, logs)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for table, want := range map[string]int{"users": 2, "profiles": 1, "posts": 2, "comments": 2} {
		response, err := client.Get("http://" + address + "/" + table)
		if err != nil {
			t.Fatal(err)
		}
		var envelope struct {
			Data []map[string]any `json:"data"`
		}
		err = json.NewDecoder(response.Body).Decode(&envelope)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK || len(envelope.Data) != want {
			t.Errorf("fresh GET /%s = %d, %d rows, %v; want %d rows", table, response.StatusCode, len(envelope.Data), err, want)
		}
	}
}
