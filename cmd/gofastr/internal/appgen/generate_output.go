package appgen

import (
	"slices"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/codegen"
)

// ReportGenerateFailure prints a generator failure using the requested
// output format without exiting, so watch mode can continue after a failure.
func ReportGenerateFailure(args []string, err error, hints ...string) {
	failGenerateArgs(args, err, hints...)
}

func showGenerateHelp(args []string, usage string) bool {
	if !cli.HasHelpFlagBeforeTerminator(args) {
		return false
	}
	cli.Println(usage)
	return true
}

func failGenerateArgs(args []string, err error, hints ...string) {
	failGenerate(slices.Contains(args, "--json"), err, hints...)
}

func failGenerate(jsonMode bool, err error, hints ...string) {
	if jsonMode {
		defer cli.BeginJSON()()
		printGeneratedErrorsJSON(err)
	}
	cli.Fail("%v", err)
	for _, hint := range hints {
		cli.Info("%s", hint)
	}
}

type generatedFileJSON struct {
	Path string `json:"path"`
	Size int    `json:"size"`
}

func generatedFilesJSON(files []GeneratedFile) []generatedFileJSON {
	output := make([]generatedFileJSON, 0, len(files))
	for _, file := range files {
		output = append(output, generatedFileJSON{Path: file.Name, Size: len(file.Content)})
	}
	return output
}

func printGeneratedFilesJSON(files []GeneratedFile) {
	emitGeneratedJSON(struct {
		Files []generatedFileJSON `json:"files"`
	}{Files: generatedFilesJSON(files)})
}

func printAdditiveFilesJSON(written []GeneratedFile, skipped, updated []string) {
	emitGeneratedJSON(struct {
		Files           []generatedFileJSON `json:"files"`
		SkippedExisting []string            `json:"skipped_existing"`
		Updated         []string            `json:"updated"`
	}{
		Files:           generatedFilesJSON(written),
		SkippedExisting: append([]string{}, skipped...),
		Updated:         append([]string{}, updated...),
	})
}

func printCodegenFilesJSON(files []codegen.GeneratedFile) {
	output := make([]generatedFileJSON, 0, len(files))
	for _, file := range files {
		output = append(output, generatedFileJSON{Path: file.Path, Size: len(file.Content)})
	}
	emitGeneratedJSON(struct {
		Files []generatedFileJSON `json:"files"`
	}{Files: output})
}

func printGeneratedErrorsJSON(errs ...error) {
	type generatedError struct {
		Message string `json:"message"`
	}
	output := make([]generatedError, 0, len(errs))
	for _, err := range errs {
		output = append(output, generatedError{Message: err.Error()})
	}
	emitGeneratedJSON(struct {
		Files  []generatedFileJSON `json:"files"`
		Errors []generatedError    `json:"errors"`
	}{Files: []generatedFileJSON{}, Errors: output})
}

func emitGeneratedJSON(document any) {
	defer cli.BeginJSON()()
	if err := cli.EmitJSON(document); err != nil {
		cli.Fail("write JSON: %v", err)
		cli.OsExit(1)
	}
}
