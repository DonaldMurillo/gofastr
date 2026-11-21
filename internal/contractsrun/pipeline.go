// Package contractsrun shares contract orchestration between the CLI and MCP.
package contractsrun

import (
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// Pipeline stages, named by PipelineError.
const (
	PipelineLoadConfig   = "load config"
	PipelineScan         = "scan"
	PipelineRun          = "run"
	PipelineReadBaseline = "read baseline"
)

// PipelineError names the failed stage and preserves the underlying message.
type PipelineError struct {
	Stage string
	Err   error
}

func (e *PipelineError) Error() string { return e.Err.Error() }
func (e *PipelineError) Unwrap() error { return e.Err }

// RunPipeline loads config, scans, runs checks and optionally applies a baseline.
// An empty baselinePath uses the conventional file under root.
func RunPipeline(root, baselinePath string, opts contracts.RunOptions, applyBaseline bool) (*contracts.Report, error) {
	cfg, err := contracts.LoadConfig(root, "")
	if err != nil {
		return nil, &PipelineError{Stage: PipelineLoadConfig, Err: err}
	}
	pass, err := contracts.NewPass(root, cfg)
	if err != nil {
		return nil, &PipelineError{Stage: PipelineScan, Err: err}
	}
	report, err := contracts.Run(pass, opts)
	if err != nil {
		return nil, &PipelineError{Stage: PipelineRun, Err: err}
	}
	if !applyBaseline {
		return report, nil
	}
	if baselinePath == "" {
		baselinePath = filepath.Join(root, contracts.BaselineFileName)
	}
	if _, err := ReadAndApplyBaseline(report, baselinePath); err != nil {
		return nil, &PipelineError{Stage: PipelineReadBaseline, Err: err}
	}
	return report, nil
}

// ReadAndApplyBaseline reports whether it applied a baseline to report.
// A missing file applies nothing. An unreadable or invalid file returns an error.
func ReadAndApplyBaseline(report *contracts.Report, path string) (bool, error) {
	baseline, err := contracts.ReadBaseline(path)
	if err != nil {
		return false, err
	}
	if baseline == nil {
		return false, nil
	}
	report.ApplyBaseline(baseline)
	return true, nil
}
