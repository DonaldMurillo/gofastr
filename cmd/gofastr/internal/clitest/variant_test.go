package clitest

import "testing"

func TestVariantBuildRunsOnlyForTheFullRunOrItsInputs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		all     string
		files   string
		inputs  []string
		wantRun bool
	}{
		{"full run", "1", "", nil, true},
		{"full run ignores the change set", "1", "README.md", []string{"examples/lms/gofastr.yml"}, true},
		{"no inputs on a pull request", "0", "framework/app.go", nil, false},
		{"input file changed", "0", "README.md\nexamples/lms/gofastr.yml", []string{"examples/lms/gofastr.yml"}, true},
		{"other file in the input's directory", "0", "examples/lms/main.go", []string{"examples/lms/gofastr.yml"}, false},
		{"file under an input directory", "0", "cmd/gofastr/internal/initcmd/init.go", []string{"cmd/gofastr/internal/initcmd/"}, true},
		{"sibling directory sharing the prefix", "0", "cmd/gofastr/internal/initcmdx/a.go", []string{"cmd/gofastr/internal/initcmd/"}, false},
		{"no change set", "", "", []string{"examples/lms/gofastr.yml"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOFASTR_TEST_ALL", tc.all)
			t.Setenv("GOFASTR_AFFECTED_FILES", tc.files)
			ran := false
			t.Run("variant", func(t *testing.T) {
				VariantBuild(t, tc.inputs...)
				ran = true
			})
			if ran != tc.wantRun {
				t.Errorf("variant ran = %v, want %v", ran, tc.wantRun)
			}
		})
	}
}
