package routegroup_test

import (
	"os"
	"reflect"
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/routegroup"
)

func TestDocumentedGroupMethodsExist(t *testing.T) {
	source, err := os.ReadFile("group.go")
	if err != nil {
		t.Fatal(err)
	}
	methodSet := reflect.TypeFor[*routegroup.RouteGroup]()
	for _, call := range regexp.MustCompile(`(?m)^//\s+(?:api|v1|v2|admin)\.(\w+)\(`).FindAllSubmatch(source, -1) {
		if _, ok := methodSet.MethodByName(string(call[1])); !ok {
			t.Errorf("package example calls missing RouteGroup.%s", call[1])
		}
	}
}
