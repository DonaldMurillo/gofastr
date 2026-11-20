package update

import (
	"os"
	"strconv"
	"testing"
)

func TestAwaitPredecessorConsumesTheVariable(t *testing.T) {
	for _, v := range []string{"junk", "0", "-4", strconv.Itoa(os.Getpid())} {
		t.Setenv(PredecessorEnv, v)
		if AwaitPredecessor() {
			t.Errorf("%s=%q made the app wait", PredecessorEnv, v)
		}
		if _, ok := os.LookupEnv(PredecessorEnv); ok {
			t.Errorf("%s=%q is still in the environment for the app's children", PredecessorEnv, v)
		}
	}
}
