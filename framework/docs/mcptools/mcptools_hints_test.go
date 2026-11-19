package mcptools

import (
	"encoding/json"
	"testing"
)

func TestDocsToolsCarryListingHints(t *testing.T) {
	const read = `{"readOnlyHint":true,"destructiveHint":false,"idempotentHint":true,"openWorldHint":false}`
	seen := 0
	for _, tool := range docsApp(t).MCP.ListTools() {
		switch tool.Name {
		case "framework_docs_list", "framework_docs_get", "framework_docs_search":
			seen++
			if raw, _ := json.Marshal(tool.Annotations); string(raw) != read {
				t.Errorf("%s: annotations %s, want %s", tool.Name, raw, read)
			}
		}
	}
	if seen != 3 {
		t.Fatalf("saw %d docs tools, want 3", seen)
	}
}
