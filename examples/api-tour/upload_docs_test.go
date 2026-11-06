package main

import (
	"os"
	"strings"
	"testing"
)

func TestUploadDocsDescribePrivateTemp(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, wording := range []string{"system temp", "gofastr-api-tour-", "0700"} {
		if !strings.Contains(string(readme), wording) {
			t.Errorf("upload docs omit %q from the private temporary storage contract", wording)
		}
	}
	if strings.Contains(string(readme), "./api-tour-uploads/") {
		t.Error("upload docs still promise a working-directory upload folder")
	}
}
