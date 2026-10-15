package handler

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"
)

func TestBindSourcePriority(t *testing.T) {
	r := httptest.NewRequest("GET", "/?id=query", nil)
	r.Header.Set("X-ID", "header")
	r.SetPathValue("id", "path")
	var dst struct {
		ID string `header:"X-ID" query:"id" path:"id"`
	}
	if err := Bind(r, &dst); err != nil {
		t.Fatal(err)
	}
	if dst.ID != "path" {
		t.Fatalf("ID=%q", dst.ID)
	}
}

type scopeRecursiveBody struct {
	*scopeRecursiveBody
	Value string `json:"value"`
}

func TestRecursiveEmbeddedBodyTerminates(t *testing.T) {
	if os.Getenv("GOFASTR_SCOPE_BODY_CHILD") == "1" {
		jsonTagSet(reflectTypeScopeBody())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRecursiveEmbeddedBodyTerminates$")
	cmd.Env = append(os.Environ(), "GORACE=atexit_sleep_ms=0", "GOFASTR_SCOPE_BODY_CHILD=1")
	if e := cmd.Run(); e != nil {
		t.Fatalf("recursive embedded struct did not terminate: %v", e)
	}
}

func reflectTypeScopeBody() reflect.Type { return reflect.TypeFor[scopeRecursiveBody]() }
