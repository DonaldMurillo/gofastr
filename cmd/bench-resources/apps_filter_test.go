package main

import (
	"flag"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestBenchRejectsUnknownApps(t *testing.T) {
	if selection := os.Getenv("GOFASTR_BENCH_TEST_APPS"); selection != "" {
		flag.CommandLine = flag.NewFlagSet("bench-resources", flag.ExitOnError)
		os.Args = []string{"bench-resources", "-apps=" + selection, "-out=" + os.Getenv("GOFASTR_BENCH_TEST_OUT")}
		main()
		return
	}
	for _, selection := range []string{"minmal", "minimal,crd", "minimal,", ",", " minimal,minimal "} {
		t.Run(selection, func(t *testing.T) {
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			command := exec.Command(binary, "-test.run=^TestBenchRejectsUnknownApps$")
			command.Env = append(os.Environ(), "GOFASTR_BENCH_TEST_APPS="+selection, "GOFASTR_BENCH_TEST_OUT="+t.TempDir(), "PATH=")
			output, err := command.CombinedOutput()
			if selection == " minimal,minimal " {
				if err != nil || strings.Count(string(output), "▶") != 1 {
					t.Fatalf("valid selection: exit=%v, output=%s", err, output)
				}
				return
			}
			if err == nil || !strings.Contains(string(output), "unknown app") || strings.Contains(string(output), "▶") {
				t.Fatalf("selection %q: exit=%v, output=%s; want unknown app refusal before any benchmark", selection, err, output)
			}
		})
	}
}
