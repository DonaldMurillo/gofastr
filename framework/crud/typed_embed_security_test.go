package crud

import (
	"os"
	"os/exec"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

type dateSelf struct {
	*dateSelf
	When time.Time
}

type dateLeft struct {
	*dateRight
	When time.Time
}

type dateRight struct {
	*dateLeft
	Other time.Time
}

func TestTypedDatesAllowEmbeddedCycles(t *testing.T) {
	if os.Getenv("GOFASTR_TEST_EMBED_CYCLE") != "1" {
		command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTypedDatesAllowEmbeddedCycles$")
		command.WaitDelay = time.Second
		command.Env = append(os.Environ(), "GOFASTR_TEST_EMBED_CYCLE=1")
		if output, err := command.CombinedOutput(); err != nil {
			summary := strings.SplitN(string(output), "\n", 5)
			t.Fatalf("recursive typed decode: %v\n%s", err, strings.Join(summary[:min(4, len(summary))], "\n"))
		}
		return
	}
	debug.SetMaxStack(128 << 10)
	want := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	for _, target := range []any{&dateSelf{}, &dateLeft{dateRight: &dateRight{}}} {
		row := map[string]any{"When": "2026-11-05", "Other": "2026-11-05"}
		if err := UnmarshalEntity(row, target); err != nil {
			t.Fatal(err)
		}
		if got := reflect.ValueOf(target).Elem().FieldByName("When").Interface().(time.Time); got != want {
			t.Fatalf("decoded date = %v, want %v", got, want)
		}
		if row["When"] != "2026-11-05" || row["Other"] != "2026-11-05" {
			t.Fatal("typed decode mutated its source row")
		}
		if mutual, ok := target.(*dateLeft); ok && mutual.Other != want {
			t.Fatalf("promoted date = %v, want %v", mutual.Other, want)
		}
	}
}
