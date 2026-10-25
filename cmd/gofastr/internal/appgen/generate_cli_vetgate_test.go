package appgen

// cliGateOptions mirrors defaultCLIOptions with the --from-openapi flag
// the spec builder reads for its header note.
func cliGateOptions() CliOptions {
	opts := defaultCLIOptions()
	opts.FromOpenAPI = "spec.json"
	return opts
}
