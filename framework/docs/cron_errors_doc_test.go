package docs

import (
	"strings"
	"testing"
)

func TestCronDocumentsDefaultErrorLog(t *testing.T) {
	document, err := Get("cron")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(document), "otherwise dropped") || !strings.Contains(string(document), "otherwise logged to `slog.Default()`") {
		t.Fatal("cron docs must describe the default error logger")
	}
}
