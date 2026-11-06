package queue

//gofastr:test-reads queue.go

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestQueueCapabilityDocsMatchBackends(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), "queue.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range source.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, specification := range group.Specs {
			typeSpec, ok := specification.(*ast.TypeSpec)
			if !ok || (typeSpec.Name.Name != "Browsable" && typeSpec.Name.Name != "Replayable") {
				continue
			}
			for _, backend := range []string{"DBQueue", "MemoryQueue", "RedisQueue"} {
				if !strings.Contains(group.Doc.Text(), backend) {
					t.Errorf("%s documentation omits implementing backend %s", typeSpec.Name.Name, backend)
				}
			}
			if typeSpec.Name.Name == "Browsable" && !strings.Contains(group.Doc.Text(), "MemoryQueue and RedisQueue list dead-letter jobs only") {
				t.Error("Browsable must scope dead-letter-only inspection to MemoryQueue and RedisQueue")
			}
		}
	}
}
